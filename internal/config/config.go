// Package config loads layered configuration from YAML + env vars.
// Env vars override YAML using PCA_ prefix; nested fields use underscore.
package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Server        ServerConfig        `mapstructure:"server"`
	DB            DBConfig            `mapstructure:"db"`
	Redis         RedisConfig         `mapstructure:"redis"`
	Auth          AuthConfig          `mapstructure:"auth"`
	Telemetry     TelemetryConfig     `mapstructure:"telemetry"`
	Observability ObservabilityConfig `mapstructure:"observability"`
	Memory        MemoryConfig        `mapstructure:"memory"`
}

// MemoryConfig drives the vector-memory pipeline. EmbedOnWrite=false is the
// operational kill switch: Create / Search degrade to slice-7 keyword-only.
type MemoryConfig struct {
	EmbeddingModel string  `mapstructure:"embedding_model"` // e.g. "default-mock:text"
	DedupThreshold float64 `mapstructure:"dedup_threshold"` // cosine sim; 0.92 default; 0 disables
	EmbedOnWrite   bool    `mapstructure:"embed_on_write"`  // true to enable vector pipeline
}

type ServerConfig struct {
	Port              int      `mapstructure:"port"`
	Mode              string   `mapstructure:"mode"` // "release" | "debug"
	WSAllowedOrigins  []string `mapstructure:"ws_allowed_origins"`
}

type DBConfig struct {
	DSN string `mapstructure:"dsn"`
}

type RedisConfig struct {
	Addr string `mapstructure:"addr"`
}

type AuthConfig struct {
	JWTSecret string        `mapstructure:"jwt_secret"`
	JWTTTL    time.Duration `mapstructure:"jwt_ttl"`
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
	return &c, nil
}
