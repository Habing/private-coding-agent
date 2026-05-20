# Slice 3 — Model Gateway Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 交付 `internal/modelgw` 包 + HTTP `/v1/chat/completions` 与 `/v1/embeddings`（OpenAI 兼容），三 provider（Ollama / OpenAI / Claude，含 Anthropic 协议适配 + SSE 流式状态机），PG 持久化 `providers` 与 `model_usage`，docker-compose 含 mock-provider service，E2E 跑通。

**Architecture:** 三层结构。HTTP 层（gin handlers + SSE writer）→ Gateway 编排（validate → Registry.Resolve → Provider 调用 → UsageRecorder 写库）→ Provider 接口三种实现（Ollama/OpenAI 共享代码；Claude 内部做 OpenAI ↔ Anthropic 双向协议转换 + SSE 状态机）。Provider 配置从 PG 读 + 60s 内存缓存 refresh；API key 通过 `api_key_env` 引用环境变量，密钥不入库。

**Tech Stack:**
- 沿用 Slice 1/2 既有：Go 1.26、gin、pgx v5、testify、dockertest、`internal/auth`/`audit`/`db`/`httpx`
- 仅用标准库 `net/http` + `encoding/json` 调用 provider（不引 openai-go / anthropic-sdk-go，避免 vendor lock）
- 测试用 `httptest.Server` 模拟 provider

---

## 前置条件

依赖 Slice 1.5 与 Slice 2 已完成（HEAD = `c4531c1`）。

---

## File Structure

```
internal/modelgw/
  types.go                 公共类型 + 错误 + 上限常量
  types_test.go
  validate.go              ChatRequest / EmbeddingsRequest 校验
  validate_test.go
  provider.go              Provider 接口
  registry.go              ProviderRegistry
  registry_test.go
  repo.go                  ProviderRepo + UsageRepo
  recorder.go              UsageRecorder
  repo_test.go             dockertest 集成
  recorder_test.go
  gateway.go               Gateway 编排
  gateway_test.go          mockProvider 测
  sse.go                   SSE writer helper
  sse_test.go
  handler.go               HTTP handlers
  handler_test.go          mockGateway 测
  provider_openai.go       OpenAI Provider
  provider_openai_test.go  httptest 集成
  provider_ollama.go       Ollama Provider (薄包装)
  provider_ollama_test.go
  claude_translate.go      Anthropic ↔ OpenAI 双向转换 (非流)
  claude_translate_test.go
  claude_stream.go         Anthropic SSE → OpenAI chunks 状态机
  claude_stream_test.go
  provider_claude.go       Claude Provider 整合
  provider_claude_test.go  httptest 集成
  mockserver/              测试与 compose 用的 OpenAI 兼容 mock
    main.go
    Dockerfile

internal/db/migrations/
  0005_create_providers.up.sql
  0005_create_providers.down.sql
  0006_create_model_usage.up.sql
  0006_create_model_usage.down.sql

cmd/server/main.go         装配 Registry / Gateway / Recorder / 路由
deploy/compose/docker-compose.yml  新增 mock-provider service
deploy/compose/test-e2e.sh         扩展 chat/embeddings 验证
README.md                  Slice 3 进度
```

---

## Task 0: Slice 2 carry-over（sandbox stdin 异步 + Exec inspect log + base 镜像 trivy 文档）

**Files:**
- Modify: `internal/sandbox/docker_driver_exec.go`
- Modify: `sandbox/image/README.md`

- [ ] **Step 1: Read 当前 exec 实现，定位 stdin 写入与 inspect 错误处理**

```bash
cd D:/IdeaProjects/private-coding-agent
PATH="/d/tools/go/bin:$PATH" grep -n "opts.Stdin\|ContainerExecInspect" internal/sandbox/docker_driver_exec.go
```

- [ ] **Step 2: 修改 stdin 写入为异步**

打开 `internal/sandbox/docker_driver_exec.go`，找到这段（约 line 58-65）：

```go
if len(opts.Stdin) > 0 {
    _, _ = attached.Conn.Write(opts.Stdin)
    _ = attached.CloseWrite()
}

stdoutBuf := newLimitedBuffer(MaxStreamBytes)
stderrBuf := newLimitedBuffer(MaxStreamBytes)

start := time.Now()
copyErr := make(chan error, 1)
go func() {
    _, err := stdcopy.StdCopy(stdoutBuf, stderrBuf, attached.Reader)
    copyErr <- err
}()
```

改为：

```go
stdoutBuf := newLimitedBuffer(MaxStreamBytes)
stderrBuf := newLimitedBuffer(MaxStreamBytes)

start := time.Now()
copyErr := make(chan error, 1)
go func() {
    _, err := stdcopy.StdCopy(stdoutBuf, stderrBuf, attached.Reader)
    copyErr <- err
}()

// 必须先启 stdcopy goroutine 才写 stdin: 否则 stdin > 64KB 时, daemon
// 反向缓冲被 stderr/stdout 填满会让 Write 阻塞死锁。
if len(opts.Stdin) > 0 {
    go func() {
        _, _ = attached.Conn.Write(opts.Stdin)
        _ = attached.CloseWrite()
    }()
}
```

- [ ] **Step 3: 改 inspect 错误打 log**

定位 `ContainerExecInspect` 调用段（约 line 88-94）：

```go
inspectCtx, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel2()
insp, err := d.cli.ContainerExecInspect(inspectCtx, created.ID)
exitCode := -1
if err == nil {
    exitCode = insp.ExitCode
}
```

改为：

```go
inspectCtx, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel2()
insp, err := d.cli.ContainerExecInspect(inspectCtx, created.ID)
exitCode := -1
if err != nil {
    log.Printf("sandbox exec: inspect %s: %v", created.ID, err)
} else {
    exitCode = insp.ExitCode
}
```

在 import 段补 `"log"`（如未含）。

- [ ] **Step 4: 在 sandbox/image/README.md 末尾追加 trivy 提示**

打开 `sandbox/image/README.md`，末尾追加：

```markdown

## 安全扫描

镜像未在 CI 中跑 trivy / grype。生产部署前建议：

```bash
trivy image pca/sandbox:base
```

发现高危 CVE 应升级 debian 基础或具体包。
```

- [ ] **Step 5: 验证编译 + 全部测试**

```bash
PATH="/d/tools/go/bin:$PATH" go vet ./...
PATH="/d/tools/go/bin:$PATH" go build ./...
PATH="/d/tools/go/bin:$PATH" go test ./... -count=1
```

期望：全 PASS。

> 注：docker_integration 测试需要本地 Docker 与 Redis；可跳过。

- [ ] **Step 6: commit**

```bash
git add internal/sandbox/docker_driver_exec.go sandbox/image/README.md
git commit -m "chore: slice 2 carry-over (async stdin, exec inspect log, trivy docs)"
```

---

## Task 1: `internal/modelgw/types.go` 公共类型

**Files:**
- Create: `internal/modelgw/types.go`
- Create: `internal/modelgw/types_test.go`

- [ ] **Step 1: 写测试 `internal/modelgw/types_test.go`**

```go
package modelgw_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/modelgw"
)

func TestRoleConstants(t *testing.T) {
	require.Equal(t, "system", string(modelgw.RoleSystem))
	require.Equal(t, "user", string(modelgw.RoleUser))
	require.Equal(t, "assistant", string(modelgw.RoleAssistant))
	require.Equal(t, "tool", string(modelgw.RoleTool))
}

func TestLimitConstants(t *testing.T) {
	require.Equal(t, 200, modelgw.MaxMessages)
	require.Equal(t, 256*1024, modelgw.MaxMessageBytes)
	require.Equal(t, 100, modelgw.MaxEmbeddingInput)
	require.Equal(t, 8*1024, modelgw.MaxEmbeddingItem)
	require.Equal(t, 120, modelgw.DefaultTimeoutSec)
}

func TestErrorSentinels(t *testing.T) {
	require.Error(t, modelgw.ErrModelInvalid)
	require.Error(t, modelgw.ErrProviderNotFound)
	require.Error(t, modelgw.ErrProviderUnreachable)
	require.Error(t, modelgw.ErrProviderError)
	require.Error(t, modelgw.ErrUnsupportedFeature)
}

func TestProviderErrorIs(t *testing.T) {
	pe := &modelgw.ProviderError{StatusCode: 503, Body: "x"}
	require.True(t, errors.Is(pe, modelgw.ErrProviderError))
	require.Equal(t, "provider 503: x", pe.Error())
}
```

- [ ] **Step 2: 跑测试验证失败**

```bash
PATH="/d/tools/go/bin:$PATH" go test ./internal/modelgw/...
```

期望：编译失败（package 不存在）。

- [ ] **Step 3: 实现 `internal/modelgw/types.go`**

```go
// Package modelgw provides a Model Gateway abstraction over multiple LLM
// providers (Ollama, OpenAI, Claude) with OpenAI-compatible HTTP endpoints
// at /v1/chat/completions and /v1/embeddings.
package modelgw

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ChatRole 是 OpenAI 协议中的消息角色。
type ChatRole string

const (
	RoleSystem    ChatRole = "system"
	RoleUser      ChatRole = "user"
	RoleAssistant ChatRole = "assistant"
	RoleTool      ChatRole = "tool"
)

// ChatMessage 兼容 OpenAI Chat Completions message。
type ChatMessage struct {
	Role       ChatRole   `json:"role"`
	Content    string     `json:"content"`
	Name       string     `json:"name,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

// ToolCall 是 OpenAI tool calling 格式。
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function ToolCallFunc `json:"function"`
}

type ToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ChatRequest 兼容 OpenAI ChatCompletionRequest 子集。
type ChatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Stream      bool          `json:"stream,omitempty"`
	Temperature *float64      `json:"temperature,omitempty"`
	TopP        *float64      `json:"top_p,omitempty"`
	MaxTokens   *int          `json:"max_tokens,omitempty"`
	Tools       []ToolDef     `json:"tools,omitempty"`
	ToolChoice  any           `json:"tool_choice,omitempty"`
	Stop        []string      `json:"stop,omitempty"`
	Seed        *int          `json:"seed,omitempty"`
}

type ToolDef struct {
	Type     string          `json:"type"`
	Function ToolDefFunction `json:"function"`
}

type ToolDefFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type ChatChoice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type ChatResponse struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []ChatChoice `json:"choices"`
	Usage   Usage        `json:"usage"`
}

type ChatStreamChunk struct {
	ID      string             `json:"id"`
	Object  string             `json:"object"`
	Created int64              `json:"created"`
	Model   string             `json:"model"`
	Choices []ChatStreamChoice `json:"choices"`
	Usage   *Usage             `json:"usage,omitempty"`
}

type ChatStreamChoice struct {
	Index        int             `json:"index"`
	Delta        ChatStreamDelta `json:"delta"`
	FinishReason *string         `json:"finish_reason,omitempty"`
}

type ChatStreamDelta struct {
	Role      ChatRole   `json:"role,omitempty"`
	Content   string     `json:"content,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

type EmbeddingsRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type Embedding struct {
	Index     int       `json:"index"`
	Object    string    `json:"object"`
	Embedding []float64 `json:"embedding"`
}

type EmbeddingsResponse struct {
	Object string      `json:"object"`
	Data   []Embedding `json:"data"`
	Model  string      `json:"model"`
	Usage  Usage       `json:"usage"`
}

// 错误哨兵
var (
	ErrModelInvalid        = errors.New("model: must be 'provider:model'")
	ErrProviderNotFound    = errors.New("provider not found")
	ErrProviderUnreachable = errors.New("provider unreachable")
	ErrProviderError       = errors.New("provider returned error")
	ErrUnsupportedFeature  = errors.New("feature not supported by this provider")
)

// ProviderError 带 HTTP status code 与原始响应体（截断 4 KB）。
type ProviderError struct {
	StatusCode int
	Body       string
}

func (e *ProviderError) Error() string {
	return fmt.Sprintf("provider %d: %s", e.StatusCode, e.Body)
}

func (e *ProviderError) Is(target error) bool {
	return target == ErrProviderError
}

// 上限/默认
const (
	MaxMessages       = 200
	MaxMessageBytes   = 256 * 1024
	MaxEmbeddingInput = 100
	MaxEmbeddingItem  = 8 * 1024
	DefaultTimeoutSec = 120
	MaxProviderBody   = 4 * 1024
	StreamIdleTimeout = 60 * time.Second
	MaxStreamSeconds  = 600 * time.Second
)

// CallEvent 是 UsageRecorder 持久化用的领域对象。
type CallEvent struct {
	TenantID     uuid.UUID
	UserID       uuid.UUID
	ProviderID   uuid.UUID
	ProviderType string
	Model        string
	Action       string
	Stream       bool
	Status       string
	ErrorClass   string
	InputTokens  int
	OutputTokens int
	DurationMS   int64
	OccurredAt   time.Time
}
```

- [ ] **Step 4: 跑测试确认通过**

```bash
PATH="/d/tools/go/bin:$PATH" go test ./internal/modelgw/... -count=1 -v
```

期望：4 测试 PASS。

- [ ] **Step 5: commit**

```bash
git add internal/modelgw/types.go internal/modelgw/types_test.go
git commit -m "feat(modelgw): types, limits, error sentinels"
```

---

## Task 2: migration 0005 + `ProviderRepo`

**Files:**
- Create: `internal/db/migrations/0005_create_providers.up.sql`
- Create: `internal/db/migrations/0005_create_providers.down.sql`
- Create: `internal/modelgw/repo.go`
- Create: `internal/modelgw/repo_test.go`

- [ ] **Step 1: 写迁移**

`internal/db/migrations/0005_create_providers.up.sql`:

```sql
CREATE TABLE providers (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name         TEXT NOT NULL UNIQUE,
    type         TEXT NOT NULL,
    base_url     TEXT NOT NULL,
    api_key_env  TEXT NOT NULL DEFAULT '',
    enabled      BOOLEAN NOT NULL DEFAULT TRUE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX providers_enabled_idx ON providers(enabled) WHERE enabled = TRUE;

INSERT INTO providers (name, type, base_url, api_key_env)
VALUES ('default-mock', 'openai', 'http://mock-provider:8081', '');
```

`internal/db/migrations/0005_create_providers.down.sql`:

```sql
DROP TABLE providers;
```

- [ ] **Step 2: 实现 `internal/modelgw/repo.go`**

```go
package modelgw

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ProviderConfig 是 providers 表行映射。
type ProviderConfig struct {
	ID        uuid.UUID
	Name      string
	Type      string
	BaseURL   string
	APIKeyEnv string
	Enabled   bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ProviderRepo 读 providers 表。
type ProviderRepo struct {
	pool *pgxpool.Pool
}

func NewProviderRepo(pool *pgxpool.Pool) *ProviderRepo {
	return &ProviderRepo{pool: pool}
}

// ListEnabled 返回所有 enabled=true 的 provider。
func (r *ProviderRepo) ListEnabled(ctx context.Context) ([]ProviderConfig, error) {
	rows, err := r.pool.Query(ctx, `
SELECT id, name, type, base_url, api_key_env, enabled, created_at, updated_at
FROM providers WHERE enabled = TRUE
ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list providers: %w", err)
	}
	defer rows.Close()

	var out []ProviderConfig
	for rows.Next() {
		var p ProviderConfig
		if err := rows.Scan(&p.ID, &p.Name, &p.Type, &p.BaseURL,
			&p.APIKeyEnv, &p.Enabled, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan provider: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetByName 用于测试 / 调试。
func (r *ProviderRepo) GetByName(ctx context.Context, name string) (*ProviderConfig, error) {
	row := r.pool.QueryRow(ctx, `
SELECT id, name, type, base_url, api_key_env, enabled, created_at, updated_at
FROM providers WHERE name = $1`, name)
	var p ProviderConfig
	if err := row.Scan(&p.ID, &p.Name, &p.Type, &p.BaseURL,
		&p.APIKeyEnv, &p.Enabled, &p.CreatedAt, &p.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrProviderNotFound
		}
		return nil, fmt.Errorf("get provider: %w", err)
	}
	return &p, nil
}
```

- [ ] **Step 3: 写集成测试 `internal/modelgw/repo_test.go`**

```go
package modelgw_test

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
	"github.com/yourorg/private-coding-agent/internal/modelgw"
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

func TestProviderRepo_ListEnabled_SeedExists(t *testing.T) {
	ctx := context.Background()
	pg, err := pgxpool.New(ctx, testDSN)
	require.NoError(t, err)
	t.Cleanup(pg.Close)

	repo := modelgw.NewProviderRepo(pg)
	list, err := repo.ListEnabled(ctx)
	require.NoError(t, err)
	var found bool
	for _, p := range list {
		if p.Name == "default-mock" {
			found = true
			require.Equal(t, "openai", p.Type)
			require.Equal(t, "http://mock-provider:8081", p.BaseURL)
			require.True(t, p.Enabled)
		}
	}
	require.True(t, found, "default-mock seed must exist")
}

func TestProviderRepo_GetByName_NotFound(t *testing.T) {
	ctx := context.Background()
	pg, err := pgxpool.New(ctx, testDSN)
	require.NoError(t, err)
	t.Cleanup(pg.Close)

	repo := modelgw.NewProviderRepo(pg)
	_, err = repo.GetByName(ctx, "nope-"+fmt.Sprint(time.Now().UnixNano()))
	require.ErrorIs(t, err, modelgw.ErrProviderNotFound)
}
```

- [ ] **Step 4: 跑测试**

```bash
PATH="/d/tools/go/bin:$PATH" go test ./internal/modelgw/... -count=1 -v
```

期望：types 4 测试 + repo 2 测试全 PASS（dockertest 启 PG ~10-20s）。

- [ ] **Step 5: commit**

```bash
git add internal/db/migrations/0005_create_providers.up.sql \
        internal/db/migrations/0005_create_providers.down.sql \
        internal/modelgw/repo.go internal/modelgw/repo_test.go
git commit -m "feat(modelgw): providers migration + ProviderRepo with default-mock seed"
```

---

## Task 3: migration 0006 + `UsageRepo` + `UsageRecorder`

**Files:**
- Create: `internal/db/migrations/0006_create_model_usage.up.sql`
- Create: `internal/db/migrations/0006_create_model_usage.down.sql`
- Modify: `internal/modelgw/repo.go`（追加 UsageRepo）
- Create: `internal/modelgw/recorder.go`
- Create: `internal/modelgw/recorder_test.go`
- Modify: `internal/modelgw/repo_test.go`（追加 UsageRepo 测试）

- [ ] **Step 1: 写迁移**

`internal/db/migrations/0006_create_model_usage.up.sql`:

```sql
CREATE TABLE model_usage (
    id              BIGSERIAL PRIMARY KEY,
    occurred_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    tenant_id       UUID NOT NULL,
    user_id         UUID NOT NULL,
    provider_id     UUID NOT NULL REFERENCES providers(id),
    provider_type   TEXT NOT NULL,
    model           TEXT NOT NULL,
    action          TEXT NOT NULL,
    stream          BOOLEAN NOT NULL,
    status          TEXT NOT NULL,
    error_class     TEXT NOT NULL DEFAULT '',
    input_tokens    INT NOT NULL DEFAULT 0,
    output_tokens   INT NOT NULL DEFAULT 0,
    duration_ms     INT NOT NULL
);

CREATE INDEX model_usage_tenant_time_idx
    ON model_usage(tenant_id, occurred_at DESC);
```

`internal/db/migrations/0006_create_model_usage.down.sql`:

```sql
DROP TABLE model_usage;
```

- [ ] **Step 2: 在 `internal/modelgw/repo.go` 追加 UsageRepo**

在文件末尾追加：

```go
// UsageRepo 写 model_usage。
type UsageRepo struct {
	pool *pgxpool.Pool
}

func NewUsageRepo(pool *pgxpool.Pool) *UsageRepo {
	return &UsageRepo{pool: pool}
}

// Insert 追加一行 model_usage。
func (r *UsageRepo) Insert(ctx context.Context, e CallEvent) error {
	_, err := r.pool.Exec(ctx, `
INSERT INTO model_usage
  (occurred_at, tenant_id, user_id, provider_id, provider_type, model,
   action, stream, status, error_class, input_tokens, output_tokens, duration_ms)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		e.OccurredAt, e.TenantID, e.UserID, e.ProviderID, e.ProviderType, e.Model,
		e.Action, e.Stream, e.Status, e.ErrorClass, e.InputTokens, e.OutputTokens, e.DurationMS)
	if err != nil {
		return fmt.Errorf("insert model_usage: %w", err)
	}
	return nil
}

// CountByTenant 测试 / 运维查询。
func (r *UsageRepo) CountByTenant(ctx context.Context, tenantID uuid.UUID) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM model_usage WHERE tenant_id=$1`, tenantID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count model_usage: %w", err)
	}
	return n, nil
}
```

- [ ] **Step 3: 写 `internal/modelgw/recorder.go`**

```go
package modelgw

import (
	"context"
	"time"
)

// usageWriteTimeout 是 UsageRecorder.Record 写库的硬上限。
const usageWriteTimeout = 5 * time.Second

// UsageRecorder 异步写 model_usage,使用 detached ctx 避免请求 ctx 被取消时丢记录。
type UsageRecorder struct {
	repo  *UsageRepo
	onErr func(error)
}

func NewUsageRecorder(repo *UsageRepo, onErr func(error)) *UsageRecorder {
	return &UsageRecorder{repo: repo, onErr: onErr}
}

// Record 写一行 model_usage。
//
// 不阻塞调用方:用 detached ctx + 5s timeout。失败仅通过 onErr 报告,
// 不影响 HTTP 响应。
func (r *UsageRecorder) Record(e CallEvent) {
	ctx, cancel := context.WithTimeout(context.Background(), usageWriteTimeout)
	defer cancel()
	if err := r.repo.Insert(ctx, e); err != nil && r.onErr != nil {
		r.onErr(err)
	}
}
```

- [ ] **Step 4: 在 `repo_test.go` 追加 UsageRepo 测试**

```go
func TestUsageRepo_InsertAndCount(t *testing.T) {
	ctx := context.Background()
	pg, err := pgxpool.New(ctx, testDSN)
	require.NoError(t, err)
	t.Cleanup(pg.Close)

	provRepo := modelgw.NewProviderRepo(pg)
	prov, err := provRepo.GetByName(ctx, "default-mock")
	require.NoError(t, err)

	tid := uuid.New()
	uid := uuid.New()
	repo := modelgw.NewUsageRepo(pg)
	require.NoError(t, repo.Insert(ctx, modelgw.CallEvent{
		TenantID: tid, UserID: uid,
		ProviderID: prov.ID, ProviderType: "openai", Model: "x",
		Action: "chat", Stream: false, Status: "ok",
		InputTokens: 10, OutputTokens: 20, DurationMS: 100,
		OccurredAt: time.Now(),
	}))

	n, err := repo.CountByTenant(ctx, tid)
	require.NoError(t, err)
	require.Equal(t, 1, n)
}
```

注意 import 段补 `"github.com/google/uuid"`。

- [ ] **Step 5: 写 `internal/modelgw/recorder_test.go`**

```go
package modelgw_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/modelgw"
)

func TestUsageRecorder_SurvivesCanceledCallerCtx(t *testing.T) {
	ctx := context.Background()
	pg, err := pgxpool.New(ctx, testDSN)
	require.NoError(t, err)
	t.Cleanup(pg.Close)

	provRepo := modelgw.NewProviderRepo(pg)
	prov, err := provRepo.GetByName(ctx, "default-mock")
	require.NoError(t, err)

	var errs []error
	var mu sync.Mutex
	rec := modelgw.NewUsageRecorder(modelgw.NewUsageRepo(pg), func(err error) {
		mu.Lock()
		errs = append(errs, err)
		mu.Unlock()
	})

	tid := uuid.New()
	// 即使我们不传任何 ctx,Record 也用 detached ctx
	rec.Record(modelgw.CallEvent{
		TenantID: tid, UserID: uuid.New(),
		ProviderID: prov.ID, ProviderType: "openai", Model: "x",
		Action: "chat", Status: "ok", DurationMS: 1,
		OccurredAt: time.Now(),
	})

	require.Empty(t, errs)
	n, err := modelgw.NewUsageRepo(pg).CountByTenant(ctx, tid)
	require.NoError(t, err)
	require.Equal(t, 1, n)
}
```

- [ ] **Step 6: 跑测试**

```bash
PATH="/d/tools/go/bin:$PATH" go vet ./...
PATH="/d/tools/go/bin:$PATH" go test ./internal/modelgw/... -count=1 -v
```

期望：所有现有测试 + UsageRepo + UsageRecorder PASS。

- [ ] **Step 7: commit**

```bash
git add internal/db/migrations/0006_create_model_usage.up.sql \
        internal/db/migrations/0006_create_model_usage.down.sql \
        internal/modelgw/repo.go internal/modelgw/repo_test.go \
        internal/modelgw/recorder.go internal/modelgw/recorder_test.go
git commit -m "feat(modelgw): model_usage migration + UsageRepo + UsageRecorder"
```

---

## Task 4: `validate.go` 请求校验

**Files:**
- Create: `internal/modelgw/validate.go`
- Create: `internal/modelgw/validate_test.go`

- [ ] **Step 1: 写测试**

```go
package modelgw_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/modelgw"
)

func validChat() modelgw.ChatRequest {
	return modelgw.ChatRequest{
		Model: "openai:gpt-4o",
		Messages: []modelgw.ChatMessage{
			{Role: modelgw.RoleUser, Content: "hi"},
		},
	}
}

func TestValidateChatRequest_OK(t *testing.T) {
	require.NoError(t, modelgw.ValidateChatRequest(validChat()))
}

func TestValidateChatRequest_BadModel(t *testing.T) {
	r := validChat()
	r.Model = "no-prefix"
	require.ErrorIs(t, modelgw.ValidateChatRequest(r), modelgw.ErrModelInvalid)
}

func TestValidateChatRequest_EmptyMessages(t *testing.T) {
	r := validChat()
	r.Messages = nil
	require.Error(t, modelgw.ValidateChatRequest(r))
}

func TestValidateChatRequest_TooManyMessages(t *testing.T) {
	r := validChat()
	r.Messages = make([]modelgw.ChatMessage, modelgw.MaxMessages+1)
	for i := range r.Messages {
		r.Messages[i] = modelgw.ChatMessage{Role: modelgw.RoleUser, Content: "x"}
	}
	require.Error(t, modelgw.ValidateChatRequest(r))
}

func TestValidateChatRequest_MessageTooLarge(t *testing.T) {
	r := validChat()
	r.Messages[0].Content = string(make([]byte, modelgw.MaxMessageBytes+1))
	require.Error(t, modelgw.ValidateChatRequest(r))
}

func TestValidateEmbeddingsRequest_OK(t *testing.T) {
	require.NoError(t, modelgw.ValidateEmbeddingsRequest(modelgw.EmbeddingsRequest{
		Model: "openai:text-embedding-3-small",
		Input: []string{"hi"},
	}))
}

func TestValidateEmbeddingsRequest_BadModel(t *testing.T) {
	r := modelgw.EmbeddingsRequest{Model: "noprefix", Input: []string{"hi"}}
	require.ErrorIs(t, modelgw.ValidateEmbeddingsRequest(r), modelgw.ErrModelInvalid)
}

func TestValidateEmbeddingsRequest_EmptyInput(t *testing.T) {
	r := modelgw.EmbeddingsRequest{Model: "openai:x", Input: nil}
	require.Error(t, modelgw.ValidateEmbeddingsRequest(r))
}

func TestValidateEmbeddingsRequest_TooManyInputs(t *testing.T) {
	in := make([]string, modelgw.MaxEmbeddingInput+1)
	for i := range in {
		in[i] = "x"
	}
	r := modelgw.EmbeddingsRequest{Model: "openai:x", Input: in}
	require.Error(t, modelgw.ValidateEmbeddingsRequest(r))
}

func TestValidateEmbeddingsRequest_ItemTooLarge(t *testing.T) {
	r := modelgw.EmbeddingsRequest{Model: "openai:x",
		Input: []string{string(make([]byte, modelgw.MaxEmbeddingItem+1))}}
	require.Error(t, modelgw.ValidateEmbeddingsRequest(r))
}
```

- [ ] **Step 2: 实现 `internal/modelgw/validate.go`**

```go
package modelgw

import (
	"fmt"
	"strings"
)

// ValidateChatRequest 校验 ChatRequest 必填与上限。
func ValidateChatRequest(r ChatRequest) error {
	if err := validateModelString(r.Model); err != nil {
		return err
	}
	if len(r.Messages) == 0 {
		return fmt.Errorf("validation: messages required")
	}
	if len(r.Messages) > MaxMessages {
		return fmt.Errorf("validation: messages count %d > %d", len(r.Messages), MaxMessages)
	}
	for i, m := range r.Messages {
		if len(m.Content) > MaxMessageBytes {
			return fmt.Errorf("validation: messages[%d].content %d > %d bytes", i, len(m.Content), MaxMessageBytes)
		}
	}
	return nil
}

// ValidateEmbeddingsRequest 校验 EmbeddingsRequest。
func ValidateEmbeddingsRequest(r EmbeddingsRequest) error {
	if err := validateModelString(r.Model); err != nil {
		return err
	}
	if len(r.Input) == 0 {
		return fmt.Errorf("validation: input required")
	}
	if len(r.Input) > MaxEmbeddingInput {
		return fmt.Errorf("validation: input count %d > %d", len(r.Input), MaxEmbeddingInput)
	}
	for i, s := range r.Input {
		if len(s) > MaxEmbeddingItem {
			return fmt.Errorf("validation: input[%d] %d > %d bytes", i, len(s), MaxEmbeddingItem)
		}
	}
	return nil
}

func validateModelString(s string) error {
	i := strings.IndexByte(s, ':')
	if i <= 0 || i == len(s)-1 {
		return ErrModelInvalid
	}
	return nil
}
```

- [ ] **Step 3: 跑测试**

```bash
PATH="/d/tools/go/bin:$PATH" go test ./internal/modelgw/... -run TestValidate -count=1 -v
```

期望：10 测试 PASS。

- [ ] **Step 4: commit**

```bash
git add internal/modelgw/validate.go internal/modelgw/validate_test.go
git commit -m "feat(modelgw): validate ChatRequest/EmbeddingsRequest"
```

---

## Task 5: `Provider` 接口 + `ProviderRegistry`

**Files:**
- Create: `internal/modelgw/provider.go`
- Create: `internal/modelgw/registry.go`
- Create: `internal/modelgw/registry_test.go`

- [ ] **Step 1: 写 `provider.go`**

```go
package modelgw

import (
	"context"

	"github.com/google/uuid"
)

// Provider 是 LLM 后端抽象。所有方法协程安全。
// model 参数是裸 model 名(无 provider 前缀);Gateway 在 Resolve 后传入。
type Provider interface {
	ID() uuid.UUID
	Type() string
	Name() string

	ChatCompletion(ctx context.Context, req ChatRequest, model string) (*ChatResponse, error)
	ChatCompletionStream(ctx context.Context, req ChatRequest, model string,
		yield func(ChatStreamChunk) error) error
	Embeddings(ctx context.Context, req EmbeddingsRequest, model string) (*EmbeddingsResponse, error)
}

// ProviderFactory 根据 ProviderConfig 构造 Provider 实例。
// 注册时一次性映射到 Type;Slice 3 含 "openai"/"ollama"/"claude" 三种工厂。
type ProviderFactory func(cfg ProviderConfig) (Provider, error)
```

- [ ] **Step 2: 写 `registry.go`**

```go
package modelgw

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// ProviderRegistry 维护活跃 provider 实例缓存。
type ProviderRegistry struct {
	repo            *ProviderRepo
	factories       map[string]ProviderFactory
	refreshInterval time.Duration

	mu     sync.RWMutex
	byName map[string]Provider
}

// NewProviderRegistry 构造。factories 由调用方注入(避免 import cycle)。
func NewProviderRegistry(repo *ProviderRepo, factories map[string]ProviderFactory,
	refresh time.Duration) *ProviderRegistry {
	return &ProviderRegistry{
		repo: repo, factories: factories,
		refreshInterval: refresh,
		byName:          map[string]Provider{},
	}
}

// Start 立刻 load 一次;失败返 error。后台 refresh 由 Run 启动。
func (r *ProviderRegistry) Start(ctx context.Context) error {
	return r.reload(ctx)
}

// Run 阻塞:每 refreshInterval 刷新一次,直到 ctx 取消。
// 调用方应 `go reg.Run(ctx)`。
func (r *ProviderRegistry) Run(ctx context.Context) {
	t := time.NewTicker(r.refreshInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := r.reload(ctx); err != nil {
				log.Printf("provider registry refresh: %v", err)
			}
		}
	}
}

func (r *ProviderRegistry) reload(ctx context.Context) error {
	configs, err := r.repo.ListEnabled(ctx)
	if err != nil {
		return fmt.Errorf("list providers: %w", err)
	}

	r.mu.RLock()
	old := r.byName
	r.mu.RUnlock()

	next := make(map[string]Provider, len(configs))
	for _, cfg := range configs {
		// 复用已有实例 (避免每次 refresh 重建连接池/客户端)
		if existing, ok := old[cfg.Name]; ok && existing.ID() == cfg.ID && existing.Type() == cfg.Type {
			next[cfg.Name] = existing
			continue
		}
		factory, ok := r.factories[cfg.Type]
		if !ok {
			log.Printf("provider registry: no factory for type %q (provider %q)", cfg.Type, cfg.Name)
			continue
		}
		p, err := factory(cfg)
		if err != nil {
			log.Printf("provider registry: factory %q failed: %v", cfg.Name, err)
			continue
		}
		next[cfg.Name] = p
	}

	r.mu.Lock()
	r.byName = next
	r.mu.Unlock()
	return nil
}

// Resolve 解析 "provider:model" → (Provider, model)。
// 冒号之后的全部内容视作 model(Ollama 模型名可含冒号,如 "qwen2.5:7b")。
func (r *ProviderRegistry) Resolve(modelStr string) (Provider, string, error) {
	i := strings.IndexByte(modelStr, ':')
	if i <= 0 || i == len(modelStr)-1 {
		return nil, "", ErrModelInvalid
	}
	providerName, model := modelStr[:i], modelStr[i+1:]
	r.mu.RLock()
	p, ok := r.byName[providerName]
	r.mu.RUnlock()
	if !ok {
		return nil, "", ErrProviderNotFound
	}
	return p, model, nil
}
```

- [ ] **Step 3: 写 `registry_test.go`（用 fakeProvider 而非真 provider）**

```go
package modelgw_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/modelgw"
)

// fakeProvider 满足 Provider,但所有 Call 方法 panic;只测 Resolve 不调用业务。
type fakeProvider struct {
	id    uuid.UUID
	typ   string
	name  string
}

func (f fakeProvider) ID() uuid.UUID { return f.id }
func (f fakeProvider) Type() string  { return f.typ }
func (f fakeProvider) Name() string  { return f.name }
func (f fakeProvider) ChatCompletion(context.Context, modelgw.ChatRequest, string) (*modelgw.ChatResponse, error) {
	panic("not for resolve tests")
}
func (f fakeProvider) ChatCompletionStream(context.Context, modelgw.ChatRequest, string,
	func(modelgw.ChatStreamChunk) error) error {
	panic("not for resolve tests")
}
func (f fakeProvider) Embeddings(context.Context, modelgw.EmbeddingsRequest, string) (*modelgw.EmbeddingsResponse, error) {
	panic("not for resolve tests")
}

func TestResolve_GoodModelStrings(t *testing.T) {
	cases := []struct {
		in   string
		want string // expected model (after prefix)
	}{
		{"openai:gpt-4o", "gpt-4o"},
		{"ollama:qwen2.5:7b", "qwen2.5:7b"},
		{"claude:claude-sonnet-4-5", "claude-sonnet-4-5"},
	}
	// 用 registry 内部 byName seed 跳过 reload
	reg := newRegistryWithSeed(map[string]modelgw.Provider{
		"openai": fakeProvider{id: uuid.New(), typ: "openai", name: "openai"},
		"ollama": fakeProvider{id: uuid.New(), typ: "ollama", name: "ollama"},
		"claude": fakeProvider{id: uuid.New(), typ: "claude", name: "claude"},
	})
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			_, model, err := reg.Resolve(c.in)
			require.NoError(t, err)
			require.Equal(t, c.want, model)
		})
	}
}

func TestResolve_BadModelStrings(t *testing.T) {
	reg := newRegistryWithSeed(nil)
	for _, in := range []string{"", "noprefix", ":only-colon", "prefix:"} {
		_, _, err := reg.Resolve(in)
		require.ErrorIs(t, err, modelgw.ErrModelInvalid, "input: %q", in)
	}
}

func TestResolve_UnknownProvider(t *testing.T) {
	reg := newRegistryWithSeed(map[string]modelgw.Provider{
		"openai": fakeProvider{id: uuid.New(), typ: "openai", name: "openai"},
	})
	_, _, err := reg.Resolve("missing:m")
	require.ErrorIs(t, err, modelgw.ErrProviderNotFound)
}

// 用 dockertest PG seed 真 reload。
func TestRegistry_Start_LoadsSeed(t *testing.T) {
	ctx := context.Background()
	pg, err := pgxpool.New(ctx, testDSN)
	require.NoError(t, err)
	t.Cleanup(pg.Close)

	repo := modelgw.NewProviderRepo(pg)
	reg := modelgw.NewProviderRegistry(repo, map[string]modelgw.ProviderFactory{
		"openai": func(cfg modelgw.ProviderConfig) (modelgw.Provider, error) {
			return fakeProvider{id: cfg.ID, typ: cfg.Type, name: cfg.Name}, nil
		},
	}, time.Minute)
	require.NoError(t, reg.Start(ctx))

	_, _, err = reg.Resolve("default-mock:gpt-4o")
	require.NoError(t, err)
}
```

补一个 helper（registry 内部 byName 暴露用，纯测试 helper）：在 `registry_test.go` 顶部：

```go
import "github.com/jackc/pgx/v5/pgxpool"

// newRegistryWithSeed 跳过 PG / factory 直接给 registry 注入 byName。
// 通过 NewProviderRegistry + reflection-free 的方式不行,所以这里用 Reload 内部路径。
// 但实际上 byName 是私有字段,只能通过 reload 注入。
// 简化:用一个零 repo / 空 factories 的 registry, 然后 manual call Resolve 走 mu。
// 解决方案:在 registry.go 内加一个 SeedForTest(map[string]Provider) 包级测试 helper。
```

> 由于 `byName` 私有，需要在 `registry.go` 加一个测试用接口。在 `registry.go` 末尾加：

```go
// SeedForTest 仅用于测试:直接把 providers 填进缓存,绕过 PG reload。
// 不要在生产代码调用。
func (r *ProviderRegistry) SeedForTest(byName map[string]Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byName = byName
}
```

然后 `newRegistryWithSeed`：

```go
func newRegistryWithSeed(byName map[string]modelgw.Provider) *modelgw.ProviderRegistry {
	r := modelgw.NewProviderRegistry(nil, nil, time.Minute)
	r.SeedForTest(byName)
	return r
}
```

- [ ] **Step 4: 跑测试**

```bash
PATH="/d/tools/go/bin:$PATH" go vet ./...
PATH="/d/tools/go/bin:$PATH" go test ./internal/modelgw/... -count=1 -v
```

期望：所有现有 + 5 个 registry 测试 PASS。

- [ ] **Step 5: commit**

```bash
git add internal/modelgw/provider.go internal/modelgw/registry.go internal/modelgw/registry_test.go
git commit -m "feat(modelgw): Provider interface + ProviderRegistry"
```

---

## Task 6: `OpenAIProvider`（含 Ollama OpenAI-compat 复用）

**Files:**
- Create: `internal/modelgw/provider_openai.go`
- Create: `internal/modelgw/provider_openai_test.go`

- [ ] **Step 1: 实现 `provider_openai.go`**

```go
package modelgw

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
)

// OpenAIProvider 通过 HTTP 直连 OpenAI 兼容端点。
// Ollama 0.4+ 提供同样的 /v1/* 路径,所以 OllamaProvider 直接复用本结构。
type OpenAIProvider struct {
	id        uuid.UUID
	name      string
	typ       string // "openai" / "ollama" (用于 record)
	baseURL   string
	apiKeyEnv string
	client    *http.Client
}

func NewOpenAIProvider(cfg ProviderConfig) (*OpenAIProvider, error) {
	return &OpenAIProvider{
		id:        cfg.ID,
		name:      cfg.Name,
		typ:       cfg.Type, // 调用方传 "openai" 或 "ollama"
		baseURL:   strings.TrimRight(cfg.BaseURL, "/"),
		apiKeyEnv: cfg.APIKeyEnv,
		client:    &http.Client{Timeout: time.Duration(DefaultTimeoutSec) * time.Second},
	}, nil
}

func (p *OpenAIProvider) ID() uuid.UUID { return p.id }
func (p *OpenAIProvider) Type() string  { return p.typ }
func (p *OpenAIProvider) Name() string  { return p.name }

func (p *OpenAIProvider) apiKey() (string, error) {
	if p.apiKeyEnv == "" {
		return "", nil
	}
	v := os.Getenv(p.apiKeyEnv)
	if v == "" {
		return "", fmt.Errorf("api key env %q is empty", p.apiKeyEnv)
	}
	return v, nil
}

func (p *OpenAIProvider) newHTTPReq(ctx context.Context, method, path string, body any) (*http.Request, error) {
	var buf io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		buf = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, p.baseURL+path, buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	key, err := p.apiKey()
	if err != nil {
		return nil, err
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	return req, nil
}

func (p *OpenAIProvider) ChatCompletion(ctx context.Context, req ChatRequest, model string) (*ChatResponse, error) {
	upstream := req
	upstream.Model = model
	upstream.Stream = false

	hreq, err := p.newHTTPReq(ctx, http.MethodPost, "/v1/chat/completions", upstream)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Do(hreq)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProviderUnreachable, err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, int64(MaxProviderBody)))
	if resp.StatusCode >= 400 {
		return nil, &ProviderError{StatusCode: resp.StatusCode, Body: string(body)}
	}

	var out ChatResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &out, nil
}

func (p *OpenAIProvider) ChatCompletionStream(ctx context.Context, req ChatRequest, model string,
	yield func(ChatStreamChunk) error) error {
	upstream := req
	upstream.Model = model
	upstream.Stream = true
	// OpenAI 需要显式打开 usage 在末帧
	type withUsage struct {
		ChatRequest
		StreamOptions map[string]any `json:"stream_options"`
	}
	bodyVal := withUsage{ChatRequest: upstream, StreamOptions: map[string]any{"include_usage": true}}

	hreq, err := p.newHTTPReq(ctx, http.MethodPost, "/v1/chat/completions", bodyVal)
	if err != nil {
		return err
	}
	hreq.Header.Set("Accept", "text/event-stream")
	resp, err := p.client.Do(hreq)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrProviderUnreachable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, int64(MaxProviderBody)))
		return &ProviderError{StatusCode: resp.StatusCode, Body: string(body)}
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			return nil
		}
		var chunk ChatStreamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue // 跳过坏帧
		}
		if err := yield(chunk); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("%w: %v", ErrProviderUnreachable, err)
	}
	return nil
}

func (p *OpenAIProvider) Embeddings(ctx context.Context, req EmbeddingsRequest, model string) (*EmbeddingsResponse, error) {
	upstream := req
	upstream.Model = model

	hreq, err := p.newHTTPReq(ctx, http.MethodPost, "/v1/embeddings", upstream)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Do(hreq)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProviderUnreachable, err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, int64(MaxProviderBody)))
	if resp.StatusCode >= 400 {
		return nil, &ProviderError{StatusCode: resp.StatusCode, Body: string(body)}
	}

	var out EmbeddingsResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &out, nil
}
```

- [ ] **Step 2: 写测试 `provider_openai_test.go`**

```go
package modelgw_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/modelgw"
)

func newOpenAIWithServer(t *testing.T, handler http.HandlerFunc) (*modelgw.OpenAIProvider, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	p, err := modelgw.NewOpenAIProvider(modelgw.ProviderConfig{
		ID: uuid.New(), Name: "test", Type: "openai", BaseURL: srv.URL,
	})
	require.NoError(t, err)
	return p, srv
}

func TestOpenAI_ChatCompletion_OK(t *testing.T) {
	p, _ := newOpenAIWithServer(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/chat/completions", r.URL.Path)
		_ = json.NewEncoder(w).Encode(modelgw.ChatResponse{
			ID: "x", Object: "chat.completion", Model: "gpt-4o",
			Choices: []modelgw.ChatChoice{{
				Index: 0, FinishReason: "stop",
				Message: modelgw.ChatMessage{Role: modelgw.RoleAssistant, Content: "hi"},
			}},
			Usage: modelgw.Usage{PromptTokens: 5, CompletionTokens: 1, TotalTokens: 6},
		})
	})
	out, err := p.ChatCompletion(context.Background(),
		modelgw.ChatRequest{Messages: []modelgw.ChatMessage{{Role: modelgw.RoleUser, Content: "hi"}}},
		"gpt-4o")
	require.NoError(t, err)
	require.Equal(t, "hi", out.Choices[0].Message.Content)
	require.Equal(t, 6, out.Usage.TotalTokens)
}

func TestOpenAI_ChatCompletion_5xx(t *testing.T) {
	p, _ := newOpenAIWithServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	_, err := p.ChatCompletion(context.Background(),
		modelgw.ChatRequest{Messages: []modelgw.ChatMessage{{Role: modelgw.RoleUser, Content: "hi"}}},
		"gpt-4o")
	require.Error(t, err)
	var pe *modelgw.ProviderError
	require.ErrorAs(t, err, &pe)
	require.Equal(t, 500, pe.StatusCode)
}

func TestOpenAI_ChatCompletion_Unreachable(t *testing.T) {
	p, err := modelgw.NewOpenAIProvider(modelgw.ProviderConfig{
		ID: uuid.New(), Name: "test", Type: "openai",
		BaseURL: "http://127.0.0.1:1",
	})
	require.NoError(t, err)
	_, err = p.ChatCompletion(context.Background(),
		modelgw.ChatRequest{Messages: []modelgw.ChatMessage{{Role: modelgw.RoleUser, Content: "hi"}}},
		"gpt-4o")
	require.ErrorIs(t, err, modelgw.ErrProviderUnreachable)
}

func TestOpenAI_Stream_ParsesChunks(t *testing.T) {
	p, _ := newOpenAIWithServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		write := func(s string) { _, _ = w.Write([]byte(s)); fl.Flush() }

		write("data: " + mustJSON(modelgw.ChatStreamChunk{
			ID: "1", Object: "chat.completion.chunk", Model: "gpt-4o",
			Choices: []modelgw.ChatStreamChoice{{Index: 0,
				Delta: modelgw.ChatStreamDelta{Content: "hi"},
			}},
		}) + "\n\n")
		write("data: " + mustJSON(modelgw.ChatStreamChunk{
			ID: "1", Object: "chat.completion.chunk", Model: "gpt-4o",
			Choices: []modelgw.ChatStreamChoice{},
			Usage:   &modelgw.Usage{PromptTokens: 5, CompletionTokens: 1, TotalTokens: 6},
		}) + "\n\n")
		write("data: [DONE]\n\n")
	})

	var got []modelgw.ChatStreamChunk
	err := p.ChatCompletionStream(context.Background(),
		modelgw.ChatRequest{Messages: []modelgw.ChatMessage{{Role: modelgw.RoleUser, Content: "hi"}}},
		"gpt-4o",
		func(c modelgw.ChatStreamChunk) error { got = append(got, c); return nil })
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, "hi", got[0].Choices[0].Delta.Content)
	require.NotNil(t, got[1].Usage)
	require.Equal(t, 6, got[1].Usage.TotalTokens)
}

func TestOpenAI_Stream_YieldErrorStops(t *testing.T) {
	p, _ := newOpenAIWithServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		for i := 0; i < 5; i++ {
			_, _ = fmt.Fprintf(w, "data: %s\n\n",
				mustJSON(modelgw.ChatStreamChunk{Choices: []modelgw.ChatStreamChoice{{
					Delta: modelgw.ChatStreamDelta{Content: "x"},
				}}}))
			fl.Flush()
		}
	})

	myErr := errors.New("client gone")
	got := 0
	err := p.ChatCompletionStream(context.Background(),
		modelgw.ChatRequest{Messages: []modelgw.ChatMessage{{Role: modelgw.RoleUser, Content: "hi"}}},
		"gpt-4o",
		func(c modelgw.ChatStreamChunk) error {
			got++
			if got == 2 {
				return myErr
			}
			return nil
		})
	require.ErrorIs(t, err, myErr)
	require.Equal(t, 2, got)
}

func TestOpenAI_Embeddings_OK(t *testing.T) {
	p, _ := newOpenAIWithServer(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/embeddings", r.URL.Path)
		_ = json.NewEncoder(w).Encode(modelgw.EmbeddingsResponse{
			Object: "list",
			Data: []modelgw.Embedding{{Index: 0, Object: "embedding", Embedding: []float64{0.1, 0.2}}},
			Model: "text-embedding-3-small",
			Usage: modelgw.Usage{PromptTokens: 1, TotalTokens: 1},
		})
	})
	out, err := p.Embeddings(context.Background(),
		modelgw.EmbeddingsRequest{Input: []string{"hi"}}, "text-embedding-3-small")
	require.NoError(t, err)
	require.Len(t, out.Data, 1)
	require.Equal(t, []float64{0.1, 0.2}, out.Data[0].Embedding)
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// silence unused
var _ = strings.TrimSpace
```

- [ ] **Step 3: 跑测试**

```bash
PATH="/d/tools/go/bin:$PATH" go test ./internal/modelgw/... -count=1 -v -run TestOpenAI
```

期望：6 测试 PASS。

- [ ] **Step 4: commit**

```bash
git add internal/modelgw/provider_openai.go internal/modelgw/provider_openai_test.go
git commit -m "feat(modelgw): OpenAIProvider (chat / stream / embeddings) over net/http"
```

---

## Task 7: `OllamaProvider`（薄包装）

**Files:**
- Create: `internal/modelgw/provider_ollama.go`
- Create: `internal/modelgw/provider_ollama_test.go`

- [ ] **Step 1: 实现 `provider_ollama.go`**

```go
package modelgw

// NewOllamaProvider 等价于 OpenAIProvider,但 Type() 返 "ollama"。
// Ollama 0.4+ 在 /v1/* 提供 OpenAI 兼容端点;请求/响应字段一致。
//
// 与 OpenAI 的实际差异:
//   - 通常无 API key (api_key_env 为空)
//   - 模型名格式 "qwen2.5:7b" 含冒号 (Registry.Resolve 已正确处理)
//   - usage 字段语义略有差异: input_tokens/output_tokens 可能为 0 (取决于版本)
//
// 没有差异需要写新代码;直接复用 OpenAIProvider。
func NewOllamaProvider(cfg ProviderConfig) (*OpenAIProvider, error) {
	cfg.Type = "ollama"
	return NewOpenAIProvider(cfg)
}
```

- [ ] **Step 2: 写测试**

```go
package modelgw_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/modelgw"
)

func TestOllama_TypeAndName(t *testing.T) {
	p, err := modelgw.NewOllamaProvider(modelgw.ProviderConfig{
		ID: uuid.New(), Name: "local-ollama", Type: "anything-overridden",
		BaseURL: "http://localhost:11434",
	})
	require.NoError(t, err)
	require.Equal(t, "ollama", p.Type())
	require.Equal(t, "local-ollama", p.Name())
}

func TestOllama_ChatCompletion_NoAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Empty(t, r.Header.Get("Authorization"))
		_ = json.NewEncoder(w).Encode(modelgw.ChatResponse{
			ID: "x", Object: "chat.completion", Model: "qwen2.5:7b",
			Choices: []modelgw.ChatChoice{{
				Index: 0, FinishReason: "stop",
				Message: modelgw.ChatMessage{Role: modelgw.RoleAssistant, Content: "yo"},
			}},
		})
	}))
	defer srv.Close()

	p, err := modelgw.NewOllamaProvider(modelgw.ProviderConfig{
		ID: uuid.New(), Name: "ollama", BaseURL: srv.URL, APIKeyEnv: "",
	})
	require.NoError(t, err)
	out, err := p.ChatCompletion(context.Background(),
		modelgw.ChatRequest{Messages: []modelgw.ChatMessage{{Role: modelgw.RoleUser, Content: "hi"}}},
		"qwen2.5:7b")
	require.NoError(t, err)
	require.Equal(t, "yo", out.Choices[0].Message.Content)
}
```

- [ ] **Step 3: 跑测试 + commit**

```bash
PATH="/d/tools/go/bin:$PATH" go test ./internal/modelgw/... -count=1 -run TestOllama -v
```

```bash
git add internal/modelgw/provider_ollama.go internal/modelgw/provider_ollama_test.go
git commit -m "feat(modelgw): OllamaProvider as thin OpenAIProvider wrapper"
```

---

## Task 8: Claude 协议适配（非流式）

**Files:**
- Create: `internal/modelgw/claude_translate.go`
- Create: `internal/modelgw/claude_translate_test.go`

- [ ] **Step 1: 写 `claude_translate.go`**

```go
package modelgw

import (
	"encoding/json"
	"strings"
)

// Anthropic Messages API 私有类型;不导出。

type anthropicTextBlock struct {
	Type string `json:"type"` // "text"
	Text string `json:"text"`
}
type anthropicToolUseBlock struct {
	Type  string          `json:"type"`  // "tool_use"
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}
type anthropicToolResultBlock struct {
	Type      string `json:"type"`        // "tool_result"
	ToolUseID string `json:"tool_use_id"`
	Content   string `json:"content"`
}

// anthropicBlock 是一个 type-tagged union。Marshal 时手动序列化对应子类型;
// 解析时用 anthropicBlockRaw 先读 type 字段。
type anthropicBlock struct {
	Text       *anthropicTextBlock
	ToolUse    *anthropicToolUseBlock
	ToolResult *anthropicToolResultBlock
}

func (b anthropicBlock) MarshalJSON() ([]byte, error) {
	switch {
	case b.Text != nil:
		return json.Marshal(b.Text)
	case b.ToolUse != nil:
		return json.Marshal(b.ToolUse)
	case b.ToolResult != nil:
		return json.Marshal(b.ToolResult)
	}
	return []byte("null"), nil
}

type anthropicMessage struct {
	Role    string           `json:"role"` // "user" / "assistant"
	Content []anthropicBlock `json:"content"`
}

type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
}

type anthropicMessagesReq struct {
	Model       string             `json:"model"`
	Messages    []anthropicMessage `json:"messages"`
	System      string             `json:"system,omitempty"`
	MaxTokens   int                `json:"max_tokens"`
	Temperature *float64           `json:"temperature,omitempty"`
	TopP        *float64           `json:"top_p,omitempty"`
	Stop        []string           `json:"stop_sequences,omitempty"`
	Stream      bool               `json:"stream,omitempty"`
	Tools       []anthropicTool    `json:"tools,omitempty"`
}

// anthropicRespBlock 用 raw 解 type 后分发。
type anthropicRespBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type anthropicMessagesResp struct {
	ID         string               `json:"id"`
	Type       string               `json:"type"` // "message"
	Role       string               `json:"role"` // "assistant"
	Content    []anthropicRespBlock `json:"content"`
	Model      string               `json:"model"`
	StopReason string               `json:"stop_reason"`
	Usage      anthropicUsage       `json:"usage"`
}

// ToAnthropicReq 把 OpenAI ChatRequest 转 Anthropic Messages 请求。
// model 参数是裸 model 名(无 provider 前缀)。
func ToAnthropicReq(in ChatRequest, model string) anthropicMessagesReq {
	out := anthropicMessagesReq{
		Model:       model,
		Temperature: in.Temperature,
		TopP:        in.TopP,
		Stop:        in.Stop,
		Stream:      in.Stream,
	}
	if in.MaxTokens != nil {
		out.MaxTokens = *in.MaxTokens
	} else {
		out.MaxTokens = 4096
	}

	var systems []string
	for _, m := range in.Messages {
		switch m.Role {
		case RoleSystem:
			systems = append(systems, m.Content)
		case RoleTool:
			out.Messages = append(out.Messages, anthropicMessage{
				Role: "user",
				Content: []anthropicBlock{{
					ToolResult: &anthropicToolResultBlock{
						Type: "tool_result", ToolUseID: m.ToolCallID, Content: m.Content,
					},
				}},
			})
		default:
			blocks := []anthropicBlock{}
			if m.Content != "" {
				blocks = append(blocks, anthropicBlock{
					Text: &anthropicTextBlock{Type: "text", Text: m.Content},
				})
			}
			for _, tc := range m.ToolCalls {
				var input json.RawMessage = []byte(tc.Function.Arguments)
				if len(input) == 0 {
					input = json.RawMessage("{}")
				}
				blocks = append(blocks, anthropicBlock{
					ToolUse: &anthropicToolUseBlock{
						Type: "tool_use", ID: tc.ID,
						Name: tc.Function.Name, Input: input,
					},
				})
			}
			out.Messages = append(out.Messages, anthropicMessage{
				Role: string(m.Role), Content: blocks,
			})
		}
	}
	if len(systems) > 0 {
		out.System = strings.Join(systems, "\n\n")
	}
	for _, t := range in.Tools {
		out.Tools = append(out.Tools, anthropicTool{
			Name: t.Function.Name, Description: t.Function.Description,
			InputSchema: t.Function.Parameters,
		})
	}
	return out
}

// FromAnthropicResp 把 Anthropic 响应转回 OpenAI ChatResponse。
// providerName 用于 ChatResponse.Model 字段 ("claude:..." 原样回显)。
func FromAnthropicResp(in anthropicMessagesResp, providerName, model string) *ChatResponse {
	msg := ChatMessage{Role: RoleAssistant}
	for _, b := range in.Content {
		switch b.Type {
		case "text":
			msg.Content += b.Text
		case "tool_use":
			args, _ := json.Marshal(json.RawMessage(b.Input))
			msg.ToolCalls = append(msg.ToolCalls, ToolCall{
				ID: b.ID, Type: "function",
				Function: ToolCallFunc{Name: b.Name, Arguments: string(args)},
			})
		}
	}
	return &ChatResponse{
		ID: in.ID, Object: "chat.completion",
		Model: providerName + ":" + model,
		Choices: []ChatChoice{{
			Index: 0, Message: msg,
			FinishReason: mapAnthropicStopReason(in.StopReason),
		}},
		Usage: Usage{
			PromptTokens:     in.Usage.InputTokens,
			CompletionTokens: in.Usage.OutputTokens,
			TotalTokens:      in.Usage.InputTokens + in.Usage.OutputTokens,
		},
	}
}

func mapAnthropicStopReason(a string) string {
	switch a {
	case "end_turn":
		return "stop"
	case "max_tokens":
		return "length"
	case "tool_use":
		return "tool_calls"
	case "stop_sequence":
		return "stop"
	}
	return "stop"
}
```

- [ ] **Step 2: 写测试 `claude_translate_test.go`**

```go
package modelgw_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/modelgw"
)

func TestToAnthropic_SystemsJoined(t *testing.T) {
	req := modelgw.ChatRequest{
		Messages: []modelgw.ChatMessage{
			{Role: modelgw.RoleSystem, Content: "a"},
			{Role: modelgw.RoleSystem, Content: "b"},
			{Role: modelgw.RoleUser, Content: "hi"},
		},
	}
	out := modelgw.ToAnthropicReq(req, "claude-sonnet-4-5")
	require.Equal(t, "a\n\nb", out.System)
	require.Len(t, out.Messages, 1)
	require.Equal(t, "user", out.Messages[0].Role)
}

func TestToAnthropic_MaxTokensDefault(t *testing.T) {
	out := modelgw.ToAnthropicReq(modelgw.ChatRequest{
		Messages: []modelgw.ChatMessage{{Role: modelgw.RoleUser, Content: "x"}},
	}, "x")
	require.Equal(t, 4096, out.MaxTokens)

	mt := 100
	out = modelgw.ToAnthropicReq(modelgw.ChatRequest{
		MaxTokens: &mt,
		Messages:  []modelgw.ChatMessage{{Role: modelgw.RoleUser, Content: "x"}},
	}, "x")
	require.Equal(t, 100, out.MaxTokens)
}

func TestToAnthropic_ToolMessageBecomesUserToolResult(t *testing.T) {
	req := modelgw.ChatRequest{
		Messages: []modelgw.ChatMessage{
			{Role: modelgw.RoleAssistant, ToolCalls: []modelgw.ToolCall{{
				ID: "call_1", Type: "function",
				Function: modelgw.ToolCallFunc{Name: "ls", Arguments: `{"path":"/"}`},
			}}},
			{Role: modelgw.RoleTool, ToolCallID: "call_1", Content: "file1\nfile2"},
		},
	}
	out := modelgw.ToAnthropicReq(req, "claude-sonnet-4-5")
	require.Len(t, out.Messages, 2)
	require.Equal(t, "assistant", out.Messages[0].Role)
	require.NotNil(t, out.Messages[0].Content[0].ToolUse)
	require.Equal(t, "call_1", out.Messages[0].Content[0].ToolUse.ID)
	require.Equal(t, "user", out.Messages[1].Role)
	require.NotNil(t, out.Messages[1].Content[0].ToolResult)
	require.Equal(t, "call_1", out.Messages[1].Content[0].ToolResult.ToolUseID)
	require.Equal(t, "file1\nfile2", out.Messages[1].Content[0].ToolResult.Content)
}

func TestToAnthropic_ToolDef(t *testing.T) {
	req := modelgw.ChatRequest{
		Messages: []modelgw.ChatMessage{{Role: modelgw.RoleUser, Content: "hi"}},
		Tools: []modelgw.ToolDef{{
			Type: "function",
			Function: modelgw.ToolDefFunction{
				Name: "ls", Description: "list files",
				Parameters: json.RawMessage(`{"type":"object"}`),
			},
		}},
	}
	out := modelgw.ToAnthropicReq(req, "claude-sonnet-4-5")
	require.Len(t, out.Tools, 1)
	require.Equal(t, "ls", out.Tools[0].Name)
}

func TestFromAnthropic_BasicText(t *testing.T) {
	in := unmarshalAnthropicResp(t, `{
		"id": "msg_1", "type": "message", "role": "assistant",
		"content": [{"type":"text","text":"hello"}],
		"model": "claude-sonnet-4-5",
		"stop_reason": "end_turn",
		"usage": {"input_tokens": 5, "output_tokens": 1}
	}`)
	out := modelgw.FromAnthropicResp(in, "claude", "claude-sonnet-4-5")
	require.Equal(t, "claude:claude-sonnet-4-5", out.Model)
	require.Equal(t, "hello", out.Choices[0].Message.Content)
	require.Equal(t, "stop", out.Choices[0].FinishReason)
	require.Equal(t, 5, out.Usage.PromptTokens)
	require.Equal(t, 1, out.Usage.CompletionTokens)
	require.Equal(t, 6, out.Usage.TotalTokens)
}

func TestFromAnthropic_ToolUse(t *testing.T) {
	in := unmarshalAnthropicResp(t, `{
		"id": "msg_1", "type": "message", "role": "assistant",
		"content": [
			{"type":"text","text":"calling ls "},
			{"type":"tool_use","id":"tu_1","name":"ls","input":{"path":"/"}}
		],
		"model": "claude-sonnet-4-5",
		"stop_reason": "tool_use",
		"usage": {"input_tokens": 10, "output_tokens": 20}
	}`)
	out := modelgw.FromAnthropicResp(in, "claude", "claude-sonnet-4-5")
	require.Equal(t, "calling ls ", out.Choices[0].Message.Content)
	require.Equal(t, "tool_calls", out.Choices[0].FinishReason)
	require.Len(t, out.Choices[0].Message.ToolCalls, 1)
	require.Equal(t, "tu_1", out.Choices[0].Message.ToolCalls[0].ID)
	require.JSONEq(t, `{"path":"/"}`, out.Choices[0].Message.ToolCalls[0].Function.Arguments)
}

func TestStopReasonMapping(t *testing.T) {
	cases := map[string]string{
		"end_turn": "stop", "max_tokens": "length",
		"tool_use": "tool_calls", "stop_sequence": "stop",
		"unknown": "stop",
	}
	for in, want := range cases {
		require.Equal(t, want, mapStopReasonForTest(in), "input %q", in)
	}
}

// 测试 helper: 用包级函数读 raw JSON 到不导出类型,通过 FromAnthropicResp 入口。
// 用 marshal/unmarshal 绕过非导出字段。
func unmarshalAnthropicResp(t *testing.T, s string) anthropicMessagesResp {
	t.Helper()
	var raw anthropicMessagesResp
	require.NoError(t, json.Unmarshal([]byte(s), &raw))
	return raw
}

// 因为 anthropicMessagesResp 是包内非导出,测试在同 package_test 包内不能直接访问。
// 方案: claude_translate.go 把 anthropicMessagesResp 导出为 AnthropicMessagesResp (公开)。
// 但 spec 要求非导出。简化:测试改用 internal test (package modelgw,不带 _test)。
```

> 注：上面测试因为引用了非导出类型 `anthropicMessagesResp`，必须用 internal test（package `modelgw`，不带 `_test` 后缀）。请把 `claude_translate_test.go` 的 `package modelgw_test` 改为 `package modelgw`，import 段移除 `"github.com/yourorg/private-coding-agent/internal/modelgw"` 与所有 `modelgw.` 前缀。`mapStopReasonForTest` 改为 `mapAnthropicStopReason`。

- [ ] **Step 3: 跑测试**

```bash
PATH="/d/tools/go/bin:$PATH" go vet ./...
PATH="/d/tools/go/bin:$PATH" go test ./internal/modelgw/... -count=1 -v -run "TestToAnthropic|TestFromAnthropic|TestStopReason"
```

期望：7 测试 PASS。

- [ ] **Step 4: commit**

```bash
git add internal/modelgw/claude_translate.go internal/modelgw/claude_translate_test.go
git commit -m "feat(modelgw): Anthropic ↔ OpenAI translate (non-stream)"
```

---

## Task 9: Claude SSE 流式状态机

**Files:**
- Create: `internal/modelgw/claude_stream.go`
- Create: `internal/modelgw/claude_stream_test.go`

- [ ] **Step 1: 实现 `claude_stream.go`**

```go
package modelgw

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// Anthropic SSE 事件类型(摘自其 API 文档)。
type anthropicEvent struct {
	Type         string          `json:"type"`
	Index        int             `json:"index,omitempty"`
	ContentBlock json.RawMessage `json:"content_block,omitempty"`
	Delta        json.RawMessage `json:"delta,omitempty"`
	Message      json.RawMessage `json:"message,omitempty"`
	Usage        anthropicUsage  `json:"usage,omitempty"`
}

type anthropicDelta struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
	StopReason  string `json:"stop_reason,omitempty"`
}

// claudeStreamState 跨事件累积 tool_use block 的部分 JSON。
type claudeStreamState struct {
	chunkID    string
	inputTok   int
	outputTok  int
	stopReason string
	blocks     map[int]*claudeBlockBuf
	roleSent   bool
	model      string // "claude:claude-sonnet-4-5"
}

type claudeBlockBuf struct {
	typ      string // "text" / "tool_use"
	toolID   string
	toolName string
	partial  []byte // 累积 input_json_delta
}

// ConvertClaudeStream 读 Anthropic SSE 流,逐事件调 yield 发出 OpenAI chunks。
// providerName + model 用于填充 chunk.Model。
//
// 结束:Anthropic 发 message_stop;实现发末帧含 usage,然后返 nil。
func ConvertClaudeStream(body io.Reader, providerName, model string,
	yield func(ChatStreamChunk) error) error {

	state := &claudeStreamState{
		blocks: map[int]*claudeBlockBuf{},
		model:  providerName + ":" + model,
	}
	now := func() int64 { return time.Now().Unix() }

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	var currentEvent string
	var dataLines [][]byte

	flushEvent := func() error {
		if currentEvent == "" || len(dataLines) == 0 {
			currentEvent = ""
			dataLines = nil
			return nil
		}
		joined := bytes.Join(dataLines, []byte("\n"))
		currentEvent = ""
		dataLines = nil
		return handleClaudeEvent(joined, state, now, yield)
	}

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			if err := flushEvent(); err != nil {
				return err
			}
			continue
		}
		if bytes.HasPrefix(line, []byte("event: ")) {
			currentEvent = string(line[len("event: "):])
			continue
		}
		if bytes.HasPrefix(line, []byte("data: ")) {
			dataLines = append(dataLines, line[len("data: "):])
			continue
		}
	}
	// 末尾缓冲
	if err := flushEvent(); err != nil {
		return err
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("%w: %v", ErrProviderUnreachable, err)
	}
	return nil
}

func handleClaudeEvent(payload []byte, s *claudeStreamState, now func() int64,
	yield func(ChatStreamChunk) error) error {

	var ev anthropicEvent
	if err := json.Unmarshal(payload, &ev); err != nil {
		return nil // 跳过坏帧
	}

	switch ev.Type {
	case "message_start":
		var msg struct {
			ID    string         `json:"id"`
			Usage anthropicUsage `json:"usage"`
		}
		_ = json.Unmarshal(ev.Message, &msg)
		s.chunkID = msg.ID
		s.inputTok = msg.Usage.InputTokens

	case "content_block_start":
		var cb anthropicRespBlock
		_ = json.Unmarshal(ev.ContentBlock, &cb)
		s.blocks[ev.Index] = &claudeBlockBuf{
			typ: cb.Type, toolID: cb.ID, toolName: cb.Name,
		}
		// 首次发 chunk 前确保角色已发
		if !s.roleSent {
			s.roleSent = true
			if err := yield(ChatStreamChunk{
				ID: s.chunkID, Object: "chat.completion.chunk",
				Created: now(), Model: s.model,
				Choices: []ChatStreamChoice{{
					Index: 0, Delta: ChatStreamDelta{Role: RoleAssistant},
				}},
			}); err != nil {
				return err
			}
		}

	case "content_block_delta":
		var d anthropicDelta
		_ = json.Unmarshal(ev.Delta, &d)
		bb := s.blocks[ev.Index]
		if bb == nil {
			return nil
		}
		switch d.Type {
		case "text_delta":
			return yield(ChatStreamChunk{
				ID: s.chunkID, Object: "chat.completion.chunk",
				Created: now(), Model: s.model,
				Choices: []ChatStreamChoice{{
					Index: 0, Delta: ChatStreamDelta{Content: d.Text},
				}},
			})
		case "input_json_delta":
			bb.partial = append(bb.partial, []byte(d.PartialJSON)...)
		}

	case "content_block_stop":
		bb := s.blocks[ev.Index]
		if bb == nil || bb.typ != "tool_use" {
			return nil
		}
		// tool_use 完整,一次性发 OpenAI chunk
		return yield(ChatStreamChunk{
			ID: s.chunkID, Object: "chat.completion.chunk",
			Created: now(), Model: s.model,
			Choices: []ChatStreamChoice{{
				Index: 0,
				Delta: ChatStreamDelta{ToolCalls: []ToolCall{{
					ID: bb.toolID, Type: "function",
					Function: ToolCallFunc{Name: bb.toolName, Arguments: string(bb.partial)},
				}}},
			}},
		})

	case "message_delta":
		var d anthropicDelta
		_ = json.Unmarshal(ev.Delta, &d)
		if d.StopReason != "" {
			s.stopReason = d.StopReason
		}
		if ev.Usage.OutputTokens > 0 {
			s.outputTok = ev.Usage.OutputTokens
		}

	case "message_stop":
		finish := mapAnthropicStopReason(s.stopReason)
		return yield(ChatStreamChunk{
			ID: s.chunkID, Object: "chat.completion.chunk",
			Created: now(), Model: s.model,
			Choices: []ChatStreamChoice{{
				Index: 0, FinishReason: &finish,
				Delta: ChatStreamDelta{},
			}},
			Usage: &Usage{
				PromptTokens: s.inputTok, CompletionTokens: s.outputTok,
				TotalTokens: s.inputTok + s.outputTok,
			},
		})
	}

	return nil
}
```

- [ ] **Step 2: 写测试 `claude_stream_test.go`**

注意：内部测试 package。

```go
package modelgw

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const claudeTextOnlyStream = `event: message_start
data: {"type":"message_start","message":{"id":"msg_1","usage":{"input_tokens":10,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" world"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}

event: message_stop
data: {"type":"message_stop"}
`

func TestConvertClaudeStream_TextOnly(t *testing.T) {
	var got []ChatStreamChunk
	err := ConvertClaudeStream(strings.NewReader(claudeTextOnlyStream), "claude", "x",
		func(c ChatStreamChunk) error { got = append(got, c); return nil })
	require.NoError(t, err)

	// 期望: role chunk + 2 个 text deltas + final usage chunk
	require.Len(t, got, 4)
	require.Equal(t, RoleAssistant, got[0].Choices[0].Delta.Role)
	require.Equal(t, "hello", got[1].Choices[0].Delta.Content)
	require.Equal(t, " world", got[2].Choices[0].Delta.Content)
	require.NotNil(t, got[3].Usage)
	require.Equal(t, 10, got[3].Usage.PromptTokens)
	require.Equal(t, 5, got[3].Usage.CompletionTokens)
	require.NotNil(t, got[3].Choices[0].FinishReason)
	require.Equal(t, "stop", *got[3].Choices[0].FinishReason)
}

const claudeToolUseStream = `event: message_start
data: {"type":"message_start","message":{"id":"msg_2","usage":{"input_tokens":20,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"calling"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"tu_1","name":"ls","input":{}}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"path\":"}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"/\"}"}}

event: content_block_stop
data: {"type":"content_block_stop","index":1}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":12}}

event: message_stop
data: {"type":"message_stop"}
`

func TestConvertClaudeStream_ToolUse(t *testing.T) {
	var got []ChatStreamChunk
	err := ConvertClaudeStream(strings.NewReader(claudeToolUseStream), "claude", "x",
		func(c ChatStreamChunk) error { got = append(got, c); return nil })
	require.NoError(t, err)

	// 期望: role + "calling" + tool_call chunk + final usage chunk
	require.Len(t, got, 4)
	require.Equal(t, RoleAssistant, got[0].Choices[0].Delta.Role)
	require.Equal(t, "calling", got[1].Choices[0].Delta.Content)

	require.Len(t, got[2].Choices[0].Delta.ToolCalls, 1)
	tc := got[2].Choices[0].Delta.ToolCalls[0]
	require.Equal(t, "tu_1", tc.ID)
	require.Equal(t, "ls", tc.Function.Name)
	require.JSONEq(t, `{"path":"/"}`, tc.Function.Arguments)

	require.NotNil(t, got[3].Usage)
	require.Equal(t, 20, got[3].Usage.PromptTokens)
	require.Equal(t, 12, got[3].Usage.CompletionTokens)
	require.NotNil(t, got[3].Choices[0].FinishReason)
	require.Equal(t, "tool_calls", *got[3].Choices[0].FinishReason)
}

func TestConvertClaudeStream_YieldErrorPropagates(t *testing.T) {
	myErr := errInjected
	err := ConvertClaudeStream(strings.NewReader(claudeTextOnlyStream), "claude", "x",
		func(c ChatStreamChunk) error { return myErr })
	require.ErrorIs(t, err, myErr)
}

var errInjected = &injErr{}

type injErr struct{}

func (e *injErr) Error() string { return "injected" }
```

- [ ] **Step 3: 跑测试**

```bash
PATH="/d/tools/go/bin:$PATH" go test ./internal/modelgw/... -count=1 -v -run TestConvertClaudeStream
```

期望：3 测试 PASS。

- [ ] **Step 4: commit**

```bash
git add internal/modelgw/claude_stream.go internal/modelgw/claude_stream_test.go
git commit -m "feat(modelgw): Anthropic SSE → OpenAI chunks state machine"
```

---

## Task 10: `ClaudeProvider` 整合

**Files:**
- Create: `internal/modelgw/provider_claude.go`
- Create: `internal/modelgw/provider_claude_test.go`

- [ ] **Step 1: 实现 `provider_claude.go`**

```go
package modelgw

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ClaudeProvider 通过 Anthropic Messages API 提供 OpenAI 兼容的 ChatCompletion。
// Embeddings 不支持(Anthropic 无官方 API),返 ErrUnsupportedFeature。
type ClaudeProvider struct {
	id        uuid.UUID
	name      string
	baseURL   string
	apiKeyEnv string
	client    *http.Client
}

func NewClaudeProvider(cfg ProviderConfig) (*ClaudeProvider, error) {
	return &ClaudeProvider{
		id:        cfg.ID,
		name:      cfg.Name,
		baseURL:   strings.TrimRight(cfg.BaseURL, "/"),
		apiKeyEnv: cfg.APIKeyEnv,
		client:    &http.Client{Timeout: time.Duration(DefaultTimeoutSec) * time.Second},
	}, nil
}

func (p *ClaudeProvider) ID() uuid.UUID { return p.id }
func (p *ClaudeProvider) Type() string  { return "claude" }
func (p *ClaudeProvider) Name() string  { return p.name }

func (p *ClaudeProvider) apiKey() (string, error) {
	if p.apiKeyEnv == "" {
		return "", fmt.Errorf("claude provider %q requires api_key_env", p.name)
	}
	v := os.Getenv(p.apiKeyEnv)
	if v == "" {
		return "", fmt.Errorf("api key env %q is empty", p.apiKeyEnv)
	}
	return v, nil
}

func (p *ClaudeProvider) newHTTPReq(ctx context.Context, body any, stream bool) (*http.Request, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.baseURL+"/v1/messages", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	key, err := p.apiKey()
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", key)
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	return req, nil
}

func (p *ClaudeProvider) ChatCompletion(ctx context.Context, req ChatRequest, model string) (*ChatResponse, error) {
	anthropicReq := ToAnthropicReq(req, model)
	anthropicReq.Stream = false

	hreq, err := p.newHTTPReq(ctx, anthropicReq, false)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Do(hreq)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProviderUnreachable, err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, int64(MaxProviderBody)))
	if resp.StatusCode >= 400 {
		return nil, &ProviderError{StatusCode: resp.StatusCode, Body: string(body)}
	}

	var ar anthropicMessagesResp
	if err := json.Unmarshal(body, &ar); err != nil {
		return nil, fmt.Errorf("decode anthropic response: %w", err)
	}
	return FromAnthropicResp(ar, p.name, model), nil
}

func (p *ClaudeProvider) ChatCompletionStream(ctx context.Context, req ChatRequest, model string,
	yield func(ChatStreamChunk) error) error {
	anthropicReq := ToAnthropicReq(req, model)
	anthropicReq.Stream = true

	hreq, err := p.newHTTPReq(ctx, anthropicReq, true)
	if err != nil {
		return err
	}
	resp, err := p.client.Do(hreq)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrProviderUnreachable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, int64(MaxProviderBody)))
		return &ProviderError{StatusCode: resp.StatusCode, Body: string(body)}
	}

	return ConvertClaudeStream(resp.Body, p.name, model, yield)
}

func (p *ClaudeProvider) Embeddings(ctx context.Context, req EmbeddingsRequest, model string) (*EmbeddingsResponse, error) {
	return nil, ErrUnsupportedFeature
}
```

- [ ] **Step 2: 写测试 `provider_claude_test.go`**

```go
package modelgw_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/modelgw"
)

func newClaudeServer(t *testing.T, handler http.HandlerFunc) (*modelgw.ClaudeProvider, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	t.Setenv("TEST_CLAUDE_KEY", "sk-test-key")
	p, err := modelgw.NewClaudeProvider(modelgw.ProviderConfig{
		ID: uuid.New(), Name: "test-claude", Type: "claude",
		BaseURL: srv.URL, APIKeyEnv: "TEST_CLAUDE_KEY",
	})
	require.NoError(t, err)
	return p, srv
}

func TestClaude_ChatCompletion_OK(t *testing.T) {
	p, _ := newClaudeServer(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/messages", r.URL.Path)
		require.Equal(t, "sk-test-key", r.Header.Get("x-api-key"))
		require.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))
		_, _ = w.Write([]byte(`{
			"id":"msg_1","type":"message","role":"assistant",
			"content":[{"type":"text","text":"hi from claude"}],
			"model":"claude-sonnet-4-5","stop_reason":"end_turn",
			"usage":{"input_tokens":5,"output_tokens":3}
		}`))
	})
	out, err := p.ChatCompletion(context.Background(),
		modelgw.ChatRequest{Messages: []modelgw.ChatMessage{
			{Role: modelgw.RoleUser, Content: "hi"},
		}}, "claude-sonnet-4-5")
	require.NoError(t, err)
	require.Equal(t, "hi from claude", out.Choices[0].Message.Content)
	require.Equal(t, 8, out.Usage.TotalTokens)
	require.Equal(t, "test-claude:claude-sonnet-4-5", out.Model)
}

func TestClaude_ChatCompletion_4xx(t *testing.T) {
	p, _ := newClaudeServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"type":"invalid_request_error","message":"bad"}}`))
	})
	_, err := p.ChatCompletion(context.Background(),
		modelgw.ChatRequest{Messages: []modelgw.ChatMessage{{Role: modelgw.RoleUser, Content: "x"}}},
		"claude-sonnet-4-5")
	require.Error(t, err)
	var pe *modelgw.ProviderError
	require.ErrorAs(t, err, &pe)
	require.Equal(t, 400, pe.StatusCode)
}

func TestClaude_Embeddings_Unsupported(t *testing.T) {
	t.Setenv("TEST_CLAUDE_KEY", "x")
	p, err := modelgw.NewClaudeProvider(modelgw.ProviderConfig{
		ID: uuid.New(), Name: "test-claude", BaseURL: "http://x",
		APIKeyEnv: "TEST_CLAUDE_KEY",
	})
	require.NoError(t, err)
	_, err = p.Embeddings(context.Background(),
		modelgw.EmbeddingsRequest{Input: []string{"hi"}}, "m")
	require.ErrorIs(t, err, modelgw.ErrUnsupportedFeature)
}

func TestClaude_Stream_TextOnly(t *testing.T) {
	p, _ := newClaudeServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		write := func(s string) { _, _ = w.Write([]byte(s)); fl.Flush() }
		write("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"usage\":{\"input_tokens\":3,\"output_tokens\":0}}}\n\n")
		write("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n")
		write("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hi\"}}\n\n")
		write("event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n")
		write("event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n")
		write("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	})

	var got []modelgw.ChatStreamChunk
	err := p.ChatCompletionStream(context.Background(),
		modelgw.ChatRequest{Messages: []modelgw.ChatMessage{{Role: modelgw.RoleUser, Content: "x"}}},
		"claude-sonnet-4-5",
		func(c modelgw.ChatStreamChunk) error { got = append(got, c); return nil })
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(got), 3)
	require.Equal(t, "hi", got[1].Choices[0].Delta.Content)
	require.NotNil(t, got[len(got)-1].Usage)
}

// 拒绝 api_key_env 空时构造可以,但调用时返错。
func TestClaude_NoAPIKeyEnv_CallFails(t *testing.T) {
	p, err := modelgw.NewClaudeProvider(modelgw.ProviderConfig{
		ID: uuid.New(), Name: "test", Type: "claude",
		BaseURL: "http://x", APIKeyEnv: "",
	})
	require.NoError(t, err)
	_, err = p.ChatCompletion(context.Background(),
		modelgw.ChatRequest{Messages: []modelgw.ChatMessage{{Role: modelgw.RoleUser, Content: "x"}}},
		"m")
	require.Error(t, err)
	// 不暴露 env 名给客户端,但内部错误带提示
	require.Contains(t, err.Error(), "api_key_env")
}

var _ = json.RawMessage{}
```

- [ ] **Step 3: 跑测试**

```bash
PATH="/d/tools/go/bin:$PATH" go test ./internal/modelgw/... -count=1 -v -run TestClaude
```

期望：5 测试 PASS。

- [ ] **Step 4: commit**

```bash
git add internal/modelgw/provider_claude.go internal/modelgw/provider_claude_test.go
git commit -m "feat(modelgw): ClaudeProvider over Anthropic Messages API"
```

---

## Task 11: `Gateway` 编排

**Files:**
- Create: `internal/modelgw/gateway.go`
- Create: `internal/modelgw/gateway_test.go`

- [ ] **Step 1: 实现 `gateway.go`**

```go
package modelgw

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Gateway 编排 validate → Resolve → Provider 调用 → record。
type Gateway struct {
	reg      *ProviderRegistry
	recorder *UsageRecorder
}

func NewGateway(reg *ProviderRegistry, recorder *UsageRecorder) *Gateway {
	return &Gateway{reg: reg, recorder: recorder}
}

func (g *Gateway) ChatCompletion(ctx context.Context, tenantID, userID uuid.UUID,
	req ChatRequest) (*ChatResponse, error) {
	if err := ValidateChatRequest(req); err != nil {
		return nil, err
	}
	provider, model, err := g.reg.Resolve(req.Model)
	if err != nil {
		return nil, err
	}

	start := time.Now()
	resp, callErr := provider.ChatCompletion(ctx, req, model)
	g.record(tenantID, userID, provider, model, "chat", false, callErr,
		safeUsage(resp), time.Since(start))
	if callErr != nil {
		return nil, callErr
	}
	resp.Model = req.Model
	return resp, nil
}

func (g *Gateway) ChatCompletionStream(ctx context.Context, tenantID, userID uuid.UUID,
	req ChatRequest, yield func(ChatStreamChunk) error) error {
	if err := ValidateChatRequest(req); err != nil {
		return err
	}
	provider, model, err := g.reg.Resolve(req.Model)
	if err != nil {
		return err
	}

	start := time.Now()
	var lastUsage *Usage
	wrapYield := func(c ChatStreamChunk) error {
		if c.Usage != nil {
			lastUsage = c.Usage
		}
		c.Model = req.Model
		return yield(c)
	}
	callErr := provider.ChatCompletionStream(ctx, req, model, wrapYield)
	g.record(tenantID, userID, provider, model, "chat", true, callErr,
		usagePtrOrZero(lastUsage), time.Since(start))
	return callErr
}

func (g *Gateway) Embeddings(ctx context.Context, tenantID, userID uuid.UUID,
	req EmbeddingsRequest) (*EmbeddingsResponse, error) {
	if err := ValidateEmbeddingsRequest(req); err != nil {
		return nil, err
	}
	provider, model, err := g.reg.Resolve(req.Model)
	if err != nil {
		return nil, err
	}

	start := time.Now()
	resp, callErr := provider.Embeddings(ctx, req, model)
	g.record(tenantID, userID, provider, model, "embed", false, callErr,
		safeEmbedUsage(resp), time.Since(start))
	if callErr != nil {
		return nil, callErr
	}
	resp.Model = req.Model
	return resp, nil
}

func (g *Gateway) record(tenantID, userID uuid.UUID, p Provider, model, action string,
	stream bool, callErr error, usage Usage, dur time.Duration) {
	status := "ok"
	errClass := ""
	if callErr != nil {
		status = "error"
		errClass = classifyError(callErr)
	}
	g.recorder.Record(CallEvent{
		TenantID: tenantID, UserID: userID,
		ProviderID: p.ID(), ProviderType: p.Type(), Model: model,
		Action: action, Stream: stream,
		Status: status, ErrorClass: errClass,
		InputTokens: usage.PromptTokens, OutputTokens: usage.CompletionTokens,
		DurationMS: dur.Milliseconds(),
		OccurredAt: time.Now(),
	})
}

func classifyError(err error) string {
	switch {
	case errors.Is(err, ErrProviderUnreachable):
		return "unreachable"
	case errors.Is(err, ErrProviderError):
		return "provider_error"
	case errors.Is(err, ErrUnsupportedFeature):
		return "unsupported_feature"
	case errors.Is(err, ErrModelInvalid), errors.Is(err, ErrProviderNotFound):
		return "validation"
	}
	return "other"
}

func safeUsage(r *ChatResponse) Usage {
	if r == nil {
		return Usage{}
	}
	return r.Usage
}

func safeEmbedUsage(r *EmbeddingsResponse) Usage {
	if r == nil {
		return Usage{}
	}
	return r.Usage
}

func usagePtrOrZero(p *Usage) Usage {
	if p == nil {
		return Usage{}
	}
	return *p
}
```

- [ ] **Step 2: 写测试 `gateway_test.go`**

```go
package modelgw_test

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/modelgw"
)

// mockProvider 满足 Provider, 行为可配置。
type mockProvider struct {
	id   uuid.UUID
	name string
	chatRet    *modelgw.ChatResponse
	chatErr    error
	streamErr  error
	streamChunks []modelgw.ChatStreamChunk
	embedRet   *modelgw.EmbeddingsResponse
	embedErr   error
}

func (m *mockProvider) ID() uuid.UUID { return m.id }
func (m *mockProvider) Type() string  { return "mock" }
func (m *mockProvider) Name() string  { return m.name }
func (m *mockProvider) ChatCompletion(context.Context, modelgw.ChatRequest, string) (*modelgw.ChatResponse, error) {
	return m.chatRet, m.chatErr
}
func (m *mockProvider) ChatCompletionStream(_ context.Context, _ modelgw.ChatRequest, _ string,
	yield func(modelgw.ChatStreamChunk) error) error {
	for _, c := range m.streamChunks {
		if err := yield(c); err != nil {
			return err
		}
	}
	return m.streamErr
}
func (m *mockProvider) Embeddings(context.Context, modelgw.EmbeddingsRequest, string) (*modelgw.EmbeddingsResponse, error) {
	return m.embedRet, m.embedErr
}

func gatewayWith(t *testing.T, mp *mockProvider) (*modelgw.Gateway, *sync.Mutex, *[]error) {
	t.Helper()
	ctx := context.Background()
	pg, err := pgxpool.New(ctx, testDSN)
	require.NoError(t, err)
	t.Cleanup(pg.Close)

	var errs []error
	var mu sync.Mutex
	rec := modelgw.NewUsageRecorder(modelgw.NewUsageRepo(pg), func(err error) {
		mu.Lock()
		errs = append(errs, err)
		mu.Unlock()
	})
	reg := modelgw.NewProviderRegistry(nil, nil, 0)
	reg.SeedForTest(map[string]modelgw.Provider{"mock": mp})
	return modelgw.NewGateway(reg, rec), &mu, &errs
}

func TestGateway_Chat_OK(t *testing.T) {
	mp := &mockProvider{
		id: uuid.New(), name: "mock",
		chatRet: &modelgw.ChatResponse{
			Choices: []modelgw.ChatChoice{{
				Index: 0, FinishReason: "stop",
				Message: modelgw.ChatMessage{Role: modelgw.RoleAssistant, Content: "ok"},
			}},
			Usage: modelgw.Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3},
		},
	}
	gw, _, _ := gatewayWith(t, mp)

	out, err := gw.ChatCompletion(context.Background(),
		uuid.New(), uuid.New(),
		modelgw.ChatRequest{Model: "mock:m",
			Messages: []modelgw.ChatMessage{{Role: modelgw.RoleUser, Content: "hi"}}})
	require.NoError(t, err)
	require.Equal(t, "ok", out.Choices[0].Message.Content)
	require.Equal(t, "mock:m", out.Model)
}

func TestGateway_Chat_ProviderUnreachable(t *testing.T) {
	mp := &mockProvider{id: uuid.New(), name: "mock", chatErr: modelgw.ErrProviderUnreachable}
	gw, _, _ := gatewayWith(t, mp)
	_, err := gw.ChatCompletion(context.Background(), uuid.New(), uuid.New(),
		modelgw.ChatRequest{Model: "mock:m",
			Messages: []modelgw.ChatMessage{{Role: modelgw.RoleUser, Content: "hi"}}})
	require.ErrorIs(t, err, modelgw.ErrProviderUnreachable)
}

func TestGateway_Chat_BadModel(t *testing.T) {
	gw, _, _ := gatewayWith(t, &mockProvider{id: uuid.New(), name: "mock"})
	_, err := gw.ChatCompletion(context.Background(), uuid.New(), uuid.New(),
		modelgw.ChatRequest{Model: "noprefix",
			Messages: []modelgw.ChatMessage{{Role: modelgw.RoleUser, Content: "hi"}}})
	require.ErrorIs(t, err, modelgw.ErrModelInvalid)
}

func TestGateway_Stream_RecordsLastUsage(t *testing.T) {
	mp := &mockProvider{
		id: uuid.New(), name: "mock",
		streamChunks: []modelgw.ChatStreamChunk{
			{Choices: []modelgw.ChatStreamChoice{{Delta: modelgw.ChatStreamDelta{Content: "a"}}}},
			{Choices: []modelgw.ChatStreamChoice{{Delta: modelgw.ChatStreamDelta{Content: "b"}}},
				Usage: &modelgw.Usage{PromptTokens: 4, CompletionTokens: 6, TotalTokens: 10}},
		},
	}
	gw, _, _ := gatewayWith(t, mp)
	var got []modelgw.ChatStreamChunk
	err := gw.ChatCompletionStream(context.Background(), uuid.New(), uuid.New(),
		modelgw.ChatRequest{Model: "mock:m",
			Messages: []modelgw.ChatMessage{{Role: modelgw.RoleUser, Content: "hi"}}},
		func(c modelgw.ChatStreamChunk) error { got = append(got, c); return nil })
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, "mock:m", got[0].Model) // gateway 应回写 Model
}

func TestGateway_Embeddings_OK(t *testing.T) {
	mp := &mockProvider{
		id: uuid.New(), name: "mock",
		embedRet: &modelgw.EmbeddingsResponse{
			Object: "list", Data: []modelgw.Embedding{{Embedding: []float64{1, 2}}},
			Usage: modelgw.Usage{PromptTokens: 1, TotalTokens: 1},
		},
	}
	gw, _, _ := gatewayWith(t, mp)
	out, err := gw.Embeddings(context.Background(), uuid.New(), uuid.New(),
		modelgw.EmbeddingsRequest{Model: "mock:m", Input: []string{"hi"}})
	require.NoError(t, err)
	require.Equal(t, "mock:m", out.Model)
}
```

- [ ] **Step 3: 跑测试**

```bash
PATH="/d/tools/go/bin:$PATH" go vet ./...
PATH="/d/tools/go/bin:$PATH" go test ./internal/modelgw/... -count=1 -v
```

期望：所有模块测试 PASS。

- [ ] **Step 4: commit**

```bash
git add internal/modelgw/gateway.go internal/modelgw/gateway_test.go
git commit -m "feat(modelgw): Gateway orchestration (validate / Resolve / record)"
```

---

## Task 12: `sse.go` + HTTP `Handler`

**Files:**
- Create: `internal/modelgw/sse.go`
- Create: `internal/modelgw/sse_test.go`
- Create: `internal/modelgw/handler.go`
- Create: `internal/modelgw/handler_test.go`

- [ ] **Step 1: 写 `sse.go`**

```go
package modelgw

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// SSEWriter 把 ChatStreamChunk 序列化为 SSE data 帧并 flush。
type SSEWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

// NewSSEWriter 设置 SSE headers 并立即 flush。
func NewSSEWriter(w http.ResponseWriter) (*SSEWriter, error) {
	fl, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("response writer does not support flush")
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no") // 防 nginx 缓冲
	w.WriteHeader(http.StatusOK)
	fl.Flush()
	return &SSEWriter{w: w, flusher: fl}, nil
}

// WriteChunk 写一条 chunk。
func (s *SSEWriter) WriteChunk(c ChatStreamChunk) error {
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(s.w, "data: %s\n\n", b); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

// WriteError 在 stream 中途写错误帧。caller 之后应停止写入。
func (s *SSEWriter) WriteError(errMsg, errType, errCode string) error {
	payload := map[string]any{
		"error": map[string]string{"message": errMsg, "type": errType, "code": errCode},
	}
	b, _ := json.Marshal(payload)
	if _, err := fmt.Fprintf(s.w, "data: %s\n\n", b); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

// WriteDone 写 OpenAI 风格的 [DONE] 末尾标记。
func (s *SSEWriter) WriteDone() error {
	if _, err := fmt.Fprint(s.w, "data: [DONE]\n\n"); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}
```

- [ ] **Step 2: 写 `sse_test.go`**

```go
package modelgw_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/modelgw"
)

func TestSSE_HeadersAndChunks(t *testing.T) {
	w := httptest.NewRecorder()
	sw, err := modelgw.NewSSEWriter(w)
	require.NoError(t, err)
	require.Equal(t, "text/event-stream; charset=utf-8", w.Header().Get("Content-Type"))
	require.Equal(t, "no-cache", w.Header().Get("Cache-Control"))

	require.NoError(t, sw.WriteChunk(modelgw.ChatStreamChunk{
		ID: "x", Object: "chat.completion.chunk",
		Choices: []modelgw.ChatStreamChoice{{Index: 0,
			Delta: modelgw.ChatStreamDelta{Content: "hi"}}},
	}))
	require.NoError(t, sw.WriteDone())

	body := w.Body.String()
	require.True(t, strings.Contains(body, `data: {`))
	require.True(t, strings.Contains(body, `"content":"hi"`))
	require.True(t, strings.HasSuffix(body, "data: [DONE]\n\n"))
}

func TestSSE_WriteError(t *testing.T) {
	w := httptest.NewRecorder()
	sw, _ := modelgw.NewSSEWriter(w)
	require.NoError(t, sw.WriteError("boom", "provider_error", "unreachable"))
	require.Contains(t, w.Body.String(), `"code":"unreachable"`)
}
```

- [ ] **Step 3: 写 `handler.go`**

```go
package modelgw

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/yourorg/private-coding-agent/internal/auth"
)

// Handler 暴露 /v1/chat/completions 和 /v1/embeddings。
type Handler struct {
	gw *Gateway
}

func NewHandler(gw *Gateway) *Handler { return &Handler{gw: gw} }

// Register 在 rg 上挂路由。rg 应已挂 auth.Middleware。
func (h *Handler) Register(rg *gin.RouterGroup) {
	v1 := rg.Group("/v1")
	v1.POST("/chat/completions", h.chat)
	v1.POST("/embeddings", h.embeddings)
}

type apiError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
}

func writeAPIError(c *gin.Context, httpCode int, msg, typ, code string) {
	c.AbortWithStatusJSON(httpCode, gin.H{"error": apiError{Message: msg, Type: typ, Code: code}})
}

func (h *Handler) claims(c *gin.Context) (*auth.Claims, bool) {
	cl := auth.FromCtx(c.Request.Context())
	if cl == nil {
		writeAPIError(c, http.StatusUnauthorized, "unauthorized", "auth_error", "missing_token")
		return nil, false
	}
	return cl, true
}

func (h *Handler) chat(c *gin.Context) {
	cl, ok := h.claims(c)
	if !ok {
		return
	}
	var req ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeAPIError(c, http.StatusBadRequest, err.Error(), "invalid_request_error", "bad_request")
		return
	}
	if req.Stream {
		h.chatStream(c, cl, req)
		return
	}
	resp, err := h.gw.ChatCompletion(c.Request.Context(), cl.TenantID, cl.UserID, req)
	if err != nil {
		mapErrorToAPI(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

func (h *Handler) chatStream(c *gin.Context, cl *auth.Claims, req ChatRequest) {
	// 先在 flush headers 前做 validate / Resolve 等可能失败但不需要 SSE 的事
	// 由 Gateway.ChatCompletionStream 内部完成 validate;Resolve 错误也会在
	// 我们启动 SSE 前以普通 error 返回 (因为 yield 还没被调过)。
	// 为避免 "validate 错却已 flush headers" 的问题,显式先做一次 validate。
	if err := ValidateChatRequest(req); err != nil {
		writeAPIError(c, http.StatusBadRequest, err.Error(), "invalid_request_error", "validation")
		return
	}
	if _, _, err := h.gw.reg.Resolve(req.Model); err != nil {
		mapErrorToAPI(c, err)
		return
	}

	sw, err := NewSSEWriter(c.Writer)
	if err != nil {
		writeAPIError(c, http.StatusInternalServerError, err.Error(), "server_error", "internal")
		return
	}
	streamErr := h.gw.ChatCompletionStream(c.Request.Context(), cl.TenantID, cl.UserID, req,
		func(chunk ChatStreamChunk) error { return sw.WriteChunk(chunk) })
	if streamErr != nil && !errors.Is(streamErr, c.Request.Context().Err()) {
		// 客户端断开不写错误帧 (写入也会失败)
		_ = sw.WriteError(streamErr.Error(), errorTypeFor(streamErr), errorCodeFor(streamErr))
		return
	}
	_ = sw.WriteDone()
}

func (h *Handler) embeddings(c *gin.Context) {
	cl, ok := h.claims(c)
	if !ok {
		return
	}
	var req EmbeddingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeAPIError(c, http.StatusBadRequest, err.Error(), "invalid_request_error", "bad_request")
		return
	}
	resp, err := h.gw.Embeddings(c.Request.Context(), cl.TenantID, cl.UserID, req)
	if err != nil {
		mapErrorToAPI(c, err)
		return
	}
	c.JSON(http.StatusOK, resp)
}

func mapErrorToAPI(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrModelInvalid):
		writeAPIError(c, http.StatusBadRequest, err.Error(), "invalid_request_error", "model_invalid")
	case errors.Is(err, ErrProviderNotFound):
		writeAPIError(c, http.StatusBadRequest, err.Error(), "invalid_request_error", "provider_not_found")
	case errors.Is(err, ErrUnsupportedFeature):
		writeAPIError(c, http.StatusBadRequest, err.Error(), "invalid_request_error", "unsupported_feature")
	case errors.Is(err, ErrProviderUnreachable):
		writeAPIError(c, http.StatusBadGateway, err.Error(), "provider_error", "unreachable")
	case errors.Is(err, ErrProviderError):
		var pe *ProviderError
		if errors.As(err, &pe) {
			switch {
			case pe.StatusCode == http.StatusTooManyRequests:
				writeAPIError(c, http.StatusTooManyRequests, pe.Error(), "rate_limit_error", "provider_rate_limit")
				return
			case pe.StatusCode >= 500:
				writeAPIError(c, http.StatusBadGateway, pe.Error(), "provider_error", "provider_5xx")
				return
			}
			writeAPIError(c, http.StatusBadGateway, pe.Error(), "provider_error", "provider_4xx")
			return
		}
		writeAPIError(c, http.StatusBadGateway, err.Error(), "provider_error", "unknown")
	default:
		// validation 形错
		if strings.HasPrefix(err.Error(), "validation:") {
			writeAPIError(c, http.StatusBadRequest, err.Error(), "invalid_request_error", "validation")
			return
		}
		writeAPIError(c, http.StatusInternalServerError, err.Error(), "server_error", "internal")
	}
}

func errorTypeFor(err error) string {
	switch {
	case errors.Is(err, ErrProviderUnreachable), errors.Is(err, ErrProviderError):
		return "provider_error"
	}
	return "server_error"
}

func errorCodeFor(err error) string {
	switch {
	case errors.Is(err, ErrProviderUnreachable):
		return "unreachable"
	case errors.Is(err, ErrProviderError):
		return "provider_error"
	}
	return "internal"
}
```

> 注意：`h.gw.reg` 访问 Gateway 的私有字段；在 `gateway.go` 加一个 getter `Registry() *ProviderRegistry`：

```go
// Registry returns the underlying registry (for handler-side pre-resolve checks).
func (g *Gateway) Registry() *ProviderRegistry { return g.reg }
```

Handler 改用 `h.gw.Registry().Resolve(req.Model)`。

- [ ] **Step 4: 写 `handler_test.go`**

```go
package modelgw_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/auth"
	"github.com/yourorg/private-coding-agent/internal/modelgw"
)

func newHandlerTestRouter(t *testing.T, mp *mockProvider) (*gin.Engine, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	gw, _, _ := gatewayWith(t, mp)
	const secret = "test-secret-thirty-two-chars-ok!"
	j := auth.NewJWT(auth.JWTConfig{Secret: secret, TTL: time.Hour})
	uid, tid := uuid.New(), uuid.New()
	tok, _ := j.Issue(uid, tid, "member")
	r := gin.New()
	g := r.Group("/")
	g.Use(auth.Middleware(j))
	modelgw.NewHandler(gw).Register(g)
	return r, "Bearer " + tok
}

func TestHandler_Chat_OK(t *testing.T) {
	mp := &mockProvider{
		id: uuid.New(), name: "mock",
		chatRet: &modelgw.ChatResponse{
			ID: "x", Object: "chat.completion", Model: "m",
			Choices: []modelgw.ChatChoice{{
				Index: 0, FinishReason: "stop",
				Message: modelgw.ChatMessage{Role: modelgw.RoleAssistant, Content: "ok"},
			}},
		},
	}
	r, tok := newHandlerTestRouter(t, mp)
	body, _ := json.Marshal(modelgw.ChatRequest{
		Model: "mock:m",
		Messages: []modelgw.ChatMessage{{Role: modelgw.RoleUser, Content: "hi"}},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Authorization", tok)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"content":"ok"`)
}

func TestHandler_Chat_NoAuth(t *testing.T) {
	r, _ := newHandlerTestRouter(t, &mockProvider{id: uuid.New(), name: "mock"})
	body, _ := json.Marshal(modelgw.ChatRequest{Model: "mock:m",
		Messages: []modelgw.ChatMessage{{Role: modelgw.RoleUser, Content: "hi"}}})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHandler_Chat_BadModel(t *testing.T) {
	r, tok := newHandlerTestRouter(t, &mockProvider{id: uuid.New(), name: "mock"})
	body, _ := json.Marshal(modelgw.ChatRequest{Model: "no-prefix",
		Messages: []modelgw.ChatMessage{{Role: modelgw.RoleUser, Content: "hi"}}})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Authorization", tok)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), `"model_invalid"`)
}

func TestHandler_Chat_Stream(t *testing.T) {
	mp := &mockProvider{
		id: uuid.New(), name: "mock",
		streamChunks: []modelgw.ChatStreamChunk{
			{ID: "x", Object: "chat.completion.chunk",
				Choices: []modelgw.ChatStreamChoice{{Delta: modelgw.ChatStreamDelta{Content: "a"}}}},
			{ID: "x", Object: "chat.completion.chunk",
				Choices: []modelgw.ChatStreamChoice{{Delta: modelgw.ChatStreamDelta{Content: "b"}}},
				Usage:   &modelgw.Usage{TotalTokens: 5}},
		},
	}
	r, tok := newHandlerTestRouter(t, mp)
	body, _ := json.Marshal(modelgw.ChatRequest{Model: "mock:m", Stream: true,
		Messages: []modelgw.ChatMessage{{Role: modelgw.RoleUser, Content: "hi"}}})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Authorization", tok)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	body2 := w.Body.String()
	require.True(t, strings.Contains(body2, `"content":"a"`))
	require.True(t, strings.Contains(body2, `"content":"b"`))
	require.True(t, strings.HasSuffix(body2, "data: [DONE]\n\n"))
}

func TestHandler_Embeddings_Unsupported(t *testing.T) {
	mp := &mockProvider{id: uuid.New(), name: "mock", embedErr: modelgw.ErrUnsupportedFeature}
	r, tok := newHandlerTestRouter(t, mp)
	body, _ := json.Marshal(modelgw.EmbeddingsRequest{Model: "mock:m", Input: []string{"hi"}})
	req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", bytes.NewReader(body))
	req.Header.Set("Authorization", tok)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "unsupported_feature")
}

func TestHandler_Chat_ProviderUnreachable_502(t *testing.T) {
	mp := &mockProvider{id: uuid.New(), name: "mock", chatErr: modelgw.ErrProviderUnreachable}
	r, tok := newHandlerTestRouter(t, mp)
	body, _ := json.Marshal(modelgw.ChatRequest{Model: "mock:m",
		Messages: []modelgw.ChatMessage{{Role: modelgw.RoleUser, Content: "hi"}}})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Authorization", tok)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadGateway, w.Code)
	require.Contains(t, w.Body.String(), "unreachable")
}

func TestHandler_Chat_Provider429(t *testing.T) {
	mp := &mockProvider{id: uuid.New(), name: "mock",
		chatErr: &modelgw.ProviderError{StatusCode: 429, Body: "rate limited"}}
	r, tok := newHandlerTestRouter(t, mp)
	body, _ := json.Marshal(modelgw.ChatRequest{Model: "mock:m",
		Messages: []modelgw.ChatMessage{{Role: modelgw.RoleUser, Content: "hi"}}})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Authorization", tok)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusTooManyRequests, w.Code)
	// 注意: 上下文 imports
	_ = context.Background
}
```

- [ ] **Step 5: 跑测试**

```bash
PATH="/d/tools/go/bin:$PATH" go vet ./...
PATH="/d/tools/go/bin:$PATH" go test ./internal/modelgw/... -count=1 -v
```

期望：所有 modelgw 包测试 PASS（含 ~30 个测试）。

- [ ] **Step 6: commit**

```bash
git add internal/modelgw/sse.go internal/modelgw/sse_test.go \
        internal/modelgw/handler.go internal/modelgw/handler_test.go internal/modelgw/gateway.go
git commit -m "feat(modelgw): SSE writer + HTTP handlers for /v1/chat/completions and /v1/embeddings"
```

---

## Task 13: main 装配 + compose + mock-provider + E2E

**Files:**
- Create: `internal/modelgw/mockserver/main.go`
- Create: `internal/modelgw/mockserver/Dockerfile`
- Modify: `cmd/server/main.go`
- Modify: `deploy/compose/docker-compose.yml`
- Modify: `deploy/compose/test-e2e.sh`
- Modify: `README.md`

- [ ] **Step 1: 写 mock-provider**

`internal/modelgw/mockserver/main.go`:

```go
// Command mockserver runs a minimal OpenAI-compatible HTTP server for E2E tests.
// It returns canned responses without calling any real model backend.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	addr := ":8081"
	if v := os.Getenv("PORT"); v != "" {
		addr = ":" + v
	}
	http.HandleFunc("/v1/chat/completions", chat)
	http.HandleFunc("/v1/embeddings", embed)
	http.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	log.Printf("mock-provider listening on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal(err)
	}
}

func chat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Stream {
		streamChat(w, req.Model)
		return
	}
	resp := map[string]any{
		"id": "mock-1", "object": "chat.completion",
		"created": time.Now().Unix(), "model": req.Model,
		"choices": []map[string]any{{
			"index": 0,
			"message": map[string]any{
				"role": "assistant", "content": "hello from mock",
			},
			"finish_reason": "stop",
		}},
		"usage": map[string]int{"prompt_tokens": 5, "completion_tokens": 4, "total_tokens": 9},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func streamChat(w http.ResponseWriter, model string) {
	w.Header().Set("Content-Type", "text/event-stream")
	fl := w.(http.Flusher)
	send := func(payload map[string]any) {
		b, _ := json.Marshal(payload)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
		fl.Flush()
	}
	send(map[string]any{
		"id": "mock-1", "object": "chat.completion.chunk", "model": model,
		"choices": []map[string]any{{"index": 0,
			"delta": map[string]any{"role": "assistant"}}},
	})
	for _, c := range []string{"hello ", "from ", "mock"} {
		send(map[string]any{
			"id": "mock-1", "object": "chat.completion.chunk", "model": model,
			"choices": []map[string]any{{"index": 0,
				"delta": map[string]any{"content": c}}},
		})
	}
	finish := "stop"
	send(map[string]any{
		"id": "mock-1", "object": "chat.completion.chunk", "model": model,
		"choices": []map[string]any{{"index": 0,
			"delta": map[string]any{}, "finish_reason": finish}},
		"usage": map[string]int{"prompt_tokens": 5, "completion_tokens": 3, "total_tokens": 8},
	})
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	fl.Flush()
}

func embed(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model string   `json:"model"`
		Input []string `json:"input"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	data := make([]map[string]any, 0, len(req.Input))
	for i := range req.Input {
		data = append(data, map[string]any{
			"index": i, "object": "embedding",
			"embedding": []float64{0.1, 0.2, 0.3},
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"object": "list", "data": data, "model": req.Model,
		"usage": map[string]int{"prompt_tokens": 1, "total_tokens": 1},
	})
}
```

`internal/modelgw/mockserver/Dockerfile`:

```dockerfile
FROM golang:1.26-alpine AS build
WORKDIR /src
ENV GOPROXY=https://goproxy.cn,direct
ENV GOSUMDB=sum.golang.org
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/mockserver ./internal/modelgw/mockserver

FROM gcr.io/distroless/static-debian12:nonroot
USER nonroot:nonroot
COPY --from=build /out/mockserver /app/mockserver
EXPOSE 8081
ENTRYPOINT ["/app/mockserver"]
```

- [ ] **Step 2: 修改 `cmd/server/main.go` 装配 modelgw**

读现有 `cmd/server/main.go`，在 sandbox 装配段后追加 modelgw 装配。具体位置：在 `sandboxHandler := sandbox.NewHandler(sandboxDriver)` 后、`reconciler` 之前，加：

```go
// Model Gateway
providerRepo := modelgw.NewProviderRepo(pool)
usageRecorder := modelgw.NewUsageRecorder(modelgw.NewUsageRepo(pool), func(err error) {
    log.Printf("model usage record: %v", err)
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
modelRegistry := modelgw.NewProviderRegistry(providerRepo, factories, 60*time.Second)
if err := modelRegistry.Start(ctx); err != nil {
    return fmt.Errorf("model registry: %w", err)
}
go modelRegistry.Run(ctx)
modelGateway := modelgw.NewGateway(modelRegistry, usageRecorder)
modelHandler := modelgw.NewHandler(modelGateway)
```

import 段加 `"github.com/yourorg/private-coding-agent/internal/modelgw"`。

在 `register` 闭包中（已有 `sandboxHandler.Register(protected)`），追加：

```go
modelHandler.Register(protected)
```

- [ ] **Step 3: 修改 `docker-compose.yml`**

在 services 块中追加 `mock-provider`：

```yaml
  mock-provider:
    build:
      context: ../..
      dockerfile: internal/modelgw/mockserver/Dockerfile
    ports:
      - "8081:8081"
    healthcheck:
      test: ["CMD", "/app/mockserver", "--help"]
      interval: 5s
      timeout: 2s
      retries: 3
```

> distroless 容器没 shell；healthcheck 用一个会立即返 0 的 binary 调用。也可直接 `disable: true` 关掉 healthcheck 等待行为，但简单起见保留弱形式。或者用：

```yaml
    healthcheck:
      disable: true
```

更可靠。改成 `disable: true`。

server service 不依赖 mock-provider 健康才起（避免循环 wait）；mock-provider 单独存在即可，server 通过 PG 表里的 base_url 找它。

完整 docker-compose.yml 修订段（在 server service 之前）：

```yaml
  mock-provider:
    build:
      context: ../..
      dockerfile: internal/modelgw/mockserver/Dockerfile
    healthcheck:
      disable: true
    ports:
      - "8081:8081"
```

server service 不改（DOCKER_HOST、PG 等保持）。

- [ ] **Step 4: 扩展 `test-e2e.sh`**

在 sandbox `verify 404 after destroy` 之后、`Invoke-Docker compose down` 之前追加：

```bash
echo "[9/12] chat completion (non-stream) via mock-provider ..."
CHAT=$(curl -fsS -X POST http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"model":"default-mock:gpt-4o","messages":[{"role":"user","content":"hi"}]}')
TEXT=$(echo "$CHAT" | jq -r '.choices[0].message.content')
[[ "$TEXT" == "hello from mock" ]] || { echo "chat content mismatch: $TEXT"; exit 1; }

echo "[10/12] chat completion (stream) via mock-provider ..."
STREAM=$(curl -fsS -N -X POST http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"model":"default-mock:gpt-4o","stream":true,"messages":[{"role":"user","content":"hi"}]}')
echo "$STREAM" | grep -q "data: \[DONE\]" || { echo "stream missing [DONE]"; exit 1; }
echo "$STREAM" | grep -q '"content":"hello "' || { echo "stream missing chunk"; exit 1; }

echo "[11/12] embeddings via mock-provider ..."
EMB=$(curl -fsS -X POST http://localhost:8080/v1/embeddings \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"model":"default-mock:text","input":["hi"]}')
LEN=$(echo "$EMB" | jq '.data[0].embedding | length')
[[ "$LEN" == "3" ]] || { echo "embedding length mismatch: $LEN"; exit 1; }

echo "[12/12] verify model_usage rows ..."
docker compose exec -T postgres psql -U app -d app -t -c \
  "SELECT count(*) FROM model_usage WHERE status='ok';" | grep -q "[1-9]" \
  || { echo "model_usage has no rows"; exit 1; }
```

把脚本顶部 `[8/8]` 改 `[8/12]`（注释字符串），并把更早的 `[N/8]` 全部改为 `[N/12]`。

- [ ] **Step 5: 更新 README**

在 README "切片进度" 处把 Slice 3 勾上：

```markdown
- [x] 切片 3：Model Gateway
```

在 "关键端点" 表追加：

```markdown
| POST | /v1/chat/completions | Bearer | OpenAI 兼容,支持 stream |
| POST | /v1/embeddings | Bearer | OpenAI 兼容 |
```

- [ ] **Step 6: 跑测试 + E2E**

```bash
cd D:/IdeaProjects/private-coding-agent
PATH="/d/tools/go/bin:$PATH" go vet ./...
PATH="/d/tools/go/bin:$PATH" go build ./...
PATH="/d/tools/go/bin:$PATH" go test ./... -count=1
```

期望：全 PASS（不含 docker_integration tag）。

E2E：

```bash
cd D:/IdeaProjects/private-coding-agent/deploy/compose
docker compose down 2>&1 | tail -1
./test-e2e.sh
```

期望：最后输出 `E2E PASS`。

- [ ] **Step 7: commit**

```bash
git add internal/modelgw/mockserver/ \
        cmd/server/main.go \
        deploy/compose/docker-compose.yml \
        deploy/compose/test-e2e.sh \
        README.md
git commit -m "feat(modelgw): main wiring + mock-provider compose service + E2E + README"
```

---

## 验收（end-of-slice checklist）

- [ ] `go test ./...` 全 PASS（不含 tag）
- [ ] `go test -tags=docker_integration ./...` 仍 PASS（Slice 2 回归）
- [ ] `go vet ./...` 干净；`go build ./...` 干净
- [ ] `pca/sandbox:base` 镜像存在（Slice 2 遗留）
- [ ] `mock-provider` 镜像由 compose 构建
- [ ] `docker compose up -d --build` 后 `/healthz` 200
- [ ] `test-e2e.sh` 跑通（最后输出 `E2E PASS`，含 chat 非流 + stream + embeddings + model_usage 校验）
- [ ] `model_usage` 表里有 status=ok 行
- [ ] `audit_log` 含 `/v1/*` 路径请求
- [ ] git tree clean

---

## Self-Review

**Spec coverage**:
- spec §4 包结构：Task 1（types）、Task 2（repo providers）、Task 3（repo usage + recorder）、Task 4（validate）、Task 5（provider+registry）、Task 6-7（OpenAI/Ollama）、Task 8-10（Claude）、Task 11（gateway）、Task 12（sse+handler）、Task 13（main+compose+E2E）—— **14 Task 全覆盖**
- spec §5.2 Provider 接口：Task 5 实现，三 provider Task 6-10 各自满足
- spec §5.3 数据库 0005/0006：Task 2/3 覆盖
- spec §5.4 HTTP 端点：Task 12 覆盖
- spec §6 数据流 1-7：流 1/2/3 Task 11+12；流 4 Task 5；流 5 Task 3；流 6 Task 8+9+10；流 7 横切
- spec §7 错误处理：Task 12 `mapErrorToAPI` 覆盖全部 11 条
- spec §8 测试策略：单元 + httptest 集成 + handler + E2E + mockserver 全部就位
- spec §9 Task 拆解：Task 0 含 Slice 2 carry-over

**Placeholder scan**: 无 TBD / "类似 Task N" / 占位代码块。

**Type consistency**:
- `ChatRequest` / `ChatResponse` / `ChatStreamChunk` / `EmbeddingsRequest` / `EmbeddingsResponse` 在 Task 1 定义，后续 Task 6-12 引用一致
- `Provider` 接口在 Task 5 定义；Task 6-10 三个 provider 实现签名一致；Task 11 Gateway / Task 12 Handler 调用一致
- `ProviderRegistry.Resolve` 在 Task 5 定义，Task 11 Gateway 和 Task 12 Handler 都通过 `Registry()` getter 调用
- `UsageRecorder.Record` 签名 `(e CallEvent)`，Task 11 Gateway 调用一致
- `mockProvider` 在 Task 11 定义，Task 12 handler 测试复用（同 package_test）
- `ProviderError.StatusCode` 在 Task 1 定义，Task 6/10 创建实例，Task 12 `mapErrorToAPI` 用 `errors.As` 提取
- Anthropic 内部类型（`anthropicMessagesReq` 等）全部非导出，仅在 Task 8/9/10 内 package 引用；测试用 internal test package
