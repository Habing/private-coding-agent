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

// snapshotYAML is the minimal YAML scaffold that registers every
// Snapshot.* key path with viper so AutomaticEnv can route PCA_SNAPSHOT_*
// env vars to the matching mapstructure fields. The values here are
// intentionally unset/false so the defaults helper has work to do.
const snapshotYAML = `
auth:
  jwt_secret: "test-secret-XXXXXXXXXXXXXXXXXXXXX"
  jwt_ttl: "1h"
snapshot:
  enabled: false
  endpoint: ""
  bucket: ""
  access_key: ""
  secret_key: ""
  region: ""
  use_ssl: false
  prefix: ""
  keep_local_image: false
`

func TestSnapshotConfig_Defaults(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.yaml")
	require.NoError(t, os.WriteFile(p, []byte(snapshotYAML), 0o600))
	cfg, err := Load(p)
	require.NoError(t, err)
	require.Equal(t, "pca-snapshots", cfg.Snapshot.Bucket)
	require.Equal(t, "us-east-1", cfg.Snapshot.Region)
	require.False(t, cfg.Snapshot.Enabled, "Enabled defaults to false; operators opt in")
	require.False(t, cfg.Snapshot.UseSSL)
	require.False(t, cfg.Snapshot.KeepLocalImage)
}

func TestSnapshotConfig_EnvOverride(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.yaml")
	require.NoError(t, os.WriteFile(p, []byte(snapshotYAML), 0o600))
	t.Setenv("PCA_SNAPSHOT_ENABLED", "true")
	t.Setenv("PCA_SNAPSHOT_BUCKET", "custom-bucket")
	t.Setenv("PCA_SNAPSHOT_ENDPOINT", "minio:9000")
	t.Setenv("PCA_SNAPSHOT_ACCESS_KEY", "key")
	t.Setenv("PCA_SNAPSHOT_SECRET_KEY", "secret")

	cfg, err := Load(p)
	require.NoError(t, err)
	require.True(t, cfg.Snapshot.Enabled)
	require.Equal(t, "custom-bucket", cfg.Snapshot.Bucket)
	require.Equal(t, "minio:9000", cfg.Snapshot.Endpoint)
	require.Equal(t, "key", cfg.Snapshot.AccessKey)
	require.Equal(t, "secret", cfg.Snapshot.SecretKey)
}

const sandboxYAML = `
auth:
  jwt_secret: "test-secret-XXXXXXXXXXXXXXXXXXXXX"
  jwt_ttl: "1h"
`

func TestSandboxConfig_Defaults(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.yaml")
	require.NoError(t, os.WriteFile(p, []byte(sandboxYAML), 0o600))
	cfg, err := Load(p)
	require.NoError(t, err)
	require.True(t, cfg.Sandbox.SeccompEnabled, "seccomp defaults to on")
}

func TestSandboxConfig_EnvDisable(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "c.yaml")
	require.NoError(t, os.WriteFile(p, []byte(sandboxYAML), 0o600))
	t.Setenv("PCA_SANDBOX_SECCOMP_ENABLED", "false")
	cfg, err := Load(p)
	require.NoError(t, err)
	require.False(t, cfg.Sandbox.SeccompEnabled, "env false must override default true")
}
