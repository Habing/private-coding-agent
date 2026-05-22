# Slice 19a — Workflow Engine

> Archived plan as implemented. See spec `2026-05-22-slice-19-workflow-engine-design.md` for design + ADRs.

## Context

Full P1 第二个切片。Slice 18 已交付 Sub-Agents 与 `agent.delegate`；本切片填上 Workflows——把高频、步骤稳定的确定性流程沉淀为可版本化、可发布、可被 Agent 调用的 DAG。

**已确认边界：**
1. 范围 = 19a 仅 Engine（19b Authoring Agent / NL→DSL / Web 列表 / 发布按钮另切）
2. Dry-Run 策略 = 在 Tool 接口加 `IsMutating()` 标记；Engine 在 dry_run=true 时拦截 mutating tool 返回 mock JSON

**非目标（明确出栈）：** Authoring Agent、NL→DSL、Web UI、`wait_event`、版本历史表、Fork / 可见范围、触发器、Sub-flow 专用 kind（通过 workflow.<id> 工具自动成立）。

## Delivered

- `internal/db/migrations/0018_workflows.{up,down}.sql`：`workflows` + `workflow_runs`
- `internal/workflow/`：types / parse / validate / expr / engine / repo / service / workflow_tool / handler + tests
- `internal/toolbus`：`Unregister(name string)` + 可选接口 `Mutating { IsMutating() bool }`
- 标 `IsMutating()=true` 的工具：fs.write、shell.exec、memory.save、memory.delete、agent.delegate
- Admin REST：`POST/GET/PUT/DELETE /admin/workflows[/:slug]`、`POST /admin/workflows/:slug/publish|unpublish`、`POST /admin/workflows/:slug/invoke`（含 `?dry_run=true`）、`GET /admin/workflows/:slug/runs`
- 发布后 → ToolBus 中 `workflow.<slug>` 出现在 `GET /tools`，可经 `POST /tools/invoke` / Agent tool_call 触发
- 启动 re-publish：`workflowService.RepublishAll(ctx)` 把所有 published 行重注册进 Bus
- 审计 7 条：`workflow.admin.{create,update,delete,publish,unpublish}` + `workflow.invoke.{start,complete}`
- OTel：`workflow.execute` 顶 span + 每节点 `workflow.step{kind,id,dry_run}`
- mock provider：识别 `E2E_WORKFLOW_V1` marker，emit `tool_call workflow.e2e-demo`
- E2E 步骤 **57-60** 全部接上

## DSL（v1 子集）

5 类叶子/控制节点：

| 节点 kind | 必填 | 行为 |
|----------|------|------|
| `tool` (隐式 — 看到 `use`) | `use`, `args` | bus.Invoke；DryRun + Mutating → mock JSON `{"dry_run":true,"tool":"...","input":...}` |
| `assign` | `assign: {var: expr, ...}` | 表达式求值后写入 vars |
| `if` | `if: expr, then: [...], else?: [...]` | bool 真假分支 |
| `foreach` | `foreach: expr, as: name, steps: [...]` | 对 expr 求值得 list，逐项迭代；每轮 `vars[as] = item` |
| `parallel` | `parallel: [[branch1...], [branch2...]]` | 每分支独立 goroutine；wait-all；任一错则 cancel 其余 |
| `wait` | `wait: <duration>` | time.Sleep；ctx-aware |

修饰字段：`timeout`（默认 60s）、`on_error: fail|continue`。

## 表达式语言

`internal/workflow/expr` 手工实现 ~300 LOC，两个入口：`Resolve(template, scope) (any, error)` 与 `EvalBool(expr, scope) (bool, error)`。路径段：`inputs.foo` / `vars.bar` / `steps.<id>.output[.path...]` / `steps.<id>.error`。

不引 `expr-lang/expr` 因为 v1 用例（lint/test/notify）只用 `== != && || !`；hand-roll 可控 + 错误信息更友好。

## 关键不变量

1. 租户隔离 — workflow 按 tenant_id 隔离；admin 路由强制 `cl.TenantID`
2. published == 在 Bus 中 — startup republish + publish/unpublish/delete 同步维护
3. PUT 后必须重新 publish — `repo.Update` 强制 `published=false`；admin 必须 re-publish
4. Dry-Run 副作用零 — `IsMutating()==true` 的 Tool 在 dry_run 路径下不被 Invoke
5. 嵌套深度上限 — validate 阶段拒绝 > 8；MaxSteps=200 兜底无限循环
6. Parallel vars race — runState 内 sync.RWMutex 防 race；文档明示 parallel 内不要 assign 共享变量
7. 配额自然透传 — workflow 内 `tool` 节点经 bus.Invoke 走原 quota
8. 错误降级而非熔断 — Engine 错误返回 `ExecutionResult{status: failed, error: ...}`；只有 ctx canceled / 写库失败才返回 Go error
9. subflow 自动成立 — `workflow.<slug>` 是 ToolBus 工具 → `tool: use: workflow.<other-slug>` 直接调
10. 重复 slug 报 409 — admin 创建时 UNIQUE 冲突直接映射 HTTP 409

## Verification

- `go test ./internal/workflow/... ./internal/toolbus/... ./internal/agent/... -count=1` 全绿
- `go build ./...` 无 error
- E2E 60/60 PASS

## Acceptance（完成情况）

- [x] 0018 迁移 up/down 干净
- [x] parse + validate 拒绝重复 step id / 嵌套超深 / 未定义节点 / DSL.id 与 slug 不匹配
- [x] `expr.Resolve` 单 `${...}` 保留类型；`expr.EvalBool` 支持 `== != < > <= >= && || !`
- [x] Engine 跑通 tool/assign/if/foreach/parallel/wait + MaxSteps/超时
- [x] Dry-Run mocks fs.write/shell.exec/memory.save/memory.delete/agent.delegate
- [x] POST /admin/workflows + publish → GET /tools 含 workflow.<slug>；unpublish/delete 立即移除
- [x] POST /tools/invoke {tool:"workflow.<slug>"} 正常返回 outputs
- [x] 启动 republish：scan workflows.published=true 全量重注册
- [x] Audit 7 个 action 齐全
- [x] OTel span 含 workflow.execute + workflow.step
- [x] 跨租户隔离 + PUT 强制 unpublish（version+=1）
- [x] E2E 60/60

## 与后续切片的接口

- **Slice 20 Reflection** — workflow_runs `failed` 行是 reflect worker 的素材
- **Slice 21 Orchestration Router** — Slice 19 提供 workflow 列表 + descriptions 给 Router 当候选
- **Slice 23 N8N** — `n8n.<name>` 与 `workflow.<slug>` 平级；Adapter 内部不走本 engine
- **Slice 19b（后续）** — Authoring Agent 用 `workflow-authoring` profile + `llm.chat` 生成 DSL 草案
