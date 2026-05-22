# Slice 18 — Sub-Agents + `agent.delegate` 设计 Spec

> Status: Draft — 待评审
> Author: planning session (2026-05-22)
> Related: 主 spec §11（ADR-1/2/3）、Slice 5（Agent Engine）、Slice 12/17（Profile/Skills）、HANDOFF §3.2
> Implementation plan: `docs/superpowers/plans/2026-05-22-slice-18-subagents-delegate.md`

## 1. 概述

Slice 5 引入了「Profile = 系统提示 + 工具白名单 + MaxSteps」的单一身份模型；P0 全程只用 `coding` 一个 Profile，拥有全部 12 个工具。本切片把这层做扁的能力打开：

1. 注册三个新 Profile（`review` / `research` / `workflow-authoring`），各自一份精简白名单与系统提示
2. 新增 `agent.delegate` 工具，让父 Run **委派子任务**到指定 Profile 跑一个独立 Run；子 Run 完成后把 final text 作为单条 `tool_result` 回灌父对话
3. Profile 注册表暴露 `GET /agent/profiles`，前端在创建会话时下拉选择
4. E2E 增加步骤 50 覆盖 delegate 链路（含 audit 校验）

**核心叙事**

- **权限收敛**：评审者只读，研究者无沙箱；从"一个 Profile 拿全部工具"演化到"按职责切片"。
- **上下文隔离**：长任务（评审一份长文档、做一次调研）拆出去单跑，父 Run 拿到的是 summary 而非整段历史。
- **为 Slice 19 预热**：`workflow-authoring` profile 提前进注册表，Slice 19 只需新增 Workflow 工具，不再改 profile.go。

**明确不在本切片范围**

- 异步子 Agent / Reflection（Slice 20）
- 编排路由层 / External MCP 发现（Slice 21）
- 子 Run 事件流式回灌到父客户端
- 会话中途切换 Profile（PATCH /sessions/:id）
- 跨租户委派
- 递归深度 > 1

---

## 2. 前置条件

- Slice 5 已交付（Agent Engine + Profile）
- Slice 9 已交付（Audit Sink 接入 detached 写）
- Slice 12 已交付（系统消息层多块注入；delegate 共用同一拼装路径）
- Slice 13 已交付（quota 中间件透传——子 Run 的 LLM/工具调用走父链同一计量）
- 沙箱由 Slice 14 注入到 `RunInput.SandboxID`；子 Run 通过 RunCtx 继承

---

## 3. 关键设计决策（ADR）

### ADR-64 — 子 Run 继承父会话的 `sandbox_id`

- 子 Run 不新建沙箱：节省资源、避免文件分叉、保持「父子共享同一工作区」直觉
- 风险：子 Profile 仍可读到父沙箱所有文件——通过工具白名单收敛（review/research 不能写）
- 替代方案（已舍弃）：每次 delegate 起新沙箱。耗资源、且子 Run 拿不到父的工作区，违背"评审者读代码"的直觉

### ADR-65 — 递归深度上限 = 1，双保险

- ctx-based 计数 `RunCtx.DelegateDepth`：每次进入 `Engine.Run` 透传父深度；`agent.delegate` 入口检查 `>= MaxDelegateDepth (=1)` 即拒
- 工具白名单兜底：子 Profile（review/research/workflow-authoring）的 `ToolAllowlist` **均不含** `agent.delegate`；即便 ctx 被错误重置，LLM 也调不到
- 替代方案（已舍弃）：无上限。LLM 一旦把自己绕进循环，token 与延时立刻爆炸；同步阻塞模型下递归就是反 ReAct 语义

### ADR-66 — `sandbox_id` 通过 ctx 透传，不依赖 LLM 推断

`toolbus.Tool.Invoke` 签名只有 `(ctx, tenantID, userID, input)`，拿不到父 `RunInput.SandboxID`。三个候选：

| 方案 | 折中 |
|------|------|
| A: LLM 把 sandbox_id 写进 tool input | 父 system prompt 已有 "Current sandbox_id: <X>"；依赖模型不出错 |
| B: ctx-based — Engine 在 ctx 里塞 `runCtxKey{}`，delegate tool 读 | 强一致；LLM 写错也没事 |
| C: 注册 delegate tool 时注入默认 sandbox | 重；不必要 |

**选 B**。新增 `internal/agent/runctx.go`：`WithRunCtx(ctx, RunCtx{SandboxID, Model, DelegateDepth})` + `RunCtxFromCtx(ctx)`。Engine.Run 入口注入；delegate tool 读出后塞进 `childIn.SandboxID/Model` 与 `DelegateDepth+1`。

直接走 `POST /tools/invoke agent.delegate` 时 ctx 没值 → `SandboxID = uuid.Nil`、`Model = ""`，子 Run 起步即失败。**接受**——`/tools/invoke agent.delegate` 不是典型用法，文档里写清楚 delegate 是 "engine-internal" 工具。

### ADR-67 — 子 Run 事件汇总成单 `tool_result`，不流式回灌父客户端

- delegate tool 内部 accumulator 收集子 Run 的 `EventFinal.Text` 作为 `result`；统计 `EventToolCall` 数与名称进 `sub_tool_calls`；step_index max 进 `sub_steps`
- 父客户端看到的链路：`tool_call agent.delegate{...}` → `tool_result {result, sub_steps, status, sub_tool_calls}`；中间子 Run 的 `assistant_delta` / 子工具调用**不外泄**
- 替代方案（已舍弃）：把子 yield 接到父 yield。流式 UI 与会话日志会出现"父子混合事件"，客户端必须懂"现在哪条 delta 是谁的"，重构成本高且不直觉
- 代价：父客户端在 delegate 耗时期间没有任何中间反馈。**接受**——异步化是 Slice 20 的范畴

### ADR-68 — 子 Run 错误降级为 `tool_result.error`，不熔断父

- delegate tool 内部对 `engine.Run` 的返回错误做容错：返回 `{status: "error", result: errMsg}`，由现有 `runToolCall` 路径包成 `tool_result.error` 给 LLM；父 Engine 继续运行
- 例外：`MaxStepsExceeded` 视为 `status: "max_steps"`（result = 最后一次 `EventFinal` 或部分进度），不是 fatal
- 替代方案（已舍弃）：子错误抛父。LLM 没机会"读到错误后另寻方案"；且违反"工具失败=LLM 自纠"的现有约定

---

## 4. 接口与数据形状

### 4.1 `RunCtx`

```go
package agent

type RunCtx struct {
    SandboxID     uuid.UUID
    Model         string
    DelegateDepth int
}
const MaxDelegateDepth = 1

func WithRunCtx(ctx context.Context, rc RunCtx) context.Context
func RunCtxFromCtx(ctx context.Context) RunCtx  // zero value if absent
```

注入点：`Engine.Run` 函数开头（在 tracer.Start 之前）。

### 4.2 `agent.delegate` 工具

**Input schema：**

```json
{
  "type": "object",
  "properties": {
    "profile":   {"type": "string", "enum": ["coding", "review", "research", "workflow-authoring"]},
    "task":      {"type": "string", "minLength": 1, "maxLength": 8000},
    "max_steps": {"type": "integer", "minimum": 1, "maximum": 8}
  },
  "required": ["profile", "task"],
  "additionalProperties": false
}
```

**Output：**

```json
{
  "result": "<final assistant text from child run>",
  "sub_steps": 3,
  "status": "ok|max_steps|error",
  "sub_tool_calls": ["fs.read", "llm.chat"]
}
```

`status="error"` 时 `result` 含错误信息；status 一律走包装而非 Tool.Invoke 抛错（这样 OTel span/audit 不会被记成 tool 失败）。

### 4.3 Profile 注册表扩展

```go
type Profile struct {
    Name          string
    Description   string  // NEW — 仅供 GET /agent/profiles 展示
    SystemPrompt  string
    ToolAllowlist []string
    MaxSteps      int
    SkillIDs      []string
}
```

| Profile | 白名单 | MaxSteps | 备注 |
|---------|--------|----------|------|
| `coding` | 现有 12 + `agent.delegate` | 16 | 唯一能 delegate 的入口 |
| `review` | `fs.read`, `fs.list`, `fs.glob`, `grep`, `memory.search`, `memory.list`, `llm.chat` | 8 | 只读；不含 `fs.write` / `shell.exec` |
| `research` | `llm.chat`, `llm.embed`, `memory.search`, `memory.list`, `memory.save` | 8 | 不操作沙箱；可写记忆 |
| `workflow-authoring` | `llm.chat`, `memory.search`, `fs.read`, `fs.glob`, `grep` | 6 | 为 Slice 19 预热 |

review profile 的 system prompt 末尾加 `Internal marker (do not echo to user): E2E_DELEGATE_SUB_V1`——配合 mock provider 识别（生产 prompt 无 marker，不影响真实行为）。

### 4.4 `GET /agent/profiles`

```http
GET /agent/profiles
Authorization: Bearer <token>

200
{"profiles":[
  {"name": "coding",             "description": "全能编码 Agent..."},
  {"name": "research",           "description": "..."},
  {"name": "review",             "description": "..."},
  {"name": "workflow-authoring", "description": "..."}
]}
```

放在 `protected` 路由组（与 `/tools` 一致）。Engine 暴露 `Profiles() []Profile` 返回按名字排序的副本。

### 4.5 Audit

| Action | Target | Metadata |
|---|---|---|
| `agent.delegate.start` | sub_profile | `parent_profile`, `task_chars`, `max_steps` |
| `agent.delegate.complete` | sub_profile | `parent_profile`, `sub_steps`, `status`, `sub_tool_calls` |

均走 `audit.Detached`（不阻塞 ReAct 主路径）。

---

## 5. 关键不变量

1. **租户/用户隔离**：子 Run TenantID/UserID 强制继承父；delegate input 不接受 tenant_id/user_id 字段（schema 限定 additionalProperties=false）
2. **深度双保险**：ctx 计数 + 子 Profile 不挂 `agent.delegate`
3. **配额自然透传**：子 Run 走原 quota middleware；一次 delegate 成本 = 子 Run 全部消耗 + 父链 1 个 tool_invoke 槽位
4. **沙箱继承**：review 不写，因白名单不含 `fs.write/shell.exec`
5. **审计完整链**：start/complete 都带 parent + sub profile
6. **trace 自然嵌套**：子 `agent.run` span 在 delegate tool 的 ctx 下，OTel 父子关系自动建立
7. **子错误不熔断**：见 ADR-68
8. **MaxSteps clamp**：input.max_steps ∈ [1, 8]，默认 4
9. **结果裁剪**：子 Run final text 走 `TruncateToolOutput`
10. **stream 事件不外泄**：见 ADR-67
11. **Profile 校验**：CreateSession 的 profile ∉ 已注册集 → 400 + `unknown_profile`

---

## 6. 验收

见 plan 的 Acceptance 段（迁移到实现仓库后 link 化）。E2E **50/50** 通过即视为 Slice 18 完成。

---

## 7. 未做项与后续

| 项 | 出栈到 |
|----|--------|
| PATCH /sessions/:id 切换 profile | 由 Slice 16 已加 Memories 入口；profile 切换业务价值低，不入路线图 |
| 异步子 Run / 流式回灌 | Slice 20 — Reflection |
| Sub-Agent pool / 并发 delegate | Slice 21 — Orchestration |
| Workflow 工具自动注册 | Slice 19 |
| External MCP server delegate | Slice 21 |
| Recursion depth > 1 | 不入路线图（同步 ReAct 下不安全） |
