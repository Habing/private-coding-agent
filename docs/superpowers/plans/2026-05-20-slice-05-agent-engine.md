# Slice 5 — Agent Engine Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 交付 `internal/agent` 包：一个 ReAct 风格的循环 (LLM 决策 → tool_call → Tool Bus → observation → LLM 再决策 → final)，配一个最小 `POST /agent/run` HTTP 端点（非流式，返回 events 数组）。覆盖 spec §4.3 `Run(session, msg) -> stream<Event>` 接口。Slice 6 会用 WebSocket 替换该端点。

**Architecture:** 单进程内的简单循环。Engine 通过本地 `Gateway` / `Bus` 接口依赖注入（接口私有于 `internal/agent`），方便 mockGateway + mockBus 驱动的单元测试，运行时由 `*modelgw.Gateway` + `*toolbus.Bus` 满足。每步：构造 `ChatRequest{Model, Messages, Tools}` 调 Gateway → 解析 `finish_reason` → tool_calls 路径走 Bus → 截断 → 回灌 `role=tool` 消息 → 下一步；stop 路径发 `final` 事件返回。每个 Event 通过 `yield(Event) error` 立即推出，允许调用方写入 WebSocket 或累加成数组。

**Tech Stack:**
- Go 1.26、gin、testify、google/uuid（沿用既有）
- 标准库 `encoding/json`、`context`、`errors`、`fmt`
- 无新增直接依赖

---

## 前置条件

依赖 Slice 1.5 / 2 / 3 / 4 已完成（HEAD = `b7339ca` "docs: README + e2e script for slice 4 (16 steps)"）。

## 本切片边界

**在本切片**：
- `internal/agent` 引擎本体 + 单 profile "coding"（带 tool allowlist + system prompt）
- 工具调用走 toolbus.Bus；LLM 走 modelgw.Gateway（**非流式** ChatCompletion）
- 工具结果 > 50 KB 截断（envelope 化）
- max_steps 上限 + 各类错误回灌 LLM 自纠
- HTTP `POST /agent/run` 非流式（events 数组 JSON），供 E2E 覆盖
- mock-provider 增强：根据 messages 决定返 tool_call 还是 final

**不在本切片**：
- Session / 消息持久化（slice 6）
- WebSocket 流式（slice 6）
- 上下文压缩 / 摘要（slice 6+）
- Memory loader / 多 profile 切换 (slice 7+)
- Reflection Agent (P1)

## File Structure

```
internal/agent/
  types.go              Task 1: RunInput / Event / EventKind 常量
  errors.go             Task 1: 错误哨兵
  errors_test.go        Task 1
  profile.go            Task 2: Profile struct + DefaultCodingProfile()
  profile_test.go       Task 2
  tooldef.go            Task 3: toolbus.ToolDef -> modelgw.ToolDef + allowlist 过滤
  tooldef_test.go       Task 3
  truncate.go           Task 4: tool 输出 > 50KB 截断
  truncate_test.go      Task 4
  engine.go             Task 5: Engine + Run() ReAct 循环
  engine_test.go        Task 5: mockGateway + mockBus 驱动的单测
  handler.go            Task 6: HTTP POST /agent/run (非流式, JSON 数组)
  handler_test.go       Task 6: mockRunner 单测

cmd/server/main.go      Task 7: 装配 Engine + AgentHandler

internal/modelgw/mockserver/main.go
                        Task 7: 增强 chat 状态机 (role=tool → final;
                                content含"list" → tool_call; 默认 → final)

deploy/compose/test-e2e.sh
                        Task 8: 增加 [17/18] [18/18] 两步;
                                把 [N/16] 改 [N/18]

README.md               Task 8: 进度勾选 + /agent/run 端点
```

---

## Task 1: 类型 + 错误哨兵

**Files:**
- Create: `internal/agent/types.go`
- Create: `internal/agent/errors.go`
- Create: `internal/agent/errors_test.go`

- [ ] **Step 1: 写 `errors_test.go`**

```go
package agent_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/agent"
)

func TestErrorSentinels(t *testing.T) {
	require.Error(t, agent.ErrUnknownProfile)
	require.Error(t, agent.ErrEmptyMessages)
	require.Error(t, agent.ErrMaxStepsExceeded)
	require.Error(t, agent.ErrLLMFailed)
	require.Error(t, agent.ErrToolCallParseFailed)
}
```

- [ ] **Step 2: 跑测试确认失败**（package 不存在）

```bash
go test ./internal/agent/...
```

- [ ] **Step 3: 实现 `errors.go`**

```go
package agent

import "errors"

var (
	ErrUnknownProfile      = errors.New("agent: unknown profile")
	ErrEmptyMessages       = errors.New("agent: empty messages")
	ErrMaxStepsExceeded    = errors.New("agent: max steps exceeded")
	ErrLLMFailed           = errors.New("agent: llm failed")
	ErrToolCallParseFailed = errors.New("agent: tool_call arguments parse failed")
)
```

- [ ] **Step 4: 实现 `types.go`**

```go
// Package agent runs the ReAct loop on top of modelgw.Gateway and toolbus.Bus.
package agent

import (
	"encoding/json"

	"github.com/google/uuid"

	"github.com/yourorg/private-coding-agent/internal/modelgw"
)

// EventKind enumerates the kinds of events emitted by Engine.Run.
type EventKind string

const (
	EventAssistantMessage EventKind = "assistant_message"
	EventToolCall         EventKind = "tool_call"
	EventToolResult       EventKind = "tool_result"
	EventFinal            EventKind = "final"
	EventError            EventKind = "error"
)

// DefaultMaxToolOutputBytes is the cap before tool results are truncated.
const DefaultMaxToolOutputBytes = 50 * 1024

// RunInput is the input to Engine.Run. Callers provide identities, a model
// reference, the conversation history, and the profile (or "" for the default).
type RunInput struct {
	TenantID    uuid.UUID
	UserID      uuid.UUID
	Model       string
	Messages    []modelgw.ChatMessage
	ProfileName string
	MaxSteps    int
}

// Event is a single observable step of the ReAct loop. Different EventKinds
// populate different fields; consumers should branch on Kind.
type Event struct {
	Kind         EventKind            `json:"kind"`
	Step         int                  `json:"step"`
	Text         string               `json:"text,omitempty"`
	ToolCallID   string               `json:"tool_call_id,omitempty"`
	ToolName     string               `json:"tool_name,omitempty"`
	ToolInput    json.RawMessage      `json:"tool_input,omitempty"`
	ToolOutput   json.RawMessage      `json:"tool_output,omitempty"`
	ToolError    string               `json:"tool_error,omitempty"`
	Truncated    bool                 `json:"truncated,omitempty"`
	OriginalSize int                  `json:"original_size,omitempty"`
	FinishReason string               `json:"finish_reason,omitempty"`
	ToolCalls    []modelgw.ToolCall   `json:"tool_calls,omitempty"`
}
```

- [ ] **Step 5: 跑测试通过**

```bash
go test ./internal/agent/... -count=1 -v
```

- [ ] **Step 6: commit**

```bash
git add internal/agent/types.go internal/agent/errors.go internal/agent/errors_test.go
git commit -m "feat(agent): Event/RunInput types + error sentinels"
```

---

## Task 2: Profile

**Files:**
- Create: `internal/agent/profile.go`
- Create: `internal/agent/profile_test.go`

- [ ] **Step 1: 写 `profile_test.go`**

```go
package agent_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/agent"
)

func TestDefaultCodingProfile(t *testing.T) {
	p := agent.DefaultCodingProfile()
	require.Equal(t, "coding", p.Name)
	require.NotEmpty(t, p.SystemPrompt)
	require.Equal(t, 16, p.MaxSteps)
	require.Contains(t, p.ToolAllowlist, "fs.read")
	require.Contains(t, p.ToolAllowlist, "shell.exec")
	require.Len(t, p.ToolAllowlist, 8)
}
```

- [ ] **Step 2: 跑测试确认失败**

- [ ] **Step 3: 实现 `profile.go`**

```go
package agent

// Profile bundles the per-run agent personality + safety boundary.
type Profile struct {
	Name          string
	SystemPrompt  string
	ToolAllowlist []string
	MaxSteps      int
}

// DefaultCodingProfile is the only profile shipped with slice 5. It allows
// every built-in tool registered in slice 4.
func DefaultCodingProfile() Profile {
	return Profile{
		Name: "coding",
		SystemPrompt: "You are a coding agent. Use the provided tools to inspect " +
			"and modify files in the user's sandbox. Prefer fs.read / fs.list / " +
			"grep before guessing. Be concise.",
		ToolAllowlist: []string{
			"fs.read", "fs.write", "fs.list", "fs.glob",
			"grep", "shell.exec",
			"llm.chat", "llm.embed",
		},
		MaxSteps: 16,
	}
}
```

- [ ] **Step 4: 跑测试通过**

- [ ] **Step 5: commit**

```bash
git add internal/agent/profile.go internal/agent/profile_test.go
git commit -m "feat(agent): Profile struct + DefaultCodingProfile"
```

---

## Task 3: tooldef 转换 + 过滤

**Files:**
- Create: `internal/agent/tooldef.go`
- Create: `internal/agent/tooldef_test.go`

- [ ] **Step 1: 写 `tooldef_test.go`**

```go
package agent_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/agent"
	"github.com/yourorg/private-coding-agent/internal/toolbus"
)

func TestBuildModelTools_AllowlistFilters(t *testing.T) {
	bus := []toolbus.ToolDef{
		{Name: "fs.read", Description: "r", Parameters: json.RawMessage(`{"type":"object"}`)},
		{Name: "fs.write", Description: "w", Parameters: json.RawMessage(`{"type":"object"}`)},
		{Name: "secret", Description: "s", Parameters: json.RawMessage(`{"type":"object"}`)},
	}
	out := agent.BuildModelTools(bus, []string{"fs.read", "fs.write"})
	require.Len(t, out, 2)
	require.Equal(t, "function", out[0].Type)
	require.Equal(t, "fs.read", out[0].Function.Name)
	require.Equal(t, "fs.write", out[1].Function.Name)
}

func TestBuildModelTools_EmptyInput(t *testing.T) {
	out := agent.BuildModelTools(nil, []string{"fs.read"})
	require.Empty(t, out)
}
```

- [ ] **Step 2: 跑测试确认失败**

- [ ] **Step 3: 实现 `tooldef.go`**

```go
package agent

import (
	"github.com/yourorg/private-coding-agent/internal/modelgw"
	"github.com/yourorg/private-coding-agent/internal/toolbus"
)

// BuildModelTools converts the tool list returned by Bus.ListTools into the
// OpenAI tool-calling-shaped slice consumed by Provider.ChatCompletion.
// Only names present in allowlist are emitted. Order follows busTools (which
// Bus.ListTools already sorts).
func BuildModelTools(busTools []toolbus.ToolDef, allowlist []string) []modelgw.ToolDef {
	allowed := make(map[string]struct{}, len(allowlist))
	for _, n := range allowlist {
		allowed[n] = struct{}{}
	}
	out := make([]modelgw.ToolDef, 0, len(busTools))
	for _, t := range busTools {
		if _, ok := allowed[t.Name]; !ok {
			continue
		}
		out = append(out, modelgw.ToolDef{
			Type: "function",
			Function: modelgw.ToolFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		})
	}
	return out
}
```

- [ ] **Step 4: 跑测试通过**

- [ ] **Step 5: commit**

```bash
git add internal/agent/tooldef.go internal/agent/tooldef_test.go
git commit -m "feat(agent): BuildModelTools (toolbus.ToolDef -> modelgw.ToolDef + allowlist)"
```

---

## Task 4: truncate

**Files:**
- Create: `internal/agent/truncate.go`
- Create: `internal/agent/truncate_test.go`

- [ ] **Step 1: 写 `truncate_test.go`**

```go
package agent_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/yourorg/private-coding-agent/internal/agent"
)

func TestTruncate_SmallReturnsOriginal(t *testing.T) {
	in := json.RawMessage(`{"ok":true}`)
	out, trunc := agent.TruncateToolOutput(in, 1024)
	require.False(t, trunc)
	require.Equal(t, []byte(in), []byte(out))
}

func TestTruncate_LargeWrapped(t *testing.T) {
	big := bytes.Repeat([]byte("x"), 60*1024)
	in := json.RawMessage(big)
	out, trunc := agent.TruncateToolOutput(in, agent.DefaultMaxToolOutputBytes)
	require.True(t, trunc)
	var env struct {
		Truncated    bool   `json:"truncated"`
		OriginalSize int    `json:"original_size"`
		Preview      string `json:"preview"`
	}
	require.NoError(t, json.Unmarshal(out, &env))
	require.True(t, env.Truncated)
	require.Equal(t, len(big), env.OriginalSize)
	require.NotEmpty(t, env.Preview)
	require.LessOrEqual(t, len(out), agent.DefaultMaxToolOutputBytes)
}

func TestTruncate_MaxZeroSkips(t *testing.T) {
	in := json.RawMessage(`{"x":1}`)
	out, trunc := agent.TruncateToolOutput(in, 0)
	require.False(t, trunc)
	require.Equal(t, []byte(in), []byte(out))
}
```

- [ ] **Step 2: 跑测试确认失败**

- [ ] **Step 3: 实现 `truncate.go`**

```go
package agent

import "encoding/json"

// TruncateToolOutput returns raw unchanged when len(raw) <= max. Otherwise it
// returns a stable envelope JSON:
//
//   {"truncated":true,"original_size":N,"preview":"..."}
//
// The envelope is itself guaranteed to be at most max bytes so the truncation
// step never increases the size sent to the LLM. max==0 disables truncation.
func TruncateToolOutput(raw json.RawMessage, max int) (json.RawMessage, bool) {
	if max <= 0 || len(raw) <= max {
		return raw, false
	}
	// Reserve overhead for the envelope fields/quotes.
	const envelopeOverhead = 80
	previewSize := max - envelopeOverhead
	if previewSize < 32 {
		previewSize = 32
	}
	preview := string(raw)
	if len(preview) > previewSize {
		preview = preview[:previewSize]
	}
	out, _ := json.Marshal(struct {
		Truncated    bool   `json:"truncated"`
		OriginalSize int    `json:"original_size"`
		Preview      string `json:"preview"`
	}{Truncated: true, OriginalSize: len(raw), Preview: preview})
	return out, true
}
```

- [ ] **Step 4: 跑测试通过**

- [ ] **Step 5: commit**

```bash
git add internal/agent/truncate.go internal/agent/truncate_test.go
git commit -m "feat(agent): TruncateToolOutput (>50KB wrapped with preview envelope)"
```

---

## Task 5: Engine + Run

**Files:**
- Create: `internal/agent/engine.go`
- Create: `internal/agent/engine_test.go`

接口（包内本地声明，配合 mockGateway/mockBus 单测）：

```go
type Gateway interface {
    ChatCompletion(ctx context.Context, tenantID, userID uuid.UUID,
        req modelgw.ChatRequest) (*modelgw.ChatResponse, error)
}
type Bus interface {
    ListTools(ctx context.Context, tenantID uuid.UUID) []toolbus.ToolDef
    Invoke(ctx context.Context, tenantID, userID uuid.UUID,
        toolName string, input json.RawMessage) (json.RawMessage, error)
}
```

`Engine{gw Gateway, bus Bus, profiles map[string]Profile, maxOutputBytes int}`，`NewEngine(gw, bus, profiles)`。

`Run(ctx, in, yield)` 算法：

1. `len(Messages)==0` → `ErrEmptyMessages`
2. 解析 profile（未知 → `ErrUnknownProfile`）；MaxSteps=0 时取 profile.MaxSteps 或 16
3. 构造 `messages = [{system}, ...in.Messages]`；`modelTools = BuildModelTools(bus.ListTools, profile.Allowlist)`；`allowed = set(allowlist)`
4. for step 1..MaxSteps：
   - `resp, err := gw.ChatCompletion(ChatRequest{Model, Messages, Tools})`
   - err 或 empty choices → yield error event + return `%w: ErrLLMFailed`
   - yield `assistant_message` (content + tool_calls + finish_reason)
   - finish_reason=="tool_calls" 且 len>0：每个 call 走 `runToolCall`，append 返回的 `role=tool` 消息，继续下一步
   - 否则：yield `final`，return nil
5. 走完循环：yield error event，return `ErrMaxStepsExceeded`

`runToolCall` 三道关卡（任一失败都构造 `role=tool` 错误消息回灌 LLM，**不中断循环**）：

- arguments 不是合法 JSON → emit tool_call+tool_result(ToolError)，返回 error message
- name 不在 allowlist → emit tool_call+tool_result(ToolError)，返回 error message
- bus.Invoke 错误 → emit tool_result(ToolError) (含 `toolbus.ErrToolNotFound` 友好化)，返回 error message
- 成功：截断 → emit tool_result(ToolOutput, Truncated, OriginalSize) → 返回 `role=tool` 含 `content:string(truncated)`

- [ ] **Step 1: 写 `engine_test.go`**（10 个用例）

测试用例：
- `TestEngine_DirectFinal` — 1 步直接 stop
- `TestEngine_SingleToolCall` — 1 步 tool_calls → 2 步 stop（验证 events 序列 + Bus.Invoke 被调用 + 第二次 LLM 调用末尾是 role=tool）
- `TestEngine_TwoToolCalls` — 3 步链路
- `TestEngine_BadArgumentsJSON` — tool_calls 但 Arguments 非 JSON → 不调 Bus → tool_result 含 ToolError → 下一步 stop
- `TestEngine_UnknownTool` — tool_calls 引用不在 allowlist 的工具 → tool_result 含 ToolError "not allowed"
- `TestEngine_LLMError` — gw 返 ProviderUnreachable → 末事件是 error → return `ErrLLMFailed` wrapped
- `TestEngine_MaxStepsExceeded` — gw 一直返 tool_calls → ErrMaxStepsExceeded
- `TestEngine_ToolOutputTruncated` — bus 返 60KB → tool_result.truncated=true + OriginalSize 准确
- `TestEngine_EmptyMessages` — `ErrEmptyMessages`
- `TestEngine_UnknownProfile` — `ErrUnknownProfile`

`mockGateway` 按 step 序列返；`mockBus` 用 outputs/errs map 按 tool 名返。

- [ ] **Step 2: 跑测试确认失败**

- [ ] **Step 3: 实现 `engine.go`**

按上文算法实现 `Engine`、`NewEngine`、`Run`、`runToolCall`、`toolErrorMessage` 等。注意：

- yield 的 error 在内部循环里用 `_ = yield(...)` 忽略——只在主循环判断点 propagate 用户的 yield error
- `runToolCall` **必须** 始终返回一条 `role=tool` 消息，即使是错误路径，这样 LLM 才能在下一步看到错误并自纠
- `bus.Invoke` 返 `toolbus.ErrToolNotFound` 时友好化为 `fmt.Sprintf("tool %q not found", name)`

- [ ] **Step 4: 跑测试通过**（10 testcases）

```bash
go vet ./...
go test ./internal/agent/... -count=1 -v
```

- [ ] **Step 5: commit**

```bash
git add internal/agent/engine.go internal/agent/engine_test.go
git commit -m "feat(agent): Engine.Run ReAct loop (mockGateway/mockBus driven tests)"
```

---

## Task 6: HTTP handler

**Files:**
- Create: `internal/agent/handler.go`
- Create: `internal/agent/handler_test.go`

- [ ] **Step 1: 写 `handler_test.go`**（7 个用例）

```go
type mockRunner struct {
	events []agent.Event
	err    error
	got    agent.RunInput
}

func (m *mockRunner) Run(_ context.Context, in agent.RunInput, yield func(agent.Event) error) error {
	m.got = in
	for _, ev := range m.events { _ = yield(ev) }
	return m.err
}
```

测试用例：
- `TestHandler_Run_OK` — 2 个 event 全返回，末 kind=final
- `TestHandler_Run_NoAuth` — 401
- `TestHandler_Run_BadRequest_NoModel` — 400 code=model_required
- `TestHandler_Run_BadRequest_NoMessages` — 400 code=messages_required
- `TestHandler_Run_UnknownProfile` — runner 返 `ErrUnknownProfile` → 400 code=unknown_profile
- `TestHandler_Run_LLMFailed` — runner 返 `ErrLLMFailed` → 502 code=llm_failed
- `TestHandler_Run_MaxSteps_Returns200WithError` — 200 body 包含 `events` + `error.code=max_steps_exceeded`

- [ ] **Step 2: 跑测试确认失败**

- [ ] **Step 3: 实现 `handler.go`**

```go
// Runner is the subset of *Engine the handler depends on.
type Runner interface {
	Run(ctx context.Context, in RunInput, yield func(Event) error) error
}

type Handler struct{ engine Runner }

func NewHandler(e Runner) *Handler { return &Handler{engine: e} }

func (h *Handler) Register(rg *gin.RouterGroup) {
	rg.POST("/agent/run", h.run)
}

type runRequest struct {
	Model    string                `json:"model"`
	Profile  string                `json:"profile"`
	Messages []modelgw.ChatMessage `json:"messages"`
	MaxSteps int                   `json:"max_steps"`
}
```

handler 逻辑：
1. `auth.FromCtx` 提 claims；nil → 401
2. bind JSON；空 → 400 bad_request
3. Model 空 → 400 model_required；Messages 空 → 400 messages_required
4. 构造 RunInput；用 yield 闭包累加 events；runErr→ `mapErrorToAPI(c, err, events)`
5. 成功 → `200 {"events":[...]}`

`mapErrorToAPI`：
- `ErrEmptyMessages` → 400 messages_required
- `ErrUnknownProfile` → 400 unknown_profile
- `ErrMaxStepsExceeded` → **200** with `{events, error:{code:"max_steps_exceeded"}}`（部分进度可见）
- `ErrLLMFailed` → 502 llm_failed
- `modelgw.ErrProviderUnreachable` → 502 provider_unreachable
- default → 500 internal

- [ ] **Step 4: 跑测试通过**

```bash
go vet ./internal/agent/...
go test ./internal/agent/... -count=1 -v
```

期望：本 task 7 个 + 之前所有共 25 个全 PASS。

- [ ] **Step 5: commit**

```bash
git add internal/agent/handler.go internal/agent/handler_test.go
git commit -m "feat(agent): HTTP POST /agent/run handler (non-streaming, JSON events)"
```

---

## Task 7: 装配 + 增强 mockserver

**Files:**
- Modify: `cmd/server/main.go`
- Modify: `internal/modelgw/mockserver/main.go`

- [ ] **Step 1: 装配 Engine**

在 `cmd/server/main.go` 的 toolBus 装配段（`toolHandler := toolbus.NewHandler(toolBus)`）之后追加：

```go
// Agent Engine (slice 5)
agentProfiles := map[string]agent.Profile{
    "coding": agent.DefaultCodingProfile(),
}
agentEngine := agent.NewEngine(modelGateway, toolBus, agentProfiles)
agentHandler := agent.NewHandler(agentEngine)
```

import 段补 `"github.com/yourorg/private-coding-agent/internal/agent"`。

`register` 闭包内 `toolHandler.Register(protected)` 之后追加 `agentHandler.Register(protected)`。

- [ ] **Step 2: 增强 `internal/modelgw/mockserver/main.go`**

把 `chat` 函数改为解析 messages 并按末条消息分支：

- 若 `req.Stream==true` → 沿用现有 `streamChat`（不动）
- 否则解析 messages，取最后一条：
  - `last.Role == "tool"` → 返 final `"done"`（关闭 ReAct 循环）
  - `last.Role == "user"` 且 `strings.Contains(lower(content), "list"|"ls")` → 返 tool_call `{id:"call_mock_1", function:{name:"fs.list", arguments:{"sandbox_id":<from message UUID>, "path":"/workspace"}}}`；finish_reason="tool_calls"
  - 默认 → 返 final `"hello from mock"`（既有行为）

UUID 提取：扫 message content 的 token，长度=36 且含 4 个 `-` 视为 sandbox UUID。无则用空串。

抽取 helper：`writeFinal(w, model, text)` / `writeToolCall(w, model, callID, name, argsJSON)` / `containsAny(s, subs...)` / `extractSandbox(s)`。

- [ ] **Step 3: 验证 build / vet / test**

```bash
go build ./cmd/server/ ./internal/modelgw/mockserver/
go vet ./...
go test ./... -count=1
```

期望：build 通过；agent + modelgw + toolbus 全 PASS。

- [ ] **Step 4: commit**

```bash
git add cmd/server/main.go internal/modelgw/mockserver/main.go
git commit -m "feat(agent,mockserver): wire agent engine into server; mockserver tool_call state machine"
```

---

## Task 8: E2E + README

**Files:**
- Modify: `deploy/compose/test-e2e.sh`
- Modify: `README.md`

- [ ] **Step 1: 把 16 个 `[N/16]` 改为 `[N/18]`**

```bash
sed -i 's|/16\]|/18\]|g' deploy/compose/test-e2e.sh
```

verify：`grep -c "/16\]"` 应为 0；`grep -c "/18\]"` 应为 16。

- [ ] **Step 2: 在 `[16/18]` 末尾、`echo "E2E PASS"` 之前追加两步**

```bash
echo "[17/18] agent.run direct final ..."
RUN=$(curl -fsS -X POST http://localhost:8080/agent/run \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"model":"default-mock:gpt-4o","profile":"coding","messages":[{"role":"user","content":"hi"}]}')
LAST_KIND=$(echo "$RUN" | jq -r '.events[-1].kind')
[[ "$LAST_KIND" == "final" ]] || { echo "expected final got $LAST_KIND"; echo "$RUN"; exit 1; }
LAST_TEXT=$(echo "$RUN" | jq -r '.events[-1].text')
[[ "$LAST_TEXT" == "hello from mock" ]] || { echo "final text mismatch: $LAST_TEXT"; exit 1; }

echo "[18/18] agent.run with tool_call chain ..."
SBA=$(curl -fsS -X POST http://localhost:8080/sandbox/sessions \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' -d '{}')
IDA=$(echo "$SBA" | jq -r .id)
RUN2=$(curl -fsS -X POST http://localhost:8080/agent/run \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d "{\"model\":\"default-mock:gpt-4o\",\"profile\":\"coding\",\"messages\":[{\"role\":\"user\",\"content\":\"list workspace files for sandbox $IDA\"}]}")
KINDS=$(echo "$RUN2" | jq -r '.events[].kind' | tr '\n' ',')
echo "  -> events: $KINDS"
echo "$KINDS" | grep -q "tool_call," || { echo "no tool_call event"; echo "$RUN2"; exit 1; }
echo "$KINDS" | grep -q "tool_result," || { echo "no tool_result event"; echo "$RUN2"; exit 1; }
LAST2=$(echo "$RUN2" | jq -r '.events[-1].kind')
[[ "$LAST2" == "final" ]] || { echo "expected final got $LAST2"; echo "$RUN2"; exit 1; }
curl -fsS -X DELETE "http://localhost:8080/sandbox/sessions/$IDA" -H "Authorization: Bearer $TOK" >/dev/null
```

- [ ] **Step 3: 修改 README**

切片进度处：
```markdown
- [x] 切片 5：Agent Engine
```

端点表追加：
```markdown
| POST | /agent/run | Bearer | ReAct 循环,返回 events 数组 (非流式) |
```

- [ ] **Step 4: 跑 E2E**

```bash
cd deploy/compose
docker compose down
./test-e2e.sh
```

期望：最后 `E2E PASS`（18 步）。

- [ ] **Step 5: 跑全包测试 sanity**

```bash
cd ..
go test ./... -count=1
go vet ./...
go build ./...
```

期望：全 PASS。

- [ ] **Step 6: commit**

```bash
git add deploy/compose/test-e2e.sh README.md
git commit -m "test(e2e): add agent.run direct final + tool_call chain steps; check slice 5"
```

---

## 验收（end-of-slice checklist）

- [ ] `go test ./...` 全 PASS（不带 tag；不含已存在 docker-依赖的 tenant 集成测试在无 docker 环境下的预期失败）
- [ ] `go vet / build` 干净
- [ ] `docker compose up -d --build` 后 `/healthz` 200
- [ ] `test-e2e.sh` 跑通（18 步，最后 `E2E PASS`）
- [ ] `/agent/run` 直返 final（既有 mock 默认路径）
- [ ] `/agent/run` 走 tool_call → tool_result → final 链路（mockserver 状态机命中 "list"）
- [ ] `tool_invocations` 表有新增 `fs.list` 行（agent → toolbus 触发）
- [ ] git tree clean

---

## Self-Review

**1. Spec coverage:**
- spec §4.3 `Run(session, msg) -> stream<Event>` 接口 ✓ Task 5 `Engine.Run(ctx, in, yield)`
- spec §5.2 ReAct 循环（LLM → tool_call → observation → LLM → final）✓ Task 5
- 单 profile "coding" ✓ Task 2；profile.ToolAllowlist 校验 ✓ Task 5 runToolCall
- 工具调用走 toolbus.Bus；LLM 走 modelgw.Gateway（非流式 ChatCompletion）✓ Task 5
- 工具结果 > 50 KB 截断 ✓ Task 4 + Task 5 集成
- max_steps 上限 ✓ Task 5；各类错误回灌 LLM 自纠 ✓ Task 5 runToolCall（三类错误都构造 role=tool 消息）
- HTTP `POST /agent/run` 非流式（events 数组）✓ Task 6
- mock-provider 增强 ✓ Task 7

**2. Placeholder scan:** 无 TBD / TODO / "类似 Task N" 占位代码块。

**3. Type consistency:**
- `EventKind` Task 1 定义；engine.go yield 用；handler.go events 数组累加；E2E `jq '.events[].kind'` 一致
- `Event` Task 1 字段：tool_input/tool_output 都是 `json.RawMessage`，与 mockserver 返的 arguments string 一致（runToolCall 转 raw 后写入）
- `Runner` interface Task 6 定义；`*Engine` 满足；mockRunner 满足
- 本地 `Gateway` / `Bus` interface Task 5 定义；`*modelgw.Gateway` / `*toolbus.Bus` 满足（自带的方法签名），mockGateway/mockBus 满足
- `Profile.ToolAllowlist` Task 2 定义 8 名；Task 3 `BuildModelTools` 用同名 map 过滤
- `RunInput.MaxSteps==0` → profile.MaxSteps → 16 兜底，Task 5 实现

**4. 测试覆盖：**
- Task 1: 1 个错误哨兵测试
- Task 2: 1 个 DefaultCodingProfile 字段测试
- Task 3: 2 个 BuildModelTools 测试（过滤 + 空入）
- Task 4: 3 个 truncate 测试（小/大/max=0）
- Task 5: 10 个 engine 测试（涵盖 happy / 错误 / 截断 / 边界）
- Task 6: 7 个 handler 测试（含 7 类错误映射）
- 合计 25 个单测；外加 E2E 18 步覆盖端到端 + 数据库行验证

**5. Task 编号 1-8 + 9（本文档自身）= 9 个 Task，无跳号。**
