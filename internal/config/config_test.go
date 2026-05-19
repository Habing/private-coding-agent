package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadFromYAML(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.yaml")
	require.NoError(t, os.WriteFile(p, []byte(`
server:
  port: 8080
db:
  dsn: "postgres://app:app@localhost:5432/app?sslmode=disable"
auth:
  jwt_secret: "test-secret"
  jwt_ttl: "24h"
telemetry:
  service_name: "pca"
`), 0o600))

	cfg, err := Load(p)
	require.NoError(t, err)
	require.Equal(t, 8080, cfg.Server.Port)
	require.Equal(t, "test-secret", cfg.Auth.JWTSecret)
}

func TestEnvOverridesYAML(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.yaml")
	require.NoError(t, os.WriteFile(p, []byte(`
server:
  port: 8080
auth:
  jwt_secret: "from-yaml"
  jwt_ttl: "1h"
`), 0o600))

	t.Setenv("PCA_AUTH_JWT_SECRET", "from-env")
	t.Setenv("PCA_SERVER_PORT", "9090")

	cfg, err := Load(p)
	require.NoError(t, err)
	require.Equal(t, 9090, cfg.Server.Port)
	require.Equal(t, "from-env", cfg.Auth.JWTSecret)
}
