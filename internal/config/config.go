// Package config loads layered configuration from YAML + env vars.
// Env vars override YAML using PCA_ prefix; nested fields use underscore.
package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"

	"github.com/yourorg/private-coding-agent/internal/skills"
)

type Config struct {
	Server        ServerConfig        `mapstructure:"server"`
	DB            DBConfig            `mapstructure:"db"`
	Redis         RedisConfig         `mapstructure:"redis"`
	Auth          AuthConfig          `mapstructure:"auth"`
	Telemetry     TelemetryConfig     `mapstructure:"telemetry"`
	Observability ObservabilityConfig `mapstructure:"observability"`
	Memory        MemoryConfig        `mapstructure:"memory"`
	Skills        skills.Config       `mapstructure:"skills"`
	Providers     ProvidersConfig     `mapstructure:"providers"`
	Quota         QuotaConfig         `mapstructure:"quota"`
	RateLimit     RateLimitConfig     `mapstructure:"rate_limit"`
	Reflection    ReflectionConfig    `mapstructure:"reflection"`
	Orchestrator  OrchestratorConfig  `mapstructure:"orchestrator"`
	MCP           MCPConfig           `mapstructure:"mcp"`
	Snapshot      SnapshotConfig      `mapstructure:"snapshot"`
}

// SnapshotConfig drives Slice 22b's sandbox snapshot → S3 object storage
// integration. Enabled=false causes main.go to skip the objstore client
// construction; the three snapshot HTTP routes still register but uniformly
// return 503 snapshot_disabled (mirrors Slice 21b MCP behavior).
//
// Endpoint is the MinIO/S3 endpoint host:port (no scheme). UseSSL toggles
// https/. Prefix is an optional path prepended to every object key; useful
// when multiple deployments share one bucket. KeepLocalImage=false (default)
// removes the committed image after upload to prevent disk bloat on the
// sandbox host; true keeps it for debugging.
type SnapshotConfig struct {
	Enabled         bool   `mapstructure:"enabled"`
	Endpoint        string `mapstructure:"endpoint"`
	Bucket          string `mapstructure:"bucket"`
	AccessKey       string `mapstructure:"access_key"`
	SecretKey       string `mapstructure:"secret_key"`
	Region          string `mapstructure:"region"`
	UseSSL          bool   `mapstructure:"use_ssl"`
	Prefix          string `mapstructure:"prefix"`
	KeepLocalImage  bool   `mapstructure:"keep_local_image"`
}

// MCPConfig drives Slice 21b's external MCP Manager. Enabled=false skips
// Manager construction and admin route registration entirely so single-tenant
// deploys that do not need external MCP servers pay no overhead.
//
// HeartbeatInterval=0 disables the heartbeat goroutine (boot republish still
// runs); 60s is the default.
type MCPConfig struct {
	Enabled           bool          `mapstructure:"enabled"`
	HeartbeatInterval time.Duration `mapstructure:"heartbeat_interval"`
	InvokeTimeout     time.Duration `mapstructure:"invoke_timeout"`
	ListToolsTimeout  time.Duration `mapstructure:"list_tools_timeout"`
}

// OrchestratorConfig drives Slice 21a's pre-Run routing pass. Enabled=false
// causes main.go to skip engine construction entirely (no audit, no metric).
// Rules live in YAML; env vars only toggle the two booleans + default_hint.
type OrchestratorConfig struct {
	Enabled     bool                    `mapstructure:"enabled"`
	InjectHint  bool                    `mapstructure:"inject_hint"`
	DefaultHint string                  `mapstructure:"default_hint"`
	Rules       []OrchestratorRuleConfig `mapstructure:"rules"`
}

// OrchestratorRuleConfig mirrors orchestrator.Rule for YAML binding. Kept in
// internal/config so internal/orchestrator stays import-free of viper.
type OrchestratorRuleConfig struct {
	Name    string                        `mapstructure:"name"`
	Match   OrchestratorRuleMatchConfig   `mapstructure:"match"`
	Suggest OrchestratorRuleSuggestConfig `mapstructure:"suggest"`
}

type OrchestratorRuleMatchConfig struct {
	Profile         []string `mapstructure:"profile"`
	ContentRegex    string   `mapstructure:"content_regex"`
	ContentContains string   `mapstructure:"content_contains"`
}

type OrchestratorRuleSuggestConfig struct {
	Type   string `mapstructure:"type"`
	Target string `mapstructure:"target"`
	Hint   string `mapstructure:"hint"`
}

// ReflectionConfig drives the Reflection Agent (slice 20). Enabled=false skips
// worker construction and admin route registration entirely.
type ReflectionConfig struct {
	Enabled               bool          `mapstructure:"enabled"`
	Model                 string        `mapstructure:"model"`
	AutoApproveThreshold  float64       `mapstructure:"auto_approve_threshold"`
	MaxMessagesPerSession int           `mapstructure:"max_messages_per_session"`
	MaxCharsPerMessage    int           `mapstructure:"max_chars_per_message"`
	WorkerBuffer          int           `mapstructure:"worker_buffer"`
	WorkerTimeout         time.Duration `mapstructure:"worker_timeout"`
}

// ProvidersConfig controls the model-provider registry (slice 13).
type ProvidersConfig struct {
	// DisallowGlobalFallback: when a tenant has no row for a provider name,
	// refuse to fall back to the platform-global row (tenant_id IS NULL).
	// Default false (fallback enabled) so single-tenant deploys keep working.
	DisallowGlobalFallback bool `mapstructure:"disallow_global_fallback"`
}

// QuotaConfig caps usage per tenant+user via Redis fixed-window counters.
// Each field set to 0 disables that check entirely.
type QuotaConfig struct {
	LLMTokensPerDay      int `mapstructure:"llm_tokens_per_day"`      // pre+completion
	SandboxMaxActive     int `mapstructure:"sandbox_max_active"`      // running per tenant
	ToolInvokePerMinute  int `mapstructure:"tool_invoke_per_minute"`  // tool calls
}

// RateLimitConfig is the per-tenant+user HTTP throttle.
type RateLimitConfig struct {
	PerMinute int `mapstructure:"per_minute"` // 0 disables
}

// MemoryConfig drives the vector-memory pipeline. EmbedOnWrite=false is the
// operational kill switch: Create / Search degrade to slice-7 keyword-only.
type MemoryConfig struct {
	EmbeddingModel string  `mapstructure:"embedding_model"` // e.g. "default-mock:text"
	DedupThreshold float64 `mapstructure:"dedup_threshold"` // cosine sim; 0.92 default; 0 disables
	EmbedOnWrite   bool    `mapstructure:"embed_on_write"`  // true to enable vector pipeline
	InjectTopK     int     `mapstructure:"inject_top_k"`    // slice 16; 0 → 5
	InjectMaxChars int     `mapstructure:"inject_max_chars"` // slice 16; 0 → 4000
}

type ServerConfig struct {
	Port             int           `mapstructure:"port"`
	Mode             string        `mapstructure:"mode"` // "release" | "debug"
	WSAllowedOrigins []string      `mapstructure:"ws_allowed_origins"`
	ReadTimeout      time.Duration `mapstructure:"read_timeout"`
	WriteTimeout     time.Duration `mapstructure:"write_timeout"`
	IdleTimeout      time.Duration `mapstructure:"idle_timeout"`
}

type DBConfig struct {
	DSN string `mapstructure:"dsn"`
}

type RedisConfig struct {
	Addr string `mapstructure:"addr"`
}

type AuthConfig struct {
	JWTSecret     string        `mapstructure:"jwt_secret"`
	JWTTTL        time.Duration `mapstructure:"jwt_ttl"`
	LocalEnabled  bool          `mapstructure:"local_enabled"`
	OIDC          OIDCAuthConfig `mapstructure:"oidc"`
}

// OIDCAuthConfig configures the OIDC authorization-code + PKCE login flow.
type OIDCAuthConfig struct {
	Enabled         bool   `mapstructure:"enabled"`
	Issuer          string `mapstructure:"issuer"`
	ClientID        string `mapstructure:"client_id"`
	ClientSecretEnv string `mapstructure:"client_secret_env"`
	RedirectURL     string `mapstructure:"redirect_url"`
	TenantSlug      string `mapstructure:"tenant_slug"`
}

type TelemetryConfig struct {
	ServiceName  string `mapstructure:"service_name"`
	OTLPEndpoint string `mapstructure:"otlp_endpoint"`
}

// ObservabilityConfig controls structured-logging output and the
// Prometheus-scraper static token. Tracing endpoint and service name remain
// in TelemetryConfig (they configure the OTel SDK, not application behavior).
type ObservabilityConfig struct {
	LogFormat    string `mapstructure:"log_format"`    // "json" (default) or "text"
	LogLevel     string `mapstructure:"log_level"`     // "debug" | "info" (default) | "warn" | "error"
	MetricsToken string `mapstructure:"metrics_token"` // Prom scraper static bearer; empty disables the static channel
}

func Load(path string) (*Config, error) {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")
	v.SetEnvPrefix("PCA")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	var c Config
	if err := v.Unmarshal(&c); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	applySkillsDefaults(&c.Skills)
	applySlice13Defaults(&c)
	applySlice15Defaults(&c)
	applySlice20Defaults(&c)
	applySlice21bDefaults(&c)
	applySlice22bDefaults(&c)
	return &c, nil
}

// applySlice22bDefaults fills SnapshotConfig defaults. Enabled stays whatever
// the caller sets (YAML/env). Defaults match the docker-compose minio service
// so a vanilla compose-up just works.
func applySlice22bDefaults(c *Config) {
	if c.Snapshot.Bucket == "" {
		c.Snapshot.Bucket = "pca-snapshots"
	}
	if c.Snapshot.Region == "" {
		c.Snapshot.Region = "us-east-1"
	}
}

// applySlice21bDefaults fills MCPConfig timeouts. Enabled stays whatever the
// caller sets (YAML/env); 0 timeouts default to 30s invoke + 10s list/init +
// 60s heartbeat which matches the plan's tested values.
func applySlice21bDefaults(c *Config) {
	if c.MCP.InvokeTimeout <= 0 {
		c.MCP.InvokeTimeout = 30 * time.Second
	}
	if c.MCP.ListToolsTimeout <= 0 {
		c.MCP.ListToolsTimeout = 10 * time.Second
	}
	if c.MCP.Enabled && c.MCP.HeartbeatInterval == 0 {
		c.MCP.HeartbeatInterval = 60 * time.Second
	}
}

// applySlice20Defaults fills Reflection defaults. Enabled is intentionally
// not defaulted to true here — operators must opt-in (or YAML/env sets it).
func applySlice20Defaults(c *Config) {
	if c.Reflection.Model == "" {
		c.Reflection.Model = "default-mock:gpt-4o"
	}
	if c.Reflection.AutoApproveThreshold == 0 {
		c.Reflection.AutoApproveThreshold = 0.85
	}
	if c.Reflection.MaxMessagesPerSession <= 0 {
		c.Reflection.MaxMessagesPerSession = 20
	}
	if c.Reflection.MaxCharsPerMessage <= 0 {
		c.Reflection.MaxCharsPerMessage = 500
	}
	if c.Reflection.WorkerBuffer <= 0 {
		c.Reflection.WorkerBuffer = 256
	}
	if c.Reflection.WorkerTimeout <= 0 {
		c.Reflection.WorkerTimeout = 5 * time.Minute
	}
}

func applySlice15Defaults(c *Config) {
	if c.Auth.OIDC.TenantSlug == "" {
		c.Auth.OIDC.TenantSlug = "default"
	}
	if c.Auth.OIDC.ClientSecretEnv == "" {
		c.Auth.OIDC.ClientSecretEnv = "OIDC_CLIENT_SECRET"
	}
}

func applySlice13Defaults(c *Config) {
	if c.Quota.LLMTokensPerDay == 0 {
		c.Quota.LLMTokensPerDay = 200000
	}
	if c.Quota.SandboxMaxActive == 0 {
		c.Quota.SandboxMaxActive = 5
	}
	if c.Quota.ToolInvokePerMinute == 0 {
		c.Quota.ToolInvokePerMinute = 120
	}
	if c.RateLimit.PerMinute == 0 {
		c.RateLimit.PerMinute = 600
	}
	if c.Server.ReadTimeout == 0 {
		c.Server.ReadTimeout = 30 * time.Second
	}
	// WriteTimeout intentionally has NO default. Setting it kills SSE
	// streams (chat completions) and WebSocket sessions because http.Server
	// applies it to the whole response lifecycle, not per-write. Operators
	// who want a global write deadline can set server.write_timeout
	// explicitly; we default to 0 (disabled) to keep streaming endpoints
	// working out of the box. ReadHeaderTimeout (10s, hard-coded in main)
	// still protects against slow-header attacks.
	if c.Server.IdleTimeout == 0 {
		c.Server.IdleTimeout = 120 * time.Second
	}
}

func applySkillsDefaults(s *skills.Config) {
	if s.MaxInjectedChars <= 0 {
		s.MaxInjectedChars = 24000
	}
	if s.MaxSkillsPerRun <= 0 {
		s.MaxSkillsPerRun = 5
	}
}
