// Command server runs the private-coding-agent HTTP service.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/docker/docker/client"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"github.com/yourorg/private-coding-agent/internal/agent"
	"github.com/yourorg/private-coding-agent/internal/audit"
	"github.com/yourorg/private-coding-agent/internal/auth"
	"github.com/yourorg/private-coding-agent/internal/config"
	"github.com/yourorg/private-coding-agent/internal/db"
	"github.com/yourorg/private-coding-agent/internal/httpx"
	"github.com/yourorg/private-coding-agent/internal/modelgw"
	"github.com/yourorg/private-coding-agent/internal/sandbox"
	"github.com/yourorg/private-coding-agent/internal/telemetry"
	"github.com/yourorg/private-coding-agent/internal/tenant"
	"github.com/yourorg/private-coding-agent/internal/toolbus"
	"github.com/yourorg/private-coding-agent/internal/toolbus/tools"
	"github.com/yourorg/private-coding-agent/internal/user"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("server: %v", err)
	}
}

func run() error {
	cfgPath := flag.String("config", "config/config.yaml", "path to config yaml")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	shutdownTel, err := telemetry.Setup(ctx, telemetry.Config{
		ServiceName:  cfg.Telemetry.ServiceName,
		OTLPEndpoint: cfg.Telemetry.OTLPEndpoint,
	})
	if err != nil {
		return fmt.Errorf("otel: %w", err)
	}
	defer func() {
		sctx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = shutdownTel(sctx)
	}()

	if err := db.Migrate(ctx, cfg.DB.DSN); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	pool, err := db.Connect(ctx, cfg.DB.DSN)
	if err != nil {
		return fmt.Errorf("db connect: %w", err)
	}
	defer pool.Close()

	// Docker client
	dockerCli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("docker: %w", err)
	}
	defer dockerCli.Close()

	// Redis
	rdb := redis.NewClient(&redis.Options{Addr: cfg.Redis.Addr})
	if err := rdb.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis: %w", err)
	}
	defer rdb.Close()

	// Sandbox driver
	sandboxRepo := sandbox.NewSessionRepo(pool)
	sandboxDriver, err := sandbox.NewDockerDriver(ctx, dockerCli, sandboxRepo, rdb, sandbox.DockerDriverConfig{})
	if err != nil {
		return fmt.Errorf("sandbox driver: %w", err)
	}
	sandboxHandler := sandbox.NewHandler(sandboxDriver)

	// Model Gateway
	providerRepo := modelgw.NewProviderRepo(pool)
	usageRecorder := modelgw.NewUsageRecorder(modelgw.NewUsageRepo(pool), func(err error) {
		log.Printf("model usage record: %v", err)
	})
	factories := map[string]modelgw.ProviderFactory{
		"openai": func(cfg modelgw.ProviderConfig) (modelgw.Provider, error) {
			return modelgw.NewOpenAIProvider(cfg)
		},
		"ollama": func(cfg modelgw.ProviderConfig) (modelgw.Provider, error) {
			return modelgw.NewOllamaProvider(cfg)
		},
		"claude": func(cfg modelgw.ProviderConfig) (modelgw.Provider, error) {
			return modelgw.NewClaudeProvider(cfg)
		},
	}
	modelRegistry := modelgw.NewProviderRegistry(providerRepo, factories, 60*time.Second)
	if err := modelRegistry.Start(ctx); err != nil {
		return fmt.Errorf("model registry: %w", err)
	}
	go modelRegistry.Run(ctx)
	modelGateway := modelgw.NewGateway(modelRegistry, usageRecorder)
	modelHandler := modelgw.NewHandler(modelGateway)

	// Tool Bus
	toolRegistry := toolbus.NewRegistry()
	_ = toolRegistry.Register(tools.NewFSRead(sandboxDriver))
	_ = toolRegistry.Register(tools.NewFSWrite(sandboxDriver))
	_ = toolRegistry.Register(tools.NewFSList(sandboxDriver))
	_ = toolRegistry.Register(tools.NewFSGlob(sandboxDriver))
	_ = toolRegistry.Register(tools.NewGrep(sandboxDriver))
	_ = toolRegistry.Register(tools.NewShellExec(sandboxDriver))
	_ = toolRegistry.Register(tools.NewLLMChat(modelGateway))
	_ = toolRegistry.Register(tools.NewLLMEmbed(modelGateway))

	toolInvocationRecorder := toolbus.NewInvocationRecorder(
		toolbus.NewInvocationRepo(pool),
		func(err error) { log.Printf("tool invocation record: %v", err) })

	toolBus, err := toolbus.NewBus(toolRegistry, toolInvocationRecorder)
	if err != nil {
		return fmt.Errorf("toolbus: %w", err)
	}
	toolHandler := toolbus.NewHandler(toolBus)

	// Agent Engine (slice 5)
	agentProfiles := map[string]agent.Profile{
		"coding": agent.DefaultCodingProfile(),
	}
	agentEngine := agent.NewEngine(modelGateway, toolBus, agentProfiles)
	agentHandler := agent.NewHandler(agentEngine)

	// Reconciler (Task 16)
	if err := sandbox.RunReconciler(ctx, sandboxRepo, dockerCli); err != nil {
		return fmt.Errorf("reconciler: %w", err)
	}

	// Standard auth/tenant/user wiring
	tenantLookup := tenant.NewLookup(tenant.NewRepo(pool))
	userSvc := user.NewService(user.NewRepo(pool))
	jwtCfg := auth.JWTConfig{Secret: cfg.Auth.JWTSecret, TTL: cfg.Auth.JWTTTL}
	if err := auth.ValidateJWTConfig(jwtCfg); err != nil {
		return fmt.Errorf("auth config: %w", err)
	}
	jwtSvc := auth.NewJWT(jwtCfg)
	auditRepo := audit.NewRepo(pool)

	var ready atomic.Bool
	ready.Store(true)

	authHandler := auth.NewHandler(auth.HandlerDeps{
		Tenants: tenantLookup, Auth: userSvc, JWT: jwtSvc,
	})

	register := func(r *gin.Engine) {
		r.Use(audit.Middleware(auditRepo, func(err error) {
			log.Printf("audit append: %v", err)
		}))
		authHandler.Register(r)

		protected := r.Group("/")
		protected.Use(auth.Middleware(jwtSvc))
		httpx.RegisterMe(protected)
		sandboxHandler.Register(protected)
		modelHandler.Register(protected)
		toolHandler.Register(protected)
		agentHandler.Register(protected)
	}

	engine := httpx.NewEngine(httpx.Deps{
		ServiceName: cfg.Telemetry.ServiceName,
		Ready:       func() bool { return ready.Load() },
		Register:    register,
	})

	srv := &http.Server{
		Addr:              ":" + strconv.Itoa(cfg.Server.Port),
		Handler:           engine,
		ReadHeaderTimeout: 10 * time.Second,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	errCh := make(chan error, 1)
	go func() {
		log.Printf("server listening on :%d", cfg.Server.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-stop:
		log.Println("shutting down...")
	case err := <-errCh:
		return fmt.Errorf("listen: %w", err)
	}

	ready.Store(false)
	sctx, cncl := context.WithTimeout(context.Background(), 10*time.Second)
	defer cncl()
	if err := srv.Shutdown(sctx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}
