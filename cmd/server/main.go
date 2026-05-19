package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
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
	cfgPath := flag.String("config", "config/config.yaml", "path to config yaml")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	shutdownTel, err := telemetry.Setup(ctx, telemetry.Config{
		ServiceName:  cfg.Telemetry.ServiceName,
		OTLPEndpoint: cfg.Telemetry.OTLPEndpoint,
	})
	if err != nil {
		log.Fatalf("otel: %v", err)
	}
	defer func() {
		sctx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = shutdownTel(sctx)
	}()

	if err := db.Migrate(cfg.DB.DSN); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	pool, err := db.Connect(ctx, cfg.DB.DSN)
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}
	defer pool.Close()

	tenantRepo := tenant.NewRepo(pool)
	tenantLookup := tenant.NewLookup(tenantRepo)
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
		// audit on all routes
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
		Addr:              ":" + itoa(cfg.Server.Port),
		Handler:           engine,
		ReadHeaderTimeout: 10 * time.Second,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Printf("server listening on :%d", cfg.Server.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %v", err)
		}
	}()

	<-stop
	log.Println("shutting down...")
	ready.Store(false)

	sctx, cncl := context.WithTimeout(context.Background(), 10*time.Second)
	defer cncl()
	_ = srv.Shutdown(sctx)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	sign := ""
	if i < 0 {
		sign = "-"
		i = -i
	}
	var b [20]byte
	n := len(b)
	for i > 0 {
		n--
		b[n] = byte('0' + i%10)
		i /= 10
	}
	return sign + string(b[n:])
}
