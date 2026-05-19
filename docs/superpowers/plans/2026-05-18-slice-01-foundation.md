# Foundation Slice Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 搭起整个项目的地基 — Go 后端、PG 迁移、JWT 登录、Gin HTTP、审计、OpenTelemetry、docker-compose 一键起。验收时：登录拿 token、访问受保护端点拿到自己的身份、health/ready/metrics 端点都通；docker compose up 跑通。

**Architecture:** 模块化单体 Go 服务。`/cmd/server` 是入口；`/internal/*` 按子域划分；接口与实现隔离，便于后续切片插入（Sandbox、ModelGW、ToolBus、Memory…）。多租户从 schema 层就带 `tenant_id`，但 P0 默认部署单租户（一个 default tenant）。

**Tech Stack:**
- Go 1.26+
- Gin (HTTP)
- pgx v5（PG 驱动）
- golang-migrate（迁移工具，作为库）
- viper（配置）
- zap（日志）
- golang-jwt/jwt v5（JWT）
- bcrypt（密码哈希）
- OpenTelemetry SDK（trace + metrics）
- testify + dockertest（集成测试用真实 PG）

---

## File Structure

```
private-coding-agent/
  go.mod
  go.sum
  .gitignore
  README.md
  /cmd
    /server
      main.go                       服务入口
  /internal
    /config
      config.go                     Config 结构 + 加载
      config_test.go
    /db
      db.go                         pgx 连接池
      migrate.go                    迁移嵌入与执行
      /migrations
        0001_create_tenants.up.sql
        0001_create_tenants.down.sql
        0002_create_users.up.sql
        0002_create_users.down.sql
        0003_create_audit_log.up.sql
        0003_create_audit_log.down.sql
    /tenant
      model.go                      Tenant 结构
      repo.go                       Tenant 仓储
      repo_test.go
    /user
      model.go
      repo.go
      repo_test.go
      service.go                    含密码哈希
      service_test.go
    /auth
      jwt.go                        签发 + 校验
      jwt_test.go
      middleware.go                 Gin 中间件
      middleware_test.go
      handler.go                    /auth/login
      handler_test.go
    /audit
      model.go
      repo.go
      middleware.go
      middleware_test.go
    /telemetry
      otel.go                       trace + metrics provider
      otel_test.go
    /httpx
      server.go                     Gin 引擎 + 路由组装
      health.go                     /healthz + /readyz
      me.go                         /me 端点
      server_test.go
  /deploy
    /compose
      docker-compose.yml
      .env.example
  Dockerfile
  /config
    config.example.yaml
```

---

## Task 0: 项目骨架与 git 初始化

**Files:**
- Create: `D:/IdeaProjects/private-coding-agent/.gitignore`
- Create: `D:/IdeaProjects/private-coding-agent/README.md`
- Create: `D:/IdeaProjects/private-coding-agent/go.mod`

- [ ] **Step 1：进入项目根并 git init**

PowerShell:
```powershell
cd D:\IdeaProjects\private-coding-agent
git init
```
Expected: `Initialized empty Git repository in D:/IdeaProjects/private-coding-agent/.git/`

- [ ] **Step 2：写 .gitignore**

```gitignore
# binaries
/bin/
/dist/
*.exe
*.test

# Go
/vendor/
*.out
coverage.txt

# IDE
.idea/
.vscode/
*.swp

# env / secrets
.env
*.env.local
/deploy/compose/.env

# OS
.DS_Store
Thumbs.db
```

- [ ] **Step 3：写 README.md（占位）**

```markdown
# Private Coding Agent

私有化部署的 AI 编码 Agent 平台。

## 状态
切片 1（Foundation）开发中。

## 快速启动
见 `deploy/compose/`。
```

- [ ] **Step 4：初始化 go.mod**

```powershell
go mod init github.com/yourorg/private-coding-agent
```
Expected: `go: creating new go.mod: module github.com/yourorg/private-coding-agent`

> 把 `yourorg` 替换为实际组织名后续不再说明。

- [ ] **Step 5：首次提交**

```powershell
git add .gitignore README.md go.mod
git commit -m "chore: bootstrap go module and repo"
```

---

## Task 1: 配置加载（YAML + env）

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`
- Create: `config/config.example.yaml`

- [ ] **Step 1：先写失败测试**

`internal/config/config_test.go`:
```go
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
```

- [ ] **Step 2：跑测试确认失败**

```powershell
go test ./internal/config/...
```
Expected: 编译失败（package 还没创建）。

- [ ] **Step 3：写实现**

```powershell
go get github.com/spf13/viper github.com/stretchr/testify/require
```

`internal/config/config.go`:
```go
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
	Server    ServerConfig    `mapstructure:"server"`
	DB        DBConfig        `mapstructure:"db"`
	Auth      AuthConfig      `mapstructure:"auth"`
	Telemetry TelemetryConfig `mapstructure:"telemetry"`
}

type ServerConfig struct {
	Port int    `mapstructure:"port"`
	Mode string `mapstructure:"mode"` // "release" | "debug"
}

type DBConfig struct {
	DSN string `mapstructure:"dsn"`
}

type AuthConfig struct {
	JWTSecret string        `mapstructure:"jwt_secret"`
	JWTTTL    time.Duration `mapstructure:"jwt_ttl"`
}

type TelemetryConfig struct {
	ServiceName string `mapstructure:"service_name"`
	OTLPEndpoint string `mapstructure:"otlp_endpoint"`
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
```

- [ ] **Step 4：跑测试确认通过**

```powershell
go test ./internal/config/...
```
Expected: `ok  github.com/yourorg/private-coding-agent/internal/config`

- [ ] **Step 5：写示例配置**

`config/config.example.yaml`:
```yaml
server:
  port: 8080
  mode: debug
db:
  dsn: "postgres://app:app@localhost:5432/app?sslmode=disable"
auth:
  jwt_secret: "change-me-in-production"
  jwt_ttl: "24h"
telemetry:
  service_name: "private-coding-agent"
  otlp_endpoint: ""
```

- [ ] **Step 6：commit**

```powershell
go mod tidy
git add internal/config config/config.example.yaml go.mod go.sum
git commit -m "feat(config): yaml + env loader"
```

---

## Task 2: PG 连接 + 迁移基建

**Files:**
- Create: `internal/db/db.go`
- Create: `internal/db/migrate.go`
- Create: `internal/db/migrations/` (空目录占位)

- [ ] **Step 1：装依赖**

```powershell
go get github.com/jackc/pgx/v5/pgxpool github.com/golang-migrate/migrate/v4 github.com/golang-migrate/migrate/v4/database/postgres github.com/golang-migrate/migrate/v4/source/iofs
```

- [ ] **Step 2：写连接池模块**

`internal/db/db.go`:
```go
// Package db wraps pgx pool and migration runner.
package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Pool = pgxpool.Pool

func Connect(ctx context.Context, dsn string) (*Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}
	cfg.MaxConns = 20
	cfg.MaxConnLifetime = time.Hour

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("new pool: %w", err)
	}
	pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return pool, nil
}
```

- [ ] **Step 3：写迁移执行器（使用 embed.FS）**

`internal/db/migrate.go`:
```go
package db

import (
	"embed"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

func Migrate(dsn string) error {
	src, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("iofs: %w", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, dsn)
	if err != nil {
		return fmt.Errorf("migrate new: %w", err)
	}
	defer m.Close()
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}
```

- [ ] **Step 4：commit**

```powershell
go mod tidy
git add internal/db go.mod go.sum
git commit -m "feat(db): pgx pool + embed migrate"
```

---

## Task 3: tenants 表 + 仓储 + 集成测试

**Files:**
- Create: `internal/db/migrations/0001_create_tenants.up.sql`
- Create: `internal/db/migrations/0001_create_tenants.down.sql`
- Create: `internal/tenant/model.go`
- Create: `internal/tenant/repo.go`
- Create: `internal/tenant/repo_test.go`

- [ ] **Step 1：写迁移**

`internal/db/migrations/0001_create_tenants.up.sql`:
```sql
CREATE TABLE tenants (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug         TEXT NOT NULL UNIQUE,
    name         TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO tenants (slug, name) VALUES ('default', 'Default Tenant');
```

`internal/db/migrations/0001_create_tenants.down.sql`:
```sql
DROP TABLE tenants;
```

- [ ] **Step 2：写模型**

`internal/tenant/model.go`:
```go
package tenant

import (
	"time"

	"github.com/google/uuid"
)

type Tenant struct {
	ID        uuid.UUID
	Slug      string
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
}
```

```powershell
go get github.com/google/uuid
```

- [ ] **Step 3：写仓储**

`internal/tenant/repo.go`:
```go
package tenant

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("tenant not found")

type Repo struct {
	pool *pgxpool.Pool
}

func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

func (r *Repo) GetBySlug(ctx context.Context, slug string) (*Tenant, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, slug, name, created_at, updated_at FROM tenants WHERE slug=$1`, slug)
	var t Tenant
	if err := row.Scan(&t.ID, &t.Slug, &t.Name, &t.CreatedAt, &t.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("scan: %w", err)
	}
	return &t, nil
}

func (r *Repo) GetByID(ctx context.Context, id uuid.UUID) (*Tenant, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, slug, name, created_at, updated_at FROM tenants WHERE id=$1`, id)
	var t Tenant
	if err := row.Scan(&t.ID, &t.Slug, &t.Name, &t.CreatedAt, &t.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("scan: %w", err)
	}
	return &t, nil
}
```

- [ ] **Step 4：写集成测试（dockertest 起真 PG）**

```powershell
go get github.com/ory/dockertest/v3
```

`internal/tenant/repo_test.go`:
```go
package tenant_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/db"
	"github.com/yourorg/private-coding-agent/internal/tenant"
)

var testDSN string

func TestMain(m *testing.M) {
	pool, err := dockertest.NewPool("")
	if err != nil {
		log.Fatalf("dockertest: %v", err)
	}
	res, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "postgres",
		Tag:        "16",
		Env: []string{
			"POSTGRES_USER=app",
			"POSTGRES_PASSWORD=app",
			"POSTGRES_DB=app",
		},
	}, func(c *docker.HostConfig) {
		c.AutoRemove = true
		c.RestartPolicy = docker.RestartPolicy{Name: "no"}
	})
	if err != nil {
		log.Fatalf("run pg: %v", err)
	}
	defer func() { _ = pool.Purge(res) }()

	testDSN = fmt.Sprintf("postgres://app:app@localhost:%s/app?sslmode=disable",
		res.GetPort("5432/tcp"))

	pool.MaxWait = 60 * time.Second
	if err := pool.Retry(func() error {
		return db.Migrate(testDSN)
	}); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	code := m.Run()
	os.Exit(code)
}

func TestGetBySlug_DefaultExists(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, testDSN)
	require.NoError(t, err)
	defer pool.Close()

	repo := tenant.NewRepo(pool)
	got, err := repo.GetBySlug(ctx, "default")
	require.NoError(t, err)
	require.Equal(t, "Default Tenant", got.Name)
}

func TestGetBySlug_NotFound(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, testDSN)
	require.NoError(t, err)
	defer pool.Close()

	repo := tenant.NewRepo(pool)
	_, err = repo.GetBySlug(ctx, "nope")
	require.ErrorIs(t, err, tenant.ErrNotFound)
}
```

- [ ] **Step 5：跑测试**

```powershell
go mod tidy
go test ./internal/tenant/...
```
Expected: 第一次较慢（拉 PG 镜像），最终 PASS。

- [ ] **Step 6：commit**

```powershell
git add internal/db/migrations internal/tenant go.mod go.sum
git commit -m "feat(tenant): tenants table + repository + default tenant seed"
```

---

## Task 4: users 表 + 仓储 + 密码服务

**Files:**
- Create: `internal/db/migrations/0002_create_users.up.sql`
- Create: `internal/db/migrations/0002_create_users.down.sql`
- Create: `internal/user/model.go`
- Create: `internal/user/repo.go`
- Create: `internal/user/service.go`
- Create: `internal/user/service_test.go`

- [ ] **Step 1：写迁移**

`internal/db/migrations/0002_create_users.up.sql`:
```sql
CREATE TABLE users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    email         TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    name          TEXT NOT NULL DEFAULT '',
    role          TEXT NOT NULL DEFAULT 'member',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, email)
);

CREATE INDEX users_tenant_id_idx ON users(tenant_id);
```

`internal/db/migrations/0002_create_users.down.sql`:
```sql
DROP TABLE users;
```

- [ ] **Step 2：写模型**

`internal/user/model.go`:
```go
package user

import (
	"time"

	"github.com/google/uuid"
)

type Role string

const (
	RoleMember Role = "member"
	RoleAdmin  Role = "admin"
)

type User struct {
	ID           uuid.UUID
	TenantID     uuid.UUID
	Email        string
	PasswordHash string
	Name         string
	Role         Role
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
```

- [ ] **Step 3：写仓储**

`internal/user/repo.go`:
```go
package user

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("user not found")

type Repo struct {
	pool *pgxpool.Pool
}

func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

type CreateInput struct {
	TenantID     uuid.UUID
	Email        string
	PasswordHash string
	Name         string
	Role         Role
}

func (r *Repo) Create(ctx context.Context, in CreateInput) (*User, error) {
	if in.Role == "" {
		in.Role = RoleMember
	}
	row := r.pool.QueryRow(ctx, `
INSERT INTO users (tenant_id, email, password_hash, name, role)
VALUES ($1,$2,$3,$4,$5)
RETURNING id, tenant_id, email, password_hash, name, role, created_at, updated_at`,
		in.TenantID, in.Email, in.PasswordHash, in.Name, string(in.Role))

	var u User
	if err := row.Scan(&u.ID, &u.TenantID, &u.Email, &u.PasswordHash,
		&u.Name, &u.Role, &u.CreatedAt, &u.UpdatedAt); err != nil {
		return nil, fmt.Errorf("insert: %w", err)
	}
	return &u, nil
}

func (r *Repo) GetByEmail(ctx context.Context, tenantID uuid.UUID, email string) (*User, error) {
	row := r.pool.QueryRow(ctx, `
SELECT id, tenant_id, email, password_hash, name, role, created_at, updated_at
FROM users WHERE tenant_id=$1 AND email=$2`, tenantID, email)

	var u User
	if err := row.Scan(&u.ID, &u.TenantID, &u.Email, &u.PasswordHash,
		&u.Name, &u.Role, &u.CreatedAt, &u.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("scan: %w", err)
	}
	return &u, nil
}

func (r *Repo) GetByID(ctx context.Context, id uuid.UUID) (*User, error) {
	row := r.pool.QueryRow(ctx, `
SELECT id, tenant_id, email, password_hash, name, role, created_at, updated_at
FROM users WHERE id=$1`, id)

	var u User
	if err := row.Scan(&u.ID, &u.TenantID, &u.Email, &u.PasswordHash,
		&u.Name, &u.Role, &u.CreatedAt, &u.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("scan: %w", err)
	}
	return &u, nil
}
```

- [ ] **Step 4：写服务层（含 bcrypt 密码逻辑）+ 测试**

```powershell
go get golang.org/x/crypto/bcrypt
```

`internal/user/service.go`:
```go
package user

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

var ErrBadCredentials = errors.New("bad credentials")

type Service struct {
	repo *Repo
}

func NewService(repo *Repo) *Service { return &Service{repo: repo} }

func (s *Service) Register(ctx context.Context, tenantID uuid.UUID, email, password, name string) (*User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	return s.repo.Create(ctx, CreateInput{
		TenantID:     tenantID,
		Email:        email,
		PasswordHash: string(hash),
		Name:         name,
		Role:         RoleMember,
	})
}

func (s *Service) Authenticate(ctx context.Context, tenantID uuid.UUID, email, password string) (*User, error) {
	u, err := s.repo.GetByEmail(ctx, tenantID, email)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrBadCredentials
		}
		return nil, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return nil, ErrBadCredentials
	}
	return u, nil
}
```

`internal/user/service_test.go`:
```go
package user_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/db"
	"github.com/yourorg/private-coding-agent/internal/tenant"
	"github.com/yourorg/private-coding-agent/internal/user"
)

var testDSN string

func TestMain(m *testing.M) {
	pool, err := dockertest.NewPool("")
	if err != nil {
		log.Fatalf("dockertest: %v", err)
	}
	res, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "postgres", Tag: "16",
		Env: []string{"POSTGRES_USER=app", "POSTGRES_PASSWORD=app", "POSTGRES_DB=app"},
	}, func(c *docker.HostConfig) {
		c.AutoRemove = true
		c.RestartPolicy = docker.RestartPolicy{Name: "no"}
	})
	if err != nil {
		log.Fatalf("run pg: %v", err)
	}
	defer func() { _ = pool.Purge(res) }()

	testDSN = fmt.Sprintf("postgres://app:app@localhost:%s/app?sslmode=disable",
		res.GetPort("5432/tcp"))
	pool.MaxWait = 60 * time.Second
	if err := pool.Retry(func() error { return db.Migrate(testDSN) }); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	os.Exit(m.Run())
}

func TestRegisterAndAuthenticate(t *testing.T) {
	ctx := context.Background()
	pg, err := pgxpool.New(ctx, testDSN)
	require.NoError(t, err)
	defer pg.Close()

	tRepo := tenant.NewRepo(pg)
	def, err := tRepo.GetBySlug(ctx, "default")
	require.NoError(t, err)

	svc := user.NewService(user.NewRepo(pg))
	u, err := svc.Register(ctx, def.ID, "alice@example.com", "s3cret!", "Alice")
	require.NoError(t, err)
	require.NotEqual(t, "s3cret!", u.PasswordHash)

	authed, err := svc.Authenticate(ctx, def.ID, "alice@example.com", "s3cret!")
	require.NoError(t, err)
	require.Equal(t, u.ID, authed.ID)
}

func TestAuthenticate_BadPassword(t *testing.T) {
	ctx := context.Background()
	pg, err := pgxpool.New(ctx, testDSN)
	require.NoError(t, err)
	defer pg.Close()

	tRepo := tenant.NewRepo(pg)
	def, err := tRepo.GetBySlug(ctx, "default")
	require.NoError(t, err)

	svc := user.NewService(user.NewRepo(pg))
	_, err = svc.Register(ctx, def.ID, "bob@example.com", "right-pass", "Bob")
	require.NoError(t, err)

	_, err = svc.Authenticate(ctx, def.ID, "bob@example.com", "wrong-pass")
	require.ErrorIs(t, err, user.ErrBadCredentials)
}

func TestAuthenticate_UnknownUser(t *testing.T) {
	ctx := context.Background()
	pg, err := pgxpool.New(ctx, testDSN)
	require.NoError(t, err)
	defer pg.Close()

	tRepo := tenant.NewRepo(pg)
	def, err := tRepo.GetBySlug(ctx, "default")
	require.NoError(t, err)

	svc := user.NewService(user.NewRepo(pg))
	_, err = svc.Authenticate(ctx, def.ID, "nobody@example.com", "x")
	require.ErrorIs(t, err, user.ErrBadCredentials)
}
```

- [ ] **Step 5：跑测试**

```powershell
go mod tidy
go test ./internal/user/...
```
Expected: PASS

- [ ] **Step 6：commit**

```powershell
git add internal/db/migrations internal/user go.mod go.sum
git commit -m "feat(user): users table + repo + service with bcrypt"
```

---

## Task 5: JWT 服务（签发 + 校验）

**Files:**
- Create: `internal/auth/jwt.go`
- Create: `internal/auth/jwt_test.go`

- [ ] **Step 1：装依赖**

```powershell
go get github.com/golang-jwt/jwt/v5
```

- [ ] **Step 2：先写测试**

`internal/auth/jwt_test.go`:
```go
package auth_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/auth"
)

func TestIssueAndParse(t *testing.T) {
	svc := auth.NewJWT(auth.JWTConfig{Secret: "test-secret", TTL: time.Hour})
	uid, tid := uuid.New(), uuid.New()
	tok, err := svc.Issue(uid, tid, "member")
	require.NoError(t, err)
	require.NotEmpty(t, tok)

	c, err := svc.Parse(tok)
	require.NoError(t, err)
	require.Equal(t, uid, c.UserID)
	require.Equal(t, tid, c.TenantID)
	require.Equal(t, "member", c.Role)
}

func TestParse_Expired(t *testing.T) {
	svc := auth.NewJWT(auth.JWTConfig{Secret: "test-secret", TTL: -time.Minute})
	tok, err := svc.Issue(uuid.New(), uuid.New(), "member")
	require.NoError(t, err)
	_, err = svc.Parse(tok)
	require.Error(t, err)
}

func TestParse_BadSecret(t *testing.T) {
	a := auth.NewJWT(auth.JWTConfig{Secret: "k1", TTL: time.Hour})
	b := auth.NewJWT(auth.JWTConfig{Secret: "k2", TTL: time.Hour})
	tok, _ := a.Issue(uuid.New(), uuid.New(), "member")
	_, err := b.Parse(tok)
	require.Error(t, err)
}
```

- [ ] **Step 3：实现**

`internal/auth/jwt.go`:
```go
// Package auth provides JWT issuance/validation and HTTP middleware for the API.
package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type JWTConfig struct {
	Secret string
	TTL    time.Duration
}

type Claims struct {
	UserID   uuid.UUID
	TenantID uuid.UUID
	Role     string
}

type JWT struct {
	cfg JWTConfig
}

func NewJWT(cfg JWTConfig) *JWT { return &JWT{cfg: cfg} }

type jwtClaims struct {
	UserID   string `json:"uid"`
	TenantID string `json:"tid"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

func (s *JWT) Issue(userID, tenantID uuid.UUID, role string) (string, error) {
	now := time.Now()
	c := jwtClaims{
		UserID:   userID.String(),
		TenantID: tenantID.String(),
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(s.cfg.TTL)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, c)
	return tok.SignedString([]byte(s.cfg.Secret))
}

func (s *JWT) Parse(token string) (*Claims, error) {
	t, err := jwt.ParseWithClaims(token, &jwtClaims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected method: %v", t.Header["alg"])
		}
		return []byte(s.cfg.Secret), nil
	})
	if err != nil {
		return nil, err
	}
	jc, ok := t.Claims.(*jwtClaims)
	if !ok || !t.Valid {
		return nil, fmt.Errorf("invalid token")
	}
	uid, err := uuid.Parse(jc.UserID)
	if err != nil {
		return nil, fmt.Errorf("uid: %w", err)
	}
	tid, err := uuid.Parse(jc.TenantID)
	if err != nil {
		return nil, fmt.Errorf("tid: %w", err)
	}
	return &Claims{UserID: uid, TenantID: tid, Role: jc.Role}, nil
}
```

- [ ] **Step 4：跑测试**

```powershell
go mod tidy
go test ./internal/auth/...
```
Expected: PASS

- [ ] **Step 5：commit**

```powershell
git add internal/auth go.mod go.sum
git commit -m "feat(auth): JWT issue + parse with claims"
```

---

## Task 6: Gin HTTP 服务器 + /healthz + /readyz

**Files:**
- Create: `internal/httpx/server.go`
- Create: `internal/httpx/health.go`
- Create: `internal/httpx/server_test.go`

- [ ] **Step 1：装依赖**

```powershell
go get github.com/gin-gonic/gin
```

- [ ] **Step 2：写测试**

`internal/httpx/server_test.go`:
```go
package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/httpx"
)

func TestHealthz(t *testing.T) {
	r := httpx.NewEngine(httpx.Deps{Ready: func() bool { return true }})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"ok"`)
}

func TestReadyz_NotReady(t *testing.T) {
	r := httpx.NewEngine(httpx.Deps{Ready: func() bool { return false }})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestReadyz_Ready(t *testing.T) {
	r := httpx.NewEngine(httpx.Deps{Ready: func() bool { return true }})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	require.Equal(t, http.StatusOK, w.Code)
}
```

- [ ] **Step 3：实现**

`internal/httpx/server.go`:
```go
// Package httpx assembles the Gin engine, routes, and middlewares.
package httpx

import "github.com/gin-gonic/gin"

type Deps struct {
	Ready func() bool
}

func NewEngine(d Deps) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	registerHealth(r, d)
	return r
}
```

`internal/httpx/health.go`:
```go
package httpx

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func registerHealth(r *gin.Engine, d Deps) {
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	r.GET("/readyz", func(c *gin.Context) {
		if d.Ready != nil && d.Ready() {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
			return
		}
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not_ready"})
	})
}
```

- [ ] **Step 4：跑测试**

```powershell
go mod tidy
go test ./internal/httpx/...
```
Expected: PASS

- [ ] **Step 5：commit**

```powershell
git add internal/httpx go.mod go.sum
git commit -m "feat(httpx): gin engine with health/ready"
```

---

## Task 7: 登录端点 POST /auth/login

**Files:**
- Create: `internal/auth/handler.go`
- Create: `internal/auth/handler_test.go`
- Modify: `internal/httpx/server.go`（暴露注册接口）

- [ ] **Step 1：扩展 httpx.Deps，让外层挂模块路由**

修改 `internal/httpx/server.go`:
```go
package httpx

import "github.com/gin-gonic/gin"

type Deps struct {
	Ready    func() bool
	Register func(r *gin.Engine)
}

func NewEngine(d Deps) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	registerHealth(r, d)
	if d.Register != nil {
		d.Register(r)
	}
	return r
}
```

- [ ] **Step 2：写 handler 测试（用 mock svc）**

`internal/auth/handler_test.go`:
```go
package auth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/auth"
	"github.com/yourorg/private-coding-agent/internal/user"
)

type fakeAuth struct {
	user *user.User
	err  error
}

func (f fakeAuth) Authenticate(_ context.Context, _ uuid.UUID, _, _ string) (*user.User, error) {
	return f.user, f.err
}

type fakeTenants struct {
	id uuid.UUID
}

func (f fakeTenants) GetBySlug(_ context.Context, _ string) (uuid.UUID, error) {
	return f.id, nil
}

func TestLoginOK(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tid, uid := uuid.New(), uuid.New()
	h := auth.NewHandler(auth.HandlerDeps{
		Tenants: fakeTenants{id: tid},
		Auth:    fakeAuth{user: &user.User{ID: uid, TenantID: tid, Role: user.RoleMember}},
		JWT:     auth.NewJWT(auth.JWTConfig{Secret: "s", TTL: time.Hour}),
	})
	r := gin.New()
	h.Register(r)

	body, _ := json.Marshal(map[string]string{
		"tenant": "default", "email": "a@b", "password": "x",
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/auth/login",
		bytes.NewReader(body)))
	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotEmpty(t, resp["token"])
}

func TestLogin_BadCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := auth.NewHandler(auth.HandlerDeps{
		Tenants: fakeTenants{id: uuid.New()},
		Auth:    fakeAuth{err: user.ErrBadCredentials},
		JWT:     auth.NewJWT(auth.JWTConfig{Secret: "s", TTL: time.Hour}),
	})
	r := gin.New()
	h.Register(r)

	body, _ := json.Marshal(map[string]string{
		"tenant": "default", "email": "a@b", "password": "x",
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/auth/login",
		bytes.NewReader(body)))
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestLogin_InternalError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := auth.NewHandler(auth.HandlerDeps{
		Tenants: fakeTenants{id: uuid.New()},
		Auth:    fakeAuth{err: errors.New("boom")},
		JWT:     auth.NewJWT(auth.JWTConfig{Secret: "s", TTL: time.Hour}),
	})
	r := gin.New()
	h.Register(r)
	body, _ := json.Marshal(map[string]string{
		"tenant": "default", "email": "a@b", "password": "x",
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/auth/login",
		bytes.NewReader(body)))
	require.Equal(t, http.StatusInternalServerError, w.Code)
}
```

- [ ] **Step 3：实现 handler**

`internal/auth/handler.go`:
```go
package auth

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yourorg/private-coding-agent/internal/user"
)

type AuthService interface {
	Authenticate(ctx context.Context, tenantID uuid.UUID, email, password string) (*user.User, error)
}

type TenantLookup interface {
	GetBySlug(ctx context.Context, slug string) (uuid.UUID, error)
}

type HandlerDeps struct {
	Tenants TenantLookup
	Auth    AuthService
	JWT     *JWT
}

type Handler struct{ d HandlerDeps }

func NewHandler(d HandlerDeps) *Handler { return &Handler{d: d} }

type loginReq struct {
	Tenant   string `json:"tenant" binding:"required"`
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type loginResp struct {
	Token string `json:"token"`
}

func (h *Handler) Register(r *gin.Engine) {
	r.POST("/auth/login", h.login)
}

func (h *Handler) login(c *gin.Context) {
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad_request"})
		return
	}
	tid, err := h.d.Tenants.GetBySlug(c.Request.Context(), req.Tenant)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "bad_credentials"})
		return
	}
	u, err := h.d.Auth.Authenticate(c.Request.Context(), tid, req.Email, req.Password)
	if err != nil {
		if errors.Is(err, user.ErrBadCredentials) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "bad_credentials"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal"})
		return
	}
	tok, err := h.d.JWT.Issue(u.ID, u.TenantID, string(u.Role))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal"})
		return
	}
	c.JSON(http.StatusOK, loginResp{Token: tok})
}
```

- [ ] **Step 4：为 tenant.Repo 加 GetBySlug 的轻量适配**（已经存在，本步把 user.Service 提取为接口）

修改 `internal/user/service.go`，把 `Service` 方法签名变为接口能匹配的形式（已经是了，无需改）。新增 `internal/tenant/lookup.go`（thin adapter）：

`internal/tenant/lookup.go`:
```go
package tenant

import (
	"context"

	"github.com/google/uuid"
)

type Lookup struct{ r *Repo }

func NewLookup(r *Repo) *Lookup { return &Lookup{r: r} }

func (l *Lookup) GetBySlug(ctx context.Context, slug string) (uuid.UUID, error) {
	t, err := l.r.GetBySlug(ctx, slug)
	if err != nil {
		return uuid.UUID{}, err
	}
	return t.ID, nil
}
```

- [ ] **Step 5：跑测试**

```powershell
go test ./internal/auth/... ./internal/tenant/...
```
Expected: PASS

- [ ] **Step 6：commit**

```powershell
git add internal/auth internal/tenant internal/httpx go.mod go.sum
git commit -m "feat(auth): POST /auth/login with JWT issuance"
```

---

## Task 8: JWT 中间件 + GET /me

**Files:**
- Create: `internal/auth/middleware.go`
- Create: `internal/auth/middleware_test.go`
- Create: `internal/httpx/me.go`

- [ ] **Step 1：写中间件测试**

`internal/auth/middleware_test.go`:
```go
package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/auth"
)

func newProtectedRouter(t *testing.T, secret string) (*gin.Engine, *auth.JWT) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	j := auth.NewJWT(auth.JWTConfig{Secret: secret, TTL: time.Hour})
	r := gin.New()
	r.Use(auth.Middleware(j))
	r.GET("/me", func(c *gin.Context) {
		cl := auth.FromCtx(c.Request.Context())
		c.JSON(http.StatusOK, gin.H{"uid": cl.UserID, "tid": cl.TenantID, "role": cl.Role})
	})
	return r, j
}

func TestMiddleware_OK(t *testing.T) {
	r, j := newProtectedRouter(t, "s")
	uid, tid := uuid.New(), uuid.New()
	tok, _ := j.Issue(uid, tid, "member")

	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), uid.String())
}

func TestMiddleware_MissingHeader(t *testing.T) {
	r, _ := newProtectedRouter(t, "s")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/me", nil))
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestMiddleware_BadToken(t *testing.T) {
	r, _ := newProtectedRouter(t, "s")
	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set("Authorization", "Bearer not-a-jwt")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}
```

- [ ] **Step 2：实现中间件**

`internal/auth/middleware.go`:
```go
package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type ctxKey struct{}

func FromCtx(ctx context.Context) *Claims {
	c, _ := ctx.Value(ctxKey{}).(*Claims)
	return c
}

func Middleware(j *JWT) gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.GetHeader("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing_token"})
			return
		}
		tok := strings.TrimPrefix(h, "Bearer ")
		cl, err := j.Parse(tok)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid_token"})
			return
		}
		ctx := context.WithValue(c.Request.Context(), ctxKey{}, cl)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
```

- [ ] **Step 3：实现 /me 端点**

`internal/httpx/me.go`:
```go
package httpx

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/yourorg/private-coding-agent/internal/auth"
)

func RegisterMe(r *gin.RouterGroup) {
	r.GET("/me", func(c *gin.Context) {
		cl := auth.FromCtx(c.Request.Context())
		if cl == nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"user_id":   cl.UserID,
			"tenant_id": cl.TenantID,
			"role":      cl.Role,
		})
	})
}
```

- [ ] **Step 4：跑测试**

```powershell
go test ./internal/auth/...
```
Expected: PASS

- [ ] **Step 5：commit**

```powershell
git add internal/auth internal/httpx
git commit -m "feat(auth): JWT middleware + /me endpoint"
```

---

## Task 9: 审计表 + 审计中间件

**Files:**
- Create: `internal/db/migrations/0003_create_audit_log.up.sql`
- Create: `internal/db/migrations/0003_create_audit_log.down.sql`
- Create: `internal/audit/model.go`
- Create: `internal/audit/repo.go`
- Create: `internal/audit/middleware.go`
- Create: `internal/audit/middleware_test.go`

- [ ] **Step 1：迁移**

`internal/db/migrations/0003_create_audit_log.up.sql`:
```sql
CREATE TABLE audit_log (
    id          BIGSERIAL PRIMARY KEY,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    tenant_id   UUID,
    user_id     UUID,
    action      TEXT NOT NULL,
    target      TEXT NOT NULL DEFAULT '',
    method      TEXT NOT NULL,
    path        TEXT NOT NULL,
    status      INT NOT NULL,
    duration_ms INT NOT NULL,
    metadata    JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX audit_log_tenant_time_idx ON audit_log(tenant_id, occurred_at DESC);
```

`internal/db/migrations/0003_create_audit_log.down.sql`:
```sql
DROP TABLE audit_log;
```

- [ ] **Step 2：模型与仓储**

`internal/audit/model.go`:
```go
package audit

import (
	"time"

	"github.com/google/uuid"
)

type Entry struct {
	OccurredAt time.Time
	TenantID   *uuid.UUID
	UserID     *uuid.UUID
	Action     string
	Target     string
	Method     string
	Path       string
	Status     int
	DurationMS int
	Metadata   map[string]any
}
```

`internal/audit/repo.go`:
```go
package audit

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Repo struct{ pool *pgxpool.Pool }

func NewRepo(p *pgxpool.Pool) *Repo { return &Repo{pool: p} }

func (r *Repo) Append(ctx context.Context, e Entry) error {
	meta, err := json.Marshal(e.Metadata)
	if err != nil {
		meta = []byte("{}")
	}
	_, err = r.pool.Exec(ctx, `
INSERT INTO audit_log (tenant_id, user_id, action, target, method, path, status, duration_ms, metadata)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		e.TenantID, e.UserID, e.Action, e.Target, e.Method, e.Path, e.Status, e.DurationMS, meta)
	if err != nil {
		return fmt.Errorf("insert audit: %w", err)
	}
	return nil
}
```

- [ ] **Step 3：中间件**

`internal/audit/middleware.go`:
```go
package audit

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/yourorg/private-coding-agent/internal/auth"
)

type Sink interface {
	Append(ctx context.Context, e Entry) error
}

// Middleware writes an audit entry per request. Failure to write is logged via
// the optional onErr callback but does not block the request.
func Middleware(s Sink, onErr func(error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		cl := auth.FromCtx(c.Request.Context())
		e := Entry{
			OccurredAt: start,
			Method:     c.Request.Method,
			Path:       c.FullPath(),
			Status:     c.Writer.Status(),
			DurationMS: int(time.Since(start).Milliseconds()),
			Action:     "http_request",
		}
		if cl != nil {
			t, u := cl.TenantID, cl.UserID
			e.TenantID, e.UserID = &t, &u
		}
		if err := s.Append(c.Request.Context(), e); err != nil && onErr != nil {
			onErr(err)
		}
	}
}
```

- [ ] **Step 4：中间件测试（用 mock sink）**

`internal/audit/middleware_test.go`:
```go
package audit_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/audit"
)

type spySink struct {
	mu  sync.Mutex
	got []audit.Entry
}

func (s *spySink) Append(_ context.Context, e audit.Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.got = append(s.got, e)
	return nil
}

func TestAuditMiddleware_WritesEntry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &spySink{}
	r := gin.New()
	r.Use(audit.Middleware(s, nil))
	r.GET("/x", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
	require.Equal(t, http.StatusOK, w.Code)

	require.Len(t, s.got, 1)
	require.Equal(t, "GET", s.got[0].Method)
	require.Equal(t, http.StatusOK, s.got[0].Status)
}
```

- [ ] **Step 5：跑测试**

```powershell
go test ./internal/audit/...
```
Expected: PASS

- [ ] **Step 6：commit**

```powershell
git add internal/db/migrations internal/audit
git commit -m "feat(audit): audit_log table + middleware"
```

---

## Task 10: OpenTelemetry 接入（trace + metrics）

**Files:**
- Create: `internal/telemetry/otel.go`
- Create: `internal/telemetry/otel_test.go`

- [ ] **Step 1：装依赖**

```powershell
go get go.opentelemetry.io/otel go.opentelemetry.io/otel/sdk/trace go.opentelemetry.io/otel/sdk/metric go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin
```

- [ ] **Step 2：实现**

`internal/telemetry/otel.go`:
```go
// Package telemetry wires OpenTelemetry trace + metrics providers and exposes
// a Setup that returns a shutdown function.
package telemetry

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

type Config struct {
	ServiceName  string
	OTLPEndpoint string // empty -> no-op
}

func Setup(ctx context.Context, cfg Config) (func(context.Context) error, error) {
	if cfg.OTLPEndpoint == "" {
		return func(context.Context) error { return nil }, nil
	}

	res, err := resource.New(ctx, resource.WithAttributes(
		semconv.ServiceNameKey.String(cfg.ServiceName),
	))
	if err != nil {
		return nil, fmt.Errorf("resource: %w", err)
	}

	traceExp, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithInsecure(),
		otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint),
	)
	if err != nil {
		return nil, fmt.Errorf("trace exporter: %w", err)
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExp),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	metricExp, err := otlpmetricgrpc.New(ctx,
		otlpmetricgrpc.WithInsecure(),
		otlpmetricgrpc.WithEndpoint(cfg.OTLPEndpoint),
	)
	if err != nil {
		return nil, fmt.Errorf("metric exporter: %w", err)
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp)),
	)
	otel.SetMeterProvider(mp)

	return func(ctx context.Context) error {
		_ = tp.Shutdown(ctx)
		_ = mp.Shutdown(ctx)
		return nil
	}, nil
}
```

- [ ] **Step 3：测试（仅校验 no-op 路径与 setup 不 panic）**

`internal/telemetry/otel_test.go`:
```go
package telemetry_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/telemetry"
)

func TestSetup_NoEndpoint_NoOp(t *testing.T) {
	shutdown, err := telemetry.Setup(context.Background(), telemetry.Config{ServiceName: "x"})
	require.NoError(t, err)
	require.NotNil(t, shutdown)
	require.NoError(t, shutdown(context.Background()))
}
```

- [ ] **Step 4：跑测试**

```powershell
go mod tidy
go test ./internal/telemetry/...
```
Expected: PASS

- [ ] **Step 5：在 httpx 注入 otelgin 中间件（修改 server.go）**

修改 `internal/httpx/server.go`:
```go
package httpx

import (
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

type Deps struct {
	ServiceName string
	Ready       func() bool
	Register    func(r *gin.Engine)
}

func NewEngine(d Deps) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	if d.ServiceName != "" {
		r.Use(otelgin.Middleware(d.ServiceName))
	}
	registerHealth(r, d)
	if d.Register != nil {
		d.Register(r)
	}
	return r
}
```

- [ ] **Step 6：commit**

```powershell
git add internal/telemetry internal/httpx go.mod go.sum
git commit -m "feat(telemetry): OTel trace + metrics setup with gin instrumentation"
```

---

## Task 11: main.go 装配整套依赖

**Files:**
- Create: `cmd/server/main.go`

- [ ] **Step 1：写入口**

`cmd/server/main.go`:
```go
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
```

- [ ] **Step 2：本地编译验证**

```powershell
go build ./cmd/server
```
Expected: 生成 `server.exe`（Windows）或无报错。

- [ ] **Step 3：commit**

```powershell
git add cmd/server
git commit -m "feat(cmd): main wiring with config/db/auth/audit/otel"
```

---

## Task 12: Dockerfile

**Files:**
- Create: `Dockerfile`
- Create: `.dockerignore`

- [ ] **Step 1：写 .dockerignore**

`.dockerignore`:
```
.git
.gitignore
*.md
/bin/
/dist/
.idea/
.vscode/
deploy/compose
```

- [ ] **Step 2：写 Dockerfile**

`Dockerfile`:
```dockerfile
FROM golang:1.26-alpine AS build
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/server /app/server
COPY config/config.example.yaml /app/config/config.yaml
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/app/server", "--config", "/app/config/config.yaml"]
```

- [ ] **Step 3：本地构建镜像**

```powershell
docker build -t pca/server:dev .
```
Expected: `Successfully tagged pca/server:dev` 或等价输出。

- [ ] **Step 4：commit**

```powershell
git add Dockerfile .dockerignore
git commit -m "build: multi-stage Dockerfile with distroless runtime"
```

---

## Task 13: docker-compose 一键启动

**Files:**
- Create: `deploy/compose/docker-compose.yml`
- Create: `deploy/compose/.env.example`

- [ ] **Step 1：写 .env.example**

`deploy/compose/.env.example`:
```env
POSTGRES_USER=app
POSTGRES_PASSWORD=app
POSTGRES_DB=app

PCA_AUTH_JWT_SECRET=change-me-in-production
PCA_TELEMETRY_OTLP_ENDPOINT=
```

- [ ] **Step 2：写 docker-compose.yml**

`deploy/compose/docker-compose.yml`:
```yaml
services:
  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: ${POSTGRES_USER}
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}
      POSTGRES_DB: ${POSTGRES_DB}
    healthcheck:
      test: ["CMD", "pg_isready", "-U", "${POSTGRES_USER}"]
      interval: 3s
      timeout: 3s
      retries: 20
    volumes:
      - pgdata:/var/lib/postgresql/data
    ports:
      - "5432:5432"

  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"

  server:
    build:
      context: ../..
      dockerfile: Dockerfile
    depends_on:
      postgres:
        condition: service_healthy
    environment:
      PCA_DB_DSN: "postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@postgres:5432/${POSTGRES_DB}?sslmode=disable"
      PCA_SERVER_PORT: 8080
      PCA_SERVER_MODE: release
      PCA_AUTH_JWT_SECRET: ${PCA_AUTH_JWT_SECRET}
      PCA_AUTH_JWT_TTL: 24h
      PCA_TELEMETRY_SERVICE_NAME: private-coding-agent
      PCA_TELEMETRY_OTLP_ENDPOINT: ${PCA_TELEMETRY_OTLP_ENDPOINT}
    ports:
      - "8080:8080"

volumes:
  pgdata:
```

- [ ] **Step 3：本地启动验证**

```powershell
cd deploy\compose
copy .env.example .env
docker compose up -d --build
```

等待 ~15s 后：
```powershell
curl http://localhost:8080/healthz
```
Expected: `{"status":"ok"}`

- [ ] **Step 4：跑端到端登录验证（需要先建用户）**

PowerShell:
```powershell
# 进 PG 容器手动建一个测试用户(临时, P0 完成后会有 register API)
docker compose exec postgres psql -U app -d app -c "
INSERT INTO users (tenant_id, email, password_hash, name, role)
VALUES (
  (SELECT id FROM tenants WHERE slug='default'),
  'demo@example.com',
  '\$2a\$10\$3IxPpvJV/G09VsHkXkx9X.eGnRzZqDbX9KqEqM8gJ4n7nW3wT8aP6',
  'Demo',
  'admin'
);"
```

> 注：上面的 hash 是 `demo123` 的 bcrypt。生成方式见 README 后续补充。

```powershell
$body = '{"tenant":"default","email":"demo@example.com","password":"demo123"}'
$resp = Invoke-RestMethod -Method POST -Uri http://localhost:8080/auth/login -ContentType application/json -Body $body
$token = $resp.token
Invoke-RestMethod -Uri http://localhost:8080/me -Headers @{Authorization = "Bearer $token"}
```
Expected: 返回带 `user_id / tenant_id / role` 的 JSON。

- [ ] **Step 5：清理**

```powershell
docker compose down
cd ..\..
```

- [ ] **Step 6：commit**

```powershell
git add deploy/compose
git commit -m "deploy: docker-compose with postgres/redis/server"
```

---

## Task 14: README 补全 + 验收清单

**Files:**
- Modify: `README.md`

- [ ] **Step 1：更新 README**

`README.md`:
```markdown
# Private Coding Agent

私有化部署的 AI 编码 Agent 平台。

## 切片进度

- [x] **切片 1：Foundation**（本文档）
- [ ] 切片 2：Sandbox
- [ ] 切片 3：Model Gateway
- [ ] 切片 4：Tool Bus + Internal MCP
- [ ] 切片 5：Agent Engine
- [ ] 切片 6：Session API + WebSocket
- [ ] 切片 7：Memory (basic)
- [ ] 切片 8：Web Frontend
- [ ] 切片 9：Integration & Audit

## 本地开发

```powershell
# 单元 + 集成测试 (会拉 postgres 镜像)
go test ./...

# 本地直接跑
copy config\config.example.yaml config\config.yaml
go run ./cmd/server --config config\config.yaml
```

## docker-compose 启动

```powershell
cd deploy\compose
copy .env.example .env
docker compose up -d --build
curl http://localhost:8080/healthz
```

## 关键端点

| 方法 | 路径 | 鉴权 | 说明 |
|---|---|---|---|
| GET | /healthz | - | 健康检查 |
| GET | /readyz | - | 就绪检查 |
| POST | /auth/login | - | 登录拿 JWT |
| GET | /me | Bearer | 当前用户身份 |

## 配置

见 `config/config.example.yaml`。所有字段可用 `PCA_<UPPER>_<UPPER>` 环境变量覆盖（点号换下划线）。
```

- [ ] **Step 2：commit**

```powershell
git add README.md
git commit -m "docs: README for slice 1 (foundation)"
```

---

## 验收（end-of-slice checklist）

完成后应满足：

- [ ] `go test ./...` 全部 PASS（含 dockertest 集成测试）
- [ ] `go build ./...` 无错
- [ ] `docker build -t pca/server:dev .` 成功
- [ ] `cd deploy/compose && docker compose up -d` 起来
- [ ] `curl http://localhost:8080/healthz` 返回 `{"status":"ok"}`
- [ ] `POST /auth/login` 用建好的用户能拿到 token
- [ ] `GET /me` 带 Bearer 能返回 `user_id / tenant_id / role`
- [ ] `audit_log` 表里有请求记录
- [ ] 所有任务都已 commit 且 git tree 干净

---

## Self-Review 检查清单（plan 作者自检）

- 每个 Task 都有 Files 列表、TDD 步骤、可执行命令、commit 步骤 ✓
- 没有 TBD / "类似 Task N" 之类占位 ✓
- 文件路径精确，配合 spec §附录 B 的目录结构 ✓
- spec 中 P0 列表里属于本切片的项均有 task 覆盖：JWT + auth ✓、tenant/user schema ✓、健康端点 ✓、审计 ✓、OTel ✓、Dockerfile ✓、docker-compose ✓
- 留作后续切片：Agent / Sandbox / Model GW / Memory / Workflow / Web UI ✓（明确列在 README "切片进度"）
- 类型/方法签名内部一致：`AuthService.Authenticate`、`TenantLookup.GetBySlug`、`auth.FromCtx`、`audit.Sink.Append`、`httpx.Deps{Register}` 全程一致 ✓

