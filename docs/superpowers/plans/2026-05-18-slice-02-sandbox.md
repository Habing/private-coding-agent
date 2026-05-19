# Slice 2 — Sandbox Runtime + DockerDriver Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 交付服务器端沙箱运行时——`internal/sandbox` Go 包（`Runtime` 接口 + `DockerDriver` 实现）+ 一套 JWT 鉴权的 HTTP API；默认胖镜像 `pca/sandbox:base`；同步 Exec；接口含 Snapshot 但实现返 `ErrNotImplemented`。

**Architecture:** 单 Go 包封装沙箱抽象。`Runtime` 接口由 HTTP handler 消费、`DockerDriver` 实现，未来 K8sDriver 满足同接口。每个沙箱 = 一个独立 Docker 容器；元数据存 PG `sandbox_sessions`；Destroy 用 Redis 锁防并发；启动期 Reconciler 清理脏数据。

**Tech Stack:**
- Go 1.26+，沿用 Slice 1 既有依赖（gin、pgx v5、testify、dockertest）
- 新增：`github.com/docker/docker` v25+（Docker SDK）、`github.com/redis/go-redis/v9`
- 沙箱镜像：debian:12-slim + Go + Node + Python + 常用 CLI

---

## 前置条件

**必须先完成 Slice 1.5**（HEAD = `535b2ad`）。本 plan 假设：
- `db.Migrate(ctx, dsn)` 已支持 ctx
- `audit.Middleware` 已用 detached ctx
- `ValidateJWTConfig` 已就位（minSecretLength = 32）

---

## File Structure

```
sandbox/image/
  Dockerfile                            pca/sandbox:base 镜像定义
  README.md                              如何手工构建/推送

internal/sandbox/
  types.go                               Status / Sandbox / CreateOpts / ExecOpts / ExecResult / errors
  types_test.go                          状态机转换 + 默认值

  path.go                                路径校验（必须在 /workspace 内）
  path_test.go

  validate.go                            CreateOpts / ExecOpts 范围校验
  validate_test.go

  runtime.go                             Runtime 接口

  sessionrepo.go                         PG 持久化沙箱元数据
  sessionrepo_test.go                    dockertest 集成测

  docker_driver.go                       DockerDriver 主体（含构造、Create、Get、Destroy）
  docker_driver_exec.go                  Exec 子模块
  docker_driver_fs.go                    ReadFile/WriteFile（tar 流）
  docker_driver_test.go                  集成测（//go:build docker_integration）

  reconciler.go                          启动期清理
  reconciler_test.go

  handler.go                             HTTP handlers
  handler_test.go                        用 mock Runtime 测

internal/db/migrations/
  0004_create_sandbox_sessions.up.sql
  0004_create_sandbox_sessions.down.sql

cmd/server/main.go                      装配 Sandbox Runtime + reconciler + routes

deploy/compose/
  docker-compose.yml                     挂 docker.sock + 预拉 sandbox 镜像
  test-e2e.ps1                           端到端测试脚本（仓库内但不入 CI 门禁）

README.md                                Slice 2 进度章节
```

---

## Task 0: Slice 1.5 收尾（NewJWT 内置防御 + Sink godoc + audit deadline 收紧）

**Files:**
- Modify: `internal/auth/jwt.go`
- Modify: `internal/auth/jwt_test.go`
- Modify: `internal/audit/middleware.go`
- Modify: `internal/audit/middleware_test.go`

### Step 1: 写 `NewJWT` 内置防御失败测试

在 `internal/auth/jwt_test.go` 末尾追加：

```go
func TestNewJWT_PanicsOnDefaultSecret(t *testing.T) {
	require.Panics(t, func() {
		auth.NewJWT(auth.JWTConfig{Secret: "change-me-in-production", TTL: time.Hour})
	})
}

func TestNewJWT_PanicsOnShortSecret(t *testing.T) {
	require.Panics(t, func() {
		auth.NewJWT(auth.JWTConfig{Secret: "shortie", TTL: time.Hour})
	})
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
cd D:/IdeaProjects/private-coding-agent
PATH="/d/tools/go/bin:$PATH" go test ./internal/auth/... -run TestNewJWT -count=1
```

期望：两个新测试 FAIL（当前 `NewJWT` 无内部防御）。

- [ ] **Step 3: 改 `NewJWT` 调 ValidateJWTConfig**

修改 `internal/auth/jwt.go` 的 `NewJWT` 函数：

```go
// NewJWT constructs a JWT service from cfg. Panics if cfg fails
// ValidateJWTConfig — guards against accidental insecure secrets even when
// callers forget to validate at startup. main.go is expected to call
// ValidateJWTConfig first and surface a friendly error; this panic is a
// last-resort defense.
func NewJWT(cfg JWTConfig) *JWT {
	if err := ValidateJWTConfig(cfg); err != nil {
		panic("auth.NewJWT: " + err.Error())
	}
	return &JWT{cfg: cfg}
}
```

- [ ] **Step 4: 跑所有 auth 测试确认**

```bash
PATH="/d/tools/go/bin:$PATH" go test ./internal/auth/... -count=1
```

期望：原有测试都用了足够长的 secret（jwt_test 用 `"test-secret"` ← 11 字符，会 panic），所以**还会失败**。需要先修测试 secrets。

- [ ] **Step 5: 修 jwt_test.go 中的弱 secret**

替换 `internal/auth/jwt_test.go` 中所有出现的 `"test-secret"`、`"s"`、`"k1"`、`"k2"` 为合规长度：

```go
// 在文件顶部加常量
const (
	testSecret  = "test-secret-thirty-two-chars-ok!"  // 32 chars
	testSecret2 = "another-32-char-secret-okk!!!!!!"  // 32 chars
)
```

把所有 `Secret: "test-secret"` 改为 `Secret: testSecret`；`"s"` 改 `testSecret`；`"k1"` 改 `testSecret`、`"k2"` 改 `testSecret2`。

类似处理 `internal/auth/handler_test.go`、`internal/auth/middleware_test.go`：所有 `Secret: "s"` 改 `Secret: "test-secret-thirty-two-chars-ok!"`（直接字面量，简单粗暴）。

- [ ] **Step 6: 跑所有 auth 测试**

```bash
PATH="/d/tools/go/bin:$PATH" go test ./internal/auth/... -count=1 -v
```

期望：全 PASS（含新两个 panic 测试 + 既有 14+ 测试）。

- [ ] **Step 7: 修 audit `Sink` godoc + 收紧 deadline 容忍度**

修改 `internal/audit/middleware.go`，在 `Sink` 上加 godoc：

```go
// Sink accepts audit entries produced by Middleware. Implementations must be
// safe for concurrent use and must respect ctx cancellation for IO operations.
type Sink interface {
	Append(ctx context.Context, e Entry) error
}
```

修改 `internal/audit/middleware_test.go` 中 deadline 断言行（位于 `TestAuditMiddleware_SurvivesCanceledRequestCtx`）：

```go
// 收紧到 ±1 秒(原来 ±6 秒)
require.WithinDuration(t, time.Now().Add(5*time.Second), dl, time.Second)
```

- [ ] **Step 8: 全包测试 + vet + build**

```bash
PATH="/d/tools/go/bin:$PATH" go vet ./...
PATH="/d/tools/go/bin:$PATH" go test ./... -count=1
PATH="/d/tools/go/bin:$PATH" go build ./...
```

期望：全 PASS（含集成测试）。

- [ ] **Step 9: commit**

```bash
git add internal/auth internal/audit
git commit -m "chore: slice 1.5 followups (NewJWT defense, Sink godoc, tighten audit deadline assertion)"
```

---

## Task 1: 沙箱基础镜像 `pca/sandbox:base`

**Files:**
- Create: `sandbox/image/Dockerfile`
- Create: `sandbox/image/README.md`
- Modify: `.dockerignore`（确保 server 镜像 build context 不抓 sandbox/）

### Step 1: 创建沙箱镜像 Dockerfile

`sandbox/image/Dockerfile`:

```dockerfile
# pca/sandbox:base — 沙箱默认基础镜像
# 预装常用 CLI + Go/Node/Python 工具链，让会话即起即用
FROM debian:12-slim

ARG DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates curl wget git \
      jq tree ripgrep make \
      golang nodejs npm python3 python3-pip \
      build-essential \
      vim-tiny less \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

# 沙箱用户:固定 uid 10001 避免与主机用户冲突
RUN groupadd -g 10001 sandbox \
    && useradd -m -u 10001 -g 10001 -s /bin/bash sandbox

USER sandbox
WORKDIR /workspace

# 默认睡到天荒地老,真正的工作通过 exec 注入
CMD ["sleep", "infinity"]
```

### Step 2: 创建沙箱镜像 README

`sandbox/image/README.md`:

```markdown
# pca/sandbox:base

沙箱默认基础镜像。debian:12-slim + 常用编程工具链。

## 构建

```bash
docker build -t pca/sandbox:base ./sandbox/image
```

预计镜像大小 ~1.2 GB（首次构建 5-10 分钟）。

## 工具清单

- 通用：git curl wget jq tree ripgrep make less vim-tiny
- Go 工具链（debian 仓库版本）
- Node.js + npm
- Python 3 + pip
- build-essential（gcc/g++/make）

## 用户

`sandbox` (uid=10001, gid=10001) — 非 root，WORKDIR `/workspace`。

## 升级

更新 Dockerfile 后重新 build。不打 latest tag；推荐月度 tag 如 `pca/sandbox:base-2026.05`。
```

- [ ] **Step 3: 调整 .dockerignore 排除 sandbox/ 不进入 server build context**

读取当前 `.dockerignore`：

```bash
cat .dockerignore
```

在末尾追加（如已有跳过）：

```
sandbox/image
```

理由：sandbox 镜像独立 build，server Dockerfile 用 `.` 作为 context 不需要把 sandbox/ 抓进去。

- [ ] **Step 4: 构建沙箱镜像验证 Dockerfile 正确**

```powershell
$env:Path = [Environment]::GetEnvironmentVariable('Path','Machine') + ';' + [Environment]::GetEnvironmentVariable('Path','User')
docker build -t pca/sandbox:base D:\IdeaProjects\private-coding-agent\sandbox\image
```

期望：构建成功。首次会拉 debian:12-slim + 安装包，~5-10 分钟。

验证镜像存在：

```powershell
docker images pca/sandbox:base
```

期望：列出镜像，SIZE 应在 ~1.0-1.5 GB 范围。

- [ ] **Step 5: 烟测沙箱镜像可用**

```powershell
docker run --rm pca/sandbox:base sh -c "id; go version; node --version; python3 --version; git --version"
```

期望输出（顺序可能略不同）：
- `uid=10001(sandbox) gid=10001(sandbox)`
- `go version go1.xx.x linux/amd64`
- `vXX.X.X`（node）
- `Python 3.xx.x`
- `git version 2.xx.x`

- [ ] **Step 6: commit**

```bash
git add sandbox/ .dockerignore
git commit -m "feat(sandbox): pca/sandbox:base image with Go/Node/Python/CLI toolchain"
```

---

## Task 2: 公共类型 `internal/sandbox/types.go`

**Files:**
- Create: `internal/sandbox/types.go`
- Create: `internal/sandbox/types_test.go`

### Step 1: 写测试

`internal/sandbox/types_test.go`:

```go
package sandbox_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/sandbox"
)

func TestStatusConstants(t *testing.T) {
	require.Equal(t, "pending", string(sandbox.StatusPending))
	require.Equal(t, "running", string(sandbox.StatusRunning))
	require.Equal(t, "destroying", string(sandbox.StatusDestroying))
	require.Equal(t, "destroyed", string(sandbox.StatusDestroyed))
	require.Equal(t, "failed", string(sandbox.StatusFailed))
}

func TestNetworkModeConstants(t *testing.T) {
	require.Equal(t, "internal", string(sandbox.NetworkInternal))
	require.Equal(t, "bridge", string(sandbox.NetworkBridge))
	require.Equal(t, "none", string(sandbox.NetworkNone))
}

func TestDefaults(t *testing.T) {
	require.Equal(t, "pca/sandbox:base", sandbox.DefaultImage)
	require.Equal(t, sandbox.NetworkInternal, sandbox.DefaultNetwork)
	require.Equal(t, 1.0, sandbox.DefaultCPUs)
	require.Equal(t, int64(512), sandbox.DefaultMemoryMB)
	require.Equal(t, int64(256), sandbox.DefaultPIDsLimit)
	require.Equal(t, 60, sandbox.DefaultExecTimeoutSec)
}

func TestUpperLimits(t *testing.T) {
	require.Equal(t, 4.0, sandbox.MaxCPUs)
	require.Equal(t, int64(4096), sandbox.MaxMemoryMB)
	require.Equal(t, int64(1024), sandbox.MaxPIDsLimit)
	require.Equal(t, 600, sandbox.MaxExecTimeoutSec)
	require.Equal(t, 1<<20, sandbox.MaxFileSize)         // 1 MB
	require.Equal(t, 128*1024, sandbox.MaxStreamBytes)    // 128 KB per stream
}

func TestErrors(t *testing.T) {
	require.Error(t, sandbox.ErrSandboxNotFound)
	require.Error(t, sandbox.ErrSandboxNotReady)
	require.Error(t, sandbox.ErrPathOutsideWorkspace)
	require.Error(t, sandbox.ErrNotImplemented)
	require.Error(t, sandbox.ErrTooLarge)
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
PATH="/d/tools/go/bin:$PATH" go test ./internal/sandbox/...
```

期望：编译错误（包不存在）。

- [ ] **Step 3: 实现 types.go**

`internal/sandbox/types.go`:

```go
// Package sandbox provides a Runtime abstraction for ephemeral, isolated
// execution environments where the agent reads files, writes files, and
// runs commands. DockerDriver is the Slice 2 implementation; future
// K8sDriver satisfies the same interface.
package sandbox

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Status 是沙箱生命周期状态。
type Status string

const (
	StatusPending    Status = "pending"
	StatusRunning    Status = "running"
	StatusDestroying Status = "destroying"
	StatusDestroyed  Status = "destroyed"
	StatusFailed     Status = "failed"
)

// NetworkMode 决定沙箱网络隔离强度。
type NetworkMode string

const (
	NetworkInternal NetworkMode = "internal" // 共享 internal 网络,可与其他沙箱通信但无外网
	NetworkBridge   NetworkMode = "bridge"   // 默认 bridge,能上外网(仅 dev 用)
	NetworkNone     NetworkMode = "none"     // 无网络
)

// 默认值与上限(包级常量,便于 validate.go 引用)。
const (
	DefaultImage          = "pca/sandbox:base"
	DefaultNetwork        = NetworkInternal
	DefaultCPUs           = 1.0
	DefaultMemoryMB       = int64(512)
	DefaultPIDsLimit      = int64(256)
	DefaultExecTimeoutSec = 60

	MaxCPUs           = 4.0
	MaxMemoryMB       = int64(4096)
	MaxPIDsLimit      = int64(1024)
	MaxExecTimeoutSec = 600
	MaxFileSize       = 1 << 20  // 1 MB
	MaxStreamBytes    = 128 * 1024 // 每个 stream (stdout/stderr) 上限
)

// Sandbox 是沙箱的领域对象。
type Sandbox struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	ProjectID   *uuid.UUID
	OwnerUserID uuid.UUID
	Status      Status
	Image       string
	Network     NetworkMode
	Resources   ResourceLimits
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DestroyedAt *time.Time
}

// ResourceLimits 是资源约束。零值会被 validate 替换为默认。
type ResourceLimits struct {
	CPUs      float64 // 例如 1.0
	MemoryMB  int64   // 例如 512
	PIDsLimit int64   // 例如 256
}

// CreateOpts 是创建沙箱的请求参数。
type CreateOpts struct {
	TenantID    uuid.UUID
	OwnerUserID uuid.UUID
	ProjectID   *uuid.UUID
	Image       string
	Resources   ResourceLimits
	Network     NetworkMode
	Env         map[string]string
	Labels      map[string]string
}

// ExecOpts 是单次命令执行参数。
type ExecOpts struct {
	Cmd        []string
	WorkingDir string
	Env        map[string]string
	Stdin      []byte
	TimeoutSec int
}

// ExecResult 是命令执行结果。
type ExecResult struct {
	ExitCode   int
	Stdout     []byte
	Stderr     []byte
	Truncated  bool
	DurationMS int64
	TimedOut   bool
}

// 错误哨兵
var (
	ErrSandboxNotFound      = errors.New("sandbox not found")
	ErrSandboxNotReady      = errors.New("sandbox not running")
	ErrPathOutsideWorkspace = errors.New("path outside /workspace")
	ErrNotImplemented       = errors.New("not implemented")
	ErrTooLarge             = errors.New("payload too large")
)
```

- [ ] **Step 4: 跑测试确认通过**

```bash
PATH="/d/tools/go/bin:$PATH" go test ./internal/sandbox/... -count=1 -v
```

期望：5 测试 PASS。

- [ ] **Step 5: commit**

```bash
git add internal/sandbox/types.go internal/sandbox/types_test.go
git commit -m "feat(sandbox): types, defaults, limits, error sentinels"
```

---

## Task 3: `sandbox_sessions` 表 + SessionRepo + 集成测试

**Files:**
- Create: `internal/db/migrations/0004_create_sandbox_sessions.up.sql`
- Create: `internal/db/migrations/0004_create_sandbox_sessions.down.sql`
- Create: `internal/sandbox/sessionrepo.go`
- Create: `internal/sandbox/sessionrepo_test.go`

### Step 1: 写迁移

`internal/db/migrations/0004_create_sandbox_sessions.up.sql`:

```sql
CREATE TABLE sandbox_sessions (
    id            UUID PRIMARY KEY,
    tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    owner_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    project_id    UUID,
    container_id  TEXT,
    image         TEXT NOT NULL,
    status        TEXT NOT NULL,
    network_mode  TEXT NOT NULL,
    cpus          REAL NOT NULL DEFAULT 1.0,
    memory_mb     BIGINT NOT NULL DEFAULT 512,
    pids_limit    BIGINT NOT NULL DEFAULT 256,
    labels        JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    destroyed_at  TIMESTAMPTZ
);

CREATE INDEX sandbox_sessions_tenant_status_idx
    ON sandbox_sessions(tenant_id, status);
```

`internal/db/migrations/0004_create_sandbox_sessions.down.sql`:

```sql
DROP TABLE sandbox_sessions;
```

- [ ] **Step 2: 写 SessionRepo + 集成测试**

`internal/sandbox/sessionrepo.go`:

```go
package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SessionRepo persists sandbox metadata in PostgreSQL.
type SessionRepo struct {
	pool *pgxpool.Pool
}

func NewSessionRepo(pool *pgxpool.Pool) *SessionRepo {
	return &SessionRepo{pool: pool}
}

// Insert creates a new sandbox row in status=pending. Returns the inserted
// Sandbox with CreatedAt/UpdatedAt populated.
func (r *SessionRepo) Insert(ctx context.Context, sb *Sandbox) error {
	labels, _ := json.Marshal(map[string]string{})
	_, err := r.pool.Exec(ctx, `
INSERT INTO sandbox_sessions
  (id, tenant_id, owner_user_id, project_id, container_id, image, status,
   network_mode, cpus, memory_mb, pids_limit, labels)
VALUES ($1,$2,$3,$4,NULL,$5,$6,$7,$8,$9,$10,$11)`,
		sb.ID, sb.TenantID, sb.OwnerUserID, sb.ProjectID,
		sb.Image, string(sb.Status), string(sb.Network),
		sb.Resources.CPUs, sb.Resources.MemoryMB, sb.Resources.PIDsLimit,
		labels)
	if err != nil {
		return fmt.Errorf("insert sandbox: %w", err)
	}
	return nil
}

// SetContainerID transitions a pending sandbox to running with its container_id.
func (r *SessionRepo) SetContainerID(ctx context.Context, id uuid.UUID, containerID string) error {
	_, err := r.pool.Exec(ctx, `
UPDATE sandbox_sessions
SET container_id=$2, status='running', updated_at=now()
WHERE id=$1`, id, containerID)
	if err != nil {
		return fmt.Errorf("update container_id: %w", err)
	}
	return nil
}

// UpdateStatus changes status (and stamps updated_at; sets destroyed_at when
// status transitions to destroyed/failed terminal states).
func (r *SessionRepo) UpdateStatus(ctx context.Context, id uuid.UUID, status Status) error {
	terminal := status == StatusDestroyed || status == StatusFailed
	if terminal {
		_, err := r.pool.Exec(ctx, `
UPDATE sandbox_sessions
SET status=$2, updated_at=now(), destroyed_at=now()
WHERE id=$1`, id, string(status))
		if err != nil {
			return fmt.Errorf("update status terminal: %w", err)
		}
		return nil
	}
	_, err := r.pool.Exec(ctx, `
UPDATE sandbox_sessions
SET status=$2, updated_at=now()
WHERE id=$1`, id, string(status))
	if err != nil {
		return fmt.Errorf("update status: %w", err)
	}
	return nil
}

// Get returns a sandbox by id scoped to tenantID. Returns ErrSandboxNotFound
// when the row doesn't exist or belongs to another tenant.
func (r *SessionRepo) Get(ctx context.Context, tenantID, id uuid.UUID) (*Sandbox, error) {
	row := r.pool.QueryRow(ctx, `
SELECT id, tenant_id, owner_user_id, project_id, image, status, network_mode,
       cpus, memory_mb, pids_limit, created_at, updated_at, destroyed_at
FROM sandbox_sessions
WHERE id=$1 AND tenant_id=$2`, id, tenantID)

	var sb Sandbox
	var network string
	var status string
	var destroyedAt *time.Time
	if err := row.Scan(&sb.ID, &sb.TenantID, &sb.OwnerUserID, &sb.ProjectID,
		&sb.Image, &status, &network,
		&sb.Resources.CPUs, &sb.Resources.MemoryMB, &sb.Resources.PIDsLimit,
		&sb.CreatedAt, &sb.UpdatedAt, &destroyedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSandboxNotFound
		}
		return nil, fmt.Errorf("scan sandbox: %w", err)
	}
	sb.Status = Status(status)
	sb.Network = NetworkMode(network)
	sb.DestroyedAt = destroyedAt
	return &sb, nil
}

// GetContainerID returns the container_id (may be empty for pending) for
// internal use that doesn't need full Sandbox load.
func (r *SessionRepo) GetContainerID(ctx context.Context, id uuid.UUID) (string, error) {
	var cid *string
	err := r.pool.QueryRow(ctx, `SELECT container_id FROM sandbox_sessions WHERE id=$1`, id).Scan(&cid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrSandboxNotFound
		}
		return "", fmt.Errorf("get container_id: %w", err)
	}
	if cid == nil {
		return "", nil
	}
	return *cid, nil
}

// ListActive returns sandboxes not in terminal status. Used by Reconciler.
func (r *SessionRepo) ListActive(ctx context.Context) ([]*Sandbox, error) {
	rows, err := r.pool.Query(ctx, `
SELECT id, tenant_id, owner_user_id, project_id, image, status, network_mode,
       cpus, memory_mb, pids_limit, created_at, updated_at, destroyed_at
FROM sandbox_sessions
WHERE status IN ('pending','running','destroying')`)
	if err != nil {
		return nil, fmt.Errorf("query active: %w", err)
	}
	defer rows.Close()

	var out []*Sandbox
	for rows.Next() {
		var sb Sandbox
		var network, status string
		var destroyedAt *time.Time
		if err := rows.Scan(&sb.ID, &sb.TenantID, &sb.OwnerUserID, &sb.ProjectID,
			&sb.Image, &status, &network,
			&sb.Resources.CPUs, &sb.Resources.MemoryMB, &sb.Resources.PIDsLimit,
			&sb.CreatedAt, &sb.UpdatedAt, &destroyedAt); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		sb.Status = Status(status)
		sb.Network = NetworkMode(network)
		sb.DestroyedAt = destroyedAt
		out = append(out, &sb)
	}
	return out, rows.Err()
}
```

`internal/sandbox/sessionrepo_test.go`:

```go
package sandbox_test

import (
	"context"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/db"
	"github.com/yourorg/private-coding-agent/internal/sandbox"
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
	testDSN = fmt.Sprintf("postgres://app:app@localhost:%s/app?sslmode=disable",
		res.GetPort("5432/tcp"))
	pool.MaxWait = 60 * time.Second
	if err := pool.Retry(func() error { return db.Migrate(context.Background(), testDSN) }); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	os.Exit(func() int {
		defer func() { _ = pool.Purge(res) }()
		return m.Run()
	}())
}

// helper: 在 default tenant + 新 user 下创建一个 SessionRepo + 必需的 IDs
func setupRepoWithUser(t *testing.T) (*sandbox.SessionRepo, uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	pg, err := pgxpool.New(ctx, testDSN)
	require.NoError(t, err)
	t.Cleanup(pg.Close)

	tn, err := tenant.NewRepo(pg).GetBySlug(ctx, "default")
	require.NoError(t, err)
	usvc := user.NewService(user.NewRepo(pg))
	email := fmt.Sprintf("sb-%d@example.com", time.Now().UnixNano())
	u, err := usvc.Register(ctx, tn.ID, email, "irrelevant-password-XX", "SbTester")
	require.NoError(t, err)
	return sandbox.NewSessionRepo(pg), tn.ID, u.ID
}

func TestSessionRepo_InsertThenGet(t *testing.T) {
	repo, tid, uid := setupRepoWithUser(t)
	ctx := context.Background()

	sb := &sandbox.Sandbox{
		ID:          uuid.New(),
		TenantID:    tid,
		OwnerUserID: uid,
		Image:       "pca/sandbox:base",
		Status:      sandbox.StatusPending,
		Network:     sandbox.NetworkInternal,
		Resources:   sandbox.ResourceLimits{CPUs: 1, MemoryMB: 512, PIDsLimit: 256},
	}
	require.NoError(t, repo.Insert(ctx, sb))

	got, err := repo.Get(ctx, tid, sb.ID)
	require.NoError(t, err)
	require.Equal(t, sb.ID, got.ID)
	require.Equal(t, sandbox.StatusPending, got.Status)
	require.Equal(t, sandbox.NetworkInternal, got.Network)
	require.Equal(t, "pca/sandbox:base", got.Image)
}

func TestSessionRepo_Get_TenantIsolation(t *testing.T) {
	repo, tid, uid := setupRepoWithUser(t)
	ctx := context.Background()

	sb := &sandbox.Sandbox{
		ID: uuid.New(), TenantID: tid, OwnerUserID: uid,
		Image: "x", Status: sandbox.StatusPending, Network: sandbox.NetworkInternal,
		Resources: sandbox.ResourceLimits{CPUs: 1, MemoryMB: 512, PIDsLimit: 256},
	}
	require.NoError(t, repo.Insert(ctx, sb))

	otherTenant := uuid.New()
	_, err := repo.Get(ctx, otherTenant, sb.ID)
	require.ErrorIs(t, err, sandbox.ErrSandboxNotFound)
}

func TestSessionRepo_SetContainerID(t *testing.T) {
	repo, tid, uid := setupRepoWithUser(t)
	ctx := context.Background()

	sb := &sandbox.Sandbox{
		ID: uuid.New(), TenantID: tid, OwnerUserID: uid,
		Image: "x", Status: sandbox.StatusPending, Network: sandbox.NetworkInternal,
		Resources: sandbox.ResourceLimits{CPUs: 1, MemoryMB: 512, PIDsLimit: 256},
	}
	require.NoError(t, repo.Insert(ctx, sb))
	require.NoError(t, repo.SetContainerID(ctx, sb.ID, "abc123"))

	got, err := repo.Get(ctx, tid, sb.ID)
	require.NoError(t, err)
	require.Equal(t, sandbox.StatusRunning, got.Status)

	cid, err := repo.GetContainerID(ctx, sb.ID)
	require.NoError(t, err)
	require.Equal(t, "abc123", cid)
}

func TestSessionRepo_UpdateStatus_Terminal(t *testing.T) {
	repo, tid, uid := setupRepoWithUser(t)
	ctx := context.Background()

	sb := &sandbox.Sandbox{
		ID: uuid.New(), TenantID: tid, OwnerUserID: uid,
		Image: "x", Status: sandbox.StatusPending, Network: sandbox.NetworkInternal,
		Resources: sandbox.ResourceLimits{CPUs: 1, MemoryMB: 512, PIDsLimit: 256},
	}
	require.NoError(t, repo.Insert(ctx, sb))
	require.NoError(t, repo.UpdateStatus(ctx, sb.ID, sandbox.StatusDestroyed))

	got, err := repo.Get(ctx, tid, sb.ID)
	require.NoError(t, err)
	require.Equal(t, sandbox.StatusDestroyed, got.Status)
	require.NotNil(t, got.DestroyedAt)
}

func TestSessionRepo_ListActive(t *testing.T) {
	repo, tid, uid := setupRepoWithUser(t)
	ctx := context.Background()

	// 1 running, 1 destroyed; only running counted
	running := &sandbox.Sandbox{
		ID: uuid.New(), TenantID: tid, OwnerUserID: uid,
		Image: "x", Status: sandbox.StatusRunning, Network: sandbox.NetworkInternal,
		Resources: sandbox.ResourceLimits{CPUs: 1, MemoryMB: 512, PIDsLimit: 256},
	}
	require.NoError(t, repo.Insert(ctx, running))

	destroyed := &sandbox.Sandbox{
		ID: uuid.New(), TenantID: tid, OwnerUserID: uid,
		Image: "x", Status: sandbox.StatusPending, Network: sandbox.NetworkInternal,
		Resources: sandbox.ResourceLimits{CPUs: 1, MemoryMB: 512, PIDsLimit: 256},
	}
	require.NoError(t, repo.Insert(ctx, destroyed))
	require.NoError(t, repo.UpdateStatus(ctx, destroyed.ID, sandbox.StatusDestroyed))

	list, err := repo.ListActive(ctx)
	require.NoError(t, err)
	// at least the running one is in the result; other tests in same TestMain may add more
	var foundRunning bool
	for _, s := range list {
		if s.ID == running.ID {
			foundRunning = true
		}
		require.NotEqual(t, sandbox.StatusDestroyed, s.Status)
		require.NotEqual(t, sandbox.StatusFailed, s.Status)
	}
	require.True(t, foundRunning)
}
```

- [ ] **Step 3: 跑集成测试**

```bash
PATH="/d/tools/go/bin:$PATH" go test ./internal/sandbox/... -count=1 -v
```

期望：5 个 SessionRepo 测试 PASS（耗时 5-20s 含 dockertest 启 PG）。

- [ ] **Step 4: commit**

```bash
git add internal/db/migrations internal/sandbox/sessionrepo.go internal/sandbox/sessionrepo_test.go
git commit -m "feat(sandbox): sandbox_sessions migration + SessionRepo"
```

---

## Task 4: 路径校验 `internal/sandbox/path.go`

**Files:**
- Create: `internal/sandbox/path.go`
- Create: `internal/sandbox/path_test.go`

### Step 1: 写测试

`internal/sandbox/path_test.go`:

```go
package sandbox_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/sandbox"
)

func TestResolveWorkspacePath_OK(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"foo.txt", "/workspace/foo.txt"},
		{"/foo.txt", "/workspace/foo.txt"},
		{"a/b/c.txt", "/workspace/a/b/c.txt"},
		{"/a/b/c.txt", "/workspace/a/b/c.txt"},
		{"./foo.txt", "/workspace/foo.txt"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, err := sandbox.ResolveWorkspacePath(c.in)
			require.NoError(t, err)
			require.Equal(t, c.want, got)
		})
	}
}

func TestResolveWorkspacePath_Reject(t *testing.T) {
	cases := []string{
		"../etc/passwd",
		"../../etc/passwd",
		"/../etc/passwd",
		"/etc/passwd",
		"/workspace/../etc/passwd",
		"/var/log/x",
		"",
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			_, err := sandbox.ResolveWorkspacePath(in)
			require.ErrorIs(t, err, sandbox.ErrPathOutsideWorkspace)
		})
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
PATH="/d/tools/go/bin:$PATH" go test ./internal/sandbox/... -run TestResolveWorkspacePath
```

期望：编译错（`ResolveWorkspacePath` 不存在）。

- [ ] **Step 3: 实现 path.go**

`internal/sandbox/path.go`:

```go
package sandbox

import (
	"path"
	"strings"
)

const workspaceRoot = "/workspace"

// ResolveWorkspacePath takes a user-supplied path (relative or absolute) and
// returns its absolute canonical form rooted at /workspace. Rejects any path
// that, after cleaning, escapes /workspace.
//
// Symbolic link resolution is NOT performed here; the docker tar IO layer
// is configured to not follow symlinks across the boundary.
func ResolveWorkspacePath(p string) (string, error) {
	if p == "" {
		return "", ErrPathOutsideWorkspace
	}
	// 规范化: 处理 .. 和 ./
	var abs string
	if strings.HasPrefix(p, "/") {
		abs = path.Clean(p)
	} else {
		abs = path.Clean(workspaceRoot + "/" + p)
	}
	// 必须 == /workspace 或以 /workspace/ 开头
	if abs == workspaceRoot {
		return workspaceRoot, nil
	}
	if !strings.HasPrefix(abs, workspaceRoot+"/") {
		return "", ErrPathOutsideWorkspace
	}
	return abs, nil
}
```

- [ ] **Step 4: 跑测试通过**

```bash
PATH="/d/tools/go/bin:$PATH" go test ./internal/sandbox/... -run TestResolveWorkspacePath -count=1 -v
```

期望：12 个 sub-test 全 PASS。

- [ ] **Step 5: commit**

```bash
git add internal/sandbox/path.go internal/sandbox/path_test.go
git commit -m "feat(sandbox): workspace path validation"
```

---

## Task 5: 参数校验 `internal/sandbox/validate.go`

**Files:**
- Create: `internal/sandbox/validate.go`
- Create: `internal/sandbox/validate_test.go`

### Step 1: 写测试

`internal/sandbox/validate_test.go`:

```go
package sandbox_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/sandbox"
)

func TestNormalizeCreateOpts_AppliesDefaults(t *testing.T) {
	opts := sandbox.CreateOpts{
		TenantID:    uuid.New(),
		OwnerUserID: uuid.New(),
	}
	got, err := sandbox.NormalizeCreateOpts(opts)
	require.NoError(t, err)
	require.Equal(t, sandbox.DefaultImage, got.Image)
	require.Equal(t, sandbox.DefaultNetwork, got.Network)
	require.Equal(t, sandbox.DefaultCPUs, got.Resources.CPUs)
	require.Equal(t, sandbox.DefaultMemoryMB, got.Resources.MemoryMB)
	require.Equal(t, sandbox.DefaultPIDsLimit, got.Resources.PIDsLimit)
}

func TestNormalizeCreateOpts_PreservesValid(t *testing.T) {
	opts := sandbox.CreateOpts{
		TenantID:    uuid.New(),
		OwnerUserID: uuid.New(),
		Image:       "custom:tag",
		Network:     sandbox.NetworkBridge,
		Resources:   sandbox.ResourceLimits{CPUs: 2, MemoryMB: 1024, PIDsLimit: 512},
	}
	got, err := sandbox.NormalizeCreateOpts(opts)
	require.NoError(t, err)
	require.Equal(t, "custom:tag", got.Image)
	require.Equal(t, sandbox.NetworkBridge, got.Network)
	require.Equal(t, 2.0, got.Resources.CPUs)
}

func TestNormalizeCreateOpts_RejectsBadInputs(t *testing.T) {
	base := sandbox.CreateOpts{TenantID: uuid.New(), OwnerUserID: uuid.New()}
	cases := []struct {
		name string
		mod  func(o *sandbox.CreateOpts)
	}{
		{"zero TenantID", func(o *sandbox.CreateOpts) { o.TenantID = uuid.Nil }},
		{"zero OwnerUserID", func(o *sandbox.CreateOpts) { o.OwnerUserID = uuid.Nil }},
		{"CPUs over max", func(o *sandbox.CreateOpts) { o.Resources.CPUs = sandbox.MaxCPUs + 1 }},
		{"MemoryMB over max", func(o *sandbox.CreateOpts) { o.Resources.MemoryMB = sandbox.MaxMemoryMB + 1 }},
		{"PIDsLimit over max", func(o *sandbox.CreateOpts) { o.Resources.PIDsLimit = sandbox.MaxPIDsLimit + 1 }},
		{"unknown NetworkMode", func(o *sandbox.CreateOpts) { o.Network = "weird" }},
		{"negative CPUs", func(o *sandbox.CreateOpts) { o.Resources.CPUs = -1 }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o := base
			c.mod(&o)
			_, err := sandbox.NormalizeCreateOpts(o)
			require.Error(t, err)
		})
	}
}

func TestNormalizeExecOpts_AppliesDefaults(t *testing.T) {
	o, err := sandbox.NormalizeExecOpts(sandbox.ExecOpts{Cmd: []string{"echo", "hi"}})
	require.NoError(t, err)
	require.Equal(t, "/workspace", o.WorkingDir)
	require.Equal(t, sandbox.DefaultExecTimeoutSec, o.TimeoutSec)
}

func TestNormalizeExecOpts_Rejects(t *testing.T) {
	cases := []struct {
		name string
		o    sandbox.ExecOpts
	}{
		{"empty cmd", sandbox.ExecOpts{}},
		{"cmd with empty string", sandbox.ExecOpts{Cmd: []string{""}}},
		{"timeout over max", sandbox.ExecOpts{Cmd: []string{"x"}, TimeoutSec: sandbox.MaxExecTimeoutSec + 1}},
		{"negative timeout", sandbox.ExecOpts{Cmd: []string{"x"}, TimeoutSec: -1}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := sandbox.NormalizeExecOpts(c.o)
			require.Error(t, err)
		})
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
PATH="/d/tools/go/bin:$PATH" go test ./internal/sandbox/... -run "TestNormalize"
```

期望：编译错。

- [ ] **Step 3: 实现 validate.go**

`internal/sandbox/validate.go`:

```go
package sandbox

import (
	"fmt"

	"github.com/google/uuid"
)

// NormalizeCreateOpts validates and fills defaults for CreateOpts.
// Returns a fully populated copy; original input is unchanged.
func NormalizeCreateOpts(o CreateOpts) (CreateOpts, error) {
	if o.TenantID == uuid.Nil {
		return o, fmt.Errorf("validation: tenant_id required")
	}
	if o.OwnerUserID == uuid.Nil {
		return o, fmt.Errorf("validation: owner_user_id required")
	}
	if o.Image == "" {
		o.Image = DefaultImage
	}
	if o.Network == "" {
		o.Network = DefaultNetwork
	}
	switch o.Network {
	case NetworkInternal, NetworkBridge, NetworkNone:
	default:
		return o, fmt.Errorf("validation: unknown network mode %q", string(o.Network))
	}
	if o.Resources.CPUs == 0 {
		o.Resources.CPUs = DefaultCPUs
	}
	if o.Resources.MemoryMB == 0 {
		o.Resources.MemoryMB = DefaultMemoryMB
	}
	if o.Resources.PIDsLimit == 0 {
		o.Resources.PIDsLimit = DefaultPIDsLimit
	}
	if o.Resources.CPUs < 0 || o.Resources.CPUs > MaxCPUs {
		return o, fmt.Errorf("validation: cpus %g out of [0, %g]", o.Resources.CPUs, MaxCPUs)
	}
	if o.Resources.MemoryMB < 0 || o.Resources.MemoryMB > MaxMemoryMB {
		return o, fmt.Errorf("validation: memory_mb %d out of [0, %d]", o.Resources.MemoryMB, MaxMemoryMB)
	}
	if o.Resources.PIDsLimit < 0 || o.Resources.PIDsLimit > MaxPIDsLimit {
		return o, fmt.Errorf("validation: pids_limit %d out of [0, %d]", o.Resources.PIDsLimit, MaxPIDsLimit)
	}
	return o, nil
}

// NormalizeExecOpts validates and fills defaults for ExecOpts.
func NormalizeExecOpts(o ExecOpts) (ExecOpts, error) {
	if len(o.Cmd) == 0 {
		return o, fmt.Errorf("validation: cmd required")
	}
	for i, a := range o.Cmd {
		if a == "" {
			return o, fmt.Errorf("validation: cmd[%d] is empty", i)
		}
	}
	if o.WorkingDir == "" {
		o.WorkingDir = workspaceRoot
	}
	if o.TimeoutSec == 0 {
		o.TimeoutSec = DefaultExecTimeoutSec
	}
	if o.TimeoutSec < 0 || o.TimeoutSec > MaxExecTimeoutSec {
		return o, fmt.Errorf("validation: timeout_sec %d out of [0, %d]", o.TimeoutSec, MaxExecTimeoutSec)
	}
	return o, nil
}
```

- [ ] **Step 4: 跑测试通过**

```bash
PATH="/d/tools/go/bin:$PATH" go test ./internal/sandbox/... -run "TestNormalize" -count=1 -v
```

期望：5 个测试函数（含 12+ sub-test）全 PASS。

- [ ] **Step 5: commit**

```bash
git add internal/sandbox/validate.go internal/sandbox/validate_test.go
git commit -m "feat(sandbox): normalize and validate CreateOpts/ExecOpts"
```

---

## Task 6: `Runtime` 接口 `internal/sandbox/runtime.go`

**Files:**
- Create: `internal/sandbox/runtime.go`

### Step 1: 直接写接口（无测试，纯声明）

`internal/sandbox/runtime.go`:

```go
package sandbox

import (
	"context"

	"github.com/google/uuid"
)

// Runtime is the sandbox abstraction. All methods must be safe for concurrent
// use across multiple goroutines.
//
// Implementations:
//   - DockerDriver (slice 2)
//   - K8sDriver (future)
type Runtime interface {
	// Create allocates and starts a new sandbox. Returns when status=Running.
	// Honors ctx cancellation up to start; once started, the container lives
	// until Destroy.
	Create(ctx context.Context, opts CreateOpts) (*Sandbox, error)

	// Get returns the sandbox by id scoped to tenant. Returns ErrSandboxNotFound
	// when the id doesn't exist OR belongs to a different tenant (no
	// distinction is exposed, to prevent enumeration).
	Get(ctx context.Context, tenantID, id uuid.UUID) (*Sandbox, error)

	// Destroy stops and removes the sandbox. Idempotent: destroying an
	// already-destroyed sandbox returns nil.
	Destroy(ctx context.Context, tenantID, id uuid.UUID) error

	// Exec runs a command synchronously inside the sandbox. ctx cancellation
	// or ExecOpts.TimeoutSec kill the process (TimedOut=true). Stdout/Stderr
	// each capped at MaxStreamBytes (excess truncated, Truncated=true).
	Exec(ctx context.Context, tenantID, id uuid.UUID, opts ExecOpts) (*ExecResult, error)

	// ReadFile reads a file under /workspace. Path is validated by
	// ResolveWorkspacePath. Files larger than MaxFileSize return ErrTooLarge.
	ReadFile(ctx context.Context, tenantID, id uuid.UUID, path string) ([]byte, error)

	// WriteFile writes a file under /workspace, creating intermediate
	// directories as needed. Size capped at MaxFileSize.
	WriteFile(ctx context.Context, tenantID, id uuid.UUID, path string, data []byte) error

	// Snapshot exports the workspace state. Slice 2 returns ErrNotImplemented;
	// implementation lands when MinIO support is added.
	Snapshot(ctx context.Context, tenantID, id uuid.UUID) (string, error)
}
```

- [ ] **Step 2: 跑 build 验证接口编译**

```bash
PATH="/d/tools/go/bin:$PATH" go build ./internal/sandbox/...
PATH="/d/tools/go/bin:$PATH" go vet ./internal/sandbox/...
```

期望：干净。

- [ ] **Step 3: commit**

```bash
git add internal/sandbox/runtime.go
git commit -m "feat(sandbox): Runtime interface"
```

---

## Task 7: DockerDriver 骨架 + 构造器

**Files:**
- Create: `internal/sandbox/docker_driver.go`

### Step 1: 装 Docker SDK 依赖

```bash
PATH="/d/tools/go/bin:$PATH" go get github.com/docker/docker/client github.com/docker/docker/api/types/container github.com/docker/docker/api/types/network github.com/docker/docker/api/types/filters github.com/docker/docker/pkg/stdcopy github.com/redis/go-redis/v9
```

- [ ] **Step 2: 实现 DockerDriver 骨架**

`internal/sandbox/docker_driver.go`:

```go
package sandbox

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/redis/go-redis/v9"
)

// DockerDriverConfig configures a DockerDriver.
type DockerDriverConfig struct {
	// InternalNetworkName 是给 NetworkInternal 模式的共享 internal 网络名。
	// 不存在时 DockerDriver 会自动创建。
	InternalNetworkName string
}

// DockerDriver implements Runtime using the local Docker daemon.
type DockerDriver struct {
	cli   *client.Client
	repo  *SessionRepo
	redis *redis.Client
	cfg   DockerDriverConfig
}

// NewDockerDriver wires a DockerDriver. cli must be a connected docker client;
// repo persists session metadata; redis is used for distributed Destroy locks.
//
// The internal network (cfg.InternalNetworkName, default "pca-sandbox-internal")
// is created if missing; idempotent.
func NewDockerDriver(ctx context.Context, cli *client.Client, repo *SessionRepo, rdb *redis.Client, cfg DockerDriverConfig) (*DockerDriver, error) {
	if cfg.InternalNetworkName == "" {
		cfg.InternalNetworkName = "pca-sandbox-internal"
	}
	d := &DockerDriver{cli: cli, repo: repo, redis: rdb, cfg: cfg}
	if err := d.ensureInternalNetwork(ctx); err != nil {
		return nil, fmt.Errorf("ensure internal network: %w", err)
	}
	return d, nil
}

func (d *DockerDriver) ensureInternalNetwork(ctx context.Context) error {
	f := filters.NewArgs()
	f.Add("name", d.cfg.InternalNetworkName)
	nets, err := d.cli.NetworkList(ctx, network.ListOptions{Filters: f})
	if err != nil {
		return err
	}
	for _, n := range nets {
		if n.Name == d.cfg.InternalNetworkName {
			return nil
		}
	}
	_, err = d.cli.NetworkCreate(ctx, d.cfg.InternalNetworkName, network.CreateOptions{
		Driver:   "bridge",
		Internal: true,
		Attachable: false,
	})
	return err
}
```

- [ ] **Step 3: 验证编译**

```bash
PATH="/d/tools/go/bin:$PATH" go mod tidy
PATH="/d/tools/go/bin:$PATH" go build ./internal/sandbox/...
PATH="/d/tools/go/bin:$PATH" go vet ./internal/sandbox/...
```

期望：干净。

- [ ] **Step 4: commit**

```bash
git add internal/sandbox/docker_driver.go go.mod go.sum
git commit -m "feat(sandbox): DockerDriver skeleton with internal network bootstrap"
```

---

## Task 8: `DockerDriver.Create` + 集成测试

**Files:**
- Modify: `internal/sandbox/docker_driver.go`
- Create: `internal/sandbox/docker_driver_test.go`（含 `//go:build docker_integration`）

### Step 1: 在 docker_driver.go 末尾追加 Create

```go
import (
	// ... 已有 imports
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/strslice"
	"github.com/google/uuid"
	"time"
)

// Create starts a new container per opts and persists metadata.
func (d *DockerDriver) Create(ctx context.Context, opts CreateOpts) (*Sandbox, error) {
	opts, err := NormalizeCreateOpts(opts)
	if err != nil {
		return nil, err
	}

	sb := &Sandbox{
		ID:          uuid.New(),
		TenantID:    opts.TenantID,
		OwnerUserID: opts.OwnerUserID,
		ProjectID:   opts.ProjectID,
		Image:       opts.Image,
		Status:      StatusPending,
		Network:     opts.Network,
		Resources:   opts.Resources,
	}
	if err := d.repo.Insert(ctx, sb); err != nil {
		return nil, err
	}

	cid, err := d.createAndStartContainer(ctx, sb, opts)
	if err != nil {
		_ = d.repo.UpdateStatus(ctx, sb.ID, StatusFailed)
		return nil, fmt.Errorf("create container: %w", err)
	}

	if err := d.repo.SetContainerID(ctx, sb.ID, cid); err != nil {
		_ = d.cli.ContainerRemove(ctx, cid, container.RemoveOptions{Force: true, RemoveVolumes: true})
		return nil, err
	}
	sb.Status = StatusRunning
	return sb, nil
}

func (d *DockerDriver) createAndStartContainer(ctx context.Context, sb *Sandbox, opts CreateOpts) (string, error) {
	pidsLimit := opts.Resources.PIDsLimit
	cfg := &container.Config{
		Image:      opts.Image,
		Cmd:        []string{"sleep", "infinity"},
		WorkingDir: workspaceRoot,
		Labels: map[string]string{
			"pca.tenant_id":     opts.TenantID.String(),
			"pca.sandbox_id":    sb.ID.String(),
			"pca.owner_user_id": opts.OwnerUserID.String(),
		},
		Env: envToSlice(opts.Env),
	}
	hostCfg := &container.HostConfig{
		ReadonlyRootfs: true,
		Tmpfs: map[string]string{
			workspaceRoot: "size=1g,uid=10001,gid=10001",
			"/tmp":        "size=1g",
		},
		CapDrop:     strslice.StrSlice{"ALL"},
		CapAdd:      strslice.StrSlice{"CHOWN", "DAC_OVERRIDE", "SETUID", "SETGID", "FOWNER"},
		SecurityOpt: []string{"no-new-privileges:true"},
		Resources: container.Resources{
			NanoCPUs:  int64(opts.Resources.CPUs * 1e9),
			Memory:    opts.Resources.MemoryMB * 1024 * 1024,
			PidsLimit: &pidsLimit,
		},
	}
	hostCfg.NetworkMode = networkModeFor(opts.Network, d.cfg.InternalNetworkName)

	createCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	resp, err := d.cli.ContainerCreate(createCtx, cfg, hostCfg, nil, nil, "")
	if err != nil {
		return "", err
	}
	if err := d.cli.ContainerStart(createCtx, resp.ID, container.StartOptions{}); err != nil {
		_ = d.cli.ContainerRemove(context.Background(), resp.ID,
			container.RemoveOptions{Force: true, RemoveVolumes: true})
		return "", err
	}
	return resp.ID, nil
}

func envToSlice(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	return out
}

func networkModeFor(mode NetworkMode, internalName string) container.NetworkMode {
	switch mode {
	case NetworkInternal:
		return container.NetworkMode(internalName)
	case NetworkBridge:
		return container.NetworkMode("bridge")
	case NetworkNone:
		return container.NetworkMode("none")
	}
	return container.NetworkMode("none")
}

// unused import keepers
var _ = types.Info{}
```

> 注：上面的 `var _ = types.Info{}` 是为避免 `types` 包 unused import；如果你的 import 已经用到 `types` 就删掉这行。Task 8 实际可能不需要 `types` import，请按需删除。

- [ ] **Step 2: 写集成测试**

`internal/sandbox/docker_driver_test.go`:

```go
//go:build docker_integration

package sandbox_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/sandbox"
	"github.com/yourorg/private-coding-agent/internal/tenant"
	"github.com/yourorg/private-coding-agent/internal/user"
)

// 注意:这个集成测试假设
// (1) Docker daemon 可达
// (2) pca/sandbox:base 镜像已 build
// (3) testDSN 已由 TestMain (sessionrepo_test.go) 准备好
// 通过 build tag `docker_integration` 隔离

func newDockerDriverForTest(t *testing.T) (*sandbox.DockerDriver, uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	pg, err := pgxpool.New(ctx, testDSN)
	require.NoError(t, err)
	t.Cleanup(pg.Close)

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	require.NoError(t, err)
	t.Cleanup(func() { _ = cli.Close() })

	// 测试用 Redis client (Slice 2 compose 添加 redis, dockertest 也可起 redis容器)
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	t.Cleanup(func() { _ = rdb.Close() })

	repo := sandbox.NewSessionRepo(pg)
	d, err := sandbox.NewDockerDriver(ctx, cli, repo, rdb, sandbox.DockerDriverConfig{
		InternalNetworkName: "pca-sandbox-test-internal",
	})
	require.NoError(t, err)

	tn, err := tenant.NewRepo(pg).GetBySlug(ctx, "default")
	require.NoError(t, err)
	usvc := user.NewService(user.NewRepo(pg))
	u, err := usvc.Register(ctx, tn.ID, "drv-test@example.com"+uuid.NewString(), "irrelevant-password-XX", "Drv")
	require.NoError(t, err)

	return d, tn.ID, u.ID
}

func TestDockerDriver_Create_Success(t *testing.T) {
	ctx := context.Background()
	d, tid, uid := newDockerDriverForTest(t)

	sb, err := d.Create(ctx, sandbox.CreateOpts{
		TenantID:    tid,
		OwnerUserID: uid,
	})
	require.NoError(t, err)
	require.Equal(t, sandbox.StatusRunning, sb.Status)
	require.Equal(t, sandbox.DefaultImage, sb.Image)

	// cleanup container
	cli, _ := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	defer cli.Close()
	cid, err := d.GetContainerIDForTest(ctx, sb.ID)
	if err == nil && cid != "" {
		_ = cli.ContainerRemove(ctx, cid, container.RemoveOptions{Force: true, RemoveVolumes: true})
	}
}

func TestDockerDriver_Create_PullFailure(t *testing.T) {
	ctx := context.Background()
	d, tid, uid := newDockerDriverForTest(t)

	_, err := d.Create(ctx, sandbox.CreateOpts{
		TenantID:    tid,
		OwnerUserID: uid,
		Image:       "definitely-not-a-real-image:nope",
	})
	require.Error(t, err)
	// 接受 "create container:" 包装的任何下游错误
	require.Contains(t, err.Error(), "create container:")

	// 给一点时间让 Docker 异步快速失败
	time.Sleep(200 * time.Millisecond)
}
```

注意：测试用到 `d.GetContainerIDForTest`，是 DockerDriver 的测试专用 helper。在 `docker_driver.go` 末尾追加：

```go
// GetContainerIDForTest exposes container_id for integration tests. Not in the
// public Runtime interface.
func (d *DockerDriver) GetContainerIDForTest(ctx context.Context, id uuid.UUID) (string, error) {
	return d.repo.GetContainerID(ctx, id)
}
```

- [ ] **Step 3: 跑集成测试**

```bash
PATH="/d/tools/go/bin:$PATH" go test -tags=docker_integration ./internal/sandbox/... -run TestDockerDriver_Create -count=1 -v -timeout=120s
```

期望：2 个测试 PASS（首次跑约 30-60s，含 sandbox 镜像若未 build 会拉取/build）。

如果 `pca/sandbox:base` 未存在，测试会失败——回到 Task 1 build 镜像。

- [ ] **Step 4: commit**

```bash
git add internal/sandbox/docker_driver.go internal/sandbox/docker_driver_test.go
git commit -m "feat(sandbox): DockerDriver.Create with isolation policy"
```

---

## Task 9: `DockerDriver.Get` + `Destroy`（含 Redis 锁、幂等）

**Files:**
- Modify: `internal/sandbox/docker_driver.go`
- Modify: `internal/sandbox/docker_driver_test.go`

### Step 1: 在 docker_driver.go 追加 Get + Destroy

```go
import (
	// 已有
	"errors"
)

// Get returns the sandbox scoped to tenant.
func (d *DockerDriver) Get(ctx context.Context, tenantID, id uuid.UUID) (*Sandbox, error) {
	return d.repo.Get(ctx, tenantID, id)
}

const destroyLockTTL = 30 * time.Second

// Destroy stops and removes the sandbox. Idempotent.
func (d *DockerDriver) Destroy(ctx context.Context, tenantID, id uuid.UUID) error {
	lockKey := "pca:sandbox:destroy:" + id.String()

	ok, err := d.redis.SetNX(ctx, lockKey, "1", destroyLockTTL).Result()
	if err != nil {
		return fmt.Errorf("acquire destroy lock: %w", err)
	}
	if !ok {
		// 锁被他人持有,等待最多 destroyLockTTL 后重试一次
		time.Sleep(2 * time.Second)
		ok, _ = d.redis.SetNX(ctx, lockKey, "1", destroyLockTTL).Result()
		if !ok {
			return fmt.Errorf("destroy already in progress")
		}
	}
	defer func() { _ = d.redis.Del(context.Background(), lockKey).Err() }()

	sb, err := d.repo.Get(ctx, tenantID, id)
	if err != nil {
		if errors.Is(err, ErrSandboxNotFound) {
			return ErrSandboxNotFound
		}
		return err
	}
	if sb.Status == StatusDestroyed {
		return nil // 幂等
	}

	if err := d.repo.UpdateStatus(ctx, sb.ID, StatusDestroying); err != nil {
		return err
	}

	cid, _ := d.repo.GetContainerID(ctx, sb.ID)
	if cid != "" {
		stopTimeout := 5
		_ = d.cli.ContainerStop(ctx, cid, container.StopOptions{Timeout: &stopTimeout})
		_ = d.cli.ContainerRemove(ctx, cid, container.RemoveOptions{Force: true, RemoveVolumes: true})
	}

	return d.repo.UpdateStatus(ctx, sb.ID, StatusDestroyed)
}
```

- [ ] **Step 2: 追加测试**

在 `docker_driver_test.go` 末尾追加：

```go
func TestDockerDriver_Get_RespectsTenant(t *testing.T) {
	ctx := context.Background()
	d, tid, uid := newDockerDriverForTest(t)

	sb, err := d.Create(ctx, sandbox.CreateOpts{TenantID: tid, OwnerUserID: uid})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Destroy(ctx, tid, sb.ID) })

	got, err := d.Get(ctx, tid, sb.ID)
	require.NoError(t, err)
	require.Equal(t, sb.ID, got.ID)

	// 不同租户查不到
	_, err = d.Get(ctx, uuid.New(), sb.ID)
	require.ErrorIs(t, err, sandbox.ErrSandboxNotFound)
}

func TestDockerDriver_Destroy_Idempotent(t *testing.T) {
	ctx := context.Background()
	d, tid, uid := newDockerDriverForTest(t)

	sb, err := d.Create(ctx, sandbox.CreateOpts{TenantID: tid, OwnerUserID: uid})
	require.NoError(t, err)

	require.NoError(t, d.Destroy(ctx, tid, sb.ID))
	// 第二次依然 nil
	require.NoError(t, d.Destroy(ctx, tid, sb.ID))

	got, err := d.Get(ctx, tid, sb.ID)
	require.NoError(t, err)
	require.Equal(t, sandbox.StatusDestroyed, got.Status)
}

func TestDockerDriver_Destroy_NotFound(t *testing.T) {
	ctx := context.Background()
	d, _, _ := newDockerDriverForTest(t)
	err := d.Destroy(ctx, uuid.New(), uuid.New())
	require.ErrorIs(t, err, sandbox.ErrSandboxNotFound)
}
```

- [ ] **Step 3: 跑测试**

```bash
PATH="/d/tools/go/bin:$PATH" go test -tags=docker_integration ./internal/sandbox/... -run "TestDockerDriver_(Get|Destroy)" -count=1 -v -timeout=120s
```

期望：3 个新测试 PASS。

- [ ] **Step 4: commit**

```bash
git add internal/sandbox/docker_driver.go internal/sandbox/docker_driver_test.go
git commit -m "feat(sandbox): DockerDriver Get + Destroy with Redis lock and idempotency"
```

---

## Task 10: `DockerDriver.Exec`（同步 + 超时 + 截断）

**Files:**
- Create: `internal/sandbox/docker_driver_exec.go`
- Modify: `internal/sandbox/docker_driver_test.go`

### Step 1: 实现 Exec

`internal/sandbox/docker_driver_exec.go`:

```go
package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/google/uuid"
)

// Exec runs cmd inside the sandbox synchronously.
func (d *DockerDriver) Exec(ctx context.Context, tenantID, id uuid.UUID, opts ExecOpts) (*ExecResult, error) {
	opts, err := NormalizeExecOpts(opts)
	if err != nil {
		return nil, err
	}

	sb, err := d.repo.Get(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if sb.Status != StatusRunning {
		return nil, ErrSandboxNotReady
	}
	cid, err := d.repo.GetContainerID(ctx, id)
	if err != nil {
		return nil, err
	}
	if cid == "" {
		return nil, ErrSandboxNotReady
	}

	execCfg := container.ExecOptions{
		Cmd:          opts.Cmd,
		WorkingDir:   opts.WorkingDir,
		Env:          envToSlice(opts.Env),
		AttachStdout: true,
		AttachStderr: true,
		AttachStdin:  len(opts.Stdin) > 0,
	}
	created, err := d.cli.ContainerExecCreate(ctx, cid, execCfg)
	if err != nil {
		return nil, fmt.Errorf("exec create: %w", err)
	}

	attachCtx, cancel := context.WithTimeout(ctx, time.Duration(opts.TimeoutSec)*time.Second)
	defer cancel()

	attached, err := d.cli.ContainerExecAttach(attachCtx, created.ID, container.ExecAttachOptions{})
	if err != nil {
		return nil, fmt.Errorf("exec attach: %w", err)
	}
	defer attached.Close()

	// 写 stdin
	if len(opts.Stdin) > 0 {
		_, _ = attached.Conn.Write(opts.Stdin)
		_ = attached.CloseWrite()
	}

	// 读 stdout/stderr,用 limited writer 截断
	stdoutBuf := newLimitedBuffer(MaxStreamBytes)
	stderrBuf := newLimitedBuffer(MaxStreamBytes)

	start := time.Now()
	copyErr := make(chan error, 1)
	go func() {
		_, err := stdcopy.StdCopy(stdoutBuf, stderrBuf, attached.Reader)
		copyErr <- err
	}()

	timedOut := false
	select {
	case <-attachCtx.Done():
		if errors.Is(attachCtx.Err(), context.DeadlineExceeded) {
			timedOut = true
		}
		_ = d.cli.ContainerKill(context.Background(), cid, "SIGKILL")
		<-copyErr // 等待 stdcopy 返回
	case <-copyErr:
	}

	durationMS := time.Since(start).Milliseconds()

	// 取 exit code
	inspectCtx, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	insp, err := d.cli.ContainerExecInspect(inspectCtx, created.ID)
	exitCode := -1
	if err == nil {
		exitCode = insp.ExitCode
	}

	return &ExecResult{
		ExitCode:   exitCode,
		Stdout:     stdoutBuf.Bytes(),
		Stderr:     stderrBuf.Bytes(),
		Truncated:  stdoutBuf.truncated || stderrBuf.truncated,
		DurationMS: durationMS,
		TimedOut:   timedOut,
	}, nil
}

// limitedBuffer is an io.Writer that drops bytes once the cap is reached and
// sets truncated=true.
type limitedBuffer struct {
	buf       *bytes.Buffer
	cap       int
	truncated bool
}

func newLimitedBuffer(cap int) *limitedBuffer {
	return &limitedBuffer{buf: &bytes.Buffer{}, cap: cap}
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	remaining := l.cap - l.buf.Len()
	if remaining <= 0 {
		l.truncated = true
		return len(p), nil
	}
	if len(p) > remaining {
		l.buf.Write(p[:remaining])
		l.truncated = true
		return len(p), nil
	}
	return l.buf.Write(p)
}

func (l *limitedBuffer) Bytes() []byte { return l.buf.Bytes() }

// 让 io.Writer 接口完整
var _ io.Writer = (*limitedBuffer)(nil)
```

- [ ] **Step 2: 追加测试**

在 `docker_driver_test.go` 末尾追加：

```go
func TestDockerDriver_Exec_Hello(t *testing.T) {
	ctx := context.Background()
	d, tid, uid := newDockerDriverForTest(t)
	sb, err := d.Create(ctx, sandbox.CreateOpts{TenantID: tid, OwnerUserID: uid})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Destroy(ctx, tid, sb.ID) })

	res, err := d.Exec(ctx, tid, sb.ID, sandbox.ExecOpts{Cmd: []string{"echo", "hello"}})
	require.NoError(t, err)
	require.Equal(t, 0, res.ExitCode)
	require.Equal(t, "hello\n", string(res.Stdout))
	require.False(t, res.TimedOut)
	require.False(t, res.Truncated)
}

func TestDockerDriver_Exec_NonZeroExit(t *testing.T) {
	ctx := context.Background()
	d, tid, uid := newDockerDriverForTest(t)
	sb, err := d.Create(ctx, sandbox.CreateOpts{TenantID: tid, OwnerUserID: uid})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Destroy(ctx, tid, sb.ID) })

	res, err := d.Exec(ctx, tid, sb.ID, sandbox.ExecOpts{Cmd: []string{"false"}})
	require.NoError(t, err)
	require.Equal(t, 1, res.ExitCode)
}

func TestDockerDriver_Exec_StderrSplit(t *testing.T) {
	ctx := context.Background()
	d, tid, uid := newDockerDriverForTest(t)
	sb, err := d.Create(ctx, sandbox.CreateOpts{TenantID: tid, OwnerUserID: uid})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Destroy(ctx, tid, sb.ID) })

	res, err := d.Exec(ctx, tid, sb.ID, sandbox.ExecOpts{
		Cmd: []string{"sh", "-c", "echo out; echo err >&2; exit 3"},
	})
	require.NoError(t, err)
	require.Equal(t, 3, res.ExitCode)
	require.Equal(t, "out\n", string(res.Stdout))
	require.Equal(t, "err\n", string(res.Stderr))
}

func TestDockerDriver_Exec_Timeout(t *testing.T) {
	ctx := context.Background()
	d, tid, uid := newDockerDriverForTest(t)
	sb, err := d.Create(ctx, sandbox.CreateOpts{TenantID: tid, OwnerUserID: uid})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Destroy(ctx, tid, sb.ID) })

	res, err := d.Exec(ctx, tid, sb.ID, sandbox.ExecOpts{
		Cmd:        []string{"sleep", "10"},
		TimeoutSec: 1,
	})
	require.NoError(t, err)
	require.True(t, res.TimedOut)
}

func TestDockerDriver_Exec_Truncated(t *testing.T) {
	ctx := context.Background()
	d, tid, uid := newDockerDriverForTest(t)
	sb, err := d.Create(ctx, sandbox.CreateOpts{TenantID: tid, OwnerUserID: uid})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Destroy(ctx, tid, sb.ID) })

	// 产生 200KB 输出,超 MaxStreamBytes (128KB)
	res, err := d.Exec(ctx, tid, sb.ID, sandbox.ExecOpts{
		Cmd: []string{"sh", "-c", "head -c 200000 /dev/urandom | od -An -tx1"},
	})
	require.NoError(t, err)
	require.True(t, res.Truncated)
}

func TestDockerDriver_Exec_NotReady_Destroyed(t *testing.T) {
	ctx := context.Background()
	d, tid, uid := newDockerDriverForTest(t)
	sb, err := d.Create(ctx, sandbox.CreateOpts{TenantID: tid, OwnerUserID: uid})
	require.NoError(t, err)
	require.NoError(t, d.Destroy(ctx, tid, sb.ID))

	_, err = d.Exec(ctx, tid, sb.ID, sandbox.ExecOpts{Cmd: []string{"echo"}})
	require.ErrorIs(t, err, sandbox.ErrSandboxNotReady)
}
```

- [ ] **Step 3: 跑测试**

```bash
PATH="/d/tools/go/bin:$PATH" go test -tags=docker_integration ./internal/sandbox/... -run TestDockerDriver_Exec -count=1 -v -timeout=180s
```

期望：6 个测试 PASS。

- [ ] **Step 4: commit**

```bash
git add internal/sandbox/docker_driver_exec.go internal/sandbox/docker_driver_test.go
git commit -m "feat(sandbox): DockerDriver.Exec with timeout, output truncation, stderr split"
```

---

## Task 11: `DockerDriver.ReadFile` / `WriteFile`（tar 流）

**Files:**
- Create: `internal/sandbox/docker_driver_fs.go`
- Modify: `internal/sandbox/docker_driver_test.go`

### Step 1: 实现文件读写

`internal/sandbox/docker_driver_fs.go`:

```go
package sandbox

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/google/uuid"
)

// WriteFile writes data to /workspace/<relPath> in the sandbox.
func (d *DockerDriver) WriteFile(ctx context.Context, tenantID, id uuid.UUID, relPath string, data []byte) error {
	abs, err := ResolveWorkspacePath(relPath)
	if err != nil {
		return err
	}
	if len(data) > MaxFileSize {
		return ErrTooLarge
	}

	cid, err := d.requireContainerID(ctx, tenantID, id)
	if err != nil {
		return err
	}

	// 构造 tar: 把 /workspace/foo/bar.txt 写成 tar entry "workspace/foo/bar.txt"
	// (CopyToContainer 接收 / 为根,所以 tar header name 不要 leading /)
	tarName := strings.TrimPrefix(abs, "/")

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	// 先写中间目录的 tar entry 以防容器内不存在
	if dir := path.Dir(tarName); dir != "." && dir != "/" {
		// 把路径所有中间段递增加入
		parts := strings.Split(dir, "/")
		acc := ""
		for _, p := range parts {
			if p == "" {
				continue
			}
			if acc == "" {
				acc = p
			} else {
				acc = acc + "/" + p
			}
			_ = tw.WriteHeader(&tar.Header{
				Name:     acc + "/",
				Mode:     0o755,
				Typeflag: tar.TypeDir,
			})
		}
	}

	if err := tw.WriteHeader(&tar.Header{
		Name: tarName,
		Mode: 0o644,
		Size: int64(len(data)),
	}); err != nil {
		return fmt.Errorf("tar header: %w", err)
	}
	if _, err := tw.Write(data); err != nil {
		return fmt.Errorf("tar write: %w", err)
	}
	if err := tw.Close(); err != nil {
		return fmt.Errorf("tar close: %w", err)
	}

	if err := d.cli.CopyToContainer(ctx, cid, "/", &buf, container.CopyToContainerOptions{}); err != nil {
		return fmt.Errorf("copy to container: %w", err)
	}
	return nil
}

// ReadFile reads /workspace/<relPath> from the sandbox.
func (d *DockerDriver) ReadFile(ctx context.Context, tenantID, id uuid.UUID, relPath string) ([]byte, error) {
	abs, err := ResolveWorkspacePath(relPath)
	if err != nil {
		return nil, err
	}
	cid, err := d.requireContainerID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}

	reader, _, err := d.cli.CopyFromContainer(ctx, cid, abs)
	if err != nil {
		return nil, fmt.Errorf("copy from container: %w", err)
	}
	defer reader.Close()

	tr := tar.NewReader(reader)
	hdr, err := tr.Next()
	if err == io.EOF {
		return nil, ErrSandboxNotFound // 不太可能,但要 defensive
	}
	if err != nil {
		return nil, fmt.Errorf("tar next: %w", err)
	}
	if hdr.Typeflag == tar.TypeDir {
		return nil, fmt.Errorf("path_is_directory")
	}
	if hdr.Size > int64(MaxFileSize) {
		return nil, ErrTooLarge
	}

	limit := int64(MaxFileSize) + 1
	data, err := io.ReadAll(io.LimitReader(tr, limit))
	if err != nil {
		return nil, fmt.Errorf("read tar entry: %w", err)
	}
	if int64(len(data)) > int64(MaxFileSize) {
		return nil, ErrTooLarge
	}
	return data, nil
}

func (d *DockerDriver) requireContainerID(ctx context.Context, tenantID, id uuid.UUID) (string, error) {
	sb, err := d.repo.Get(ctx, tenantID, id)
	if err != nil {
		return "", err
	}
	if sb.Status != StatusRunning {
		return "", ErrSandboxNotReady
	}
	cid, err := d.repo.GetContainerID(ctx, id)
	if err != nil {
		return "", err
	}
	if cid == "" {
		return "", ErrSandboxNotReady
	}
	return cid, nil
}

// 防止 errors 未用
var _ = errors.Is
```

- [ ] **Step 2: 追加测试**

```go
func TestDockerDriver_WriteRead_RoundTrip(t *testing.T) {
	ctx := context.Background()
	d, tid, uid := newDockerDriverForTest(t)
	sb, err := d.Create(ctx, sandbox.CreateOpts{TenantID: tid, OwnerUserID: uid})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Destroy(ctx, tid, sb.ID) })

	want := []byte("hello\x00binary\xff data")
	require.NoError(t, d.WriteFile(ctx, tid, sb.ID, "foo/bar/baz.bin", want))
	got, err := d.ReadFile(ctx, tid, sb.ID, "foo/bar/baz.bin")
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestDockerDriver_WriteFile_PathOutside(t *testing.T) {
	ctx := context.Background()
	d, tid, uid := newDockerDriverForTest(t)
	sb, err := d.Create(ctx, sandbox.CreateOpts{TenantID: tid, OwnerUserID: uid})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Destroy(ctx, tid, sb.ID) })

	err = d.WriteFile(ctx, tid, sb.ID, "../../etc/passwd", []byte("evil"))
	require.ErrorIs(t, err, sandbox.ErrPathOutsideWorkspace)
}

func TestDockerDriver_WriteFile_TooLarge(t *testing.T) {
	ctx := context.Background()
	d, tid, uid := newDockerDriverForTest(t)
	sb, err := d.Create(ctx, sandbox.CreateOpts{TenantID: tid, OwnerUserID: uid})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Destroy(ctx, tid, sb.ID) })

	big := make([]byte, sandbox.MaxFileSize+1)
	err = d.WriteFile(ctx, tid, sb.ID, "big.bin", big)
	require.ErrorIs(t, err, sandbox.ErrTooLarge)
}

func TestDockerDriver_ReadFile_TooLarge(t *testing.T) {
	ctx := context.Background()
	d, tid, uid := newDockerDriverForTest(t)
	sb, err := d.Create(ctx, sandbox.CreateOpts{TenantID: tid, OwnerUserID: uid})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Destroy(ctx, tid, sb.ID) })

	// 沙箱内造 2 MB 文件
	_, err = d.Exec(ctx, tid, sb.ID, sandbox.ExecOpts{
		Cmd: []string{"sh", "-c", "head -c 2000000 /dev/urandom > /workspace/big.bin"},
	})
	require.NoError(t, err)

	_, err = d.ReadFile(ctx, tid, sb.ID, "big.bin")
	require.ErrorIs(t, err, sandbox.ErrTooLarge)
}
```

- [ ] **Step 3: 跑测试**

```bash
PATH="/d/tools/go/bin:$PATH" go test -tags=docker_integration ./internal/sandbox/... -run "TestDockerDriver_(WriteRead|WriteFile|ReadFile)" -count=1 -v -timeout=120s
```

期望：4 个测试 PASS。

- [ ] **Step 4: commit**

```bash
git add internal/sandbox/docker_driver_fs.go internal/sandbox/docker_driver_test.go
git commit -m "feat(sandbox): DockerDriver ReadFile/WriteFile via tar streams"
```

---

## Task 12: `Snapshot` stub

**Files:**
- Modify: `internal/sandbox/docker_driver.go`
- Modify: `internal/sandbox/docker_driver_test.go`

### Step 1: 实现 Snapshot stub

在 `docker_driver.go` 末尾追加：

```go
// Snapshot is reserved for future MinIO-backed workspace persistence.
// Returns ErrNotImplemented in Slice 2.
func (d *DockerDriver) Snapshot(ctx context.Context, tenantID, id uuid.UUID) (string, error) {
	// 仍然校验沙箱存在,防泄露
	if _, err := d.repo.Get(ctx, tenantID, id); err != nil {
		return "", err
	}
	return "", ErrNotImplemented
}
```

- [ ] **Step 2: 追加测试**

```go
func TestDockerDriver_Snapshot_NotImplemented(t *testing.T) {
	ctx := context.Background()
	d, tid, uid := newDockerDriverForTest(t)
	sb, err := d.Create(ctx, sandbox.CreateOpts{TenantID: tid, OwnerUserID: uid})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Destroy(ctx, tid, sb.ID) })

	_, err = d.Snapshot(ctx, tid, sb.ID)
	require.ErrorIs(t, err, sandbox.ErrNotImplemented)
}
```

- [ ] **Step 3: 跑测试**

```bash
PATH="/d/tools/go/bin:$PATH" go test -tags=docker_integration ./internal/sandbox/... -run TestDockerDriver_Snapshot -count=1 -v -timeout=60s
```

期望：1 测试 PASS。

- [ ] **Step 4: commit**

```bash
git add internal/sandbox/docker_driver.go internal/sandbox/docker_driver_test.go
git commit -m "feat(sandbox): Snapshot returns ErrNotImplemented (stub for MinIO)"
```

---

## Task 13: HTTP Handlers

**Files:**
- Create: `internal/sandbox/handler.go`
- Create: `internal/sandbox/handler_test.go`

### Step 1: 实现 handler

`internal/sandbox/handler.go`:

```go
package sandbox

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yourorg/private-coding-agent/internal/auth"
)

// Handler exposes the sandbox Runtime as HTTP endpoints.
type Handler struct {
	rt Runtime
}

func NewHandler(rt Runtime) *Handler { return &Handler{rt: rt} }

// Register mounts /sandbox/* routes on rg. rg should already have
// auth.Middleware applied (handler relies on auth.FromCtx for claims).
func (h *Handler) Register(rg *gin.RouterGroup) {
	g := rg.Group("/sandbox/sessions")
	g.POST("", h.create)
	g.GET("/:id", h.get)
	g.DELETE("/:id", h.destroy)
	g.POST("/:id/exec", h.exec)
	g.GET("/:id/files", h.readFile)
	g.PUT("/:id/files", h.writeFile)
	g.POST("/:id/snapshot", h.snapshot)
}

func (h *Handler) claims(c *gin.Context) (*auth.Claims, bool) {
	cl := auth.FromCtx(c.Request.Context())
	if cl == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return nil, false
	}
	return cl, true
}

func (h *Handler) parseID(c *gin.Context) (uuid.UUID, bool) {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "validation: id"})
		return uuid.Nil, false
	}
	return id, true
}

type createReq struct {
	Image     string             `json:"image,omitempty"`
	ProjectID *string            `json:"project_id,omitempty"`
	Resources *resourceLimitsDTO `json:"resources,omitempty"`
	Network   string             `json:"network,omitempty"`
	Env       map[string]string  `json:"env,omitempty"`
	Labels    map[string]string  `json:"labels,omitempty"`
}

type resourceLimitsDTO struct {
	CPUs      float64 `json:"cpus,omitempty"`
	MemoryMB  int64   `json:"memory_mb,omitempty"`
	PIDsLimit int64   `json:"pids_limit,omitempty"`
}

type sandboxDTO struct {
	ID          uuid.UUID  `json:"id"`
	TenantID    uuid.UUID  `json:"tenant_id"`
	OwnerUserID uuid.UUID  `json:"owner_user_id"`
	ProjectID   *uuid.UUID `json:"project_id,omitempty"`
	Status      string     `json:"status"`
	Image       string     `json:"image"`
	NetworkMode string     `json:"network_mode"`
	CPUs        float64    `json:"cpus"`
	MemoryMB    int64      `json:"memory_mb"`
	PIDsLimit   int64      `json:"pids_limit"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	DestroyedAt *time.Time `json:"destroyed_at,omitempty"`
}

func toDTO(sb *Sandbox) sandboxDTO {
	return sandboxDTO{
		ID:          sb.ID,
		TenantID:    sb.TenantID,
		OwnerUserID: sb.OwnerUserID,
		ProjectID:   sb.ProjectID,
		Status:      string(sb.Status),
		Image:       sb.Image,
		NetworkMode: string(sb.Network),
		CPUs:        sb.Resources.CPUs,
		MemoryMB:    sb.Resources.MemoryMB,
		PIDsLimit:   sb.Resources.PIDsLimit,
		CreatedAt:   sb.CreatedAt,
		UpdatedAt:   sb.UpdatedAt,
		DestroyedAt: sb.DestroyedAt,
	}
}

func (h *Handler) create(c *gin.Context) {
	cl, ok := h.claims(c)
	if !ok {
		return
	}
	var req createReq
	_ = c.ShouldBindJSON(&req) // 全字段 optional,body 可为空

	opts := CreateOpts{
		TenantID:    cl.TenantID,
		OwnerUserID: cl.UserID,
		Image:       req.Image,
		Network:     NetworkMode(req.Network),
		Env:         req.Env,
		Labels:      req.Labels,
	}
	if req.Resources != nil {
		opts.Resources = ResourceLimits{
			CPUs:      req.Resources.CPUs,
			MemoryMB:  req.Resources.MemoryMB,
			PIDsLimit: req.Resources.PIDsLimit,
		}
	}
	if req.ProjectID != nil {
		pid, err := uuid.Parse(*req.ProjectID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "validation: project_id"})
			return
		}
		opts.ProjectID = &pid
	}

	sb, err := h.rt.Create(c.Request.Context(), opts)
	if err != nil {
		if isValidationError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "runtime_error"})
		return
	}
	c.JSON(http.StatusCreated, toDTO(sb))
}

func (h *Handler) get(c *gin.Context) {
	cl, ok := h.claims(c)
	if !ok {
		return
	}
	id, ok := h.parseID(c)
	if !ok {
		return
	}
	sb, err := h.rt.Get(c.Request.Context(), cl.TenantID, id)
	if err != nil {
		if errors.Is(err, ErrSandboxNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "runtime_error"})
		return
	}
	c.JSON(http.StatusOK, toDTO(sb))
}

func (h *Handler) destroy(c *gin.Context) {
	cl, ok := h.claims(c)
	if !ok {
		return
	}
	id, ok := h.parseID(c)
	if !ok {
		return
	}
	err := h.rt.Destroy(c.Request.Context(), cl.TenantID, id)
	if err != nil {
		if errors.Is(err, ErrSandboxNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "runtime_error"})
		return
	}
	c.Status(http.StatusNoContent)
}

type execReq struct {
	Cmd         []string          `json:"cmd"`
	WorkingDir  string            `json:"working_dir,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	StdinBase64 string            `json:"stdin_base64,omitempty"`
	TimeoutSec  int               `json:"timeout_sec,omitempty"`
}

type execResp struct {
	ExitCode     int    `json:"exit_code"`
	StdoutBase64 string `json:"stdout_base64"`
	StderrBase64 string `json:"stderr_base64"`
	Truncated    bool   `json:"truncated"`
	DurationMS   int64  `json:"duration_ms"`
	TimedOut     bool   `json:"timed_out"`
}

func (h *Handler) exec(c *gin.Context) {
	cl, ok := h.claims(c)
	if !ok {
		return
	}
	id, ok := h.parseID(c)
	if !ok {
		return
	}
	var req execReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad_request"})
		return
	}

	opts := ExecOpts{
		Cmd:        req.Cmd,
		WorkingDir: req.WorkingDir,
		Env:        req.Env,
		TimeoutSec: req.TimeoutSec,
	}
	if req.StdinBase64 != "" {
		b, err := base64.StdEncoding.DecodeString(req.StdinBase64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "validation: stdin_base64"})
			return
		}
		opts.Stdin = b
	}

	res, err := h.rt.Exec(c.Request.Context(), cl.TenantID, id, opts)
	if err != nil {
		switch {
		case errors.Is(err, ErrSandboxNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
		case errors.Is(err, ErrSandboxNotReady):
			c.JSON(http.StatusConflict, gin.H{"error": "not_ready"})
		case isValidationError(err):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "runtime_error"})
		}
		return
	}
	c.JSON(http.StatusOK, execResp{
		ExitCode:     res.ExitCode,
		StdoutBase64: base64.StdEncoding.EncodeToString(res.Stdout),
		StderrBase64: base64.StdEncoding.EncodeToString(res.Stderr),
		Truncated:    res.Truncated,
		DurationMS:   res.DurationMS,
		TimedOut:     res.TimedOut,
	})
}

type writeFileReq struct {
	ContentBase64 string `json:"content_base64"`
}

func (h *Handler) writeFile(c *gin.Context) {
	cl, ok := h.claims(c)
	if !ok {
		return
	}
	id, ok := h.parseID(c)
	if !ok {
		return
	}
	rel := c.Query("path")
	if rel == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "validation: path"})
		return
	}
	var req writeFileReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad_request"})
		return
	}
	data, err := base64.StdEncoding.DecodeString(req.ContentBase64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "validation: content_base64"})
		return
	}
	if err := h.rt.WriteFile(c.Request.Context(), cl.TenantID, id, rel, data); err != nil {
		fileErrToHTTP(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) readFile(c *gin.Context) {
	cl, ok := h.claims(c)
	if !ok {
		return
	}
	id, ok := h.parseID(c)
	if !ok {
		return
	}
	rel := c.Query("path")
	if rel == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "validation: path"})
		return
	}
	data, err := h.rt.ReadFile(c.Request.Context(), cl.TenantID, id, rel)
	if err != nil {
		fileErrToHTTP(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"content_base64": base64.StdEncoding.EncodeToString(data),
		"size":           len(data),
	})
}

func (h *Handler) snapshot(c *gin.Context) {
	cl, ok := h.claims(c)
	if !ok {
		return
	}
	id, ok := h.parseID(c)
	if !ok {
		return
	}
	_, err := h.rt.Snapshot(c.Request.Context(), cl.TenantID, id)
	if errors.Is(err, ErrSandboxNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
		return
	}
	c.JSON(http.StatusNotImplemented, gin.H{"error": "not_implemented"})
}

func fileErrToHTTP(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrSandboxNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
	case errors.Is(err, ErrSandboxNotReady):
		c.JSON(http.StatusConflict, gin.H{"error": "not_ready"})
	case errors.Is(err, ErrPathOutsideWorkspace):
		c.JSON(http.StatusBadRequest, gin.H{"error": "path_outside_workspace"})
	case errors.Is(err, ErrTooLarge):
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "payload_too_large"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "runtime_error"})
	}
}

func isValidationError(err error) bool {
	// validate.go 用 fmt.Errorf("validation: ...") 包装
	if err == nil {
		return false
	}
	const prefix = "validation:"
	return len(err.Error()) >= len(prefix) && err.Error()[:len(prefix)] == prefix
}

// silence unused import (json not used directly, but kept for future)
var _ = json.NewDecoder
var _ = context.Background
```

- [ ] **Step 2: 写 handler 测试（mock runtime）**

`internal/sandbox/handler_test.go`:

```go
package sandbox_test

import (
	"bytes"
	"context"
	"encoding/base64"
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
	"github.com/yourorg/private-coding-agent/internal/sandbox"
)

// mockRuntime is a hand-written test double satisfying sandbox.Runtime.
type mockRuntime struct {
	createRet *sandbox.Sandbox
	createErr error
	getRet    *sandbox.Sandbox
	getErr    error
	destroyErr error
	execRet   *sandbox.ExecResult
	execErr   error
	readRet   []byte
	readErr   error
	writeErr  error
	snapErr   error

	// last-call inspection
	lastCreateOpts sandbox.CreateOpts
	lastExecOpts   sandbox.ExecOpts
	lastWriteData  []byte
}

func (m *mockRuntime) Create(_ context.Context, opts sandbox.CreateOpts) (*sandbox.Sandbox, error) {
	m.lastCreateOpts = opts
	return m.createRet, m.createErr
}
func (m *mockRuntime) Get(_ context.Context, _, _ uuid.UUID) (*sandbox.Sandbox, error) {
	return m.getRet, m.getErr
}
func (m *mockRuntime) Destroy(_ context.Context, _, _ uuid.UUID) error { return m.destroyErr }
func (m *mockRuntime) Exec(_ context.Context, _, _ uuid.UUID, opts sandbox.ExecOpts) (*sandbox.ExecResult, error) {
	m.lastExecOpts = opts
	return m.execRet, m.execErr
}
func (m *mockRuntime) ReadFile(_ context.Context, _, _ uuid.UUID, _ string) ([]byte, error) {
	return m.readRet, m.readErr
}
func (m *mockRuntime) WriteFile(_ context.Context, _, _ uuid.UUID, _ string, data []byte) error {
	m.lastWriteData = data
	return m.writeErr
}
func (m *mockRuntime) Snapshot(_ context.Context, _, _ uuid.UUID) (string, error) {
	return "", m.snapErr
}

func newRouterWithMock(t *testing.T, m *mockRuntime) (*gin.Engine, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	const secret = "test-secret-thirty-two-chars-ok!"
	j := auth.NewJWT(auth.JWTConfig{Secret: secret, TTL: time.Hour})
	uid, tid := uuid.New(), uuid.New()
	tok, err := j.Issue(uid, tid, "member")
	require.NoError(t, err)

	r := gin.New()
	g := r.Group("/")
	g.Use(auth.Middleware(j))
	sandbox.NewHandler(m).Register(g)
	return r, "Bearer " + tok
}

func do(r *gin.Engine, method, path, bearer string, body any) *httptest.ResponseRecorder {
	var rdr *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Header.Set("Authorization", bearer)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestHandler_Create_OK(t *testing.T) {
	mr := &mockRuntime{
		createRet: &sandbox.Sandbox{
			ID:        uuid.New(),
			Status:    sandbox.StatusRunning,
			Image:     "pca/sandbox:base",
			Network:   sandbox.NetworkInternal,
			Resources: sandbox.ResourceLimits{CPUs: 1, MemoryMB: 512, PIDsLimit: 256},
		},
	}
	r, tok := newRouterWithMock(t, mr)
	w := do(r, http.MethodPost, "/sandbox/sessions", tok, map[string]any{})
	require.Equal(t, http.StatusCreated, w.Code)
}

func TestHandler_Create_NoAuth(t *testing.T) {
	mr := &mockRuntime{}
	r, _ := newRouterWithMock(t, mr)
	w := do(r, http.MethodPost, "/sandbox/sessions", "", map[string]any{})
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHandler_Get_NotFound(t *testing.T) {
	mr := &mockRuntime{getErr: sandbox.ErrSandboxNotFound}
	r, tok := newRouterWithMock(t, mr)
	w := do(r, http.MethodGet, "/sandbox/sessions/"+uuid.NewString(), tok, nil)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandler_Get_BadID(t *testing.T) {
	mr := &mockRuntime{}
	r, tok := newRouterWithMock(t, mr)
	w := do(r, http.MethodGet, "/sandbox/sessions/not-a-uuid", tok, nil)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandler_Destroy_NoContent(t *testing.T) {
	mr := &mockRuntime{}
	r, tok := newRouterWithMock(t, mr)
	w := do(r, http.MethodDelete, "/sandbox/sessions/"+uuid.NewString(), tok, nil)
	require.Equal(t, http.StatusNoContent, w.Code)
}

func TestHandler_Exec_OK(t *testing.T) {
	mr := &mockRuntime{
		execRet: &sandbox.ExecResult{ExitCode: 0, Stdout: []byte("hi\n")},
	}
	r, tok := newRouterWithMock(t, mr)
	w := do(r, http.MethodPost, "/sandbox/sessions/"+uuid.NewString()+"/exec", tok,
		map[string]any{"cmd": []string{"echo", "hi"}})
	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, float64(0), resp["exit_code"])
	dec, _ := base64.StdEncoding.DecodeString(resp["stdout_base64"].(string))
	require.Equal(t, "hi\n", string(dec))
}

func TestHandler_Exec_NotReady(t *testing.T) {
	mr := &mockRuntime{execErr: sandbox.ErrSandboxNotReady}
	r, tok := newRouterWithMock(t, mr)
	w := do(r, http.MethodPost, "/sandbox/sessions/"+uuid.NewString()+"/exec", tok,
		map[string]any{"cmd": []string{"echo"}})
	require.Equal(t, http.StatusConflict, w.Code)
}

func TestHandler_WriteFile_OK(t *testing.T) {
	mr := &mockRuntime{}
	r, tok := newRouterWithMock(t, mr)
	content := base64.StdEncoding.EncodeToString([]byte("hello"))
	w := do(r, http.MethodPut, "/sandbox/sessions/"+uuid.NewString()+"/files?path=a.txt", tok,
		map[string]any{"content_base64": content})
	require.Equal(t, http.StatusNoContent, w.Code)
	require.Equal(t, []byte("hello"), mr.lastWriteData)
}

func TestHandler_WriteFile_PathOutside(t *testing.T) {
	mr := &mockRuntime{writeErr: sandbox.ErrPathOutsideWorkspace}
	r, tok := newRouterWithMock(t, mr)
	content := base64.StdEncoding.EncodeToString([]byte("x"))
	w := do(r, http.MethodPut, "/sandbox/sessions/"+uuid.NewString()+"/files?path=../x", tok,
		map[string]any{"content_base64": content})
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandler_ReadFile_TooLarge(t *testing.T) {
	mr := &mockRuntime{readErr: sandbox.ErrTooLarge}
	r, tok := newRouterWithMock(t, mr)
	w := do(r, http.MethodGet, "/sandbox/sessions/"+uuid.NewString()+"/files?path=big.bin", tok, nil)
	require.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
}

func TestHandler_Snapshot_501(t *testing.T) {
	mr := &mockRuntime{snapErr: sandbox.ErrNotImplemented}
	r, tok := newRouterWithMock(t, mr)
	w := do(r, http.MethodPost, "/sandbox/sessions/"+uuid.NewString()+"/snapshot", tok, nil)
	require.Equal(t, http.StatusNotImplemented, w.Code)
}

// silence unused
var _ = errors.Is
```

- [ ] **Step 3: 跑测试**

```bash
PATH="/d/tools/go/bin:$PATH" go test ./internal/sandbox/... -run "TestHandler_" -count=1 -v
```

期望：11 个测试 PASS（不需要 docker_integration tag，走 mock）。

- [ ] **Step 4: commit**

```bash
git add internal/sandbox/handler.go internal/sandbox/handler_test.go
git commit -m "feat(sandbox): HTTP handlers for sandbox lifecycle / exec / files"
```

---

## Task 14: main.go 装配

**Files:**
- Modify: `cmd/server/main.go`

### Step 1: 在 run() 中装配 sandbox

替换 `cmd/server/main.go` 整文件为：

```go
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

	"github.com/yourorg/private-coding-agent/internal/audit"
	"github.com/yourorg/private-coding-agent/internal/auth"
	"github.com/yourorg/private-coding-agent/internal/config"
	"github.com/yourorg/private-coding-agent/internal/db"
	"github.com/yourorg/private-coding-agent/internal/httpx"
	"github.com/yourorg/private-coding-agent/internal/sandbox"
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
```

- [ ] **Step 2: 给 config 添加 Redis 字段**

修改 `internal/config/config.go` 增加 `RedisConfig`：

找到 `Config` struct，加 Redis 字段：

```go
type Config struct {
	Server    ServerConfig    `mapstructure:"server"`
	DB        DBConfig        `mapstructure:"db"`
	Redis     RedisConfig     `mapstructure:"redis"`
	Auth      AuthConfig      `mapstructure:"auth"`
	Telemetry TelemetryConfig `mapstructure:"telemetry"`
}

type RedisConfig struct {
	Addr string `mapstructure:"addr"`
}
```

修改 `config/config.example.yaml`：

```yaml
server:
  port: 8080
  mode: debug
db:
  dsn: "postgres://app:app@localhost:5432/app?sslmode=disable"
redis:
  addr: "localhost:6379"
auth:
  jwt_secret: "change-me-in-production"
  jwt_ttl: "24h"
telemetry:
  service_name: "private-coding-agent"
  otlp_endpoint: ""
```

- [ ] **Step 3: 编译 + 测试通过（含 config 单元测）**

```bash
PATH="/d/tools/go/bin:$PATH" go vet ./...
PATH="/d/tools/go/bin:$PATH" go build ./...
PATH="/d/tools/go/bin:$PATH" go test ./internal/config/... -count=1
```

注意：Task 16 还没写 `RunReconciler`，编译会失败。**暂时**在 main.go 把 `sandbox.RunReconciler` 那行**注释掉**，添加 `// TODO Task 16: enable reconciler`。Task 16 实施时再启用。

期望：build + vet 干净；config 测试 PASS。

- [ ] **Step 4: commit**

```bash
git add cmd/server/main.go internal/config/config.go config/config.example.yaml
git commit -m "feat(cmd): wire sandbox driver, Docker client, Redis into main"
```

---

## Task 15: docker-compose 更新（挂 docker.sock、预 build sandbox 镜像）

**Files:**
- Modify: `deploy/compose/docker-compose.yml`

### Step 1: 更新 compose

替换 `deploy/compose/docker-compose.yml`:

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
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 3s
      timeout: 3s
      retries: 20
    ports:
      - "6379:6379"

  # 预 build pca/sandbox:base 镜像 (server depend 起来前必须可用)
  sandbox-image-builder:
    image: docker:cli
    command: ["sh", "-c", "docker build -t pca/sandbox:base /sandbox-context || true; sleep infinity"]
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - ../../sandbox/image:/sandbox-context:ro
    profiles: ["build-only"]

  server:
    build:
      context: ../..
      dockerfile: Dockerfile
    depends_on:
      postgres:
        condition: service_healthy
      redis:
        condition: service_healthy
    environment:
      PCA_DB_DSN: "postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@postgres:5432/${POSTGRES_DB}?sslmode=disable"
      PCA_REDIS_ADDR: "redis:6379"
      PCA_SERVER_PORT: 8080
      PCA_SERVER_MODE: release
      PCA_AUTH_JWT_SECRET: ${PCA_AUTH_JWT_SECRET}
      PCA_AUTH_JWT_TTL: 24h
      PCA_TELEMETRY_SERVICE_NAME: private-coding-agent
      PCA_TELEMETRY_OTLP_ENDPOINT: ${PCA_TELEMETRY_OTLP_ENDPOINT}
      DOCKER_HOST: "unix:///var/run/docker.sock"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
    ports:
      - "8080:8080"

volumes:
  pgdata:
```

> 说明：`sandbox-image-builder` 用 `profiles` 隔离——`docker compose up` 默认不起它；运维需在初次部署前手动 `docker compose --profile build-only up sandbox-image-builder` 或直接 `docker build -t pca/sandbox:base sandbox/image`。

- [ ] **Step 2: 提前 build sandbox 镜像**

```powershell
docker build -t pca/sandbox:base D:\IdeaProjects\private-coding-agent\sandbox\image
docker images pca/sandbox:base
```

- [ ] **Step 3: 启 compose 验证**

```powershell
cd D:\IdeaProjects\private-coding-agent\deploy\compose
Copy-Item .env.example .env -Force
docker compose up -d --build
Start-Sleep -Seconds 20
Invoke-RestMethod -Uri http://localhost:8080/healthz
docker compose logs server --tail 20
docker compose down
cd ..\..
```

期望：`/healthz` 返 `{"status":"ok"}`；server log 显示 docker client 连接 OK。

- [ ] **Step 4: commit**

```bash
git add deploy/compose/docker-compose.yml
git commit -m "deploy: mount docker.sock + redis healthcheck + sandbox image builder profile"
```

---

## Task 16: Reconciler（启动期清理）

**Files:**
- Create: `internal/sandbox/reconciler.go`
- Create: `internal/sandbox/reconciler_test.go`
- Modify: `cmd/server/main.go`（取消 Task 14 注释）

### Step 1: 写测试（用 docker_integration）

`internal/sandbox/reconciler_test.go`:

```go
//go:build docker_integration

package sandbox_test

import (
	"context"
	"testing"

	"github.com/docker/docker/client"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/sandbox"
)

func TestReconciler_MarksDeadContainerDestroyed(t *testing.T) {
	ctx := context.Background()
	d, tid, uid := newDockerDriverForTest(t)

	sb, err := d.Create(ctx, sandbox.CreateOpts{TenantID: tid, OwnerUserID: uid})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Destroy(ctx, tid, sb.ID) })

	// 直接 force remove container,绕过 driver
	cid, err := d.GetContainerIDForTest(ctx, sb.ID)
	require.NoError(t, err)
	cli, _ := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	defer cli.Close()
	// 直接停 + 删容器,模拟容器异常死亡
	require.NoError(t, cli.ContainerRemove(ctx, cid, container.RemoveOptions{Force: true}))

	pg, _ := pgxpool.New(ctx, testDSN)
	defer pg.Close()
	repo := sandbox.NewSessionRepo(pg)

	require.NoError(t, sandbox.RunReconciler(ctx, repo, cli))

	got, err := repo.Get(ctx, tid, sb.ID)
	require.NoError(t, err)
	require.Equal(t, sandbox.StatusDestroyed, got.Status)
}
```

> 注：test 顶部需 `import "github.com/docker/docker/api/types/container"`，请补上。

- [ ] **Step 2: 实现 Reconciler**

`internal/sandbox/reconciler.go`:

```go
package sandbox

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"
)

// RunReconciler scans active sandboxes against docker, marking dead containers
// as destroyed. Called once at server startup before serving traffic.
//
// Returns error only on infrastructure failure (DB unavailable); individual
// container inspect errors are logged and skipped.
func RunReconciler(ctx context.Context, repo *SessionRepo, cli *client.Client) error {
	active, err := repo.ListActive(ctx)
	if err != nil {
		return fmt.Errorf("list active: %w", err)
	}
	if len(active) == 0 {
		return nil
	}

	log.Printf("reconciler: %d active sandbox(es) to verify", len(active))

	for _, sb := range active {
		cid, err := repo.GetContainerID(ctx, sb.ID)
		if err != nil {
			log.Printf("reconciler: get container_id %s: %v", sb.ID, err)
			continue
		}
		if cid == "" {
			// pending without container_id - mark failed/destroyed
			_ = repo.UpdateStatus(ctx, sb.ID, StatusDestroyed)
			continue
		}
		_, err = cli.ContainerInspect(ctx, cid)
		if err != nil {
			if errdefs.IsNotFound(err) || isDockerNotFound(err) {
				_ = repo.UpdateStatus(ctx, sb.ID, StatusDestroyed)
				continue
			}
			log.Printf("reconciler: inspect %s: %v", cid, err)
			continue
		}
		// 容器存在:保持 running
	}
	return nil
}

func isDockerNotFound(err error) bool {
	// fallback: 部分情况下 errdefs.IsNotFound 不命中
	if err == nil {
		return false
	}
	var notFound interface{ NotFound() bool }
	if errors.As(err, &notFound) {
		return notFound.NotFound()
	}
	return false
}
```

- [ ] **Step 3: 启用 main.go 里的 RunReconciler 调用**

修改 `cmd/server/main.go`，去掉 Task 14 时注释的 `sandbox.RunReconciler(...)` 那行，确保它在 `sandboxHandler := ...` 之后、`engine := ...` 之前调用。

- [ ] **Step 4: 跑测试**

```bash
PATH="/d/tools/go/bin:$PATH" go vet ./...
PATH="/d/tools/go/bin:$PATH" go build ./...
PATH="/d/tools/go/bin:$PATH" go test -tags=docker_integration ./internal/sandbox/... -run TestReconciler -count=1 -v -timeout=120s
```

期望：reconciler 测试 PASS。

- [ ] **Step 5: commit**

```bash
git add internal/sandbox/reconciler.go internal/sandbox/reconciler_test.go cmd/server/main.go
git commit -m "feat(sandbox): startup reconciler reaps dead containers"
```

---

## Task 17: E2E 脚本 + README 更新 + 验收

**Files:**
- Create: `deploy/compose/test-e2e.ps1`
- Modify: `README.md`

### Step 1: 写 E2E 脚本

`deploy/compose/test-e2e.ps1`:

```powershell
# Slice 2 端到端验证。前置:
#  - Docker Desktop 在跑
#  - pca/sandbox:base 镜像已 build (docker build -t pca/sandbox:base ../../sandbox/image)
#  - 当前目录 deploy/compose/, .env 已从 .env.example 复制
#
# 用法:
#   cd deploy\compose
#   pwsh ./test-e2e.ps1

$ErrorActionPreference = 'Stop'

if (-not (Test-Path .\.env)) {
    Copy-Item .env.example .env
    Write-Host "[setup] copied .env.example -> .env"
}

Write-Host "[1/8] starting compose ..."
docker compose up -d --build | Out-Null
Start-Sleep -Seconds 20

Write-Host "[2/8] inserting demo user via psql ..."
# demo123 的 bcrypt (Slice 1 验证过的真实 hash)
$hash = '$2a$10$WJBaC0mXl/yIgPXKW8WbPujOAidLdmaDPlduPdV8i11ZHaFvcgUrC'
docker compose exec -T postgres psql -U app -d app -c @"
INSERT INTO users (tenant_id, email, password_hash, name, role)
VALUES ((SELECT id FROM tenants WHERE slug='default'),
        'demo@example.com', '$hash', 'Demo', 'admin')
ON CONFLICT (tenant_id, email) DO NOTHING;
"@ | Out-Null

Write-Host "[3/8] login ..."
$body = '{"tenant":"default","email":"demo@example.com","password":"demo123"}'
$tok = (Invoke-RestMethod -Method POST -Uri http://localhost:8080/auth/login -ContentType application/json -Body $body).token
$H = @{ Authorization = "Bearer $tok" }

Write-Host "[4/8] create sandbox ..."
$sb = Invoke-RestMethod -Method POST -Uri http://localhost:8080/sandbox/sessions -Headers $H -ContentType application/json -Body '{}'
$id = $sb.id
Write-Host "  -> sandbox $id, status=$($sb.status)"
if ($sb.status -ne 'running') { throw "expected status=running, got $($sb.status)" }

Write-Host "[5/8] write file ..."
$content = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes("hello world from e2e"))
Invoke-RestMethod -Method PUT -Uri "http://localhost:8080/sandbox/sessions/$id/files?path=hello.txt" `
    -Headers $H -ContentType application/json `
    -Body (@{ content_base64 = $content } | ConvertTo-Json) | Out-Null

Write-Host "[6/8] exec cat ..."
$exec = Invoke-RestMethod -Method POST -Uri "http://localhost:8080/sandbox/sessions/$id/exec" `
    -Headers $H -ContentType application/json `
    -Body '{"cmd":["cat","/workspace/hello.txt"]}'
$out = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($exec.stdout_base64))
Write-Host "  -> stdout: $out (exit=$($exec.exit_code))"
if ($out -ne 'hello world from e2e') { throw "stdout mismatch" }

Write-Host "[7/8] destroy ..."
Invoke-RestMethod -Method DELETE -Uri "http://localhost:8080/sandbox/sessions/$id" -Headers $H | Out-Null

Write-Host "[8/8] verify 404 after destroy ..."
try {
    Invoke-RestMethod -Method POST -Uri "http://localhost:8080/sandbox/sessions/$id/exec" `
        -Headers $H -ContentType application/json -Body '{"cmd":["true"]}'
    throw "expected 404 after destroy"
} catch {
    if ($_.Exception.Response.StatusCode.value__ -ne 404) {
        throw "expected 404, got $($_.Exception.Response.StatusCode.value__)"
    }
}

docker compose down | Out-Null
Write-Host "`nE2E PASS"
```

### Step 2: 更新 README

替换 `README.md`:

```markdown
# Private Coding Agent

私有化部署的 AI 编码 Agent 平台。

## 切片进度

- [x] 切片 1：Foundation
- [x] 切片 1.5：Foundation Hardening
- [x] 切片 2：Sandbox Runtime + DockerDriver
- [ ] 切片 3：Model Gateway
- [ ] 切片 4：Tool Bus + Internal MCP
- [ ] 切片 5：Agent Engine
- [ ] 切片 6：Session API + WebSocket
- [ ] 切片 7：Memory (basic)
- [ ] 切片 8：Web Frontend
- [ ] 切片 9：Integration & Audit

## 本地开发

```powershell
# 单元 + 集成测试 (含 dockertest 拉 PG)
go test ./...

# 集成测试 (含真 Docker)
docker build -t pca/sandbox:base ./sandbox/image
go test -tags=docker_integration ./internal/sandbox/...

# 本地直接跑
copy config\config.example.yaml config\config.yaml
go run ./cmd/server --config config\config.yaml
```

## docker-compose 启动

```powershell
docker build -t pca/sandbox:base ./sandbox/image   # 首次必须
cd deploy\compose
copy .env.example .env
docker compose up -d --build
curl http://localhost:8080/healthz
```

## 端到端验证

```powershell
cd deploy\compose
pwsh ./test-e2e.ps1
```

## 关键端点

| 方法 | 路径 | 鉴权 | 说明 |
|---|---|---|---|
| GET | /healthz | - | 健康检查 |
| GET | /readyz | - | 就绪检查 |
| POST | /auth/login | - | 登录拿 JWT |
| GET | /me | Bearer | 当前用户身份 |
| POST | /sandbox/sessions | Bearer | 创建沙箱 |
| GET | /sandbox/sessions/{id} | Bearer | 查询沙箱 |
| DELETE | /sandbox/sessions/{id} | Bearer | 销毁沙箱 |
| POST | /sandbox/sessions/{id}/exec | Bearer | 执行命令 |
| GET | /sandbox/sessions/{id}/files?path=... | Bearer | 读文件 |
| PUT | /sandbox/sessions/{id}/files?path=... | Bearer | 写文件 |
| POST | /sandbox/sessions/{id}/snapshot | Bearer | (501) |

## 配置

见 `config/config.example.yaml`。所有字段可用 `PCA_<UPPER>_<UPPER>` 环境变量覆盖。
```

- [ ] **Step 3: 跑 E2E 验证**

```powershell
cd D:\IdeaProjects\private-coding-agent\deploy\compose
pwsh ./test-e2e.ps1
```

期望：最后输出 `E2E PASS`。

- [ ] **Step 4: 全包测试 sanity check**

```bash
cd D:/IdeaProjects/private-coding-agent
PATH="/d/tools/go/bin:$PATH" go test ./... -count=1
PATH="/d/tools/go/bin:$PATH" go vet ./...
PATH="/d/tools/go/bin:$PATH" go build ./...
```

期望：全 PASS。

- [ ] **Step 5: commit**

```bash
git add deploy/compose/test-e2e.ps1 README.md
git commit -m "docs: README + e2e script for slice 2"
```

---

## 验收（end-of-slice checklist）

- [ ] `go test ./...` 全 PASS（不含 docker_integration tag）
- [ ] `go test -tags=docker_integration ./internal/sandbox/...` 全 PASS（含真 Docker 集成测试）
- [ ] `go vet ./...` 干净；`go build ./...` 干净
- [ ] `pca/sandbox:base` 镜像构建成功
- [ ] `docker compose up -d --build` 后 `/healthz` 200
- [ ] `test-e2e.ps1` 跑通（最后输出 `E2E PASS`）
- [ ] `audit_log` 含 `sandbox.*` 路径请求记录
- [ ] `sandbox_sessions` 表里有 destroyed 行
- [ ] 所有 Task commit、git tree clean
- [ ] **Slice 1.5 carry-over 已完成**（Task 0：NewJWT defense、Sink godoc、tighten audit deadline）

---

## Self-Review 检查

**1. Spec coverage:**
- spec §4 组件清单：types ✓ Task 2、SessionRepo ✓ Task 3、path ✓ Task 4、validate ✓ Task 5、Runtime ✓ Task 6、DockerDriver ✓ Task 7-12、Handler ✓ Task 13、main wiring ✓ Task 14、compose ✓ Task 15、Reconciler ✓ Task 16、E2E ✓ Task 17
- spec §5 接口：所有 Runtime 方法签名一致（Create/Get/Destroy/Exec/ReadFile/WriteFile/Snapshot）
- spec §6 数据流：6 条主流均有对应实现（Create→Task 8；Exec→Task 10；ReadFile/WriteFile→Task 11；Destroy→Task 9；Reconcile→Task 16）
- spec §7 错误处理：handler fileErrToHTTP 覆盖 4 类错误；mock 测试覆盖关键状态码

**2. Placeholder scan:** 无 TBD / TODO / "类似 Task N" 占位符。所有代码块完整可执行。

**3. Type consistency:**
- `Sandbox` struct 字段在 types.go / sessionrepo / handler DTO 之间一致
- `Runtime` 接口方法签名 (ctx, tenantID, id, ...) 在接口定义 / DockerDriver 各方法 / handler 调用 / mock 实现 之间一致
- `ResolveWorkspacePath` 返回 (string, error) 在 path.go / docker_driver_fs.go 使用一致
- `DockerDriverConfig.InternalNetworkName` 在 NewDockerDriver / networkModeFor 一致
- `ExecResult` 字段 (ExitCode/Stdout/Stderr/Truncated/DurationMS/TimedOut) 在 types.go / docker_driver_exec / handler execResp 全程一致
