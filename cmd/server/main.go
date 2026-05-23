// Command server runs the private-coding-agent HTTP service.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/docker/docker/client"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/yourorg/private-coding-agent/internal/agent"
	"github.com/yourorg/private-coding-agent/internal/audit"
	"github.com/yourorg/private-coding-agent/internal/auth"
	"github.com/yourorg/private-coding-agent/internal/config"
	"github.com/yourorg/private-coding-agent/internal/db"
	"github.com/yourorg/private-coding-agent/internal/httpx"
	"github.com/yourorg/private-coding-agent/internal/logx"
	"github.com/yourorg/private-coding-agent/internal/mcp"
	"github.com/yourorg/private-coding-agent/internal/memory"
	"github.com/yourorg/private-coding-agent/internal/metrics"
	"github.com/yourorg/private-coding-agent/internal/modelgw"
	"github.com/yourorg/private-coding-agent/internal/objstore"
	"github.com/yourorg/private-coding-agent/internal/orchestrator"
	"github.com/yourorg/private-coding-agent/internal/quota"
	"github.com/yourorg/private-coding-agent/internal/reflection"
	"github.com/yourorg/private-coding-agent/internal/sandbox"
	"github.com/yourorg/private-coding-agent/internal/session"
	"github.com/yourorg/private-coding-agent/internal/skills"
	"github.com/yourorg/private-coding-agent/internal/telemetry"
	"github.com/yourorg/private-coding-agent/internal/tenant"
	"github.com/yourorg/private-coding-agent/internal/toolbus"
	"github.com/yourorg/private-coding-agent/internal/toolbus/tools"
	"github.com/yourorg/private-coding-agent/internal/user"
	"github.com/yourorg/private-coding-agent/internal/webui"
	"github.com/yourorg/private-coding-agent/internal/workflow"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server fatal", "err", err.Error())
		os.Exit(1)
	}
}

func run() error {
	cfgPath := flag.String("config", "config/config.yaml", "path to config yaml")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	logx.Install(logx.New(logx.Config{
		Format: cfg.Observability.LogFormat,
		Level:  cfg.Observability.LogLevel,
	}))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	promReg := prometheus.NewRegistry()
	shutdownTel, err := telemetry.Setup(ctx, telemetry.Config{
		ServiceName:  cfg.Telemetry.ServiceName,
		OTLPEndpoint: cfg.Telemetry.OTLPEndpoint,
		PromRegistry: promReg,
	})
	if err != nil {
		return fmt.Errorf("otel: %w", err)
	}
	if err := metrics.Init(); err != nil {
		return fmt.Errorf("metrics init: %w", err)
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

	// Docker client. Only built when sandbox.driver=docker; the k8s driver
	// path keeps it nil and the reconciler is skipped.
	var dockerCli *client.Client
	if cfg.Sandbox.Driver == "docker" {
		dockerCli, err = client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
		if err != nil {
			return fmt.Errorf("docker: %w", err)
		}
		defer dockerCli.Close()
	}

	// Redis
	rdb := redis.NewClient(&redis.Options{Addr: cfg.Redis.Addr})
	if err := rdb.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis: %w", err)
	}
	defer rdb.Close()

	// Quota service (slice 13). Backed by Redis fixed-window counters.
	quotaSvc := quota.NewService(rdb, quota.Limits{
		LLMTokensPerDay:     cfg.Quota.LLMTokensPerDay,
		SandboxMaxActive:    cfg.Quota.SandboxMaxActive,
		ToolInvokePerMinute: cfg.Quota.ToolInvokePerMinute,
	})

	// Sandbox driver. Slice 22d1 made this a 2-way switch:
	//   - docker (default): host Docker daemon, one container per sandbox
	//   - k8s: Kubernetes API server, one Pod per sandbox
	// The Runtime interface is identical across drivers; downstream
	// consumers (tools, handler, session.Service) take Runtime, so nothing
	// else in the wiring changes by driver.
	sandboxRepo := sandbox.NewSessionRepo(pool)
	var sandboxDriver sandbox.Runtime
	switch cfg.Sandbox.Driver {
	case "docker":
		// Slice 22c: load the embedded hardened seccomp profile at boot and
		// inject it into every sandbox HostConfig. If the operator disabled
		// seccomp via PCA_SANDBOX_SECCOMP_ENABLED=false, pass empty and the
		// driver falls back to Docker's runtime default seccomp.
		var seccompProfile string
		if cfg.Sandbox.SeccompEnabled {
			seccompProfile, err = sandbox.LoadSeccompProfile()
			if err != nil {
				return fmt.Errorf("load seccomp profile: %w", err)
			}
			slog.Info("sandbox: hardened seccomp profile loaded", "size_bytes", len(seccompProfile))
		} else {
			slog.Warn("sandbox: seccomp disabled via config — falling back to Docker default profile")
		}
		sandboxDriver, err = sandbox.NewDockerDriver(ctx, dockerCli, sandboxRepo, rdb, sandbox.DockerDriverConfig{
			KeepLocalImage: cfg.Snapshot.KeepLocalImage,
			SeccompProfile: seccompProfile,
		})
		if err != nil {
			return fmt.Errorf("sandbox docker driver: %w", err)
		}
	case "k8s":
		restCfg, err := buildK8sRestConfig(cfg.Sandbox.K8s)
		if err != nil {
			return fmt.Errorf("sandbox k8s rest config: %w", err)
		}
		k8sClient, err := kubernetes.NewForConfig(restCfg)
		if err != nil {
			return fmt.Errorf("sandbox k8s clientset: %w", err)
		}
		sandboxDriver, err = sandbox.NewK8sDriver(k8sClient, restCfg, sandboxRepo, rdb, sandbox.K8sDriverConfig{
			Namespace:               cfg.Sandbox.K8s.Namespace,
			ServiceAccount:          cfg.Sandbox.K8s.ServiceAccount,
			SeccompLocalhostProfile: cfg.Sandbox.K8s.SeccompLocalhostProfile,
			PodReadyTimeout:         time.Duration(cfg.Sandbox.K8s.PodReadyTimeoutSec) * time.Second,
		})
		if err != nil {
			return fmt.Errorf("sandbox k8s driver: %w", err)
		}
		slog.Info("sandbox: k8s driver active",
			"namespace", cfg.Sandbox.K8s.Namespace,
			"in_cluster", cfg.Sandbox.K8s.InCluster,
			"service_account", cfg.Sandbox.K8s.ServiceAccount,
			"seccomp_localhost", cfg.Sandbox.K8s.SeccompLocalhostProfile != "")
	default:
		// applySlice22dDefaults already rejected unknown values; this is
		// belt-and-braces for future refactor safety.
		return fmt.Errorf("sandbox.driver=%q is not supported", cfg.Sandbox.Driver)
	}
	sandboxHandler := sandbox.NewHandler(sandboxDriver).WithQuota(quotaSvc, sandboxRepo)
	// Will set sandboxHandler.WithAuditSink after auditRepo is constructed.

	// Model Gateway
	providerRepo := modelgw.NewProviderRepo(pool)
	usageRecorder := modelgw.NewUsageRecorder(modelgw.NewUsageRepo(pool), func(err error) {
		slog.Error("model usage record", "err", err.Error())
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
	modelRegistry := modelgw.NewProviderRegistry(providerRepo, factories,
		60*time.Second, !cfg.Providers.DisallowGlobalFallback)
	if err := modelRegistry.Start(ctx); err != nil {
		return fmt.Errorf("model registry: %w", err)
	}
	go modelRegistry.Run(ctx)
	modelGateway := modelgw.NewGateway(modelRegistry, usageRecorder).WithQuota(quotaSvc)
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

	// Memory subsystem (slice 7 base + slice 11 vector pipeline).
	// Embedder is constructed once and shared; tenant/user are resolved per
	// call from request context. Nil embedder = vector pipeline off.
	memCfg := memory.MemoryConfig{
		EmbeddingModel: cfg.Memory.EmbeddingModel,
		DedupThreshold: cfg.Memory.DedupThreshold,
		EmbedOnWrite:   cfg.Memory.EmbedOnWrite,
	}
	var memEmbedder memory.Embedder
	if memCfg.EmbedOnWrite && memCfg.EmbeddingModel != "" {
		memEmbedder = memory.NewGatewayEmbedder(modelGateway, memCfg.EmbeddingModel)
	}
	memoryService := memory.NewService(memory.NewRepo(pool), memEmbedder, memCfg)
	memoryHandler := memory.NewHandler(memoryService)
	_ = toolRegistry.Register(tools.NewMemorySave(memoryService))
	_ = toolRegistry.Register(tools.NewMemorySearch(memoryService))
	_ = toolRegistry.Register(tools.NewMemoryList(memoryService))
	_ = toolRegistry.Register(tools.NewMemoryDelete(memoryService))

	toolInvocationRecorder := toolbus.NewInvocationRecorder(
		toolbus.NewInvocationRepo(pool),
		func(err error) { slog.Error("tool invocation record", "err", err.Error()) })

	toolBus, err := toolbus.NewBus(toolRegistry, toolInvocationRecorder)
	if err != nil {
		return fmt.Errorf("toolbus: %w", err)
	}
	toolBus.WithQuota(quotaSvc)
	toolHandler := toolbus.NewHandler(toolBus)

	// Agent Engine (slice 5) + Skills subsystem (slice 12) + Sub-Agent
	// profiles (slice 18). All four profiles are registered up front so the
	// delegate tool can target any of them; new profiles plug in here without
	// touching wiring further down.
	agentProfiles := map[string]agent.Profile{
		"coding":             agent.DefaultCodingProfile(),
		"review":             agent.DefaultReviewProfile(),
		"research":           agent.DefaultResearchProfile(),
		"workflow-authoring": agent.DefaultWorkflowAuthoringProfile(),
	}
	var skillRegistry *skills.Registry
	var skillDBRepo *skills.DBRepo
	var composer agent.ContextComposer = agent.NoopComposer{}
	skillsCfg := cfg.Skills
	if skillsCfg.Enabled {
		skillRegistry = skills.NewRegistry()
		if len(skillsCfg.Dirs) > 0 {
			n, errs := skillRegistry.LoadFromDirs(skillsCfg.Dirs)
			for _, e := range errs {
				slog.Warn("skills.load", "err", e.Error())
			}
			slog.Info("skills.loaded", "count", n, "dirs", skillsCfg.Dirs)
		}
		skillDBRepo = skills.NewDBRepo(pool)
		resolver := skills.NewResolver(skillRegistry, skillsCfg).WithDBLookup(skillDBRepo)
		composer = agent.NewSkillComposer(resolver, skillsCfg)
	}
	composer = agent.WrapMemoryComposer(composer)
	memLoader := memory.NewLoader(memoryService, memory.LoaderConfig{
		TopK:     cfg.Memory.InjectTopK,
		MaxChars: cfg.Memory.InjectMaxChars,
	})
	agentEngine := agent.NewEngine(modelGateway, toolBus, agentProfiles, composer)
	agentHandler := agent.NewHandler(agentEngine)

	// Session Orchestrator (slice 6)
	sessionService := session.NewService(
		session.NewSessionRepo(pool),
		session.NewMessageRepo(pool),
		agentEngine,
	).WithSandbox(sandboxDriver).WithQuota(quotaSvc, sandboxRepo).WithMemoryLoader(memLoader)
	sessionHandler := session.NewHandler(sessionService)
	wsAllowed := cfg.Server.WSAllowedOrigins
	if len(wsAllowed) == 0 {
		wsAllowed = []string{"*"}
	}
	sessionWSHandler := session.NewWSHandler(sessionService, wsAllowed)

	// Reconciler (Task 16). Docker-only — K8s reconciliation (via watch loop
	// across pod lifecycle events) is 22d-v2 work; on k8s we skip and rely on
	// Pod restartPolicy=Never + Destroy idempotence to converge state.
	if dockerCli != nil {
		if err := sandbox.RunReconciler(ctx, sandboxRepo, dockerCli); err != nil {
			return fmt.Errorf("reconciler: %w", err)
		}
	} else {
		slog.Info("sandbox reconciler skipped: non-docker driver")
	}

	// Standard auth/tenant/user wiring
	tenantLookup := tenant.NewLookup(tenant.NewRepo(pool))
	userSvc := user.NewService(user.NewRepo(pool))
	jwtCfg := auth.JWTConfig{Secret: cfg.Auth.JWTSecret, TTL: cfg.Auth.JWTTTL}
	if err := auth.ValidateJWTConfig(jwtCfg); err != nil {
		return fmt.Errorf("auth config: %w", err)
	}
	jwtSvc := auth.NewJWT(jwtCfg)
	jwtRevoker := auth.NewRedisRevoker(rdb)
	auditRepo := audit.NewRepo(pool)
	sandboxHandler.WithAuditSink(auditRepo)
	sessionService.WithAuditSink(auditRepo)
	sessionWSHandler.WithAuditSink(auditRepo)
	toolBus.WithAuditSink(auditRepo)
	agentEngine.WithAuditSink(auditRepo)

	// Slice 21a — Orchestration Router. Build the rule engine eagerly so a
	// bad regex / empty match block fails the server boot rather than
	// surfacing per-request. router=nil → engine short-circuits to a zero
	// Decision and emits no audit / metric.
	if cfg.Orchestrator.Enabled {
		routerCfg := orchestrator.Config{
			Enabled:     cfg.Orchestrator.Enabled,
			InjectHint:  cfg.Orchestrator.InjectHint,
			DefaultHint: cfg.Orchestrator.DefaultHint,
			Rules:       toOrchestratorRules(cfg.Orchestrator.Rules),
		}
		routerEng, err := orchestrator.NewEngine(routerCfg)
		if err != nil {
			return fmt.Errorf("orchestrator: %w", err)
		}
		agentEngine.WithRouter(routerEng)
		slog.Info("orchestrator router enabled",
			"inject_hint", cfg.Orchestrator.InjectHint,
			"rules", len(cfg.Orchestrator.Rules))
	}

	// Slice 18: late-register agent.delegate now that both the engine and
	// the audit sink are available. The tool needs an *Engine reference (for
	// child Run dispatch) and a sink (for delegate.{start,complete} events),
	// so it can't be registered alongside the built-in tools above.
	delegateTool := agent.NewDelegateTool(agentEngine, agentProfiles, auditRepo)
	if err := toolBus.Register(delegateTool); err != nil {
		return fmt.Errorf("register agent.delegate: %w", err)
	}

	// Slice 19 — Workflow Engine. Wire repo + engine (backed by the bus
	// adapter) + service, then re-register every published workflow into the
	// Bus so process restarts don't drop workflow.<slug> tools.
	workflowRepo := workflow.NewRepo(pool)
	workflowEngine := workflow.NewEngine(workflow.BusStepRunner{Bus: toolBus}, workflow.DefaultConfig())
	workflowService := workflow.NewService(workflowRepo, workflowEngine, toolBus, auditRepo)
	for _, t := range workflow.NewAdminTools(workflowService) {
		if err := toolBus.Register(t); err != nil {
			return fmt.Errorf("register %s: %w", t.Name(), err)
		}
	}
	if err := workflowService.RepublishAll(ctx); err != nil {
		slog.Warn("workflow: republish on boot", "err", err.Error())
	}
	workflow.StartRunsRetention(ctx, workflowRepo, cfg.Workflow.RunsRetentionDays, cfg.Workflow.RetentionInterval)
	slog.Info("workflow retention",
		"runs_retention_days", cfg.Workflow.RunsRetentionDays,
		"interval", cfg.Workflow.RetentionInterval)

	// Slice 21b — External MCP Manager. cfg.MCP.Enabled=false skips
	// construction and the admin handler returns 503; boot republish runs
	// best-effort so a single unreachable server does not abort startup.
	var mcpManager *mcp.Manager
	var mcpRepo *mcp.Repo
	if cfg.MCP.Enabled {
		mcpRepo = mcp.NewRepo(pool)
		mcpManager = mcp.NewManager(mcpRepo, toolBus, auditRepo, mcp.Config{
			Enabled:           true,
			HeartbeatInterval: cfg.MCP.HeartbeatInterval,
			InvokeTimeout:     cfg.MCP.InvokeTimeout,
			ListToolsTimeout:  cfg.MCP.ListToolsTimeout,
		})
		if err := mcpManager.Start(ctx); err != nil {
			return fmt.Errorf("mcp manager start: %w", err)
		}
		defer mcpManager.Stop()
		slog.Info("mcp manager enabled",
			"heartbeat", cfg.MCP.HeartbeatInterval,
			"invoke_timeout", cfg.MCP.InvokeTimeout)
	}

	// Slice 22b — Sandbox→S3 snapshot. cfg.Snapshot.Enabled=false skips
	// construction; sandboxHandler.snapshot route then returns 503
	// snapshot_disabled via ErrSnapshotDisabled.
	if cfg.Snapshot.Enabled {
		osClient, err := objstore.New(objstore.Config{
			Endpoint:  cfg.Snapshot.Endpoint,
			Bucket:    cfg.Snapshot.Bucket,
			AccessKey: cfg.Snapshot.AccessKey,
			SecretKey: cfg.Snapshot.SecretKey,
			Region:    cfg.Snapshot.Region,
			UseSSL:    cfg.Snapshot.UseSSL,
		})
		if err != nil {
			return fmt.Errorf("objstore new: %w", err)
		}
		ensureCtx, ensureCancel := context.WithTimeout(ctx, 10*time.Second)
		if err := osClient.EnsureBucket(ensureCtx); err != nil {
			ensureCancel()
			return fmt.Errorf("objstore ensure bucket: %w", err)
		}
		ensureCancel()
		snapRepo := sandbox.NewSnapshotRepo(pool)
		// Slice 22d1: SetSnapshotDeps is docker-specific (commit+image-save
		// have no K8s equivalent yet — kaniko-based snapshot is 22d-v2).
		// K8sDriver exposes SetSnapshotRepo so its Destroy can null
		// session_id on snapshots that survived a docker→k8s migration.
		if dd, ok := sandboxDriver.(interface {
			SetSnapshotDeps(*sandbox.SnapshotRepo, sandbox.SnapshotStore, string)
		}); ok {
			dd.SetSnapshotDeps(snapRepo, osClient, cfg.Snapshot.Prefix)
		} else if kd, ok := sandboxDriver.(interface {
			SetSnapshotRepo(*sandbox.SnapshotRepo)
		}); ok {
			kd.SetSnapshotRepo(snapRepo)
			slog.Warn("snapshot.enabled=true but driver does not implement snapshot create/restore; admin /snapshot routes will return 503",
				"driver", cfg.Sandbox.Driver)
		}
		sandboxHandler.WithSnapshotRepo(snapRepo)
		slog.Info("objstore: bucket ready",
			"bucket", cfg.Snapshot.Bucket,
			"endpoint", cfg.Snapshot.Endpoint,
			"prefix", cfg.Snapshot.Prefix)
	}

	auditSvc := audit.NewService(auditRepo)
	auditHandler := audit.NewHandler(auditSvc, func(c *gin.Context) (uuid.UUID, bool) {
		cl := auth.FromCtx(c.Request.Context())
		if cl == nil {
			return uuid.Nil, false
		}
		return cl.TenantID, true
	})

	// Slice 20 — Reflection Agent. Worker is in-process; on Enabled=false the
	// hook stays nil so ArchiveSession is a no-op for this subsystem.
	var reflectionAdmin *reflection.AdminHandler
	if cfg.Reflection.Enabled {
		reflRepo := reflection.NewRepo(pool)
		reflJobRepo := reflection.NewJobRepo(pool)
		reflMem := newReflectionMemoryAdapter(memoryService)
		reflCfg := reflection.Config{
			Enabled:               true,
			Model:                 cfg.Reflection.Model,
			AutoApproveThreshold:  cfg.Reflection.AutoApproveThreshold,
			MaxMessagesPerSession: cfg.Reflection.MaxMessagesPerSession,
			MaxCharsPerMessage:    cfg.Reflection.MaxCharsPerMessage,
			WorkerBuffer:          cfg.Reflection.WorkerBuffer,
			WorkerTimeout:         cfg.Reflection.WorkerTimeout,
		}
		reflector := reflection.NewReflector(modelGateway, reflMem,
			session.NewMessageRepo(pool), reflRepo, auditRepo, reflCfg)
		reflWorker := reflection.NewWorker(reflector, reflCfg.WorkerBuffer, reflCfg.WorkerTimeout, reflection.WorkerOptions{
			Store:                  reflJobRepo,
			MaxAttempts:            cfg.Reflection.MaxAttempts,
			RetryBaseInterval:      cfg.Reflection.RetryBaseInterval,
			PollInterval:           cfg.Reflection.PollInterval,
			ProposalPendingTTLDays: cfg.Reflection.ProposalPendingTTLDays,
		})
		go reflWorker.Run(ctx)
		sessionService.WithReflectionHook(reflWorker.Enqueue)
		reflectionAdmin = reflection.NewAdminHandler(reflRepo, reflMem).WithAuditSink(auditRepo)
		slog.Info("reflection enabled",
			"model", reflCfg.Model,
			"auto_approve_threshold", reflCfg.AutoApproveThreshold,
			"worker_buffer", reflCfg.WorkerBuffer,
			"max_attempts", cfg.Reflection.MaxAttempts,
			"poll_interval", cfg.Reflection.PollInterval)
	}

	var ready atomic.Bool
	ready.Store(true)

	oidcCfg := auth.OIDCConfig{
		Enabled:         cfg.Auth.OIDC.Enabled,
		Issuer:          cfg.Auth.OIDC.Issuer,
		ClientID:        cfg.Auth.OIDC.ClientID,
		ClientSecretEnv: cfg.Auth.OIDC.ClientSecretEnv,
		RedirectURL:     cfg.Auth.OIDC.RedirectURL,
		TenantSlug:      cfg.Auth.OIDC.TenantSlug,
	}
	var oidcRT *auth.OIDCRuntime
	if oidcCfg.Enabled {
		if err := oidcCfg.Valid(); err != nil {
			return fmt.Errorf("oidc config: %w", err)
		}
		oidcClient, err := auth.NewOIDCClient(ctx, oidcCfg)
		if err != nil {
			return fmt.Errorf("oidc client: %w", err)
		}
		oidcRT = &auth.OIDCRuntime{
			Config: oidcCfg, Client: oidcClient, CookieSecret: cfg.Auth.JWTSecret,
		}
		slog.Info("auth.oidc enabled", "issuer", oidcCfg.Issuer, "tenant", oidcCfg.TenantSlug)
	}
	if !cfg.Auth.LocalEnabled && !oidcCfg.Enabled {
		return fmt.Errorf("auth: enable local_enabled or oidc.enabled")
	}

	authHandler := auth.NewHandler(auth.HandlerDeps{
		Tenants: tenantLookup, Auth: userSvc, OIDCUsers: userSvc, JWT: jwtSvc,
		Audit: auditRepo, Revoker: jwtRevoker,
		LocalEnabled: cfg.Auth.LocalEnabled, OIDC: oidcRT,
	})

	register := func(r *gin.Engine) {
		r.Use(audit.Middleware(auditRepo, func(c *gin.Context) (*uuid.UUID, *uuid.UUID) {
			cl := auth.FromCtx(c.Request.Context())
			if cl == nil {
				return nil, nil
			}
			tid, uid := cl.TenantID, cl.UserID
			return &tid, &uid
		}, func(err error) {
			slog.Error("audit append", "err", err.Error())
		}))
		authHandler.Register(r)

		protected := r.Group("/")
		protected.Use(auth.Middleware(jwtSvc, auth.WithRevoker(jwtRevoker)))
		protected.Use(httpx.RateLimitMiddleware(rdb, httpx.RateLimitConfig{
			PerMinute: cfg.RateLimit.PerMinute,
		}))
		httpx.RegisterMe(protected)
		sandboxHandler.Register(protected)
		modelHandler.Register(protected)
		toolHandler.Register(protected)
		agentHandler.Register(protected)
		sessionHandler.Register(protected)
		memoryHandler.Register(protected)
		skills.NewHandler(skillRegistry).WithDBLookup(skillDBRepo).Register(protected)

		// Admin-only routes: same auth.Middleware to decode Claims, plus
		// RequireAdmin to enforce role == "admin". Sits on its own group so the
		// admin gate cannot accidentally leak onto non-admin endpoints.
		adminGroup := r.Group("/")
		adminGroup.Use(auth.Middleware(jwtSvc, auth.WithRevoker(jwtRevoker)))
		adminGroup.Use(auth.RequireAdmin())
		auditHandler.Register(adminGroup)
		if skillDBRepo != nil {
			skills.NewAdminHandler(skillDBRepo).WithAuditSink(auditRepo).Register(adminGroup)
		}
		workflow.NewAdminHandler(workflowService).Register(adminGroup)
		memory.NewAdminHandler(memoryService).WithAuditSink(auditRepo).Register(adminGroup)
		if reflectionAdmin != nil {
			reflectionAdmin.Register(adminGroup)
		}
		// MCP admin is always mounted so the WebUI sees a deterministic 503
		// when disabled instead of a 404. NewAdminHandler tolerates nil mgr.
		mcp.NewAdminHandler(mcpManager, mcpRepo, auditRepo).Register(adminGroup)

		// /metrics — Prometheus exposition. Authenticated via the dual-channel
		// metrics.Auth middleware: static token bypass (for Prom scrape jobs)
		// or admin JWT. Mounted standalone (not under protected/admin groups)
		// so its custom auth chain isn't shadowed by the standard one.
		r.GET("/metrics",
			metrics.Auth(metrics.AuthConfig{
				JWT:         jwtSvc,
				StaticToken: cfg.Observability.MetricsToken,
			}),
			metrics.Handler(promReg),
		)

		// WebSocket group: browsers cannot set Authorization headers on the WS
		// upgrade, so a narrow query-token shim runs before auth.Middleware on
		// this group only. Keep this off the REST group to avoid token leakage
		// in proxy access logs for the API surface.
		wsGroup := r.Group("/")
		wsGroup.Use(auth.WSTokenFromQuery())
		wsGroup.Use(auth.Middleware(jwtSvc, auth.WithRevoker(jwtRevoker)))
		sessionWSHandler.Register(wsGroup)

		// SPA fallback last: API routes already on the tree take precedence;
		// unmatched GETs serve index.html so client-side routing works.
		if fsys, err := webui.FS(); err == nil {
			if err := httpx.RegisterSPAFallback(r, fsys); err != nil {
				slog.Warn("spa fallback disabled", "err", err.Error())
			}
		} else {
			slog.Warn("spa fallback disabled: webui.FS", "err", err.Error())
		}
	}

	engine := httpx.NewEngine(httpx.Deps{
		ServiceName: cfg.Telemetry.ServiceName,
		Ready:       func() bool { return ready.Load() },
		Register:    register,
		Info: map[string]any{
			"sandbox": map[string]any{
				"driver": cfg.Sandbox.Driver,
			},
		},
	})

	srv := &http.Server{
		Addr:              ":" + strconv.Itoa(cfg.Server.Port),
		Handler:           engine,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       cfg.Server.ReadTimeout,
		WriteTimeout:      cfg.Server.WriteTimeout,
		IdleTimeout:       cfg.Server.IdleTimeout,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	errCh := make(chan error, 1)
	go func() {
		slog.Info("server listening", "port", cfg.Server.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-stop:
		slog.Info("shutting down")
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

// reflectionMemoryAdapter wires memory.Service.Create into the
// reflection.MemoryCreator interface used by both the Reflector
// (auto-approve) and the admin Approve handler. dedupHit = !Created so
// auditing can record whether the row was newly inserted or merged.
type reflectionMemoryAdapter struct {
	svc *memory.Service
}

func newReflectionMemoryAdapter(svc *memory.Service) *reflectionMemoryAdapter {
	return &reflectionMemoryAdapter{svc: svc}
}

func (a *reflectionMemoryAdapter) CreateForReflection(ctx context.Context,
	tenantID, userID uuid.UUID, typ, content string, tags []string,
	sourceMsgID *uuid.UUID) (uuid.UUID, bool, error) {
	res, err := a.svc.Create(ctx, tenantID, userID, memory.CreateRequest{
		Type:        typ,
		Content:     content,
		Tags:        tags,
		Source:      memory.SourceReflection,
		SourceMsgID: sourceMsgID,
	})
	if err != nil {
		return uuid.Nil, false, err
	}
	return res.Memory.ID, !res.Created, nil
}

// toOrchestratorRules converts the config-layer rule structs into the
// orchestrator package's Rule type. The two packages stay decoupled (config
// imports skills; orchestrator imports nothing internal) at the cost of this
// trivial shim.
func toOrchestratorRules(in []config.OrchestratorRuleConfig) []orchestrator.Rule {
	out := make([]orchestrator.Rule, 0, len(in))
	for _, r := range in {
		out = append(out, orchestrator.Rule{
			Name: r.Name,
			Match: orchestrator.RuleMatch{
				Profile:         r.Match.Profile,
				ContentRegex:    r.Match.ContentRegex,
				ContentContains: r.Match.ContentContains,
			},
			Suggest: orchestrator.RuleSuggest{
				Type:   r.Suggest.Type,
				Target: r.Suggest.Target,
				Hint:   r.Suggest.Hint,
			},
		})
	}
	return out
}

// buildK8sRestConfig builds a *rest.Config from sandbox.k8s config. Two
// modes are supported: in-cluster (the recommended posture when this
// binary runs inside K8s itself) and kubeconfig (the dev/debug posture).
//
// In-cluster requires the controller pod to have a ServiceAccount with
// pods.create/delete/get/list rights in the target namespace; the chart
// in 22d2 will own that RBAC.
//
// Kubeconfig path resolution defers to clientcmd's standard rules
// ($KUBECONFIG → $HOME/.kube/config) when cfg.Kubeconfig is empty.
func buildK8sRestConfig(cfg config.SandboxK8sConfig) (*rest.Config, error) {
	if cfg.InCluster {
		rc, err := rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("in-cluster config: %w", err)
		}
		return rc, nil
	}
	loading := clientcmd.NewDefaultClientConfigLoadingRules()
	if cfg.Kubeconfig != "" {
		loading.ExplicitPath = cfg.Kubeconfig
	}
	rc, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loading, &clientcmd.ConfigOverrides{}).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("kubeconfig: %w", err)
	}
	return rc, nil
}
