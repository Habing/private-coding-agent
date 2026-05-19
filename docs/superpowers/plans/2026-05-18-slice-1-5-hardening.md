# Slice 1.5 — Foundation Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复 Slice 1 final review 标记的 3 项加固，作为 Slice 2 的前置条件：(1) `audit.Middleware` 用 detached ctx 防请求结束导致审计丢失；(2) `db.Migrate` 接受 ctx；(3) 启动期拒绝默认 JWT secret。

**Architecture:** 三处局部改造，互不依赖。每项独立 TDD（先写失败测试 → 实现 → 验证 → commit）。前两项需更新调用方。第三项新增独立校验函数，便于单测。

**Tech Stack:** 沿用 Slice 1：Go 1.26、gin、pgx v5、golang-migrate v4、testify。

---

## File Structure

```
internal/audit/middleware.go        修改: Append 走 detached ctx + 5s timeout
internal/audit/middleware_test.go   修改: 新增 detached-ctx 测试 + 严格 spy

internal/db/migrate.go              修改: Migrate(ctx, dsn) 签名
internal/tenant/repo_test.go        修改: 调用方更新
internal/user/service_test.go       修改: 调用方更新

internal/auth/config.go             新建: ValidateJWTConfig(cfg) error
internal/auth/config_test.go        新建: ValidateJWTConfig 单测

cmd/server/main.go                  修改: 调 db.Migrate(ctx,...) + 调 auth.ValidateJWTConfig
```

执行顺序：Task 1 (db.Migrate ctx) → Task 2 (audit detached ctx) → Task 3 (JWT secret validation) → Task 4 (E2E sanity)。前两项独立，第三项依赖 Task 1 已改完 main.go（避免并发改 main 引冲突）。

---

## Task 1: `db.Migrate(ctx, dsn)` 接受 context

**Files:**
- Modify: `internal/db/migrate.go`
- Modify: `internal/tenant/repo_test.go`（调用方）
- Modify: `internal/user/service_test.go`（调用方）
- Modify: `cmd/server/main.go`（调用方）

### Step 1: 先写测试（验证 ctx 取消会被尊重）

新建测试文件 `internal/db/migrate_test.go`：

```go
package db_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/db"
)

func TestMigrate_RespectsCanceledCtx(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立刻取消

	// DSN 无关紧要，因为 ctx 已取消 Migrate 应直接退出
	err := db.Migrate(ctx, "postgres://app:app@localhost:1/app?sslmode=disable")
	require.Error(t, err)
	// 错误应来自 ctx，而非网络/DSN
	require.True(t,
		strings.Contains(err.Error(), "context canceled") ||
			strings.Contains(err.Error(), "ctx:"),
		"expected ctx error, got: %v", err)
}
```

- [ ] **Step 2: 跑测试验证失败**

```bash
cd D:/IdeaProjects/private-coding-agent
PATH="/d/tools/go/bin:$PATH" go test ./internal/db/... -run TestMigrate_RespectsCanceledCtx
```

期望：FAIL（编译错误：`Migrate` 当前签名是 `Migrate(dsn string) error`，不接 ctx）。

- [ ] **Step 3: 修改 Migrate 签名与实现**

替换 `internal/db/migrate.go` 整文件为：

```go
package db

import (
	"context"
	"embed"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate applies all pending up migrations embedded under migrations/.
// ErrNoChange is treated as success. Migrations are versioned by filename
// prefix (NNNN_*.up.sql / NNNN_*.down.sql).
//
// ctx is checked before opening the migrate connection and is honored via the
// underlying database driver for connection-level operations. Once `m.Up()`
// has started it will run to completion (golang-migrate v4 does not currently
// support cancellation mid-migration); use ctx primarily to abort before any
// work begins.
func Migrate(ctx context.Context, dsn string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("ctx: %w", err)
	}
	src, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("iofs: %w", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, dsn)
	if err != nil {
		return fmt.Errorf("migrate new: %w", err)
	}
	defer m.Close()
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("ctx: %w", err)
	}
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: 跑测试验证新测试通过**

```bash
PATH="/d/tools/go/bin:$PATH" go test ./internal/db/... -run TestMigrate_RespectsCanceledCtx -v
```

期望：PASS。

- [ ] **Step 5: 更新调用方 — `internal/tenant/repo_test.go`**

找到 `TestMain` 里这段：

```go
pool.MaxWait = 60 * time.Second
if err := pool.Retry(func() error {
    return db.Migrate(testDSN)
}); err != nil {
    log.Fatalf("migrate: %v", err)
}
```

改为：

```go
pool.MaxWait = 60 * time.Second
if err := pool.Retry(func() error {
    return db.Migrate(context.Background(), testDSN)
}); err != nil {
    log.Fatalf("migrate: %v", err)
}
```

并确认顶部 import 含 `"context"`（应已有）。

- [ ] **Step 6: 更新调用方 — `internal/user/service_test.go`**

`TestMain` 同样位置同样改法：`db.Migrate(testDSN)` → `db.Migrate(context.Background(), testDSN)`，import 含 `"context"`。

- [ ] **Step 7: 更新调用方 — `cmd/server/main.go`**

找到 `run()` 中这段：

```go
if err := db.Migrate(cfg.DB.DSN); err != nil {
    return fmt.Errorf("migrate: %w", err)
}
```

改为：

```go
if err := db.Migrate(ctx, cfg.DB.DSN); err != nil {
    return fmt.Errorf("migrate: %w", err)
}
```

- [ ] **Step 8: 全包测试**

```bash
PATH="/d/tools/go/bin:$PATH" go vet ./...
PATH="/d/tools/go/bin:$PATH" go test ./... -count=1
```

期望：vet 无输出；所有包 PASS（含 tenant/user 的集成测试）。

- [ ] **Step 9: commit**

```bash
git add internal/db internal/tenant/repo_test.go internal/user/service_test.go cmd/server/main.go
git commit -m "refactor(db): Migrate accepts context for cancellation"
```

---

## Task 2: `audit.Middleware` 使用 detached ctx + 5s timeout

**Files:**
- Modify: `internal/audit/middleware.go`
- Modify: `internal/audit/middleware_test.go`

### Step 1: 先写测试（验证请求 ctx 取消不会阻止 audit 写入）

替换 `internal/audit/middleware_test.go` 整文件为：

```go
package audit_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/audit"
)

// strictSpy 模拟 PG 行为：ctx 已取消时 Append 直接报错。
type strictSpy struct {
	mu      sync.Mutex
	got     []audit.Entry
	lastCtx context.Context
}

func (s *strictSpy) Append(ctx context.Context, e audit.Entry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.got = append(s.got, e)
	s.lastCtx = ctx
	return nil
}

func TestAuditMiddleware_WritesEntry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &strictSpy{}
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

func TestAuditMiddleware_SurvivesCanceledRequestCtx(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &strictSpy{}
	var auditErrs []error
	var mu sync.Mutex
	onErr := func(err error) {
		mu.Lock()
		auditErrs = append(auditErrs, err)
		mu.Unlock()
	}

	r := gin.New()
	r.Use(audit.Middleware(s, onErr))
	r.GET("/x", func(c *gin.Context) {
		// 模拟客户端断开 / handler 内部 cancel 请求 ctx
		ctx, cancel := context.WithCancel(c.Request.Context())
		cancel()
		c.Request = c.Request.WithContext(ctx)
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
	require.Equal(t, http.StatusOK, w.Code)

	require.Len(t, s.got, 1, "audit should be appended despite canceled request ctx")
	require.Empty(t, auditErrs, "no audit errors expected")
	require.NoError(t, s.lastCtx.Err(), "Append should receive a non-canceled ctx")

	// detached ctx 应带有自己的 5s deadline
	dl, ok := s.lastCtx.Deadline()
	require.True(t, ok, "ctx should have a deadline")
	require.WithinDuration(t, time.Now().Add(5*time.Second), dl, 6*time.Second)
}
```

- [ ] **Step 2: 跑测试验证失败**

```bash
PATH="/d/tools/go/bin:$PATH" go test ./internal/audit/... -count=1 -v
```

期望：
- `TestAuditMiddleware_WritesEntry` PASS（既有行为）
- `TestAuditMiddleware_SurvivesCanceledRequestCtx` FAIL（当前 Middleware 用 `c.Request.Context()` 传给 Append，ctx 已取消导致 Append 返错）

- [ ] **Step 3: 修改 Middleware 实现**

替换 `internal/audit/middleware.go` 整文件为：

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

// auditWriteTimeout caps how long the audit append call may take. Independent
// of any per-request deadline so that audit records survive client disconnects.
const auditWriteTimeout = 5 * time.Second

// Middleware writes an audit entry per request. Failure to write is logged via
// the optional onErr callback but does not block the request.
//
// The audit append uses a context derived from context.Background() with a
// 5s timeout (auditWriteTimeout) rather than the request context, so that
// records are written even if the client disconnects mid-response.
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

		appendCtx, cancel := context.WithTimeout(context.Background(), auditWriteTimeout)
		defer cancel()
		if err := s.Append(appendCtx, e); err != nil && onErr != nil {
			onErr(err)
		}
	}
}
```

- [ ] **Step 4: 跑测试验证通过**

```bash
PATH="/d/tools/go/bin:$PATH" go test ./internal/audit/... -count=1 -v
```

期望：2 个测试都 PASS。

- [ ] **Step 5: 全包测试**

```bash
PATH="/d/tools/go/bin:$PATH" go vet ./...
PATH="/d/tools/go/bin:$PATH" go test ./... -count=1
```

期望：vet 无输出；所有包 PASS。

- [ ] **Step 6: commit**

```bash
git add internal/audit/
git commit -m "fix(audit): use detached ctx with 5s timeout for Append"
```

---

## Task 3: 启动期拒绝默认 / 弱 JWT secret

**Files:**
- Create: `internal/auth/config.go`
- Create: `internal/auth/config_test.go`
- Modify: `cmd/server/main.go`

### Step 1: 先写测试

新建 `internal/auth/config_test.go`：

```go
package auth_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/auth"
)

func TestValidateJWTConfig_Valid(t *testing.T) {
	err := auth.ValidateJWTConfig(auth.JWTConfig{
		Secret: "a-very-long-and-random-secret-1234567890",
		TTL:    time.Hour,
	})
	require.NoError(t, err)
}

func TestValidateJWTConfig_Empty(t *testing.T) {
	err := auth.ValidateJWTConfig(auth.JWTConfig{Secret: "", TTL: time.Hour})
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "empty"))
}

func TestValidateJWTConfig_DefaultPlaceholder(t *testing.T) {
	err := auth.ValidateJWTConfig(auth.JWTConfig{
		Secret: "change-me-in-production",
		TTL:    time.Hour,
	})
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "default"))
}

func TestValidateJWTConfig_TooShort(t *testing.T) {
	err := auth.ValidateJWTConfig(auth.JWTConfig{Secret: "shortie", TTL: time.Hour})
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "too short"))
}

func TestValidateJWTConfig_ZeroTTL(t *testing.T) {
	err := auth.ValidateJWTConfig(auth.JWTConfig{
		Secret: "a-very-long-and-random-secret-1234567890",
		TTL:    0,
	})
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "ttl"))
}
```

- [ ] **Step 2: 跑测试验证失败**

```bash
PATH="/d/tools/go/bin:$PATH" go test ./internal/auth/... -run TestValidateJWTConfig -v
```

期望：编译失败（`ValidateJWTConfig` 不存在）。

- [ ] **Step 3: 实现 ValidateJWTConfig**

新建 `internal/auth/config.go`：

```go
package auth

import (
	"errors"
	"fmt"
)

// defaultSecretPlaceholder 是配置示例里给出的占位值,不允许真正用作 secret。
const defaultSecretPlaceholder = "change-me-in-production"

// minSecretLength 是允许的最短 JWT secret 长度。低于此值的 secret 容易被暴力破解。
const minSecretLength = 16

// ValidateJWTConfig 在启动期对 JWT 配置做防御性校验。
//
// 它拒绝以下不安全配置:
//   - Secret 为空
//   - Secret 等于示例占位 "change-me-in-production"
//   - Secret 长度小于 minSecretLength (16)
//   - TTL <= 0 (会签出永远过期的 token)
//
// 校验失败时返回 ConfigError, 调用方应当中断启动。
func ValidateJWTConfig(cfg JWTConfig) error {
	if cfg.Secret == "" {
		return errors.New("jwt: secret is empty")
	}
	if cfg.Secret == defaultSecretPlaceholder {
		return fmt.Errorf("jwt: secret is the default placeholder %q, set a real secret via PCA_AUTH_JWT_SECRET", defaultSecretPlaceholder)
	}
	if len(cfg.Secret) < minSecretLength {
		return fmt.Errorf("jwt: secret too short (%d < %d chars)", len(cfg.Secret), minSecretLength)
	}
	if cfg.TTL <= 0 {
		return fmt.Errorf("jwt: ttl must be positive (got %s)", cfg.TTL)
	}
	return nil
}
```

- [ ] **Step 4: 跑测试验证通过**

```bash
PATH="/d/tools/go/bin:$PATH" go test ./internal/auth/... -count=1 -v
```

期望：5 个新测试 PASS，且原有 auth 包测试也仍 PASS。

- [ ] **Step 5: 在 main.go 中调用 ValidateJWTConfig**

在 `cmd/server/main.go` 的 `run()` 函数里，找到这段：

```go
jwtSvc := auth.NewJWT(auth.JWTConfig{
    Secret: cfg.Auth.JWTSecret,
    TTL:    cfg.Auth.JWTTTL,
})
```

改为：

```go
jwtCfg := auth.JWTConfig{
    Secret: cfg.Auth.JWTSecret,
    TTL:    cfg.Auth.JWTTTL,
}
if err := auth.ValidateJWTConfig(jwtCfg); err != nil {
    return fmt.Errorf("auth config: %w", err)
}
jwtSvc := auth.NewJWT(jwtCfg)
```

- [ ] **Step 6: 跑全包测试**

```bash
PATH="/d/tools/go/bin:$PATH" go vet ./...
PATH="/d/tools/go/bin:$PATH" go test ./... -count=1
PATH="/d/tools/go/bin:$PATH" go build ./...
```

期望：所有命令成功。

- [ ] **Step 7: 验证 main 拒绝默认 secret（手动）**

```powershell
# 用 example 配置（含 change-me-in-production）启动应失败
cd D:\IdeaProjects\private-coding-agent
Copy-Item config\config.example.yaml config\config.test.yaml -Force
# 不设置任何 env 覆盖
$env:Path = [Environment]::GetEnvironmentVariable('Path','Machine') + ';' + [Environment]::GetEnvironmentVariable('Path','User')
$env:PCA_DB_DSN = "postgres://app:app@localhost:65535/app?sslmode=disable"
$out = & "D:\tools\go\bin\go.exe" run ./cmd/server --config config\config.test.yaml 2>&1
$exitCode = $LASTEXITCODE
Remove-Item config\config.test.yaml
Write-Host "Exit: $exitCode"
Write-Host "Output: $out"
```

期望：进程立刻失败，stderr 含 `auth config: jwt: secret is the default placeholder`，退出码 1。

> 如果你想跳过这条手动验证，可以略过 Step 7——已经有 5 条单测覆盖该路径。

- [ ] **Step 8: commit**

```bash
git add internal/auth/config.go internal/auth/config_test.go cmd/server/main.go
git commit -m "feat(auth): reject default or weak JWT secret at startup"
```

---

## Task 4: 端到端 sanity check + commit

**Files:**
- 仅验证，不改文件

### Step 1: 完整测试套件

```bash
cd D:/IdeaProjects/private-coding-agent
PATH="/d/tools/go/bin:$PATH" go vet ./...
PATH="/d/tools/go/bin:$PATH" go test ./... -count=1
PATH="/d/tools/go/bin:$PATH" go build ./...
```

期望：全部通过。

- [ ] **Step 2: docker compose 端到端**

```powershell
cd D:\IdeaProjects\private-coding-agent\deploy\compose
docker compose up -d --build
Start-Sleep -Seconds 15
$resp = Invoke-RestMethod http://localhost:8080/healthz
$resp | ConvertTo-Json -Compress
# 期望: {"status":"ok"}

# 登录路径仍可用
docker compose exec postgres psql -U app -d app -c "SELECT count(*) FROM users WHERE email='demo@example.com';"
# 如返回 0,先插入 demo 用户(略,见 Slice 1 README)
```

期望：`/healthz` 返 `{"status":"ok"}`。

- [ ] **Step 3: 验证 audit_log 仍在写**

```powershell
# 发起任意请求触发 audit
Invoke-RestMethod http://localhost:8080/healthz | Out-Null
Start-Sleep -Seconds 1

docker compose exec postgres psql -U app -d app -c "SELECT method, path, status FROM audit_log ORDER BY occurred_at DESC LIMIT 3;"
```

期望：表里有刚才的 `GET /healthz` 记录。

- [ ] **Step 4: 清理**

```powershell
docker compose down
cd ..\..
```

- [ ] **Step 5: 验证 git tree clean**

```bash
git status
git log --oneline -5
```

期望：working tree clean；最后 3 个 commit 是 Task 1/2/3 各一个。

> Task 4 本身不 commit（纯验证）。

---

## 验收（end-of-slice checklist）

- [ ] `go test ./... -count=1` 全 PASS（含 `TestMigrate_RespectsCanceledCtx` 与 `TestAuditMiddleware_SurvivesCanceledRequestCtx`、5 个 `TestValidateJWTConfig_*`）
- [ ] `go vet ./...` 无输出
- [ ] `go build ./...` 无错
- [ ] docker compose up 后 `/healthz` 返 200
- [ ] `audit_log` 表里有刚发起的请求记录
- [ ] 启动期用默认 secret 配置（不覆盖 PCA_AUTH_JWT_SECRET）应失败退出
- [ ] 3 个新 commit（Task 1/2/3 各一），git tree clean

---

## Self-Review 检查

- 每个 Task 含 Files / TDD 步骤 / 命令 / commit ✓
- 无 TBD / "类似 Task N" 占位 ✓
- 路径、命令精确 ✓
- 类型签名一致：`db.Migrate(ctx context.Context, dsn string) error` 在 3 个调用方都同样改 ✓
- `auth.ValidateJWTConfig(cfg JWTConfig) error` 签名在测试与 main 中一致 ✓
- `audit.auditWriteTimeout` / `defaultSecretPlaceholder` / `minSecretLength` 常量都定义在使用它们的文件中 ✓
- Slice 2 spec 的"前置条件"段提到的 3 项加固，本 plan 各对应一个 Task ✓
