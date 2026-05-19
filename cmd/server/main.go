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

	"github.com/gin-gonic/gin"

	"github.com/yourorg/private-coding-agent/internal/audit"
	"github.com/yourorg/private-coding-agent/internal/auth"
	"github.com/yourorg/private-coding-agent/internal/config"
	"github.com/yourorg/private-coding-agent/internal/db"
	"github.com/yourorg/private-coding-agent/internal/httpx"
	"github.com/yourorg/private-coding-agent/internal/telemetry"
	"github.com/yourorg/private-coding-agent/internal/tenant"
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

	if err := db.Migrate(cfg.DB.DSN); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	pool, err := db.Connect(ctx, cfg.DB.DSN)
	if err != nil {
		return fmt.Errorf("db connect: %w", err)
	}
	defer pool.Close()

	tenantLookup := tenant.NewLookup(tenant.NewRepo(pool))
	userSvc := user.NewService(user.NewRepo(pool))
	jwtSvc := auth.NewJWT(auth.JWTConfig{
		Secret: cfg.Auth.JWTSecret,
		TTL:    cfg.Auth.JWTTTL,
	})
	auditRepo := audit.NewRepo(pool)

	var ready atomic.Bool
	ready.Store(true)

	authHandler := auth.NewHandler(auth.HandlerDeps{
		Tenants: tenantLookup,
		Auth:    userSvc,
		JWT:     jwtSvc,
	})

	register := func(r *gin.Engine) {
		r.Use(audit.Middleware(auditRepo, func(err error) {
			log.Printf("audit append: %v", err)
		}))
		authHandler.Register(r)

		protected := r.Group("/")
		protected.Use(auth.Middleware(jwtSvc))
		httpx.RegisterMe(protected)
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
