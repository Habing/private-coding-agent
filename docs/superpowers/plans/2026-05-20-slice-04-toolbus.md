# Slice 4 — Tool Bus + Internal MCP Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 交付 `internal/toolbus` 包 + `POST /tools/invoke` + `GET /tools` HTTP API，含 8 个 internal tools（fs.read/write/list/glob、grep、shell.exec、llm.chat、llm.embed），所有工具通过 `Tool` 接口 in-process Go 调度；fs/shell 走 `sandbox.Runtime`，llm 走 `modelgw.Gateway`；input/output 仅记 sha256；JSON Schema 校验；新 `tool_invocations` 表。

**Architecture:** 三层。HTTP handler → `Bus`（取 tool / schema 校验 / sha256 / invoke / record）→ `Tool` 接口（8 实现）。Tool 实现通过构造时依赖注入 `sandbox.Runtime` / `modelgw.Gateway`。schema 用 `santhosh-tekuri/jsonschema/v6` 启动期编译缓存；invocation 持久化用 detached ctx 不阻塞主路径。

**Tech Stack:**
- Go 1.26、gin、pgx v5、testify、dockertest（沿用既有）
- 新增直接依赖：`github.com/santhosh-tekuri/jsonschema/v6`、`github.com/bmatcuk/doublestar/v4`
- sha256 用标准库 `crypto/sha256`

---

## 前置条件

依赖 Slice 1.5 / 2 / 3 已完成（HEAD = `53822a5`）。

## File Structure

```
internal/modelgw/
  redact.go                       新建 (Task 0): redact env value in error body
  redact_test.go                  新建

internal/toolbus/
  tool.go                         Tool 接口 + ToolDef
  errors.go                       错误哨兵
  errors_test.go
  registry.go                     Registry
  registry_test.go
  schema.go                       JSON Schema 编译/校验 helper
  schema_test.go
  repo.go                         InvocationRepo (PG)
  recorder.go                     InvocationRecorder
  repo_test.go                    dockertest 集成 (TestMain 起 PG)
  recorder_test.go                dockertest 集成
  bus.go                          Bus 编排
  bus_test.go                     mockTool 单测
  handler.go                      HTTP handlers + 错误映射
  handler_test.go                 mockBus 单测
  tools/
    fs.go                         fs.read / fs.write / fs.list / fs.glob (4 tools)
    fs_test.go                    mockRuntime 单测
    fs_integration_test.go        docker_integration tag, 真沙箱
    grep.go
    grep_test.go
    grep_integration_test.go
    shell.go
    shell_test.go
    shell_integration_test.go
    llm.go                        llm.chat / llm.embed (2 tools)
    llm_test.go                   mockGateway 单测
    llm_integration_test.go       httptest mock-provider

internal/db/migrations/
  0007_create_tool_invocations.up.sql
  0007_create_tool_invocations.down.sql

cmd/server/main.go                修改：装配 ToolBus + 注册 8 个 tool + routes
deploy/compose/test-e2e.sh        修改：增加 [13/16]-[16/16] 4 步
README.md                         修改：进度勾选 + /tools/* 端点
```

---

## Task 0: Slice 3 carry-over — ProviderError redact

**Files:**
- Create: `internal/modelgw/redact.go`
- Create: `internal/modelgw/redact_test.go`
- Modify: `internal/modelgw/provider_openai.go`（2 处 `&ProviderError{...}` 构造）
- Modify: `internal/modelgw/provider_claude.go`（2 处 `&ProviderError{...}` 构造）

### Step 1: 写 redact_test.go

`internal/modelgw/redact_test.go`:

```go
package modelgw

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRedact_ReplacesEnvValue(t *testing.T) {
	t.Setenv("TEST_REDACT_KEY", "sk-secret-1234567890abcdef")
	out := redact("error: bad key sk-secret-1234567890abcdef seen", []string{"TEST_REDACT_KEY"})
	require.Equal(t, "error: bad key [REDACTED] seen", out)
}

func TestRedact_NoEnvNoChange(t *testing.T) {
	out := redact("plain body", []string{"TEST_REDACT_KEY_UNSET"})
	require.Equal(t, "plain body", out)
}

func TestRedact_EmptyEnvSkipped(t *testing.T) {
	t.Setenv("TEST_REDACT_KEY", "")
	out := redact("plain body", []string{"TEST_REDACT_KEY"})
	require.Equal(t, "plain body", out)
}

func TestRedact_ShortValueSkipped(t *testing.T) {
	// secret too short (< 8 chars) -> 不替换避免误伤短字符串
	t.Setenv("TEST_SHORT", "short")
	out := redact("contains short here", []string{"TEST_SHORT"})
	require.True(t, strings.Contains(out, "short"))
}

func TestRedact_MultipleEnvs(t *testing.T) {
	t.Setenv("TEST_A", "alpha-secret-12345")
	t.Setenv("TEST_B", "beta-secret-12345")
	out := redact("alpha-secret-12345 and beta-secret-12345", []string{"TEST_A", "TEST_B"})
	require.Equal(t, "[REDACTED] and [REDACTED]", out)
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
cd D:/IdeaProjects/private-coding-agent
PATH="/d/tools/go/bin:$PATH" go test ./internal/modelgw/... -run TestRedact -count=1
```

期望：编译失败（redact 不存在）。

- [ ] **Step 3: 实现 `internal/modelgw/redact.go`**

```go
package modelgw

import (
	"os"
	"strings"
)

// redact replaces any occurrence of env var values (named in envNames) with
// "[REDACTED]" in s. Only env values >= 8 characters are replaced to avoid
// matching trivially short secrets.
//
// Used by Provider implementations when building ProviderError to prevent
// API keys from leaking into audit_log via provider error bodies.
func redact(s string, envNames []string) string {
	for _, name := range envNames {
		v := os.Getenv(name)
		if v != "" && len(v) >= 8 {
			s = strings.ReplaceAll(s, v, "[REDACTED]")
		}
	}
	return s
}
```

- [ ] **Step 4: 跑 redact 测试通过**

```bash
PATH="/d/tools/go/bin:$PATH" go test ./internal/modelgw/... -run TestRedact -count=1 -v
```

期望：5 测试 PASS。

- [ ] **Step 5: 修改 `provider_openai.go` 4 处 ProviderError 构造**

Read `internal/modelgw/provider_openai.go`，找到 4 处 `&ProviderError{StatusCode: ..., Body: string(body)}` 调用（ChatCompletion / ChatCompletionStream 起始错检 / Embeddings 共 3 处；如果只 3 处也照做）。

每处都改为：

```go
return nil, &ProviderError{StatusCode: resp.StatusCode, Body: redact(string(body), []string{p.apiKeyEnv})}
```

或对返 error 的语句：

```go
return &ProviderError{StatusCode: resp.StatusCode, Body: redact(string(body), []string{p.apiKeyEnv})}
```

- [ ] **Step 6: 修改 `provider_claude.go` 同样处理**

Read `internal/modelgw/provider_claude.go`，2 处 `&ProviderError{...}`（ChatCompletion / ChatCompletionStream），每处都把 `Body: string(body)` 改为 `Body: redact(string(body), []string{p.apiKeyEnv})`。

- [ ] **Step 7: 跑所有 modelgw 测试**

```bash
PATH="/d/tools/go/bin:$PATH" go vet ./...
PATH="/d/tools/go/bin:$PATH" go test ./internal/modelgw/... -count=1
```

期望：所有现有测试 + 5 个 redact 测试 PASS。

- [ ] **Step 8: commit**

```bash
git add internal/modelgw/redact.go internal/modelgw/redact_test.go \
        internal/modelgw/provider_openai.go internal/modelgw/provider_claude.go
git commit -m "fix(modelgw): redact env-derived secrets in ProviderError.Body"
```

---

## Task 1: `Tool` 接口 + 错误哨兵

**Files:**
- Create: `internal/toolbus/tool.go`
- Create: `internal/toolbus/errors.go`
- Create: `internal/toolbus/errors_test.go`

- [ ] **Step 1: 写测试**

`internal/toolbus/errors_test.go`:

```go
package toolbus_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/toolbus"
)

func TestErrorSentinels(t *testing.T) {
	require.Error(t, toolbus.ErrToolNotFound)
	require.Error(t, toolbus.ErrInvalidArguments)
	require.Error(t, toolbus.ErrSandboxIDRequired)
	require.Error(t, toolbus.ErrToolFailed)
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
PATH="/d/tools/go/bin:$PATH" go test ./internal/toolbus/...
```

期望：编译失败（package 不存在）。

- [ ] **Step 3: 实现 `tool.go` 与 `errors.go`**

`internal/toolbus/tool.go`:

```go
// Package toolbus dispatches tool invocations from Agents / Workflows to
// concrete implementations (in-process Go for built-in tools).
//
// Each Tool advertises a JSON Schema for its inputs. Bus.Invoke validates
// input against the schema, hashes input/output for audit, calls Tool.Invoke,
// and records a row in tool_invocations.
package toolbus

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
)

// Tool is the unit dispatched by Bus. Implementations must be safe for
// concurrent use.
type Tool interface {
	// Name returns the unique tool name, e.g. "fs.read".
	Name() string

	// Description is human-readable, surfaced to LLMs.
	Description() string

	// Schema returns a JSON Schema for input args. Must be OpenAI tool
	// calling compatible (i.e. usable as tools[].function.parameters).
	Schema() json.RawMessage

	// Invoke executes the tool. input is the raw JSON already validated
	// against Schema(); implementations unmarshal it into their own struct.
	// ctx carries timeout/cancellation. tenantID/userID are passed through
	// to downstream services (sandbox.Runtime / modelgw.Gateway) for
	// authorization and auditing.
	Invoke(ctx context.Context, tenantID, userID uuid.UUID,
		input json.RawMessage) (json.RawMessage, error)
}

// ToolDef is the OpenAI-tool-calling-compatible definition returned by
// Bus.ListTools.
type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}
```

`internal/toolbus/errors.go`:

```go
package toolbus

import "errors"

// Sentinel errors. Callers use errors.Is to map to HTTP status codes.
var (
	ErrToolNotFound      = errors.New("toolbus: tool not found")
	ErrInvalidArguments  = errors.New("toolbus: invalid arguments")
	ErrSandboxIDRequired = errors.New("toolbus: sandbox_id required")
	ErrToolFailed        = errors.New("toolbus: tool execution failed")
)
```

- [ ] **Step 4: 跑测试**

```bash
PATH="/d/tools/go/bin:$PATH" go test ./internal/toolbus/... -count=1 -v
```

期望：1 测试 PASS。

- [ ] **Step 5: commit**

```bash
git add internal/toolbus/tool.go internal/toolbus/errors.go internal/toolbus/errors_test.go
git commit -m "feat(toolbus): Tool interface + error sentinels"
```

---

## Task 2: `Registry`

**Files:**
- Create: `internal/toolbus/registry.go`
- Create: `internal/toolbus/registry_test.go`

- [ ] **Step 1: 写测试**

`internal/toolbus/registry_test.go`:

```go
package toolbus_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/toolbus"
)

// noopTool 是测试 helper:满足 Tool 接口但实际不工作。
type noopTool struct {
	name string
}

func (n noopTool) Name() string                 { return n.name }
func (n noopTool) Description() string          { return "noop" }
func (n noopTool) Schema() json.RawMessage      { return json.RawMessage(`{"type":"object"}`) }
func (n noopTool) Invoke(_ context.Context, _, _ uuid.UUID, _ json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := toolbus.NewRegistry()
	require.NoError(t, r.Register(noopTool{name: "fs.read"}))

	got, ok := r.Get("fs.read")
	require.True(t, ok)
	require.Equal(t, "fs.read", got.Name())
}

func TestRegistry_Get_NotFound(t *testing.T) {
	r := toolbus.NewRegistry()
	_, ok := r.Get("missing")
	require.False(t, ok)
}

func TestRegistry_DuplicateName(t *testing.T) {
	r := toolbus.NewRegistry()
	require.NoError(t, r.Register(noopTool{name: "a"}))
	require.Error(t, r.Register(noopTool{name: "a"}))
}

func TestRegistry_List_Sorted(t *testing.T) {
	r := toolbus.NewRegistry()
	require.NoError(t, r.Register(noopTool{name: "z.t"}))
	require.NoError(t, r.Register(noopTool{name: "a.t"}))
	require.NoError(t, r.Register(noopTool{name: "m.t"}))

	list := r.List()
	require.Len(t, list, 3)
	require.Equal(t, "a.t", list[0].Name())
	require.Equal(t, "m.t", list[1].Name())
	require.Equal(t, "z.t", list[2].Name())
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
PATH="/d/tools/go/bin:$PATH" go test ./internal/toolbus/... -run TestRegistry
```

- [ ] **Step 3: 实现 `registry.go`**

```go
package toolbus

import (
	"fmt"
	"sort"
	"sync"
)

// Registry holds the active in-process tools by name.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

func NewRegistry() *Registry {
	return &Registry{tools: map[string]Tool{}}
}

// Register adds a tool. Returns an error on duplicate name.
func (r *Registry) Register(t Tool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	name := t.Name()
	if _, exists := r.tools[name]; exists {
		return fmt.Errorf("toolbus: duplicate tool name %q", name)
	}
	r.tools[name] = t
	return nil
}

// Get returns the tool by name (ok=false if missing).
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// List returns all tools sorted by name.
func (r *Registry) List() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}
```

- [ ] **Step 4: 跑测试**

```bash
PATH="/d/tools/go/bin:$PATH" go test ./internal/toolbus/... -count=1 -v
```

期望：4 测试 PASS。

- [ ] **Step 5: commit**

```bash
git add internal/toolbus/registry.go internal/toolbus/registry_test.go
git commit -m "feat(toolbus): Registry with duplicate detection and sorted List"
```

---

## Task 3: `schema.go` — JSON Schema 编译与校验

**Files:**
- Create: `internal/toolbus/schema.go`
- Create: `internal/toolbus/schema_test.go`

- [ ] **Step 1: 装依赖**

```bash
PATH="/d/tools/go/bin:$PATH" go get github.com/santhosh-tekuri/jsonschema/v6
```

- [ ] **Step 2: 写测试**

`internal/toolbus/schema_test.go`:

```go
package toolbus_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/toolbus"
)

func TestCompileSchema_ValidObject(t *testing.T) {
	raw := json.RawMessage(`{
        "type":"object",
        "properties":{"x":{"type":"integer"}},
        "required":["x"]
    }`)
	s, err := toolbus.CompileSchema(raw)
	require.NoError(t, err)
	require.NotNil(t, s)
}

func TestCompileSchema_BadJSON(t *testing.T) {
	_, err := toolbus.CompileSchema(json.RawMessage(`not json`))
	require.Error(t, err)
}

func TestValidate_OK(t *testing.T) {
	s, _ := toolbus.CompileSchema(json.RawMessage(`{
        "type":"object",
        "properties":{"x":{"type":"integer"}},
        "required":["x"]
    }`))
	require.NoError(t, toolbus.Validate(s, json.RawMessage(`{"x":5}`)))
}

func TestValidate_MissingRequired(t *testing.T) {
	s, _ := toolbus.CompileSchema(json.RawMessage(`{
        "type":"object",
        "properties":{"x":{"type":"integer"}},
        "required":["x"]
    }`))
	err := toolbus.Validate(s, json.RawMessage(`{}`))
	require.Error(t, err)
}

func TestValidate_TypeMismatch(t *testing.T) {
	s, _ := toolbus.CompileSchema(json.RawMessage(`{
        "type":"object",
        "properties":{"x":{"type":"integer"}},
        "required":["x"]
    }`))
	err := toolbus.Validate(s, json.RawMessage(`{"x":"not-int"}`))
	require.Error(t, err)
}
```

- [ ] **Step 3: 跑测试确认失败**

```bash
PATH="/d/tools/go/bin:$PATH" go test ./internal/toolbus/... -run TestCompileSchema -count=1
```

- [ ] **Step 4: 实现 `schema.go`**

```go
package toolbus

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// CompileSchema compiles a JSON Schema document. The result is reusable across
// many Validate calls (thread-safe).
func CompileSchema(raw json.RawMessage) (*jsonschema.Schema, error) {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("schema: parse: %w", err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("inline.json", doc); err != nil {
		return nil, fmt.Errorf("schema: add: %w", err)
	}
	s, err := c.Compile("inline.json")
	if err != nil {
		return nil, fmt.Errorf("schema: compile: %w", err)
	}
	return s, nil
}

// Validate checks input against the compiled schema. Returns an error wrapping
// ErrInvalidArguments on failure.
func Validate(s *jsonschema.Schema, input json.RawMessage) error {
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(input))
	if err != nil {
		return fmt.Errorf("%w: input not JSON: %v", ErrInvalidArguments, err)
	}
	if err := s.Validate(inst); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidArguments, err)
	}
	return nil
}
```

- [ ] **Step 5: 跑测试**

```bash
PATH="/d/tools/go/bin:$PATH" go vet ./...
PATH="/d/tools/go/bin:$PATH" go test ./internal/toolbus/... -count=1 -v
```

期望：5 测试 PASS。

- [ ] **Step 6: commit**

```bash
PATH="/d/tools/go/bin:$PATH" go mod tidy
git add internal/toolbus/schema.go internal/toolbus/schema_test.go go.mod go.sum
git commit -m "feat(toolbus): JSON Schema compile + validate via santhosh-tekuri/jsonschema/v6"
```

---

## Task 4: migration 0007 + `InvocationRepo` + `InvocationRecorder`

**Files:**
- Create: `internal/db/migrations/0007_create_tool_invocations.up.sql`
- Create: `internal/db/migrations/0007_create_tool_invocations.down.sql`
- Create: `internal/toolbus/repo.go`
- Create: `internal/toolbus/recorder.go`
- Create: `internal/toolbus/repo_test.go`
- Create: `internal/toolbus/recorder_test.go`

- [ ] **Step 1: 写迁移**

`internal/db/migrations/0007_create_tool_invocations.up.sql`:

```sql
CREATE TABLE tool_invocations (
    id              BIGSERIAL PRIMARY KEY,
    occurred_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    tenant_id       UUID NOT NULL,
    user_id         UUID NOT NULL,
    tool_name       TEXT NOT NULL,
    status          TEXT NOT NULL,
    error_class     TEXT NOT NULL DEFAULT '',
    duration_ms     INT NOT NULL,
    input_sha256    TEXT NOT NULL DEFAULT '',
    output_sha256   TEXT NOT NULL DEFAULT ''
);

CREATE INDEX tool_invocations_tenant_time_idx
    ON tool_invocations(tenant_id, occurred_at DESC);
```

`internal/db/migrations/0007_create_tool_invocations.down.sql`:

```sql
DROP TABLE tool_invocations;
```

- [ ] **Step 2: 写 `repo.go` + `recorder.go`**

`internal/toolbus/repo.go`:

```go
package toolbus

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// InvocationEvent is the persisted record of a single tool invocation.
type InvocationEvent struct {
	TenantID     uuid.UUID
	UserID       uuid.UUID
	ToolName     string
	Status       string // "ok" / "error"
	ErrorClass   string
	DurationMS   int64
	InputSHA256  string
	OutputSHA256 string
	OccurredAt   time.Time
}

// InvocationRepo writes tool_invocations rows.
type InvocationRepo struct {
	pool *pgxpool.Pool
}

func NewInvocationRepo(pool *pgxpool.Pool) *InvocationRepo {
	return &InvocationRepo{pool: pool}
}

// Insert appends one row.
func (r *InvocationRepo) Insert(ctx context.Context, e InvocationEvent) error {
	_, err := r.pool.Exec(ctx, `
INSERT INTO tool_invocations
  (occurred_at, tenant_id, user_id, tool_name, status, error_class,
   duration_ms, input_sha256, output_sha256)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		e.OccurredAt, e.TenantID, e.UserID, e.ToolName, e.Status, e.ErrorClass,
		e.DurationMS, e.InputSHA256, e.OutputSHA256)
	if err != nil {
		return fmt.Errorf("insert tool_invocations: %w", err)
	}
	return nil
}

// CountByTenant is a test/ops query.
func (r *InvocationRepo) CountByTenant(ctx context.Context, tenantID uuid.UUID) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM tool_invocations WHERE tenant_id=$1`, tenantID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count tool_invocations: %w", err)
	}
	return n, nil
}
```

`internal/toolbus/recorder.go`:

```go
package toolbus

import (
	"context"
	"time"
)

const invocationWriteTimeout = 5 * time.Second

// InvocationRecorder writes tool invocations with a detached context, so a
// canceled request context never drops the audit.
type InvocationRecorder struct {
	repo  *InvocationRepo
	onErr func(error)
}

func NewInvocationRecorder(repo *InvocationRepo, onErr func(error)) *InvocationRecorder {
	return &InvocationRecorder{repo: repo, onErr: onErr}
}

// Record writes one row. Failures only call onErr; never block the caller.
func (r *InvocationRecorder) Record(e InvocationEvent) {
	ctx, cancel := context.WithTimeout(context.Background(), invocationWriteTimeout)
	defer cancel()
	if err := r.repo.Insert(ctx, e); err != nil && r.onErr != nil {
		r.onErr(err)
	}
}
```

- [ ] **Step 3: 写 dockertest 集成测试**

`internal/toolbus/repo_test.go`:

```go
package toolbus_test

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
	"github.com/yourorg/private-coding-agent/internal/toolbus"
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

func TestInvocationRepo_InsertAndCount(t *testing.T) {
	ctx := context.Background()
	pg, err := pgxpool.New(ctx, testDSN)
	require.NoError(t, err)
	t.Cleanup(pg.Close)

	tid := uuid.New()
	repo := toolbus.NewInvocationRepo(pg)
	require.NoError(t, repo.Insert(ctx, toolbus.InvocationEvent{
		TenantID: tid, UserID: uuid.New(),
		ToolName: "fs.read", Status: "ok",
		DurationMS: 10,
		InputSHA256: "abc", OutputSHA256: "def",
		OccurredAt: time.Now(),
	}))

	n, err := repo.CountByTenant(ctx, tid)
	require.NoError(t, err)
	require.Equal(t, 1, n)
}
```

`internal/toolbus/recorder_test.go`:

```go
package toolbus_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/toolbus"
)

func TestInvocationRecorder_DetachedCtx(t *testing.T) {
	ctx := context.Background()
	pg, err := pgxpool.New(ctx, testDSN)
	require.NoError(t, err)
	t.Cleanup(pg.Close)

	var errs []error
	var mu sync.Mutex
	rec := toolbus.NewInvocationRecorder(toolbus.NewInvocationRepo(pg), func(err error) {
		mu.Lock()
		errs = append(errs, err)
		mu.Unlock()
	})

	tid := uuid.New()
	rec.Record(toolbus.InvocationEvent{
		TenantID: tid, UserID: uuid.New(),
		ToolName: "fs.read", Status: "ok",
		DurationMS: 1, OccurredAt: time.Now(),
	})

	require.Empty(t, errs)
	n, err := toolbus.NewInvocationRepo(pg).CountByTenant(ctx, tid)
	require.NoError(t, err)
	require.Equal(t, 1, n)
}
```

- [ ] **Step 4: 跑测试**

```bash
PATH="/d/tools/go/bin:$PATH" go vet ./...
PATH="/d/tools/go/bin:$PATH" go test ./internal/toolbus/... -count=1 -v
```

期望：所有现有 + 2 集成测试 PASS（PG 启动 ~5s）。

- [ ] **Step 5: commit**

```bash
git add internal/db/migrations/0007_create_tool_invocations.up.sql \
        internal/db/migrations/0007_create_tool_invocations.down.sql \
        internal/toolbus/repo.go internal/toolbus/recorder.go \
        internal/toolbus/repo_test.go internal/toolbus/recorder_test.go
git commit -m "feat(toolbus): tool_invocations migration + InvocationRepo + Recorder"
```

---

## Task 5: `Bus` 编排

**Files:**
- Create: `internal/toolbus/bus.go`
- Create: `internal/toolbus/bus_test.go`

- [ ] **Step 1: 写测试**

`internal/toolbus/bus_test.go`:

```go
package toolbus_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/toolbus"
)

// mockTool is configurable for bus_test.
type mockTool struct {
	name        string
	schema      json.RawMessage
	invokeRet   json.RawMessage
	invokeErr   error
	calledTimes int
	mu          sync.Mutex
}

func (m *mockTool) Name() string                 { return m.name }
func (m *mockTool) Description() string          { return "mock " + m.name }
func (m *mockTool) Schema() json.RawMessage      { return m.schema }
func (m *mockTool) Invoke(_ context.Context, _, _ uuid.UUID, _ json.RawMessage) (json.RawMessage, error) {
	m.mu.Lock()
	m.calledTimes++
	m.mu.Unlock()
	return m.invokeRet, m.invokeErr
}

func busWith(t *testing.T, tools ...toolbus.Tool) (*toolbus.Bus, *sync.Mutex, *[]error) {
	t.Helper()
	ctx := context.Background()
	pg, err := pgxpool.New(ctx, testDSN)
	require.NoError(t, err)
	t.Cleanup(pg.Close)

	reg := toolbus.NewRegistry()
	for _, tool := range tools {
		require.NoError(t, reg.Register(tool))
	}
	var errs []error
	var mu sync.Mutex
	rec := toolbus.NewInvocationRecorder(toolbus.NewInvocationRepo(pg), func(err error) {
		mu.Lock()
		errs = append(errs, err)
		mu.Unlock()
	})
	bus, err := toolbus.NewBus(reg, rec)
	require.NoError(t, err)
	return bus, &mu, &errs
}

const objSchemaWithX = `{"type":"object","properties":{"x":{"type":"integer"}},"required":["x"]}`

func TestBus_Invoke_OK(t *testing.T) {
	tool := &mockTool{name: "t.ok", schema: json.RawMessage(objSchemaWithX),
		invokeRet: json.RawMessage(`{"ok":true}`)}
	bus, _, _ := busWith(t, tool)

	out, err := bus.Invoke(context.Background(), uuid.New(), uuid.New(),
		"t.ok", json.RawMessage(`{"x":5}`))
	require.NoError(t, err)
	require.JSONEq(t, `{"ok":true}`, string(out))
	require.Equal(t, 1, tool.calledTimes)
}

func TestBus_Invoke_NotFound(t *testing.T) {
	bus, _, _ := busWith(t)
	_, err := bus.Invoke(context.Background(), uuid.New(), uuid.New(),
		"missing", json.RawMessage(`{}`))
	require.ErrorIs(t, err, toolbus.ErrToolNotFound)
}

func TestBus_Invoke_SchemaFail(t *testing.T) {
	tool := &mockTool{name: "t.schema", schema: json.RawMessage(objSchemaWithX)}
	bus, _, _ := busWith(t, tool)

	_, err := bus.Invoke(context.Background(), uuid.New(), uuid.New(),
		"t.schema", json.RawMessage(`{"x":"not-int"}`))
	require.ErrorIs(t, err, toolbus.ErrInvalidArguments)
	require.Equal(t, 0, tool.calledTimes)
}

func TestBus_Invoke_ToolError_RecordedAsError(t *testing.T) {
	tool := &mockTool{name: "t.err", schema: json.RawMessage(objSchemaWithX),
		invokeErr: errors.New("downstream boom")}
	bus, _, _ := busWith(t, tool)

	_, err := bus.Invoke(context.Background(), uuid.New(), uuid.New(),
		"t.err", json.RawMessage(`{"x":1}`))
	require.Error(t, err)
}

func TestBus_ListTools(t *testing.T) {
	bus, _, _ := busWith(t,
		&mockTool{name: "a", schema: json.RawMessage(objSchemaWithX)},
		&mockTool{name: "b", schema: json.RawMessage(objSchemaWithX)})
	list := bus.ListTools(context.Background(), uuid.New())
	require.Len(t, list, 2)
	require.Equal(t, "a", list[0].Name)
	require.Equal(t, "b", list[1].Name)
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
PATH="/d/tools/go/bin:$PATH" go test ./internal/toolbus/... -run TestBus
```

- [ ] **Step 3: 实现 `bus.go`**

```go
package toolbus

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Bus orchestrates tool invocation: schema-validate input, hash for audit,
// invoke the tool, persist InvocationEvent. Stateless and concurrent-safe.
type Bus struct {
	reg      *Registry
	recorder *InvocationRecorder
	schemas  map[string]*jsonschema.Schema
}

// NewBus compiles each registered tool's schema once. Returns an error if any
// schema fails to compile (callers should not start the server in that case).
func NewBus(reg *Registry, recorder *InvocationRecorder) (*Bus, error) {
	schemas := map[string]*jsonschema.Schema{}
	for _, t := range reg.List() {
		s, err := CompileSchema(t.Schema())
		if err != nil {
			return nil, fmt.Errorf("toolbus: compile schema for %q: %w", t.Name(), err)
		}
		schemas[t.Name()] = s
	}
	return &Bus{reg: reg, recorder: recorder, schemas: schemas}, nil
}

// ListTools returns all registered tools as OpenAI-tool-calling-compatible defs.
func (b *Bus) ListTools(ctx context.Context, tenantID uuid.UUID) []ToolDef {
	tools := b.reg.List()
	out := make([]ToolDef, 0, len(tools))
	for _, t := range tools {
		out = append(out, ToolDef{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  t.Schema(),
		})
	}
	return out
}

// Invoke runs the named tool with the given input. Records every call to
// tool_invocations (status=ok or error). Returns the tool's raw JSON output.
func (b *Bus) Invoke(ctx context.Context, tenantID, userID uuid.UUID,
	toolName string, input json.RawMessage) (json.RawMessage, error) {

	tool, ok := b.reg.Get(toolName)
	if !ok {
		return nil, ErrToolNotFound
	}
	schema := b.schemas[toolName]
	if err := Validate(schema, input); err != nil {
		return nil, err
	}

	inputSHA := sha256Hex(input)
	start := time.Now()
	output, callErr := tool.Invoke(ctx, tenantID, userID, input)
	dur := time.Since(start)

	event := InvocationEvent{
		TenantID:    tenantID, UserID: userID,
		ToolName:    toolName,
		DurationMS:  dur.Milliseconds(),
		InputSHA256: inputSHA,
		OccurredAt:  time.Now(),
	}
	if callErr != nil {
		event.Status = "error"
		event.ErrorClass = classifyError(callErr)
	} else {
		event.Status = "ok"
		event.OutputSHA256 = sha256Hex(output)
	}
	b.recorder.Record(event)

	return output, callErr
}

func sha256Hex(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// classifyError maps known sentinels to short stable strings for analytics.
func classifyError(err error) string {
	switch {
	case errors.Is(err, ErrInvalidArguments), errors.Is(err, ErrSandboxIDRequired):
		return "validation"
	case errors.Is(err, ErrToolNotFound):
		return "tool_not_found"
	}
	return "other"
}
```

- [ ] **Step 4: 跑测试**

```bash
PATH="/d/tools/go/bin:$PATH" go vet ./...
PATH="/d/tools/go/bin:$PATH" go test ./internal/toolbus/... -count=1 -v
```

期望：5 个 bus 测试 + 所有现有测试 PASS。

- [ ] **Step 5: commit**

```bash
git add internal/toolbus/bus.go internal/toolbus/bus_test.go
git commit -m "feat(toolbus): Bus orchestration (schema validate + sha256 + record)"
```

---

## Task 6: `tools/fs.go` — 4 个 fs 工具

**Files:**
- Create: `internal/toolbus/tools/fs.go`
- Create: `internal/toolbus/tools/fs_test.go`

- [ ] **Step 1: 装 doublestar**

```bash
PATH="/d/tools/go/bin:$PATH" go get github.com/bmatcuk/doublestar/v4
```

- [ ] **Step 2: 写测试 `tools/fs_test.go`**

```go
package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/sandbox"
	"github.com/yourorg/private-coding-agent/internal/toolbus/tools"
)

// mockRuntime captures the args of the last call.
type mockRuntime struct {
	readRet  []byte
	readErr  error
	writeErr error
	execRet  *sandbox.ExecResult
	execErr  error

	lastRead  string
	lastWrite struct {
		path string
		data []byte
	}
	lastExec sandbox.ExecOpts
}

func (m *mockRuntime) Create(_ context.Context, _ sandbox.CreateOpts) (*sandbox.Sandbox, error) {
	panic("not used in fs tests")
}
func (m *mockRuntime) Get(_ context.Context, _, _ uuid.UUID) (*sandbox.Sandbox, error) {
	panic("not used")
}
func (m *mockRuntime) Destroy(_ context.Context, _, _ uuid.UUID) error { panic("not used") }
func (m *mockRuntime) Exec(_ context.Context, _, _ uuid.UUID, opts sandbox.ExecOpts) (*sandbox.ExecResult, error) {
	m.lastExec = opts
	return m.execRet, m.execErr
}
func (m *mockRuntime) ReadFile(_ context.Context, _, _ uuid.UUID, path string) ([]byte, error) {
	m.lastRead = path
	return m.readRet, m.readErr
}
func (m *mockRuntime) WriteFile(_ context.Context, _, _ uuid.UUID, path string, data []byte) error {
	m.lastWrite.path = path
	m.lastWrite.data = data
	return m.writeErr
}
func (m *mockRuntime) Snapshot(_ context.Context, _, _ uuid.UUID) (string, error) {
	panic("not used")
}

const validSandboxJSON = `"00000000-0000-0000-0000-000000000001"`

func TestFSRead_OK(t *testing.T) {
	rt := &mockRuntime{readRet: []byte("hello")}
	tool := tools.NewFSRead(rt)
	out, err := tool.Invoke(context.Background(), uuid.New(), uuid.New(),
		json.RawMessage(`{"sandbox_id":`+validSandboxJSON+`,"path":"foo.txt"}`))
	require.NoError(t, err)
	require.JSONEq(t, `{"content":"hello","size":5}`, string(out))
	require.Equal(t, "foo.txt", rt.lastRead)
}

func TestFSRead_DownstreamError(t *testing.T) {
	rt := &mockRuntime{readErr: sandbox.ErrSandboxNotFound}
	tool := tools.NewFSRead(rt)
	_, err := tool.Invoke(context.Background(), uuid.New(), uuid.New(),
		json.RawMessage(`{"sandbox_id":`+validSandboxJSON+`,"path":"foo.txt"}`))
	require.ErrorIs(t, err, sandbox.ErrSandboxNotFound)
}

func TestFSRead_InvalidUTF8Replaced(t *testing.T) {
	rt := &mockRuntime{readRet: []byte{0xff, 0xfe, 'a'}}
	tool := tools.NewFSRead(rt)
	out, err := tool.Invoke(context.Background(), uuid.New(), uuid.New(),
		json.RawMessage(`{"sandbox_id":`+validSandboxJSON+`,"path":"x"}`))
	require.NoError(t, err)
	// 0xff 0xfe 各被 � 替换;"a" 保留
	var got struct {
		Content string `json:"content"`
		Size    int    `json:"size"`
	}
	require.NoError(t, json.Unmarshal(out, &got))
	require.Contains(t, got.Content, "a")
	require.Equal(t, 3, got.Size)
}

func TestFSWrite_OK(t *testing.T) {
	rt := &mockRuntime{}
	tool := tools.NewFSWrite(rt)
	out, err := tool.Invoke(context.Background(), uuid.New(), uuid.New(),
		json.RawMessage(`{"sandbox_id":`+validSandboxJSON+`,"path":"a.txt","content":"hello"}`))
	require.NoError(t, err)
	require.JSONEq(t, `{"bytes_written":5}`, string(out))
	require.Equal(t, "a.txt", rt.lastWrite.path)
	require.Equal(t, []byte("hello"), rt.lastWrite.data)
}

func TestFSList_ParsesFindOutput(t *testing.T) {
	rt := &mockRuntime{execRet: &sandbox.ExecResult{
		ExitCode: 0,
		Stdout:   []byte("src\td\t4096\ngo.mod\tf\t123\n"),
	}}
	tool := tools.NewFSList(rt)
	out, err := tool.Invoke(context.Background(), uuid.New(), uuid.New(),
		json.RawMessage(`{"sandbox_id":`+validSandboxJSON+`,"path":"."}`))
	require.NoError(t, err)
	var got struct {
		Entries []struct {
			Name string `json:"name"`
			Type string `json:"type"`
			Size *int   `json:"size,omitempty"`
		} `json:"entries"`
	}
	require.NoError(t, json.Unmarshal(out, &got))
	require.Len(t, got.Entries, 2)
	require.Equal(t, "src", got.Entries[0].Name)
	require.Equal(t, "dir", got.Entries[0].Type)
	require.Equal(t, "go.mod", got.Entries[1].Name)
	require.Equal(t, "file", got.Entries[1].Type)
	require.NotNil(t, got.Entries[1].Size)
	require.Equal(t, 123, *got.Entries[1].Size)
}

func TestFSGlob_FiltersWithDoublestar(t *testing.T) {
	// find returns all files; tool filters with doublestar pattern.
	rt := &mockRuntime{execRet: &sandbox.ExecResult{
		ExitCode: 0,
		Stdout:   []byte("src/main.go\nsrc/test.txt\nsrc/lib/util.go\n"),
	}}
	tool := tools.NewFSGlob(rt)
	out, err := tool.Invoke(context.Background(), uuid.New(), uuid.New(),
		json.RawMessage(`{"sandbox_id":`+validSandboxJSON+`,"pattern":"**/*.go"}`))
	require.NoError(t, err)
	var got struct {
		Matches []string `json:"matches"`
	}
	require.NoError(t, json.Unmarshal(out, &got))
	require.ElementsMatch(t, []string{"src/main.go", "src/lib/util.go"}, got.Matches)
}

func TestFSRead_MissingSandboxID(t *testing.T) {
	rt := &mockRuntime{}
	tool := tools.NewFSRead(rt)
	_, err := tool.Invoke(context.Background(), uuid.New(), uuid.New(),
		json.RawMessage(`{"path":"foo.txt"}`))
	require.Error(t, err)
	// uuid.Nil 视作未设
}

// silence unused
var _ = errors.Is
```

- [ ] **Step 3: 跑测试确认失败**

```bash
PATH="/d/tools/go/bin:$PATH" go test ./internal/toolbus/tools/... -count=1
```

- [ ] **Step 4: 实现 `tools/fs.go`**

```go
// Package tools holds concrete Tool implementations registered with toolbus.Bus.
package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/google/uuid"

	"github.com/yourorg/private-coding-agent/internal/sandbox"
	"github.com/yourorg/private-coding-agent/internal/toolbus"
)

// Runtime is the subset of sandbox.Runtime used by fs/shell/grep tools.
// Declared locally to keep tools narrowly typed.
type Runtime interface {
	Exec(ctx context.Context, tenantID, id uuid.UUID, opts sandbox.ExecOpts) (*sandbox.ExecResult, error)
	ReadFile(ctx context.Context, tenantID, id uuid.UUID, path string) ([]byte, error)
	WriteFile(ctx context.Context, tenantID, id uuid.UUID, path string, data []byte) error
}

// ---------- fs.read ----------

type fsRead struct{ rt Runtime }

func NewFSRead(rt Runtime) toolbus.Tool { return &fsRead{rt: rt} }

func (t *fsRead) Name() string        { return "fs.read" }
func (t *fsRead) Description() string {
	return "Read a UTF-8 text file from the sandbox workspace. Path is relative to /workspace."
}
func (t *fsRead) Schema() json.RawMessage {
	return json.RawMessage(`{
        "type":"object",
        "properties":{
            "sandbox_id":{"type":"string","format":"uuid"},
            "path":{"type":"string"}
        },
        "required":["sandbox_id","path"],
        "additionalProperties":false
    }`)
}

type fsReadIn struct {
	SandboxID uuid.UUID `json:"sandbox_id"`
	Path      string    `json:"path"`
}

func (t *fsRead) Invoke(ctx context.Context, tenantID, _ uuid.UUID, input json.RawMessage) (json.RawMessage, error) {
	var in fsReadIn
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, fmt.Errorf("%w: %v", toolbus.ErrInvalidArguments, err)
	}
	if in.SandboxID == uuid.Nil {
		return nil, toolbus.ErrSandboxIDRequired
	}
	data, err := t.rt.ReadFile(ctx, tenantID, in.SandboxID, in.Path)
	if err != nil {
		return nil, err
	}
	content := data
	if !utf8.Valid(data) {
		content = []byte(strings.ToValidUTF8(string(data), "�"))
	}
	return json.Marshal(struct {
		Content string `json:"content"`
		Size    int    `json:"size"`
	}{Content: string(content), Size: len(data)})
}

// ---------- fs.write ----------

type fsWrite struct{ rt Runtime }

func NewFSWrite(rt Runtime) toolbus.Tool { return &fsWrite{rt: rt} }

func (t *fsWrite) Name() string { return "fs.write" }
func (t *fsWrite) Description() string {
	return "Write content to a file in the sandbox workspace. Creates intermediate directories. Overwrites if exists."
}
func (t *fsWrite) Schema() json.RawMessage {
	return json.RawMessage(`{
        "type":"object",
        "properties":{
            "sandbox_id":{"type":"string","format":"uuid"},
            "path":{"type":"string"},
            "content":{"type":"string"}
        },
        "required":["sandbox_id","path","content"],
        "additionalProperties":false
    }`)
}

type fsWriteIn struct {
	SandboxID uuid.UUID `json:"sandbox_id"`
	Path      string    `json:"path"`
	Content   string    `json:"content"`
}

func (t *fsWrite) Invoke(ctx context.Context, tenantID, _ uuid.UUID, input json.RawMessage) (json.RawMessage, error) {
	var in fsWriteIn
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, fmt.Errorf("%w: %v", toolbus.ErrInvalidArguments, err)
	}
	if in.SandboxID == uuid.Nil {
		return nil, toolbus.ErrSandboxIDRequired
	}
	data := []byte(in.Content)
	if err := t.rt.WriteFile(ctx, tenantID, in.SandboxID, in.Path, data); err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		BytesWritten int `json:"bytes_written"`
	}{BytesWritten: len(data)})
}

// ---------- fs.list ----------

type fsList struct{ rt Runtime }

func NewFSList(rt Runtime) toolbus.Tool { return &fsList{rt: rt} }

func (t *fsList) Name() string { return "fs.list" }
func (t *fsList) Description() string {
	return "List files and directories under a sandbox path. Non-recursive."
}
func (t *fsList) Schema() json.RawMessage {
	return json.RawMessage(`{
        "type":"object",
        "properties":{
            "sandbox_id":{"type":"string","format":"uuid"},
            "path":{"type":"string"}
        },
        "required":["sandbox_id"],
        "additionalProperties":false
    }`)
}

type fsListIn struct {
	SandboxID uuid.UUID `json:"sandbox_id"`
	Path      string    `json:"path"`
}

type fsListEntry struct {
	Name string `json:"name"`
	Type string `json:"type"` // "file" / "dir"
	Size *int   `json:"size,omitempty"`
}

func (t *fsList) Invoke(ctx context.Context, tenantID, _ uuid.UUID, input json.RawMessage) (json.RawMessage, error) {
	var in fsListIn
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, fmt.Errorf("%w: %v", toolbus.ErrInvalidArguments, err)
	}
	if in.SandboxID == uuid.Nil {
		return nil, toolbus.ErrSandboxIDRequired
	}
	path := in.Path
	if path == "" {
		path = "."
	}
	root := "/workspace"
	if path != "." {
		root = "/workspace/" + strings.TrimPrefix(path, "/")
	}
	res, err := t.rt.Exec(ctx, tenantID, in.SandboxID, sandbox.ExecOpts{
		Cmd:        []string{"find", root, "-mindepth", "1", "-maxdepth", "1", "-printf", "%f\t%y\t%s\n"},
		TimeoutSec: 30,
	})
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("%w: find exit %d: %s", toolbus.ErrToolFailed, res.ExitCode, string(res.Stderr))
	}
	entries := parseFindList(res.Stdout)
	return json.Marshal(struct {
		Entries []fsListEntry `json:"entries"`
	}{Entries: entries})
}

func parseFindList(stdout []byte) []fsListEntry {
	var out []fsListEntry
	sc := bufio.NewScanner(bytes.NewReader(stdout))
	for sc.Scan() {
		line := sc.Text()
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 3 {
			continue
		}
		name := parts[0]
		typ := "file"
		if parts[1] == "d" {
			typ = "dir"
		}
		entry := fsListEntry{Name: name, Type: typ}
		if typ == "file" {
			if n, err := strconv.Atoi(parts[2]); err == nil {
				entry.Size = &n
			}
		}
		out = append(out, entry)
	}
	return out
}

// ---------- fs.glob ----------

type fsGlob struct{ rt Runtime }

func NewFSGlob(rt Runtime) toolbus.Tool { return &fsGlob{rt: rt} }

func (t *fsGlob) Name() string { return "fs.glob" }
func (t *fsGlob) Description() string {
	return "Find files in the sandbox matching a glob pattern (e.g. '**/*.go', 'src/**/*.test.ts')."
}
func (t *fsGlob) Schema() json.RawMessage {
	return json.RawMessage(`{
        "type":"object",
        "properties":{
            "sandbox_id":{"type":"string","format":"uuid"},
            "pattern":{"type":"string"},
            "path":{"type":"string"}
        },
        "required":["sandbox_id","pattern"],
        "additionalProperties":false
    }`)
}

type fsGlobIn struct {
	SandboxID uuid.UUID `json:"sandbox_id"`
	Pattern   string    `json:"pattern"`
	Path      string    `json:"path"`
}

func (t *fsGlob) Invoke(ctx context.Context, tenantID, _ uuid.UUID, input json.RawMessage) (json.RawMessage, error) {
	var in fsGlobIn
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, fmt.Errorf("%w: %v", toolbus.ErrInvalidArguments, err)
	}
	if in.SandboxID == uuid.Nil {
		return nil, toolbus.ErrSandboxIDRequired
	}
	root := "/workspace"
	if in.Path != "" {
		root = "/workspace/" + strings.TrimPrefix(in.Path, "/")
	}
	res, err := t.rt.Exec(ctx, tenantID, in.SandboxID, sandbox.ExecOpts{
		Cmd:        []string{"sh", "-c", "cd " + shellEscape(root) + " && find . -type f -printf '%P\\n'"},
		TimeoutSec: 60,
	})
	if err != nil {
		return nil, err
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("%w: find exit %d", toolbus.ErrToolFailed, res.ExitCode)
	}

	var matches []string
	sc := bufio.NewScanner(bytes.NewReader(res.Stdout))
	for sc.Scan() {
		p := sc.Text()
		ok, _ := doublestar.PathMatch(in.Pattern, p)
		if ok {
			matches = append(matches, p)
		}
	}
	return json.Marshal(struct {
		Matches []string `json:"matches"`
	}{Matches: matches})
}

func shellEscape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
```

- [ ] **Step 5: 跑测试**

```bash
PATH="/d/tools/go/bin:$PATH" go mod tidy
PATH="/d/tools/go/bin:$PATH" go vet ./...
PATH="/d/tools/go/bin:$PATH" go test ./internal/toolbus/tools/... -count=1 -v
```

期望：7 测试 PASS。

- [ ] **Step 6: commit**

```bash
git add internal/toolbus/tools/fs.go internal/toolbus/tools/fs_test.go go.mod go.sum
git commit -m "feat(toolbus): fs.read / fs.write / fs.list / fs.glob tools"
```

---

## Task 7: `tools/grep.go`

**Files:**
- Create: `internal/toolbus/tools/grep.go`
- Create: `internal/toolbus/tools/grep_test.go`

- [ ] **Step 1: 写测试**

`internal/toolbus/tools/grep_test.go`:

```go
package tools_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/sandbox"
	"github.com/yourorg/private-coding-agent/internal/toolbus/tools"
)

const ripgrepJSON = `{"type":"match","data":{"path":{"text":"src/foo.go"},"line_number":12,"lines":{"text":"func Foo() {}\n"}}}
{"type":"match","data":{"path":{"text":"src/bar.go"},"line_number":3,"lines":{"text":"func Foo2() {}\n"}}}
`

func TestGrep_ParsesRipgrepJSON(t *testing.T) {
	rt := &mockRuntime{execRet: &sandbox.ExecResult{
		ExitCode: 0,
		Stdout:   []byte(ripgrepJSON),
	}}
	tool := tools.NewGrep(rt)
	out, err := tool.Invoke(context.Background(), uuid.New(), uuid.New(),
		json.RawMessage(`{"sandbox_id":`+validSandboxJSON+`,"pattern":"Foo"}`))
	require.NoError(t, err)

	var got struct {
		Matches []struct {
			Path string `json:"path"`
			Line int    `json:"line"`
			Text string `json:"text"`
		} `json:"matches"`
	}
	require.NoError(t, json.Unmarshal(out, &got))
	require.Len(t, got.Matches, 2)
	require.Equal(t, "src/foo.go", got.Matches[0].Path)
	require.Equal(t, 12, got.Matches[0].Line)
}

func TestGrep_MaxResults(t *testing.T) {
	// Build 200 matching lines to test truncation at default 100.
	var sb []byte
	for i := 0; i < 200; i++ {
		sb = append(sb, []byte(`{"type":"match","data":{"path":{"text":"a.go"},"line_number":1,"lines":{"text":"x\n"}}}`+"\n")...)
	}
	rt := &mockRuntime{execRet: &sandbox.ExecResult{ExitCode: 0, Stdout: sb}}
	tool := tools.NewGrep(rt)
	out, err := tool.Invoke(context.Background(), uuid.New(), uuid.New(),
		json.RawMessage(`{"sandbox_id":`+validSandboxJSON+`,"pattern":"x"}`))
	require.NoError(t, err)
	var got struct {
		Matches []map[string]any `json:"matches"`
	}
	require.NoError(t, json.Unmarshal(out, &got))
	require.Len(t, got.Matches, 100)
}

func TestGrep_RGExit1NoMatchIsOK(t *testing.T) {
	// ripgrep returns exit 1 when no matches; tool should treat that as ok with empty list.
	rt := &mockRuntime{execRet: &sandbox.ExecResult{ExitCode: 1, Stdout: nil}}
	tool := tools.NewGrep(rt)
	out, err := tool.Invoke(context.Background(), uuid.New(), uuid.New(),
		json.RawMessage(`{"sandbox_id":`+validSandboxJSON+`,"pattern":"missing"}`))
	require.NoError(t, err)
	require.JSONEq(t, `{"matches":[]}`, string(out))
}
```

- [ ] **Step 2: 跑测试失败**

```bash
PATH="/d/tools/go/bin:$PATH" go test ./internal/toolbus/tools/... -run TestGrep
```

- [ ] **Step 3: 实现 `tools/grep.go`**

```go
package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/yourorg/private-coding-agent/internal/sandbox"
	"github.com/yourorg/private-coding-agent/internal/toolbus"
)

type grepTool struct{ rt Runtime }

func NewGrep(rt Runtime) toolbus.Tool { return &grepTool{rt: rt} }

func (t *grepTool) Name() string { return "grep" }
func (t *grepTool) Description() string {
	return "Search file contents in the sandbox using regex. Returns lines matching the pattern with file:line context."
}
func (t *grepTool) Schema() json.RawMessage {
	return json.RawMessage(`{
        "type":"object",
        "properties":{
            "sandbox_id":{"type":"string","format":"uuid"},
            "pattern":{"type":"string"},
            "path":{"type":"string"},
            "case_insensitive":{"type":"boolean"},
            "max_results":{"type":"integer","minimum":1,"maximum":1000}
        },
        "required":["sandbox_id","pattern"],
        "additionalProperties":false
    }`)
}

type grepIn struct {
	SandboxID       uuid.UUID `json:"sandbox_id"`
	Pattern         string    `json:"pattern"`
	Path            string    `json:"path"`
	CaseInsensitive bool      `json:"case_insensitive"`
	MaxResults      int       `json:"max_results"`
}

type grepMatch struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

func (t *grepTool) Invoke(ctx context.Context, tenantID, _ uuid.UUID, input json.RawMessage) (json.RawMessage, error) {
	var in grepIn
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, fmt.Errorf("%w: %v", toolbus.ErrInvalidArguments, err)
	}
	if in.SandboxID == uuid.Nil {
		return nil, toolbus.ErrSandboxIDRequired
	}
	if in.MaxResults == 0 {
		in.MaxResults = 100
	}
	root := "/workspace"
	if in.Path != "" {
		root = "/workspace/" + strings.TrimPrefix(in.Path, "/")
	}
	args := []string{"rg", "--json", "-n"}
	if in.CaseInsensitive {
		args = append(args, "-i")
	}
	args = append(args, "--", in.Pattern, root)

	res, err := t.rt.Exec(ctx, tenantID, in.SandboxID, sandbox.ExecOpts{
		Cmd:        args,
		TimeoutSec: 60,
	})
	if err != nil {
		return nil, err
	}
	// ripgrep exit codes: 0=matches, 1=no matches, 2=error.
	if res.ExitCode == 1 {
		return json.Marshal(struct {
			Matches []grepMatch `json:"matches"`
		}{Matches: []grepMatch{}})
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("%w: rg exit %d: %s", toolbus.ErrToolFailed, res.ExitCode, string(res.Stderr))
	}

	matches := []grepMatch{}
	sc := bufio.NewScanner(bytes.NewReader(res.Stdout))
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() && len(matches) < in.MaxResults {
		var ev struct {
			Type string `json:"type"`
			Data struct {
				Path       struct{ Text string } `json:"path"`
				LineNumber int                   `json:"line_number"`
				Lines      struct{ Text string } `json:"lines"`
			} `json:"data"`
		}
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			continue
		}
		if ev.Type != "match" {
			continue
		}
		matches = append(matches, grepMatch{
			Path: strings.TrimPrefix(ev.Data.Path.Text, root+"/"),
			Line: ev.Data.LineNumber,
			Text: strings.TrimRight(ev.Data.Lines.Text, "\n"),
		})
	}
	return json.Marshal(struct {
		Matches []grepMatch `json:"matches"`
	}{Matches: matches})
}
```

- [ ] **Step 4: 跑测试**

```bash
PATH="/d/tools/go/bin:$PATH" go test ./internal/toolbus/tools/... -count=1 -v
```

期望：所有 fs 测试 + 3 grep 测试 PASS。

- [ ] **Step 5: commit**

```bash
git add internal/toolbus/tools/grep.go internal/toolbus/tools/grep_test.go
git commit -m "feat(toolbus): grep tool (ripgrep --json parser)"
```

---

## Task 8: `tools/shell.go`

**Files:**
- Create: `internal/toolbus/tools/shell.go`
- Create: `internal/toolbus/tools/shell_test.go`

- [ ] **Step 1: 写测试**

```go
package tools_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/sandbox"
	"github.com/yourorg/private-coding-agent/internal/toolbus/tools"
)

func TestShellExec_ForwardsCmdAndReturnsResult(t *testing.T) {
	rt := &mockRuntime{execRet: &sandbox.ExecResult{
		ExitCode: 0, Stdout: []byte("hi\n"), Stderr: nil,
		DurationMS: 5, Truncated: false, TimedOut: false,
	}}
	tool := tools.NewShellExec(rt)
	out, err := tool.Invoke(context.Background(), uuid.New(), uuid.New(),
		json.RawMessage(`{"sandbox_id":`+validSandboxJSON+`,"cmd":["echo","hi"]}`))
	require.NoError(t, err)
	require.JSONEq(t, `{"exit_code":0,"stdout":"hi\n","stderr":"","truncated":false,"duration_ms":5,"timed_out":false}`,
		string(out))
	require.Equal(t, []string{"echo", "hi"}, rt.lastExec.Cmd)
}

func TestShellExec_TimeoutForwarded(t *testing.T) {
	rt := &mockRuntime{execRet: &sandbox.ExecResult{}}
	tool := tools.NewShellExec(rt)
	_, err := tool.Invoke(context.Background(), uuid.New(), uuid.New(),
		json.RawMessage(`{"sandbox_id":`+validSandboxJSON+`,"cmd":["true"],"timeout_sec":15}`))
	require.NoError(t, err)
	require.Equal(t, 15, rt.lastExec.TimeoutSec)
}
```

- [ ] **Step 2: 跑测试失败**

```bash
PATH="/d/tools/go/bin:$PATH" go test ./internal/toolbus/tools/... -run TestShellExec
```

- [ ] **Step 3: 实现 `tools/shell.go`**

```go
package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/yourorg/private-coding-agent/internal/sandbox"
	"github.com/yourorg/private-coding-agent/internal/toolbus"
)

type shellExec struct{ rt Runtime }

func NewShellExec(rt Runtime) toolbus.Tool { return &shellExec{rt: rt} }

func (t *shellExec) Name() string { return "shell.exec" }
func (t *shellExec) Description() string {
	return "Run a shell command inside the sandbox. Returns exit code, stdout, stderr."
}
func (t *shellExec) Schema() json.RawMessage {
	return json.RawMessage(`{
        "type":"object",
        "properties":{
            "sandbox_id":{"type":"string","format":"uuid"},
            "cmd":{"type":"array","items":{"type":"string"},"minItems":1},
            "working_dir":{"type":"string"},
            "timeout_sec":{"type":"integer","minimum":1,"maximum":600}
        },
        "required":["sandbox_id","cmd"],
        "additionalProperties":false
    }`)
}

type shellExecIn struct {
	SandboxID  uuid.UUID `json:"sandbox_id"`
	Cmd        []string  `json:"cmd"`
	WorkingDir string    `json:"working_dir"`
	TimeoutSec int       `json:"timeout_sec"`
}

type shellExecOut struct {
	ExitCode   int    `json:"exit_code"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	Truncated  bool   `json:"truncated"`
	DurationMS int64  `json:"duration_ms"`
	TimedOut   bool   `json:"timed_out"`
}

func (t *shellExec) Invoke(ctx context.Context, tenantID, _ uuid.UUID, input json.RawMessage) (json.RawMessage, error) {
	var in shellExecIn
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, fmt.Errorf("%w: %v", toolbus.ErrInvalidArguments, err)
	}
	if in.SandboxID == uuid.Nil {
		return nil, toolbus.ErrSandboxIDRequired
	}
	res, err := t.rt.Exec(ctx, tenantID, in.SandboxID, sandbox.ExecOpts{
		Cmd:        in.Cmd,
		WorkingDir: in.WorkingDir,
		TimeoutSec: in.TimeoutSec,
	})
	if err != nil {
		return nil, err
	}
	return json.Marshal(shellExecOut{
		ExitCode:   res.ExitCode,
		Stdout:     string(res.Stdout),
		Stderr:     string(res.Stderr),
		Truncated:  res.Truncated,
		DurationMS: res.DurationMS,
		TimedOut:   res.TimedOut,
	})
}
```

- [ ] **Step 4: 跑测试**

```bash
PATH="/d/tools/go/bin:$PATH" go test ./internal/toolbus/tools/... -count=1 -v
```

期望：所有 tools 测试 PASS。

- [ ] **Step 5: commit**

```bash
git add internal/toolbus/tools/shell.go internal/toolbus/tools/shell_test.go
git commit -m "feat(toolbus): shell.exec tool (sandbox.Runtime.Exec pass-through)"
```

---

## Task 9: `tools/llm.go`

**Files:**
- Create: `internal/toolbus/tools/llm.go`
- Create: `internal/toolbus/tools/llm_test.go`

- [ ] **Step 1: 写测试**

```go
package tools_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/modelgw"
	"github.com/yourorg/private-coding-agent/internal/toolbus/tools"
)

type mockGateway struct {
	chatRet  *modelgw.ChatResponse
	chatErr  error
	embedRet *modelgw.EmbeddingsResponse
	embedErr error
	lastChat modelgw.ChatRequest
	lastEmbed modelgw.EmbeddingsRequest
}

func (m *mockGateway) ChatCompletion(_ context.Context, _, _ uuid.UUID, req modelgw.ChatRequest) (*modelgw.ChatResponse, error) {
	m.lastChat = req
	return m.chatRet, m.chatErr
}
func (m *mockGateway) Embeddings(_ context.Context, _, _ uuid.UUID, req modelgw.EmbeddingsRequest) (*modelgw.EmbeddingsResponse, error) {
	m.lastEmbed = req
	return m.embedRet, m.embedErr
}

func TestLLMChat_OK(t *testing.T) {
	gw := &mockGateway{
		chatRet: &modelgw.ChatResponse{
			Choices: []modelgw.ChatChoice{{
				Index: 0, FinishReason: "stop",
				Message: modelgw.ChatMessage{Role: modelgw.RoleAssistant, Content: "hi back"},
			}},
			Usage: modelgw.Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3},
		},
	}
	tool := tools.NewLLMChat(gw)
	out, err := tool.Invoke(context.Background(), uuid.New(), uuid.New(),
		json.RawMessage(`{"model":"default-mock:gpt-4o","messages":[{"role":"user","content":"hi"}]}`))
	require.NoError(t, err)
	var got struct {
		Content string        `json:"content"`
		Usage   modelgw.Usage `json:"usage"`
	}
	require.NoError(t, json.Unmarshal(out, &got))
	require.Equal(t, "hi back", got.Content)
	require.Equal(t, 3, got.Usage.TotalTokens)
	require.Equal(t, "default-mock:gpt-4o", gw.lastChat.Model)
}

func TestLLMChat_ForwardsTemperature(t *testing.T) {
	gw := &mockGateway{
		chatRet: &modelgw.ChatResponse{Choices: []modelgw.ChatChoice{{}}},
	}
	tool := tools.NewLLMChat(gw)
	_, _ = tool.Invoke(context.Background(), uuid.New(), uuid.New(),
		json.RawMessage(`{"model":"x:y","messages":[{"role":"user","content":"x"}],"temperature":0.5}`))
	require.NotNil(t, gw.lastChat.Temperature)
	require.InDelta(t, 0.5, *gw.lastChat.Temperature, 0.001)
}

func TestLLMEmbed_OK(t *testing.T) {
	gw := &mockGateway{
		embedRet: &modelgw.EmbeddingsResponse{
			Data: []modelgw.Embedding{
				{Index: 0, Embedding: []float64{0.1, 0.2}},
				{Index: 1, Embedding: []float64{0.3, 0.4}},
			},
			Usage: modelgw.Usage{PromptTokens: 5, TotalTokens: 5},
		},
	}
	tool := tools.NewLLMEmbed(gw)
	out, err := tool.Invoke(context.Background(), uuid.New(), uuid.New(),
		json.RawMessage(`{"model":"x:y","input":["a","b"]}`))
	require.NoError(t, err)
	var got struct {
		Vectors [][]float64 `json:"vectors"`
		Usage   modelgw.Usage `json:"usage"`
	}
	require.NoError(t, json.Unmarshal(out, &got))
	require.Len(t, got.Vectors, 2)
	require.Equal(t, []float64{0.1, 0.2}, got.Vectors[0])
	require.Equal(t, []float64{0.3, 0.4}, got.Vectors[1])
}
```

- [ ] **Step 2: 跑测试失败**

```bash
PATH="/d/tools/go/bin:$PATH" go test ./internal/toolbus/tools/... -run "TestLLM"
```

- [ ] **Step 3: 实现 `tools/llm.go`**

```go
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/google/uuid"

	"github.com/yourorg/private-coding-agent/internal/modelgw"
	"github.com/yourorg/private-coding-agent/internal/toolbus"
)

// Gateway is the subset of modelgw.Gateway used by llm.* tools.
type Gateway interface {
	ChatCompletion(ctx context.Context, tenantID, userID uuid.UUID, req modelgw.ChatRequest) (*modelgw.ChatResponse, error)
	Embeddings(ctx context.Context, tenantID, userID uuid.UUID, req modelgw.EmbeddingsRequest) (*modelgw.EmbeddingsResponse, error)
}

// ---------- llm.chat ----------

type llmChat struct{ gw Gateway }

func NewLLMChat(gw Gateway) toolbus.Tool { return &llmChat{gw: gw} }

func (t *llmChat) Name() string { return "llm.chat" }
func (t *llmChat) Description() string {
	return "Send a Chat Completion request to the configured LLM provider. Returns the assistant message."
}
func (t *llmChat) Schema() json.RawMessage {
	return json.RawMessage(`{
        "type":"object",
        "properties":{
            "model":{"type":"string"},
            "messages":{"type":"array","items":{
                "type":"object",
                "properties":{
                    "role":{"type":"string","enum":["system","user","assistant","tool"]},
                    "content":{"type":"string"}
                },
                "required":["role","content"]
            }},
            "temperature":{"type":"number"},
            "max_tokens":{"type":"integer"}
        },
        "required":["model","messages"],
        "additionalProperties":false
    }`)
}

type llmChatIn struct {
	Model       string                `json:"model"`
	Messages    []modelgw.ChatMessage `json:"messages"`
	Temperature *float64              `json:"temperature,omitempty"`
	MaxTokens   *int                  `json:"max_tokens,omitempty"`
}

func (t *llmChat) Invoke(ctx context.Context, tenantID, userID uuid.UUID, input json.RawMessage) (json.RawMessage, error) {
	var in llmChatIn
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, fmt.Errorf("%w: %v", toolbus.ErrInvalidArguments, err)
	}
	req := modelgw.ChatRequest{
		Model:       in.Model,
		Messages:    in.Messages,
		Temperature: in.Temperature,
		MaxTokens:   in.MaxTokens,
	}
	resp, err := t.gw.ChatCompletion(ctx, tenantID, userID, req)
	if err != nil {
		return nil, err
	}
	var content string
	if len(resp.Choices) > 0 {
		content = resp.Choices[0].Message.Content
	}
	return json.Marshal(struct {
		Content string        `json:"content"`
		Usage   modelgw.Usage `json:"usage"`
	}{Content: content, Usage: resp.Usage})
}

// ---------- llm.embed ----------

type llmEmbed struct{ gw Gateway }

func NewLLMEmbed(gw Gateway) toolbus.Tool { return &llmEmbed{gw: gw} }

func (t *llmEmbed) Name() string { return "llm.embed" }
func (t *llmEmbed) Description() string {
	return "Compute embedding vectors for one or more text strings."
}
func (t *llmEmbed) Schema() json.RawMessage {
	return json.RawMessage(`{
        "type":"object",
        "properties":{
            "model":{"type":"string"},
            "input":{"type":"array","items":{"type":"string"},"minItems":1,"maxItems":100}
        },
        "required":["model","input"],
        "additionalProperties":false
    }`)
}

type llmEmbedIn struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

func (t *llmEmbed) Invoke(ctx context.Context, tenantID, userID uuid.UUID, input json.RawMessage) (json.RawMessage, error) {
	var in llmEmbedIn
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, fmt.Errorf("%w: %v", toolbus.ErrInvalidArguments, err)
	}
	req := modelgw.EmbeddingsRequest{Model: in.Model, Input: in.Input}
	resp, err := t.gw.Embeddings(ctx, tenantID, userID, req)
	if err != nil {
		return nil, err
	}
	// Sort by Index to keep vector positions stable.
	embs := append([]modelgw.Embedding(nil), resp.Data...)
	sort.Slice(embs, func(i, j int) bool { return embs[i].Index < embs[j].Index })
	vectors := make([][]float64, len(embs))
	for i, e := range embs {
		vectors[i] = e.Embedding
	}
	return json.Marshal(struct {
		Vectors [][]float64   `json:"vectors"`
		Usage   modelgw.Usage `json:"usage"`
	}{Vectors: vectors, Usage: resp.Usage})
}
```

- [ ] **Step 4: 跑测试**

```bash
PATH="/d/tools/go/bin:$PATH" go test ./internal/toolbus/tools/... -count=1 -v
```

期望：3 个 llm 测试 + 所有现有 tools 测试 PASS。

- [ ] **Step 5: commit**

```bash
git add internal/toolbus/tools/llm.go internal/toolbus/tools/llm_test.go
git commit -m "feat(toolbus): llm.chat + llm.embed tools (modelgw.Gateway pass-through)"
```

---

## Task 10: `handler.go` HTTP handlers

**Files:**
- Create: `internal/toolbus/handler.go`
- Create: `internal/toolbus/handler_test.go`

- [ ] **Step 1: 写测试**

```go
package toolbus_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/auth"
	"github.com/yourorg/private-coding-agent/internal/modelgw"
	"github.com/yourorg/private-coding-agent/internal/sandbox"
	"github.com/yourorg/private-coding-agent/internal/toolbus"
)

// mockBus satisfies the small surface used by Handler.
type mockBus struct {
	listRet   []toolbus.ToolDef
	invokeRet json.RawMessage
	invokeErr error
}

func (m *mockBus) ListTools(_ context.Context, _ uuid.UUID) []toolbus.ToolDef {
	return m.listRet
}
func (m *mockBus) Invoke(_ context.Context, _, _ uuid.UUID, _ string, _ json.RawMessage) (json.RawMessage, error) {
	return m.invokeRet, m.invokeErr
}

func newRouter(t *testing.T, mb *mockBus) (*gin.Engine, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	const secret = "test-secret-thirty-two-chars-ok!"
	j := auth.NewJWT(auth.JWTConfig{Secret: secret, TTL: time.Hour})
	uid, tid := uuid.New(), uuid.New()
	tok, _ := j.Issue(uid, tid, "member")
	r := gin.New()
	g := r.Group("/")
	g.Use(auth.Middleware(j))
	toolbus.NewHandler(mb).Register(g)
	return r, "Bearer " + tok
}

func TestHandler_List_OK(t *testing.T) {
	mb := &mockBus{listRet: []toolbus.ToolDef{
		{Name: "fs.read", Description: "x", Parameters: json.RawMessage(`{}`)},
	}}
	r, tok := newRouter(t, mb)
	req := httptest.NewRequest(http.MethodGet, "/tools", nil)
	req.Header.Set("Authorization", tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"fs.read"`)
}

func TestHandler_Invoke_OK(t *testing.T) {
	mb := &mockBus{invokeRet: json.RawMessage(`{"ok":true}`)}
	r, tok := newRouter(t, mb)
	body, _ := json.Marshal(map[string]any{
		"tool":  "fs.read",
		"input": map[string]string{"sandbox_id": uuid.NewString(), "path": "x"},
	})
	req := httptest.NewRequest(http.MethodPost, "/tools/invoke", bytes.NewReader(body))
	req.Header.Set("Authorization", tok)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"ok":true`)
}

func TestHandler_Invoke_NoAuth(t *testing.T) {
	mb := &mockBus{}
	r, _ := newRouter(t, mb)
	body, _ := json.Marshal(map[string]any{"tool": "x", "input": map[string]string{}})
	req := httptest.NewRequest(http.MethodPost, "/tools/invoke", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHandler_Invoke_ToolMissing(t *testing.T) {
	mb := &mockBus{}
	r, tok := newRouter(t, mb)
	body, _ := json.Marshal(map[string]any{"input": map[string]string{}})
	req := httptest.NewRequest(http.MethodPost, "/tools/invoke", bytes.NewReader(body))
	req.Header.Set("Authorization", tok)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "tool_required")
}

func TestHandler_Invoke_ToolNotFound(t *testing.T) {
	mb := &mockBus{invokeErr: toolbus.ErrToolNotFound}
	r, tok := newRouter(t, mb)
	body, _ := json.Marshal(map[string]any{"tool": "x", "input": map[string]string{}})
	req := httptest.NewRequest(http.MethodPost, "/tools/invoke", bytes.NewReader(body))
	req.Header.Set("Authorization", tok)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandler_Invoke_InvalidArgs(t *testing.T) {
	mb := &mockBus{invokeErr: toolbus.ErrInvalidArguments}
	r, tok := newRouter(t, mb)
	body, _ := json.Marshal(map[string]any{"tool": "x", "input": map[string]string{}})
	req := httptest.NewRequest(http.MethodPost, "/tools/invoke", bytes.NewReader(body))
	req.Header.Set("Authorization", tok)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandler_Invoke_SandboxNotFound(t *testing.T) {
	mb := &mockBus{invokeErr: sandbox.ErrSandboxNotFound}
	r, tok := newRouter(t, mb)
	body, _ := json.Marshal(map[string]any{"tool": "x", "input": map[string]string{}})
	req := httptest.NewRequest(http.MethodPost, "/tools/invoke", bytes.NewReader(body))
	req.Header.Set("Authorization", tok)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandler_Invoke_ProviderUnreachable(t *testing.T) {
	mb := &mockBus{invokeErr: modelgw.ErrProviderUnreachable}
	r, tok := newRouter(t, mb)
	body, _ := json.Marshal(map[string]any{"tool": "x", "input": map[string]string{}})
	req := httptest.NewRequest(http.MethodPost, "/tools/invoke", bytes.NewReader(body))
	req.Header.Set("Authorization", tok)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadGateway, w.Code)
}
```

- [ ] **Step 2: 跑测试失败**

```bash
PATH="/d/tools/go/bin:$PATH" go test ./internal/toolbus/... -run TestHandler
```

- [ ] **Step 3: 实现 `handler.go`**

```go
package toolbus

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/yourorg/private-coding-agent/internal/auth"
	"github.com/yourorg/private-coding-agent/internal/modelgw"
	"github.com/yourorg/private-coding-agent/internal/sandbox"
)

// BusInterface is implemented by *Bus; handler depends on the interface to
// keep the test seam narrow.
type BusInterface interface {
	ListTools(ctx context.Context, tenantID uuid.UUID) []ToolDef
	Invoke(ctx context.Context, tenantID, userID uuid.UUID, toolName string, input json.RawMessage) (json.RawMessage, error)
}

type Handler struct{ bus BusInterface }

func NewHandler(b BusInterface) *Handler { return &Handler{bus: b} }

func (h *Handler) Register(rg *gin.RouterGroup) {
	rg.GET("/tools", h.list)
	rg.POST("/tools/invoke", h.invoke)
}

type apiError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
}

func writeAPIError(c *gin.Context, code int, msg, typ, errCode string) {
	c.AbortWithStatusJSON(code, gin.H{"error": apiError{Message: msg, Type: typ, Code: errCode}})
}

func (h *Handler) claims(c *gin.Context) (*auth.Claims, bool) {
	cl := auth.FromCtx(c.Request.Context())
	if cl == nil {
		writeAPIError(c, http.StatusUnauthorized, "unauthorized", "auth_error", "missing_token")
		return nil, false
	}
	return cl, true
}

func (h *Handler) list(c *gin.Context) {
	cl, ok := h.claims(c)
	if !ok {
		return
	}
	tools := h.bus.ListTools(c.Request.Context(), cl.TenantID)
	c.JSON(http.StatusOK, gin.H{"tools": tools})
}

type invokeReq struct {
	Tool  string          `json:"tool"`
	Input json.RawMessage `json:"input"`
}

func (h *Handler) invoke(c *gin.Context) {
	cl, ok := h.claims(c)
	if !ok {
		return
	}
	var req invokeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		writeAPIError(c, http.StatusBadRequest, err.Error(), "invalid_request_error", "bad_request")
		return
	}
	if req.Tool == "" {
		writeAPIError(c, http.StatusBadRequest, "tool field required", "invalid_request_error", "tool_required")
		return
	}
	if len(req.Input) == 0 {
		req.Input = json.RawMessage(`{}`)
	}
	out, err := h.bus.Invoke(c.Request.Context(), cl.TenantID, cl.UserID, req.Tool, req.Input)
	if err != nil {
		mapErrorToAPI(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"output": json.RawMessage(out)})
}

func mapErrorToAPI(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrToolNotFound):
		writeAPIError(c, http.StatusNotFound, err.Error(), "invalid_request_error", "tool_not_found")
	case errors.Is(err, ErrInvalidArguments):
		writeAPIError(c, http.StatusBadRequest, err.Error(), "invalid_request_error", "invalid_arguments")
	case errors.Is(err, ErrSandboxIDRequired):
		writeAPIError(c, http.StatusBadRequest, err.Error(), "invalid_request_error", "sandbox_id_required")
	case errors.Is(err, sandbox.ErrSandboxNotFound):
		writeAPIError(c, http.StatusNotFound, err.Error(), "invalid_request_error", "sandbox_not_found")
	case errors.Is(err, sandbox.ErrSandboxNotReady):
		writeAPIError(c, http.StatusConflict, err.Error(), "invalid_request_error", "sandbox_not_ready")
	case errors.Is(err, sandbox.ErrPathOutsideWorkspace):
		writeAPIError(c, http.StatusBadRequest, err.Error(), "invalid_request_error", "path_outside_workspace")
	case errors.Is(err, sandbox.ErrTooLarge):
		writeAPIError(c, http.StatusRequestEntityTooLarge, err.Error(), "invalid_request_error", "payload_too_large")
	case errors.Is(err, modelgw.ErrUnsupportedFeature):
		writeAPIError(c, http.StatusBadRequest, err.Error(), "invalid_request_error", "unsupported_feature")
	case errors.Is(err, modelgw.ErrProviderUnreachable):
		writeAPIError(c, http.StatusBadGateway, err.Error(), "provider_error", "provider_unreachable")
	case errors.Is(err, modelgw.ErrProviderError):
		var pe *modelgw.ProviderError
		if errors.As(err, &pe) && pe.StatusCode == http.StatusTooManyRequests {
			writeAPIError(c, http.StatusTooManyRequests, err.Error(), "rate_limit_error", "provider_rate_limit")
			return
		}
		writeAPIError(c, http.StatusBadGateway, err.Error(), "provider_error", "provider_error")
	default:
		writeAPIError(c, http.StatusInternalServerError, err.Error(), "server_error", "internal")
	}
}
```

- [ ] **Step 4: 跑测试**

```bash
PATH="/d/tools/go/bin:$PATH" go vet ./...
PATH="/d/tools/go/bin:$PATH" go test ./internal/toolbus/... -count=1 -v
```

期望：所有 modelgw、sandbox、toolbus 测试 PASS。

- [ ] **Step 5: commit**

```bash
git add internal/toolbus/handler.go internal/toolbus/handler_test.go
git commit -m "feat(toolbus): HTTP handlers for /tools and /tools/invoke"
```

---

## Task 11: main 装配

**Files:**
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Read 当前 main.go 定位 modelHandler 装配段后位置**

```bash
PATH="/d/tools/go/bin:$PATH" grep -n "modelHandler" cmd/server/main.go
```

- [ ] **Step 2: 在 modelHandler 装配段之后追加 ToolBus 装配**

在 `cmd/server/main.go` 中，找到 `modelHandler := modelgw.NewHandler(modelGateway)` 那行。在它之后追加：

```go
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

toolInvocationRecorder := toolbus.NewInvocationRecorder(
	toolbus.NewInvocationRepo(pool),
	func(err error) { log.Printf("tool invocation record: %v", err) })

toolBus, err := toolbus.NewBus(toolRegistry, toolInvocationRecorder)
if err != nil {
	return fmt.Errorf("toolbus: %w", err)
}
toolHandler := toolbus.NewHandler(toolBus)
```

import 段补：

```go
"github.com/yourorg/private-coding-agent/internal/toolbus"
"github.com/yourorg/private-coding-agent/internal/toolbus/tools"
```

- [ ] **Step 3: 在 register 闭包追加路由**

找到 `register := func(r *gin.Engine) {` 内 `modelHandler.Register(protected)` 那行，在其后追加：

```go
toolHandler.Register(protected)
```

- [ ] **Step 4: 验证 build / vet / test**

```bash
PATH="/d/tools/go/bin:$PATH" go vet ./...
PATH="/d/tools/go/bin:$PATH" go build ./...
PATH="/d/tools/go/bin:$PATH" go test ./... -count=1
```

期望：全 PASS。

- [ ] **Step 5: commit**

```bash
git add cmd/server/main.go
git commit -m "feat(cmd): wire ToolBus + 8 internal tools into server"
```

---

## Task 12: 集成测试（`docker_integration` tag）

**Files:**
- Create: `internal/toolbus/tools/fs_integration_test.go`
- Create: `internal/toolbus/tools/shell_integration_test.go`
- Create: `internal/toolbus/tools/llm_integration_test.go`

- [ ] **Step 1: 写 `fs_integration_test.go`**

```go
//go:build docker_integration

package tools_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/docker/docker/client"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/sandbox"
	"github.com/yourorg/private-coding-agent/internal/tenant"
	"github.com/yourorg/private-coding-agent/internal/toolbus/tools"
	"github.com/yourorg/private-coding-agent/internal/user"
)

func newDriverForToolsTest(t *testing.T) (*sandbox.DockerDriver, uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	pg, err := pgxpool.New(ctx, testDSN)
	require.NoError(t, err)
	t.Cleanup(pg.Close)

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	require.NoError(t, err)
	t.Cleanup(func() { _ = cli.Close() })

	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	t.Cleanup(func() { _ = rdb.Close() })

	repo := sandbox.NewSessionRepo(pg)
	d, err := sandbox.NewDockerDriver(ctx, cli, repo, rdb, sandbox.DockerDriverConfig{
		InternalNetworkName: "pca-sandbox-tools-test",
	})
	require.NoError(t, err)

	tn, err := tenant.NewRepo(pg).GetBySlug(ctx, "default")
	require.NoError(t, err)
	usvc := user.NewService(user.NewRepo(pg))
	u, err := usvc.Register(ctx, tn.ID,
		"tools-it-"+uuid.NewString()+"@example.com", "irrelevant-password", "tools")
	require.NoError(t, err)
	return d, tn.ID, u.ID
}

func TestFSRead_Integration(t *testing.T) {
	ctx := context.Background()
	d, tid, uid := newDriverForToolsTest(t)
	sb, err := d.Create(ctx, sandbox.CreateOpts{TenantID: tid, OwnerUserID: uid})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Destroy(ctx, tid, sb.ID) })

	// Write a file via WriteFile, then read via fs.read tool.
	require.NoError(t, d.WriteFile(ctx, tid, sb.ID, "hello.txt", []byte("hi from fs.read")))

	tool := tools.NewFSRead(d)
	in, _ := json.Marshal(map[string]string{"sandbox_id": sb.ID.String(), "path": "hello.txt"})
	out, err := tool.Invoke(ctx, tid, uid, in)
	require.NoError(t, err)
	require.Contains(t, string(out), "hi from fs.read")
}

func TestFSWriteThenList_Integration(t *testing.T) {
	ctx := context.Background()
	d, tid, uid := newDriverForToolsTest(t)
	sb, err := d.Create(ctx, sandbox.CreateOpts{TenantID: tid, OwnerUserID: uid})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Destroy(ctx, tid, sb.ID) })

	writeTool := tools.NewFSWrite(d)
	in, _ := json.Marshal(map[string]string{
		"sandbox_id": sb.ID.String(), "path": "src/main.go", "content": "package main",
	})
	_, err = writeTool.Invoke(ctx, tid, uid, in)
	require.NoError(t, err)

	listTool := tools.NewFSList(d)
	lin, _ := json.Marshal(map[string]string{"sandbox_id": sb.ID.String(), "path": "."})
	lout, err := listTool.Invoke(ctx, tid, uid, lin)
	require.NoError(t, err)
	require.Contains(t, string(lout), "src")
}
```

> 注：本 integration 测试依赖与 sandbox 包同一个 `testDSN` 变量。复用 `internal/toolbus/repo_test.go` 中 `TestMain` 起的 PG（同 package_test 里所有 `_test.go` 共享）。tools 包里 `_test.go` 是 `package tools_test`，但 `testDSN` 在不同 package，所以要在 tools_test 里也有 `TestMain`——简化方案：每个 integration_test 都 build tag 隔离，且每个文件自己 declare 一个 testDSN，启动期复用 sandbox/repo dockertest 启 PG 的同款 helper。

为了避免重复 TestMain 冲突，**实际操作**：把 `testDSN` 改为本 tools_test package 自己的 dockertest setup。最简单：再起一份 TestMain：

把上面文件改为：

```go
//go:build docker_integration

package tools_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"github.com/docker/docker/client"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/db"
	"github.com/yourorg/private-coding-agent/internal/sandbox"
	"github.com/yourorg/private-coding-agent/internal/tenant"
	"github.com/yourorg/private-coding-agent/internal/toolbus/tools"
	"github.com/yourorg/private-coding-agent/internal/user"
)

var testDSN string

func TestMain(m *testing.M) {
	pool, err := dockertest.NewPool("")
	if err != nil { log.Fatal(err) }
	res, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "postgres", Tag: "16",
		Env: []string{"POSTGRES_USER=app", "POSTGRES_PASSWORD=app", "POSTGRES_DB=app"},
	}, func(c *docker.HostConfig) {
		c.AutoRemove = true
		c.RestartPolicy = docker.RestartPolicy{Name: "no"}
	})
	if err != nil { log.Fatal(err) }
	testDSN = fmt.Sprintf("postgres://app:app@localhost:%s/app?sslmode=disable", res.GetPort("5432/tcp"))
	pool.MaxWait = 60 * time.Second
	if err := pool.Retry(func() error { return db.Migrate(context.Background(), testDSN) }); err != nil {
		log.Fatal(err)
	}
	os.Exit(func() int { defer func() { _ = pool.Purge(res) }(); return m.Run() }())
}

// ... 然后 newDriverForToolsTest 和 Test* 同上
```

把完整代码（TestMain + helper + 2 测试）合并到 `fs_integration_test.go`。

- [ ] **Step 2: 写 `shell_integration_test.go`**

```go
//go:build docker_integration

package tools_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/sandbox"
	"github.com/yourorg/private-coding-agent/internal/toolbus/tools"
)

func TestShellExec_Integration(t *testing.T) {
	ctx := context.Background()
	d, tid, uid := newDriverForToolsTest(t)
	sb, err := d.Create(ctx, sandbox.CreateOpts{TenantID: tid, OwnerUserID: uid})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Destroy(ctx, tid, sb.ID) })

	tool := tools.NewShellExec(d)
	in, _ := json.Marshal(map[string]any{
		"sandbox_id": sb.ID.String(),
		"cmd":        []string{"echo", "hello-from-shell"},
	})
	out, err := tool.Invoke(ctx, tid, uid, in)
	require.NoError(t, err)
	require.Contains(t, string(out), "hello-from-shell")
	require.Contains(t, string(out), `"exit_code":0`)
}
```

- [ ] **Step 3: 写 `llm_integration_test.go`**

```go
//go:build docker_integration

package tools_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/modelgw"
	"github.com/yourorg/private-coding-agent/internal/toolbus/tools"
)

func TestLLMChat_Integration(t *testing.T) {
	// httptest mock provider (OpenAI-compatible).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(modelgw.ChatResponse{
			ID: "x", Object: "chat.completion", Model: "m",
			Choices: []modelgw.ChatChoice{{
				Index: 0, FinishReason: "stop",
				Message: modelgw.ChatMessage{Role: modelgw.RoleAssistant, Content: "hello from mock"},
			}},
			Usage: modelgw.Usage{TotalTokens: 1},
		})
	}))
	defer srv.Close()

	p, err := modelgw.NewOpenAIProvider(modelgw.ProviderConfig{
		ID: uuid.New(), Name: "it-mock", Type: "openai", BaseURL: srv.URL,
	})
	require.NoError(t, err)

	ctx := context.Background()
	pg, err := pgxpool.New(ctx, testDSN)
	require.NoError(t, err)
	t.Cleanup(pg.Close)

	reg := modelgw.NewProviderRegistry(nil, nil, 0)
	reg.SeedForTest(map[string]modelgw.Provider{"it-mock": p})
	rec := modelgw.NewUsageRecorder(modelgw.NewUsageRepo(pg), func(err error) {})
	gw := modelgw.NewGateway(reg, rec)

	tool := tools.NewLLMChat(gw)
	in, _ := json.Marshal(map[string]any{
		"model":    "it-mock:m",
		"messages": []map[string]string{{"role": "user", "content": "hi"}},
	})
	out, err := tool.Invoke(ctx, uuid.New(), uuid.New(), in)
	require.NoError(t, err)
	require.Contains(t, string(out), "hello from mock")
	_ = sync.WaitGroup{} // silence import
}
```

- [ ] **Step 4: 跑集成测试**

```bash
PATH="/d/tools/go/bin:$PATH" go test -tags=docker_integration ./internal/toolbus/tools/... -count=1 -v -timeout=300s
```

期望：4 个集成测试 PASS（含 TestMain 起 PG ~10s + 2 个建沙箱 ~6s 每个）。

> 前提：Docker Desktop + 本地 redis 在跑（`docker run -d --rm --name pca-redis-test -p 6379:6379 redis:7-alpine`）。

- [ ] **Step 5: commit**

```bash
git add internal/toolbus/tools/fs_integration_test.go \
        internal/toolbus/tools/shell_integration_test.go \
        internal/toolbus/tools/llm_integration_test.go
git commit -m "test(toolbus): integration tests for fs / shell / llm tools"
```

---

## Task 13: E2E + README

**Files:**
- Modify: `deploy/compose/test-e2e.sh`
- Modify: `README.md`

- [ ] **Step 1: 读 test-e2e.sh 定位最后 destroy 后的位置**

```bash
grep -n "\[12/12\]" deploy/compose/test-e2e.sh
```

- [ ] **Step 2: 把 `[1/12]`-`[12/12]` 全替换为 `[N/16]`**

把脚本前面 12 个步骤的编号 `[N/12]` 改为 `[N/16]`。

- [ ] **Step 3: 在 `[12/16] verify model_usage rows ...` 行之后、`echo "E2E PASS"` 之前追加 4 步**

```bash
echo "[13/16] list tools ..."
TOOLS=$(curl -fsS http://localhost:8080/tools -H "Authorization: Bearer $TOK")
NAMES=$(echo "$TOOLS" | jq -r '.tools[].name' | sort | tr '\n' ',')
[[ "$NAMES" == "fs.glob,fs.list,fs.read,fs.write,grep,llm.chat,llm.embed,shell.exec," ]] \
  || { echo "tools list mismatch: $NAMES"; exit 1; }

echo "[14/16] fs.write + fs.read round-trip ..."
SB2=$(curl -fsS -X POST http://localhost:8080/sandbox/sessions \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' -d '{}')
ID2=$(echo "$SB2" | jq -r .id)
WRITE=$(curl -fsS -X POST http://localhost:8080/tools/invoke \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d "{\"tool\":\"fs.write\",\"input\":{\"sandbox_id\":\"$ID2\",\"path\":\"a.txt\",\"content\":\"tool e2e\"}}")
BW=$(echo "$WRITE" | jq -r '.output.bytes_written')
[[ "$BW" == "8" ]] || { echo "bytes_written mismatch: $BW"; exit 1; }
READ=$(curl -fsS -X POST http://localhost:8080/tools/invoke \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d "{\"tool\":\"fs.read\",\"input\":{\"sandbox_id\":\"$ID2\",\"path\":\"a.txt\"}}")
CONTENT=$(echo "$READ" | jq -r '.output.content')
[[ "$CONTENT" == "tool e2e" ]] || { echo "fs.read content mismatch: $CONTENT"; exit 1; }

echo "[15/16] shell.exec ls ..."
SHOUT=$(curl -fsS -X POST http://localhost:8080/tools/invoke \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d "{\"tool\":\"shell.exec\",\"input\":{\"sandbox_id\":\"$ID2\",\"cmd\":[\"ls\",\"/workspace\"]}}")
echo "$SHOUT" | jq -r '.output.stdout' | grep -q "a.txt" || { echo "shell.exec stdout missing a.txt"; exit 1; }
curl -fsS -X DELETE "http://localhost:8080/sandbox/sessions/$ID2" -H "Authorization: Bearer $TOK" >/dev/null

echo "[16/16] llm.chat + tool_invocations ..."
CHATTOOL=$(curl -fsS -X POST http://localhost:8080/tools/invoke \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"tool":"llm.chat","input":{"model":"default-mock:gpt-4o","messages":[{"role":"user","content":"hi"}]}}')
TEXT2=$(echo "$CHATTOOL" | jq -r '.output.content')
[[ "$TEXT2" == "hello from mock" ]] || { echo "llm.chat content mismatch: $TEXT2"; exit 1; }
docker compose exec -T postgres psql -U app -d app -t -c \
  "SELECT count(*) FROM tool_invocations WHERE status='ok';" | grep -q "[1-9]" \
  || { echo "tool_invocations has no rows"; exit 1; }
```

- [ ] **Step 4: 修改 README**

切片进度处把 Slice 4 勾上：

```markdown
- [x] 切片 4：Tool Bus + Internal MCP
```

"关键端点" 表追加：

```markdown
| GET | /tools | Bearer | 列出 8 个 internal tools |
| POST | /tools/invoke | Bearer | 调用 tool |
```

- [ ] **Step 5: 跑 E2E**

```bash
cd D:/IdeaProjects/private-coding-agent/deploy/compose
docker compose down 2>&1 | tail -1
./test-e2e.sh
```

期望：最后 `E2E PASS`（16 步）。

- [ ] **Step 6: 跑全包测试 sanity**

```bash
cd D:/IdeaProjects/private-coding-agent
PATH="/d/tools/go/bin:$PATH" go test ./... -count=1
PATH="/d/tools/go/bin:$PATH" go vet ./...
PATH="/d/tools/go/bin:$PATH" go build ./...
```

期望：全 PASS。

- [ ] **Step 7: commit**

```bash
git add deploy/compose/test-e2e.sh README.md
git commit -m "docs: README + e2e script for slice 4 (16 steps)"
```

---

## 验收（end-of-slice checklist）

- [ ] `go test ./...` 全 PASS（不带 tag）
- [ ] `go test -tags=docker_integration ./...` 全 PASS（含 Slice 2/3/4 集成）
- [ ] `go vet / build` 干净
- [ ] `docker compose up -d --build` 后 `/healthz` 200
- [ ] `test-e2e.sh` 跑通（16 步，最后 `E2E PASS`）
- [ ] `tool_invocations` 表有 status=ok 行
- [ ] `audit_log` 含 `/tools/*` 路径
- [ ] GET /tools 列出 8 个 tool
- [ ] redact 生效：modelgw redact 5 测试 PASS
- [ ] git tree clean

---

## Self-Review

**1. Spec coverage:**
- spec §3 决策：协议（HTTP `/tools/*`）✓ Task 10；8 个 tool ✓ Task 6-9；in-process Go 接口 ✓ Task 1；sandbox_id 在 args ✓ 全 fs/shell tool 实现；JSON Schema 手写 ✓ 全 tool；schema 库 santhosh-tekuri ✓ Task 3；doublestar ✓ Task 6
- spec §5 接口：Tool 接口 ✓ Task 1；Bus ✓ Task 5；Registry ✓ Task 2；schema ✓ Task 3；providers/usage 修改 ✓ Task 0；tool_invocations 新表 ✓ Task 4；HTTP ✓ Task 10；包结构对齐
- spec §6 数据流：流 1 ListTools ✓ Task 10；流 2 Invoke ✓ Task 5 + Task 10；流 3 fs.read chain ✓ Task 6 + integration Task 12；流 4 llm.chat chain ✓ Task 9 + integration；流 5 shell.exec pass-through ✓ Task 8；流 6 redact ✓ Task 0；流 7 OTel/audit ⏭（已沿用 Slice 1 audit middleware）；流 8 装配 ✓ Task 11
- spec §7 错误映射：14 条全部覆盖 mapErrorToAPI

**2. Placeholder scan:** 无 TBD / TODO / "类似 Task N" 占位代码块。

**3. Type consistency:**
- `Tool` 接口签名 `Invoke(ctx, tenantID, userID, input) (output, error)` Task 1 定义；Task 5 Bus 调用一致；Task 6-9 八个 tool 实现一致；Task 10 mockBus.Invoke 接口一致
- `Runtime` interface (tools 包内本地声明) Task 6 定义；Task 6-8 fs/grep/shell 用同一份；Task 12 integration test 直接传 `*sandbox.DockerDriver`（满足该接口）
- `Gateway` interface (tools 包内本地声明) Task 9 定义；Task 11 main 装配传 `*modelgw.Gateway`（满足）
- `ToolDef{Name, Description, Parameters}` Task 1 定义；Task 5 Bus.ListTools 返；Task 10 handler list 返 `{"tools": [...]}`
- `InvocationEvent` Task 4 定义；Task 5 Bus.Invoke 构造；Task 4 Recorder.Record 接收
- `mockTool` (bus_test.go) / `mockRuntime` (tools/fs_test.go) / `mockGateway` (tools/llm_test.go) / `mockBus` (handler_test.go) 各自的私有 helper，无跨文件冲突
- `redact` (modelgw 包内私有) Task 0 引入；Task 0 改 `provider_openai.go` 与 `provider_claude.go` 调用点

**4. Task 0 - 13 全 14 Task 编号正确，无跳号。**
