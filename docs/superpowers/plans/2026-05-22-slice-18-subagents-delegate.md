# Slice 18 — Sub-Agents + `agent.delegate`

## Context

MVP-P1（Slice 13–17）已收口，进入 Full P1。本切片把当前"单 Profile 单 Agent"扩成"多 Profile + 父子 Agent 协作"。具体痛点：

1. **Coding profile 拿了 12 个工具的全部权限**——做 code review 时不应该有 `shell.exec`/`fs.write`，做 research 时连沙箱都不该碰。粒度过粗。
2. **没法把"找资料"或"评审"作为一次独立调用扔出去**——所有事都堆在主对话历史里，长上下文容易把模型带偏。
3. **Workflow Engine（Slice 19）需要 `workflow-authoring` profile**——提前把它接进 Profile 注册表，Slice 19 不用再改 profile.go。

本切片闭环：注册 `review` / `research` / `workflow-authoring` 三个新 Profile（各自最小工具白名单 + 专用 system prompt）；新增 `agent.delegate` 工具，让父 Run 一次性发包给子 Profile，子 Run 跑完返回结果作为 tool message；Home 页加 Profile 下拉让用户在创建会话时选；E2E 步骤 50 覆盖 delegate 链路。

**用户已确认的边界**：
1. 子 Run **继承父会话的 sandbox_id**（不新建沙箱、不强制 explicit 传参）
2. 递归深度上限 **= 1**（子 Run 不能再 delegate；通过 ctx-based 计数 + 子 Profile 不挂 `agent.delegate` 双保险）
3. Profile 集 = **review + research + workflow-authoring**（一次到位，Slice 19 不用再改）
4. WebUI **只在 Home 页加创建时下拉**，不做"会话中途切 Profile"（不需要 PATCH /sessions/:id）

## Goal

- `internal/agent/profile.go`：新增 `DefaultReviewProfile()` / `DefaultResearchProfile()` / `DefaultWorkflowAuthoringProfile()`；每个含独立 system prompt + 最小工具白名单；Profile struct 加 `Description string`
- `internal/agent/delegate_tool.go` + `_test.go`：新工具 `agent.delegate`，input `{profile, task, max_steps?}`，output `{result, sub_steps, status, sub_tool_calls}`
- `internal/agent/runctx.go` + `_test.go`：ctx-based RunCtx 透传 `SandboxID` + `DelegateDepth`
- `internal/agent/engine.go`：Run 入口注入 RunCtx；不改其他逻辑
- `internal/agent/handler.go`：新增 `GET /agent/profiles`，返回 `[{name, description}]`
- Audit：`agent.delegate.start{parent_profile, sub_profile, max_steps, task_chars}` + `agent.delegate.complete{sub_profile, sub_steps, status, duration_ms}`，都走 `audit.Detached`
- `internal/webui/src/pages/Home.tsx`：profile 下拉（从 `GET /agent/profiles` 拉取），默认 coding
- `internal/webui/src/types/api.ts`：新增 `ProfileInfo` 类型 + `listProfiles()` API
- `internal/modelgw/mockserver/main.go`：识别 `E2E_DELEGATE_PARENT_V1` 与 `E2E_DELEGATE_SUB_V1` 系统标记，分别返回 `tool_call agent.delegate{...}` 与 `delegate-sub-marker-ok`
- `cmd/server/main.go`：注册三个新 Profile；注册 `agent.delegate` 工具；coding profile 把 `agent.delegate` 也加进白名单
- `deploy/compose/test-e2e.sh`：49 步 → 50 步（追加 [50/50] delegate round-trip）
- 文档：`docs/superpowers/specs/2026-05-22-slice-18-subagents-delegate-design.md`、README 切片进度勾选、SLICE-VERIFICATION.md 加 Slice 18 行、HANDOFF.md 更新

## 非目标（明确出栈）

- **PATCH /sessions/:id 切换 profile** — Slice 18 不做；Home 创建时锁死
- **会话内 profile 热切** — 同上
- **递归深度 > 1** — 子 Profile 不挂 `agent.delegate`；ctx 计数仅做双保险
- **新建独立沙箱给子 Run** — 始终继承父 sandbox_id
- **流式回灌子 Run 事件** — 子 Run 的 assistant_delta / tool_call 不暴露到父客户端流；子 Run 结果汇总成单条 tool_result 返回
- **跨租户 delegate** — 子 Run 强制继承父 TenantID/UserID
- **Orchestration Router** — Slice 21 的活
- **External MCP server discovery** — Slice 21
- **Reflection / 异步子 Agent** — Slice 20

## Architecture

```
父 Run (profile=coding, sandbox=X)
  ├─ user: "请 review SECURITY-SANDBOX.md 的网络隔离章节"
  ├─ assistant tool_call: agent.delegate
  │    input: {profile: "review", task: "review SECURITY-SANDBOX.md §4"}
  ├─ Engine.runToolCall → bus.Invoke("agent.delegate", input)
  │
  │   ┌── delegateTool.Invoke(ctx, tenantID, userID, input) ────────────┐
  │   │  1. depth := RunCtxFromCtx(ctx).DelegateDepth                   │
  │   │     reject if depth >= MaxDelegateDepth (=1)                     │
  │   │  2. validate input.profile in registry                          │
  │   │  3. audit: agent.delegate.start                                 │
  │   │  4. childCtx = WithRunCtx(ctx, RunCtx{                          │
  │   │       SandboxID:     parentRunCtx.SandboxID,                    │
  │   │       DelegateDepth: depth + 1,                                  │
  │   │     })                                                            │
  │   │  5. childIn := agent.RunInput{                                  │
  │   │       TenantID/UserID 继承父; Model 继承父;                      │
  │   │       Messages: [{role:user, content:task}];                    │
  │   │       ProfileName: input.profile;                               │
  │   │       SandboxID: parentRunCtx.SandboxID;                        │
  │   │       MaxSteps: clamp(input.max_steps, 1, 8) 默认 4              │
  │   │     }                                                            │
  │   │  6. accumulator 收集 Events:                                     │
  │   │       - 取最后 EventFinal.Text 作为 result                        │
  │   │       - 计数 EventToolCall 进 sub_tool_calls                      │
  │   │       - 计步 max step_index 进 sub_steps                          │
  │   │  7. Engine.Run(childCtx, childIn, accum.yield)                  │
  │   │  8. audit: agent.delegate.complete                              │
  │   │  9. return JSON({result, sub_steps, status, sub_tool_calls})   │
  │   └────────────────────────────────────────────────────────────────┘
  │
  ├─ tool_result: {result: "...", sub_steps: 3, status: "ok"}
  └─ assistant final: "..."
```

### sandbox_id 怎么传给 delegate tool

Tool.Invoke 拿不到父 RunInput 的 SandboxID（Tool 接口只有 ctx + tenantID + userID + input）。三个候选：

| 方案 | 折中 |
|------|------|
| A: LLM 把 sandbox_id 写进 tool input | 父 system prompt 已有 `Current sandbox_id: <X>`，LLM 能填；但依赖模型不出错 |
| B: ctx-based — Engine 在 ctx 里塞 `runCtxKey{}`，delegate tool 读 | 强一致，LLM 写错也没事；改动小 |
| C: 注册 delegate tool 时注入 Engine refs 包括默认 sandbox | 最重；不必要 |

**选 B**。新增 `internal/agent/runctx.go`：`WithRunCtx(ctx, RunCtx{SandboxID, DelegateDepth})` + `RunCtxFromCtx(ctx) RunCtx`。Engine.Run 入口注入 SandboxID；delegate tool 读出后塞进 childIn.SandboxID，并把 DelegateDepth+1 透传给 child Engine.Run。如果谁直接 `POST /tools/invoke agent.delegate`，ctx 没值 → SandboxID = uuid.Nil（子 Run 没沙箱，fs.* 会失败）。**接受**——`/tools/invoke` 直接调 delegate 本来就是非典型用法。

### Profile 工具白名单

| Profile | system prompt 要点 | tool 白名单 |
|---------|-------------------|------------|
| `coding` (已存在) | 现有；**新增 `agent.delegate`** | 现有 12 + `agent.delegate` |
| `review` | 你是代码评审者；只读 + 给意见；**禁止修改文件** | `fs.read`, `fs.list`, `fs.glob`, `grep`, `memory.search`, `memory.list`, `llm.chat` |
| `research` | 你是研究助手；搜资料 + 总结；不操作沙箱 | `llm.chat`, `llm.embed`, `memory.search`, `memory.list`, `memory.save` |
| `workflow-authoring` | 你帮用户把自然语言需求转成 SKILL.md draft（为 Slice 19 预热） | `llm.chat`, `memory.search`, `fs.read`, `fs.glob`, `grep` |

子 Profile **均不含** `agent.delegate`——递归靠 ctx 深度计数兜底；白名单是第一道闸。

### Engine.Run 改动（极小）

只在 Run 函数最前面（在 tracer.Start 之前）附加：

```go
parentRC := RunCtxFromCtx(ctx)
ctx = WithRunCtx(ctx, RunCtx{
    SandboxID:     in.SandboxID,                  // 永远以 RunInput 为准（child 的 RunInput.SandboxID 由 delegate tool 填）
    DelegateDepth: parentRC.DelegateDepth,         // 0 第一次进入；child Run 进来时已是 1
})
```

不动其他逻辑。

## 关键不变量

1. **租户/用户隔离** — 子 Run 的 TenantID/UserID 强制继承父；delegate tool 不接受 input.tenant_id/user_id
2. **深度兜底双保险** — ctx 计数 + 子 Profile 不挂 `agent.delegate`；任一防线失效另一道仍生效
3. **配额自然透传** — 子 Run 的 `llm.chat` / `tool.invoke` 走原来的 quota middleware，无需特殊处理；一次 delegate 的成本 = 子 Run 全部消耗 + 父链 1 个 tool_invoke 槽位
4. **沙箱继承不破坏隔离** — 子 Profile 工具白名单限制了"评审者不能改沙箱"；review 白名单不含 fs.write/shell.exec
5. **审计完整链** — start/complete 两条都带父 + 子 profile；trace 自然嵌套（child Run 的 `agent.run` span 在 delegate tool 的 ctx 下，OTel 父子关系自动建立）
6. **子 Run 错误不杀父 Run** — delegate tool 返回错误 → 走现有 `runToolCall` 的 ErrorMessage 路径，LLM 看到错误后可自行处理；不抛 Engine 级错
7. **MaxSteps 强制 clamp** — `max_steps` ∈ [1, 8]，默认 4；防止子 Run 失控烧 token
8. **结果裁剪** — 子 Run 的 final text 走 `TruncateToolOutput`，与其他 tool_result 一致（DefaultMaxToolOutputBytes）
9. **stream 事件不外泄** — delegate tool 内部 `accum.yield` 闭包**不调用父 yield**；客户端只看到 delegate 的 tool_call + tool_result
10. **Profile 字段校验** — POST /sessions 创建时若 profile ∉ {coding, review, research, workflow-authoring} → 400；当前 Service 已有 unknown profile 校验（`ErrUnknownProfile`），只需扩展 known set

## 工作分解

### Task 0 — design spec

Create `docs/superpowers/specs/2026-05-22-slice-18-subagents-delegate-design.md`。ADR 重点：
- ADR-64 子 Run 继承父 sandbox（vs 新建）
- ADR-65 递归深度上限 = 1（白名单 + ctx 双保险）
- ADR-66 sandbox_id 通过 ctx 透传（不走 LLM 推断）
- ADR-67 子 Run 事件不流到父客户端（汇总成单 tool_result）
- ADR-68 子 Run 错误降级为 tool_result.error（不熔断父）

### Task 1 — Profile 注册表扩展

**Files:** `internal/agent/profile.go`

新增三个 constructor。所有都把 `MaxSteps` 设小一些（review/research 默认 8，workflow-authoring 默认 6）。coding profile 的 ToolAllowlist 末尾追加 `"agent.delegate"`。Profile struct 新增 `Description string` 字段（用于 `GET /agent/profiles` 输出）。

### Task 2 — RunCtx + depth 工具

**Files:** Create `internal/agent/runctx.go` + `_test.go`

```go
package agent

import (
    "context"
    "github.com/google/uuid"
)

type RunCtx struct {
    SandboxID     uuid.UUID
    DelegateDepth int
}
type runCtxKey struct{}

func WithRunCtx(ctx context.Context, rc RunCtx) context.Context
func RunCtxFromCtx(ctx context.Context) RunCtx  // zero value if missing

const MaxDelegateDepth = 1
```

测试覆盖：set / get / 重入覆盖 / 空 ctx 返回 zero value。

### Task 3 — delegate tool

**Files:** Create `internal/agent/delegate_tool.go` + `delegate_tool_test.go`

```go
type DelegateTool struct {
    engine    *Engine
    profiles  map[string]Profile
    auditSink audit.Sink
}

func NewDelegateTool(e *Engine, profiles map[string]Profile, sink audit.Sink) toolbus.Tool { ... }

func (t *DelegateTool) Name() string { return "agent.delegate" }
func (t *DelegateTool) Schema() json.RawMessage { ... 见 input shape ... }

func (t *DelegateTool) Invoke(ctx context.Context, tenantID, userID uuid.UUID, raw json.RawMessage) (json.RawMessage, error) {
    // 1. unmarshal input
    // 2. depth check: if RunCtxFromCtx(ctx).DelegateDepth >= MaxDelegateDepth -> error
    // 3. validate profile name in t.profiles & ≠ caller profile if same name not allowed (实际无所谓 — 即使是 coding->coding 深度也防住)
    // 4. clamp max_steps to [1, 8] default 4
    // 5. audit start
    // 6. accumulate events into struct {result string; sub_steps int; sub_tool_calls []string}
    // 7. parentRC := RunCtxFromCtx(ctx)
    //    childCtx := WithRunCtx(ctx, RunCtx{SandboxID: parentRC.SandboxID, DelegateDepth: parentRC.DelegateDepth + 1})
    // 8. childIn := RunInput{...; SandboxID: parentRC.SandboxID; MaxSteps: clamped; Model: 从 ctx 拿? -> see below}
    // 9. engine.Run(childCtx, childIn, accum.yield)
    // 10. audit complete
    // 11. return JSON
}
```

**Model 怎么传给子 Run**：Tool.Invoke 拿不到父的 Model。两个选择：
- A: tool input 加 `model` 字段（可选，默认走 mock 或继承）
- B: 通过 RunCtx 透传 Model

**选 B**——RunCtx 加 `Model string` 字段；Engine.Run 入口注入 in.Model。一致性更好。RunCtx 现在三个字段：SandboxID / Model / DelegateDepth。

Input schema:
```json
{
  "type": "object",
  "properties": {
    "profile": {"type": "string", "enum": ["coding", "review", "research", "workflow-authoring"]},
    "task":    {"type": "string", "minLength": 1, "maxLength": 8000},
    "max_steps": {"type": "integer", "minimum": 1, "maximum": 8}
  },
  "required": ["profile", "task"],
  "additionalProperties": false
}
```

Output:
```json
{
  "result": "<final assistant text from child run>",
  "sub_steps": 3,
  "status": "ok|max_steps|error",
  "sub_tool_calls": ["fs.read", "llm.chat", ...]
}
```

### Task 4 — Engine.Run 注入 RunCtx

**Files:** Modify `internal/agent/engine.go`

在 Run 函数开头（在 tracer.Start 之前）：
```go
parentRC := RunCtxFromCtx(ctx)
ctx = WithRunCtx(ctx, RunCtx{
    SandboxID:     in.SandboxID,
    Model:         in.Model,
    DelegateDepth: parentRC.DelegateDepth, // 0 顶层；child 进来时是 1
})
```

不动其他逻辑。

### Task 5 — GET /agent/profiles

**Files:** Modify `internal/agent/handler.go` + `handler_test.go`

```go
GET /agent/profiles -> 200
{"profiles":[
  {"name":"coding", "description":"..."},
  {"name":"review", "description":"..."},
  {"name":"research", "description":"..."},
  {"name":"workflow-authoring", "description":"..."}
]}
```

Description 字段加进 Profile struct（同时 `profile.go` 的四个 constructor 各填一句）。Handler 需要持有 profiles map ref；目前 NewHandler(engine *Engine) 拿不到，需要 Engine 暴露 `Profiles() []Profile`（返回 slice 副本，sorted by name）。

### Task 6 — mock provider delegate 标记

**Files:** Modify `internal/modelgw/mockserver/main.go`

参考已有 `tenantSkillMarker` 模式新增：

```go
const (
    delegateParentMarker = "E2E_DELEGATE_PARENT_V1"
    delegateSubMarker    = "E2E_DELEGATE_SUB_V1"  // 注入 review profile 的 system prompt 里
)

func hasDelegateParentMarker(msgs []mockMessage) bool { ... }   // 扫 user/system 找 marker
func hasDelegateSubMarker(msgs []mockMessage) bool { ... }      // 扫 system 找 marker
func hasToolMessage(msgs []mockMessage) bool { ... }            // 扫是否已有 role=tool

// 在 chat handler 分发处(优先级从高到低):
//   1. hasDelegateSubMarker -> 直接 final "delegate-sub-marker-ok"
//      (子 Run 的 review profile system prompt 含 marker;不依赖父链历史)
//   2. hasDelegateParentMarker && hasToolMessage -> final "delegate-parent-final: ..."
//   3. hasDelegateParentMarker && !hasToolMessage -> tool_call agent.delegate {profile:"review", task:"..."}
```

review profile 的 system prompt 末尾追加一行：`Internal marker (do not echo to user): E2E_DELEGATE_SUB_V1`——这样所有 review 子 Run 都会被 mock provider 识别为子调用。production prompt 无 marker（不影响真实行为）。

抽个 helper `pickDeterministicResponse(msgs []mockMessage) mockResponse` 把分发集中到一处，便于 Slice 19 继续扩展。

### Task 7 — main.go wiring

**Files:** Modify `cmd/server/main.go`

```go
agentProfiles := map[string]agent.Profile{
    "coding":              agent.DefaultCodingProfile(),
    "review":              agent.DefaultReviewProfile(),
    "research":            agent.DefaultResearchProfile(),
    "workflow-authoring":  agent.DefaultWorkflowAuthoringProfile(),
}
// ... existing engine init (agentEngine := agent.NewEngine(...))

// agent.delegate tool: 需要 Engine + profiles + auditSink
// 注意顺序: agentEngine 与 auditRepo 都已就绪后再 register
delegateTool := agent.NewDelegateTool(agentEngine, agentProfiles, auditRepo)
_ = toolRegistry.Register(delegateTool)
```

当前 `auditRepo` 在 Engine 构造之后才声明；把 delegate tool 注册挪到 `auditRepo := audit.NewRepo(pool)` 行之后即可。Engine 已经在前面拿到 toolBus / toolRegistry，不会因为 delegateTool 在后注册而看不到——`bus.ListTools` 在每次 `Engine.Run` 调时才扫 registry，不缓存。

### Task 8 — WebUI Profile 下拉

**Files:** Modify `internal/webui/src/pages/Home.tsx`、`internal/webui/src/types/api.ts`、`internal/webui/src/lib/api.ts`（如果存在）

- `types/api.ts` 加 `ProfileInfo { name: string; description: string }`
- API client 加 `listProfiles(): Promise<ProfileInfo[]>` （走 `GET /agent/profiles`）
- `Home.tsx` 增加 react-query `useQuery(['profiles'], listProfiles)`；放一个 `<Select>` 控件（shadcn/ui，参考 SkillsAdmin.tsx 里用过的）
- 创建 session 时把选中的 profile 传进 `CreateSessionRequest.profile`
- 列表 / Chat 页**不动**

### Task 9 — E2E 步骤 50

**Files:** Modify `deploy/compose/test-e2e.sh`

- 全文 `/49]` sed 换成 `/50]`
- 末尾追加 [50/50]：
  1. 创建 session profile=coding model=default-mock:gpt-4o
  2. 通过 WS 发 user message："E2E_DELEGATE_PARENT_V1 请委托 review profile 评审一下"
  3. 等 done；断言 messages 表里有 tool_call agent.delegate（role=tool，name=agent.delegate）+ 父 final 含 `delegate-parent-final`
  4. tool_result 的 content 解析出 JSON `{result: "delegate-sub-marker-ok", status: "ok", sub_steps >= 1}`
  5. 查 audit_log 含 `agent.delegate.start` 与 `agent.delegate.complete`，且都带 sub_profile=review
  6. 清理 session

### Task 10 — 文档收口

**Files:** README.md、docs/SLICE-VERIFICATION.md、HANDOFF.md、Plan 归档到 `docs/superpowers/plans/2026-05-22-slice-18-subagents-delegate.md`

- README 切片进度 18 勾选 + 新增"Sub-Agents"小节（4 个 profile 表 + delegate 工具签名）
- SLICE-VERIFICATION.md：Slice 18 行（L1: agent 包测试；L3: 50/50）
- HANDOFF.md：进度表 18 ✅；E2E 数 49 → 50；HEAD 等 commit 后回填

## 关键文件清单

**新增（6 个）：**
- `internal/agent/delegate_tool.go` + `_test.go`
- `internal/agent/runctx.go` + `_test.go`
- `docs/superpowers/specs/2026-05-22-slice-18-subagents-delegate-design.md`
- `docs/superpowers/plans/2026-05-22-slice-18-subagents-delegate.md`（归档）

**修改（约 11 个）：**
- `internal/agent/profile.go`（+ Description 字段，+ 3 new constructors，coding 加 agent.delegate）
- `internal/agent/engine.go`（Run 入口注入 RunCtx；加 Profiles() helper）
- `internal/agent/handler.go`（GET /agent/profiles）
- `internal/agent/handler_test.go`
- `internal/modelgw/mockserver/main.go`（delegate parent/sub marker + 抽取 pickDeterministicResponse）
- `cmd/server/main.go`（profiles map + delegate tool wiring）
- `internal/webui/src/pages/Home.tsx`（profile 下拉）
- `internal/webui/src/types/api.ts`（ProfileInfo + listProfiles）
- `deploy/compose/test-e2e.sh`（[1/49] → [1/50]，追加 [50/50]）
- `README.md`
- `docs/SLICE-VERIFICATION.md`
- `HANDOFF.md`

## 验证

```bash
# L1
go test ./internal/agent/... ./internal/modelgw/... -count=1
go vet ./...

# L2
cd internal/webui && npm run build && cd ../..
go build -o bin/server ./cmd/server

# L3
cd deploy/compose && ./test-e2e.sh   # 期望 50/50 PASS

# 手工 smoke (compose up 之后):
TOK=$(curl -s -X POST http://localhost:8080/auth/login -H 'Content-Type: application/json' \
  -d '{"tenant":"default","email":"demo@example.com","password":"demo123"}' | jq -r .token)

# 1) GET /agent/profiles 返回 4 个
curl -s -H "Authorization: Bearer $TOK" http://localhost:8080/agent/profiles | jq .

# 2) 直接走 /agent/run delegate
curl -s -X POST -H "Authorization: Bearer $TOK" http://localhost:8080/agent/run \
  -H 'Content-Type: application/json' \
  -d '{"model":"default-mock:gpt-4o","profile":"coding","messages":[
        {"role":"user","content":"E2E_DELEGATE_PARENT_V1 委托 review 评审"}
      ]}' | jq '.events | map(.kind)'
# 期望事件序列: ... tool_call(agent.delegate) -> tool_result -> final

# 3) audit
curl -s -H "Authorization: Bearer $TOK" "http://localhost:8080/audit?action=agent.delegate.start" | jq '.entries | length'
# 期望 >= 1
```

## Acceptance

- [ ] `GET /agent/profiles` 返回 4 个 profile，含 description
- [ ] coding profile 含 `agent.delegate`；review/research/workflow-authoring **不含**
- [ ] `agent.delegate` 工具列在 `GET /tools` 输出中（13 个工具）
- [ ] 单测：delegate tool 在 ctx 已有 `DelegateDepth=1` 时拒绝（返回 error JSON）
- [ ] 单测：delegate tool 在 profile 不存在时返回 422 / error
- [ ] 单测：delegate tool clamp `max_steps` 到 [1, 8]，默认 4
- [ ] 单测：子 Run 错误不导致父 Engine.Run 返回错误（错误被包成 tool_result.error）
- [ ] 子 Run **继承父 sandbox_id**（通过 RunCtx ctx 透传）
- [ ] 子 Run 的 assistant_delta / tool_call 事件**不进**父客户端流（仅 delegate 的 tool_call/tool_result 可见）
- [ ] Audit 含 `agent.delegate.start` + `agent.delegate.complete`，metadata 含 parent_profile / sub_profile / sub_steps / status
- [ ] OTel：子 `agent.run` span 是父 `tool.invoke{tool=agent.delegate}` 的子 span
- [ ] WebUI Home 页 profile 下拉显示 4 项，默认 coding；选中后创建的 session.profile 与选择一致
- [ ] E2E 50/50 PASS
- [ ] `git tree clean`，4 个 Conventional Commits 切分：
  - `feat(agent): profile registry + runctx + delegate tool`
  - `feat(agent): GET /agent/profiles + engine RunCtx injection`
  - `feat(webui): profile dropdown on Home`
  - `feat(modelgw,e2e,docs): delegate markers + E2E 50 + Slice 18 docs`

## 风险与折衷

1. **delegate tool 直接 /tools/invoke 调用时 ctx 没 RunCtx** — SandboxID = uuid.Nil，子 Run 没沙箱；Model 也是空——会直接 LLM 报错。**接受**：这不是典型用法，文档里写清楚 delegate 是"engine-internal"工具。
2. **mock provider 多 marker 分发** — 已有 skill / tenant skill 两套；再加 delegate parent/sub 两套，分支变多。**缓解**：抽出 `pickDeterministicResponse` helper。Slice 19 还要加 workflow marker；现在就把抽象做出来。
3. **子 Run 同步阻塞父 tool_call** — 大模型可能花几秒；用户客户端看到的就是 tool_result 久久不到。**接受**——这是 ReAct 语义。Slice 19/20 异步化才能改。
4. **跨 Profile token 飙升** — 一次 delegate 实际产生 2 次 ChatCompletion（父第一轮 + 子 Run）+ 子 Run 的 N 次 tool 步。**缓解**：`max_steps` 默认 4 + clamp [1,8]；quota 自然兜底（超 daily token 直接 429）。
5. **review profile 用 fs.read 看到父沙箱** — 等价于"评审者能看到所有代码"，符合直觉；但如果父沙箱里有写好但未提交的密钥之类，子 Profile 也能读到。**接受**——同租户同用户内的隔离不在本切片范畴。
6. **Profile description 文案** — 4 个 description 文案不动模型行为（只给前端展示）；不必过度推敲。
7. **GET /agent/profiles 是否要鉴权** — 选放进 `protected` 组（要 JWT），保持与 `/tools` 一致。匿名暴露 profile 列表无安全风险但无收益。
8. **递归深度 ctx 计数被外部代码 reset 风险** — `WithRunCtx` 只在 agent 包内调用（Engine.Run + delegate tool）；保证只增不减；外部代码无法 reset。
9. **RunCtx Model 字段污染 Tool 行为** — Model 仅给 delegate tool 用；其他工具 Invoke 不读 RunCtx；纯透传字段，无副作用。
