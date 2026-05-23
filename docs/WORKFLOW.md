# Workflow 使用说明

Slice 19a 把高频、步骤稳定的确定性流程从 ReAct 推理里下沉成 **可版本化、可发布、可被 Agent 调用** 的 YAML DSL DAG。发布后 `workflow.<slug>` 自动成为一条 ToolBus 工具——Agent 通过 `tool_call`、用户通过 `POST /tools/invoke`、其他 workflow 通过 `tool: use: workflow.<other>` 都能触发。

适用场景：CI/巡检/发版预检/通知编排等"流程确定、token 浪费在让 LLM 一步步推"的任务。

---

## 1. 一分钟上手

启动 compose 后用 admin JWT 登录：

```bash
TOK=$(curl -s -X POST http://localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"tenant":"default","email":"demo@example.com","password":"demo123"}' \
  | jq -r .token)
```

创建一个最简 workflow：

```bash
curl -X POST http://localhost:8080/admin/workflows \
  -H "Authorization: Bearer $TOK" \
  -H 'Content-Type: application/json' \
  -d '{
    "slug": "hello",
    "name": "Hello",
    "dsl_yaml": "id: hello\nname: Hello\nsteps:\n  - id: greet\n    assign:\n      who: ${inputs.name}\noutputs:\n  msg: hello ${vars.who}"
  }'
```

发布并调用：

```bash
curl -X POST http://localhost:8080/admin/workflows/hello/publish \
  -H "Authorization: Bearer $TOK"

curl -X POST http://localhost:8080/tools/invoke \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"tool":"workflow.hello","input":{"name":"World"}}'
# -> {"output":{"msg":"hello World"}}
```

发布后 `GET /tools` 列表里会出现 `workflow.hello`；任何 Agent 都能通过 `tool_call` 触发它。

---

## 2. DSL 结构

```yaml
id: deploy-check               # 与 URL slug 一致（kebab-case）
name: "Deployment Pre-flight"
version: 1                     # 后端维护，客户端只读
description: "lint + test + notify"

inputs:
  branch:     { type: string, default: "main" }
  commit_sha: { type: string }

steps:
  - id: lint
    use: shell.exec
    args: { command: "make lint" }
    timeout: 30s
    on_error: fail              # fail | continue

  - id: lint_ok
    assign:
      passed: ${steps.lint.output.exit_code == 0}

  - id: gate
    if: ${vars.passed}
    then:
      - id: test
        use: shell.exec
        args: { command: "make test" }
    else:
      - id: notify
        use: llm.chat
        args:
          model: ${inputs.model}
          messages:
            - { role: user, content: "Lint failed: ${steps.lint.output.stderr}" }

  - id: pause
    wait: 100ms

outputs:
  passed:    ${vars.passed}
  test_exit: ${steps.test.output.exit_code}
```

### 6 类节点

| Kind | 触发字段 | 行为 |
|------|----------|------|
| `tool` | `use:` | `bus.Invoke`；DryRun + mutating → 返回 mock JSON |
| `assign` | `assign:` | 求值表达式，写入 `vars` |
| `if` | `if: / then: / else:` | bool 真假分支 |
| `foreach` | `foreach: / as: / steps:` | 对 expr 求值得 list，逐项迭代；`vars[as]=item` |
| `parallel` | `parallel: [[...], [...]]` | 各分支独立 goroutine；wait-all；first-error-cancels-siblings |
| `wait` | `wait: 100ms` | ctx-aware `time.Sleep` |

修饰字段：`timeout`（默认 60s）、`on_error: fail|continue`。

### 表达式

| 语法 | 例子 |
|------|------|
| 引用 input | `${inputs.branch}` |
| 引用 vars | `${vars.passed}` |
| 引用上游 step 输出 | `${steps.lint.output.exit_code}` |
| 引用上游 step 错误 | `${steps.lint.error}` |
| 比较 | `==  !=  <  >  <=  >=` |
| 逻辑 | `&&  ||  !` |

单 `${expr}` 保留底层类型（int/bool/map/list 都行）；多段拼接走 `fmt.Sprint`。不支持算术、字符串函数、嵌套括号——v1 够用，后续需要会替换求值器但 DSL 不变。

---

## 3. Admin REST API

所有 `/admin/workflows*` 路由要求 `role=admin`，强制按 `cl.TenantID` 隔离（同 slug 不同租户互不可见）。

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/admin/workflows` | 创建（`published=false`，version=1） |
| `GET` | `/admin/workflows` | 列出（不返 `dsl_yaml`） |
| `GET` | `/admin/workflows/:slug` | 详情（含 `dsl_yaml`） |
| `PUT` | `/admin/workflows/:slug` | 更新；`version+=1`；**强制 `published=false`** |
| `DELETE` | `/admin/workflows/:slug` | 已发布则同步 `bus.Unregister` |
| `POST` | `/admin/workflows/:slug/publish` | 校验 + `Bus.Register("workflow.<slug>")` |
| `POST` | `/admin/workflows/:slug/unpublish` | `Bus.Unregister` |
| `POST` | `/admin/workflows/:slug/invoke` | body `{inputs, dry_run?}` |
| `GET` | `/admin/workflows/:slug/runs` | 最近 N 次 run |
| `POST` | `/admin/workflows/graph-preview` | body `{dsl_yaml}` → 只读 Graph JSON（Slice 19d） |
| `GET` | `/admin/workflows/:slug/graph` | 已保存 workflow 的流程图 |

Agent（member+）：`GET /agent/workflow/proposals/:id/graph` — proposal 卡片迷你图。详见 §9。

错误码：slug 冲突 `409`、未找到 `404`、DSL 校验失败 `400`。

`POST /admin/workflows/:slug/invoke` 与 `/tools/invoke {tool:"workflow.<slug>"}` 的区别只有 `dry_run` 是否可用——两者都走同一条 `Service.Invoke`，都写 `workflow_runs`、都发 `workflow.invoke.{start,complete}` 审计。

---

## 4. Agent 自然语言触发

发布后 `workflow.<slug>` 在 ToolBus 全局可见，Agent 在 ReAct 循环里能直接 `tool_call`：

```bash
curl -X POST http://localhost:8080/agent/run \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{
    "model": "default-mock:text",
    "messages": [
      {"role":"user","content":"请跑一下 hello workflow"}
    ]
  }'
```

事件流里会看到 `tool_call workflow.hello → tool_result → final`。`agent.delegate` 也认这套——子 Run 同样能 `tool_call workflow.<slug>`。

### subflow（workflow 调 workflow）

不需要专门节点 kind——`workflow.<other>` 本身就是 Bus 工具：

```yaml
steps:
  - id: sub
    use: workflow.other-slug
    args:
      param1: ${inputs.x}
```

嵌套深度上限 8 层（`MaxNestingDepth`），且每层都过 quota 中间件。

---

## 5. Dry-Run

`dry_run=true` 时，标了 `IsMutating()=true` 的工具节点 **不会被实际调用**，引擎返回固定结构的 mock：

```json
{ "dry_run": true, "tool": "shell.exec", "input": { "command": "rm -rf /" } }
```

当前标 mutating 的工具：`fs.write` / `shell.exec` / `memory.save` / `memory.delete` / `agent.delegate`。其余（`fs.read/list/glob` / `grep` / `llm.chat` / `llm.embed` / `memory.list` / `memory.search`）正常执行——Dry-Run 主要是让你看清流程会走哪条分支、调用哪些副作用 tool，而不破坏沙箱/记忆/外部状态。

调用：

```bash
curl -X POST http://localhost:8080/admin/workflows/hello/invoke \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"inputs":{"name":"World"}, "dry_run": true}'
```

`workflow_runs.dry_run=true` 的行落表但不动副作用。

---

## 6. 运维与不变量

| 项 | 值 |
|----|-----|
| `MaxSteps` | 200（叶子节点累计，超出 → `status=max_steps`） |
| `MaxParallelFanout` | 8（单 parallel 分支数上限） |
| `MaxNestingDepth` | 8（foreach/if/parallel 总嵌套深度） |
| `DefaultStepTimeout` | 60s（单 tool 节点） |

**关键不变量：**

1. **published == 在 Bus 中**——`Service.RepublishAll` 在 server 启动时把所有 `published=true` 的行重注册回 Bus；重启不丢工具
2. **PUT 后强制 unpublish**——避免静默替换已发布版本；admin 必须显式 re-publish
3. **跨租户隔离**——`workflow.<slug>` 在 Bus 中是全局名字，但 `WorkflowTool.Invoke` 在 boundary 拒绝跨租户调用
4. **配额自然透传**——workflow 内 `tool` 节点走 `bus.Invoke`，原 `quota.tool_invoke_per_minute` 中间件自动生效；嵌套 workflow 链式调用不会绕开 quota
5. **错误降级而非熔断**——Engine 错误返回 `ExecutionResult{status: failed, error: "..."}`；只有 ctx canceled / 写库失败这类系统错才返回 Go error

### 审计

| Action | 说明 |
|--------|------|
| `workflow.admin.create` / `update` / `delete` / `publish` / `unpublish` | admin CRUD + 发布动作 |
| `workflow.invoke.start` / `complete` | 每次 invoke 一对（含 `run_id`、`dry_run`、`status`、`steps`、`duration_ms`） |

可在 `/audit?action=workflow.` 前缀查询。

### 可观测

OTel:

- 顶 span：`workflow.execute`（`workflow.slug`、`workflow.dry_run`）
- 子 span：`workflow.step{id, kind, dry_run}` 每节点一个

Prometheus：workflow 内的 tool 节点照旧打 `pca_tool_invocations_total{tool, outcome}` + `pca_tool_invocation_duration_seconds{tool}`，没有 workflow 专属计数器（v1 留作 backlog）。

### `workflow_runs` 表

每次 invoke 写一行，包含 `inputs_json` / `outputs_json` / `status` / `duration_ms` / `dry_run`。**Compose 试点（2026-05-23）** 已接入自动 retention：`workflow.runs_retention_days` 默认 90 天，启动时 purge 一次 + 后台 daily ticker（见 [`docs/P2-COMPOSE-PILOT.md`](P2-COMPOSE-PILOT.md) #14）。设为 `0` 禁用。`GET /admin/workflows/:slug/runs` 默认返回最近 N 条。

---

## 6.5 Agent 直接起草 / 修改 Workflow

ToolBus 提供 4 个 admin-only 工具供 Agent 在会话里写 DSL 草稿，**publish / delete / invoke 仍只在 admin REST 上**——这是有意的安全边界，让人留在 loop 里做"上线"动作。

| 工具 | mutating | 作用 |
|------|----------|------|
| `workflow.create` | ✅ | 起草新 workflow（slug + name + dsl_yaml）；`published=false` |
| `workflow.update` | ✅ | 改名/描述/DSL；`version+=1`；如已发布则**强制 unpublish**，返回 `requires_publish=true` 提示 |
| `workflow.list` | — | 列出当前租户 workflows（不含 DSL 主体） |
| `workflow.get` | — | 取单条 workflow 含 DSL 主体（改之前先 get） |

**鉴权**：每个工具自己再校验一遍 `auth.FromCtx(ctx).Role == "admin"` 且 `tenant 与 invocation 一致`，非 admin 会拿到 `{ok:false, error:"permission_denied"}`（envelope，不抛 Go error），LLM 能基于这个 string code 决定下一步。

**Profile 配套**：`coding` 与 `workflow-authoring` 两个 profile 的白名单已经加上这 4 个工具。普通用户即使把 LLM 模型骗去调 workflow.create，也会被工具内的 admin 检查挡住。

**典型对话**：

> User（admin）："帮我建一个叫 `lint-check` 的 workflow，跑 `make lint` 然后把 exit_code 写到 outputs 里。"
> 
> Agent: `tool_call workflow.list` → 看现有 workflow → 写 DSL → `tool_call workflow.create {slug: lint-check, name: "Lint Check", dsl_yaml: ...}` → 拿到 `{ok:true, version:1, published:false}` → 告诉用户："草稿已建好；请在 admin REST 上 `POST /admin/workflows/lint-check/publish` 来上线。"

**Dry-Run 兼容**：workflow.create / update 标 `IsMutating()=true`，所以一个 workflow 里如果通过子 workflow 间接调它们（极少见，但理论上可以），dry-run 路径会拦截返回 mock JSON 而不真写库。

**审计**：4 个 admin 工具的成功调用走 Service.Create/Update，最终落 `workflow.admin.create` / `workflow.admin.update` 审计事件——与 REST 入口共用一条审计路径。

## 7. 限制（v1 不做）

- **Web UI（admin）**：`/workflows` CRUD + 流程图只读预览（Slice 19d）+ `/toolbox` 工具浏览（Slice 19b Web UI）；NL 建流对话卡片见 §8
- **NL→DSL**：`workflow.propose` / `workflow.create` + 模板 catalog（Slice 19b NL）；publish 走 confirm / `workflow.publish`
- **没有 versions 表**：单行 + `version int` 单调递增；历史靠 audit + `workflow_runs.version_at_run` 还原
- **没有 `wait_event`**：事件挂起节点要等 Slice 20 Reflection 配套
- **没有 trigger（v1）**：cron/webhook 见 **Slice 24**（[`§10`](#10-触发器-slice-24-进行中)）；event 触发仍 P2
- **没有 step-level trace 落盘**：详情靠 OTel；workflow_runs 不存每节点日志
- **表达式简化**：不支持算术 / 字符串函数 / 嵌套括号；现实负载够用

## 8. NL 建流（Slice 19b NL Authoring ✅）

B+C 混合：**模板填槽（C）** + **自由 DSL（B）** → Dry-Run → 对话/REST 确认发布。

计划：[`docs/superpowers/plans/2026-05-23-slice-19b-nl-workflow-authoring.md`](superpowers/plans/2026-05-23-slice-19b-nl-workflow-authoring.md)

### 8.1 数据与模板层

| 组件 | 说明 |
|------|------|
| `workflow_proposals` 表 | 迁移 `0024`；草案 + dry_run 快照 + 审批状态 |
| `ProposalService` | `Create` / `CreateFromTemplate` / `Confirm` / `Approve` / `Reject` |
| `internal/workflow/template` | 5 内置模板 + slot 校验 + render + 关键词填槽（`ClassifyAndExtract`） |

**流程：**

1. `CreateFromTemplate(template_id, slots, slug, …)` 或 `Create(dsl_yaml, …)` 或 REST `user_message` 自动 classify
2. 自动写入 draft `workflows` 行并 `Invoke(dry_run=true)`
3. admin `Confirm` → `Publish`；member `Confirm` → `pending_approval` → admin `Approve`

**审计：** `workflow.proposal.create` / `workflow.proposal.confirm` / `workflow.proposal.reject`

### 8.2 HTTP + ToolBus

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/agent/workflow/templates` | 5 个内置模板 catalog |
| POST | `/agent/workflow/proposals` | 创建 proposal + 自动 dry_run |
| GET | `/agent/workflow/proposals/:id` | 详情 |
| POST | `/agent/workflow/proposals/:id/confirm` | admin 发布 / member 提交审批 |
| POST | `/admin/workflow/proposals/:id/approve` | admin 批准 |
| POST | `/admin/workflow/proposals/:id/reject` | admin 拒绝 |

**ToolBus：** `workflow.propose`（登录用户）、`workflow.publish`（仅 admin）

**Profile：** `coding` 含 propose+publish + delegate；`workflow-authoring` 含 propose（无 publish）

**Skill：** `workflow-template-authoring`（Path C 填槽规则）；复杂 DSL 走 `workflow-dsl-authoring` + Path B

### 8.3 Orchestrator + Web UI

| 组件 | 说明 |
|------|------|
| `nl-workflow-author` 规则 | `config.example.yaml`；识别「建自动化/工作流」意图 → hint 建议 `workflow.propose` 或 delegate `workflow-authoring` |
| `WorkflowProposalCard` | 聊天页解析 `workflow.propose` 的 `tool_result`，展示 Dry-Run 结果；admin「确认发布」/ member「提交审批」 |
| 审批 UI | 无独立 admin 列表页（同 Reflection 的 `/admin/memory-proposals` 可后续补）；当前 REST + 对话卡片 |

### 8.4 E2E（compose 步骤 70–75）

| 步 | 场景 |
|----|------|
| 70 | templates ≥5 + `E2E_NL_WF_AUTHOR_V1` orchestrator hint |
| 71 | template `llm-summarize-notify` → dry_run_ok |
| 72 | admin confirm → `workflow.e2e-nl-template` 进 `/tools` |
| 73 | member confirm → pending → admin approve |
| 74 | `E2E_WF_PROPOSAL_V1` → `workflow.propose` tool_call |
| 75 | `E2E_WF_FREEFORM_V1` → freeform DSL propose（mock 短路 Path B） |

### 8.5 已知限制 / 后续

- 无 cron/webhook **triggers**（→ Slice 24 §10）
- 模板 notify 占位 `llm.chat`；Slack 等连接器（Slice 25）
- template classify v1 为关键词规则；embedding 分类推 P2
- 无 `workflow_proposal` 专用 SSE 事件（Web UI 解析 `tool_result`）
- Helm values 未同步 `nl-workflow-author` 规则（compose 镜像内置 `config.example.yaml`）
- 只读流程图见 **Slice 19d**（[`WORKFLOW.md`](../../WORKFLOW.md) §9）；**19c** 仍为可选模板市场

---

## 9. 只读流程图（Slice 19d Visualization ✅）

DSL 解析为 Graph IR，在管理页与 proposal 卡片中以 **只读** React Flow 流程图展示（非可视化编辑器）。

计划：[`docs/superpowers/plans/2026-05-24-slice-19d-workflow-visualization.md`](superpowers/plans/2026-05-24-slice-19d-workflow-visualization.md)

### 9.1 API

| 方法 | 路径 | 鉴权 | 说明 |
|------|------|------|------|
| POST | `/admin/workflows/graph-preview` | admin | body `{ "dsl_yaml": "..." }` → Graph JSON |
| GET | `/admin/workflows/:slug/graph` | admin | 已保存 workflow 的图 |
| GET | `/agent/workflow/proposals/:id/graph` | member+ | NL 草案卡片迷你图 |

Parse 失败 → 400 `{ "error": "parse", "detail": "..." }`；graph-preview 空 DSL → 400 `dsl_required`。

### 9.2 Web UI

| 位置 | 行为 |
|------|------|
| `/workflows` 编辑展开 | 三栏：YAML（Monaco）\| **流程图预览** \| 名称/描述/invoke/runs；DSL 变更防抖 ~400ms 调 graph-preview |
| 聊天 `WorkflowProposalCard` | `dry_run_ok` 时加载 proposal graph，220px 紧凑图（无 Controls/MiniMap） |

节点按 kind 着色（tool/assign/if/foreach/parallel/wait/start/end）；分支边橙色、并行边动画。

### 9.3 手工冒烟

```bash
TOK=$(curl -s -X POST http://localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"tenant":"default","email":"demo@example.com","password":"demo123"}' \
  | jq -r .token)

curl -s -X POST http://localhost:8080/admin/workflows/graph-preview \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"dsl_yaml":"id: x\nname: X\nsteps:\n  - id: a\n    assign:\n      v: \"1\"\n"}' \
  | jq '.nodes | length'   # 期望 ≥3（start + a + end）
```

---

## 10. 触发器（Slice 24 🚧）

> **状态：** spec/plan 已批准，实现进行中。设计：[`2026-05-24-slice-24-workflow-triggers-design.md`](superpowers/specs/2026-05-24-slice-24-workflow-triggers-design.md)

### 10.1 DSL（计划）

```yaml
triggers:
  - id: weekday-morning
    cron: "0 9 * * 1-5"
    timezone: UTC
    inputs:
      channel: team
  - id: inbound-hook
    webhook: {}
    inputs:
      payload: {}
```

### 10.2 计划 API

| 方法 | 路径 | 说明 |
|------|------|------|
| — | `POST /hooks/workflow/:token` | Webhook 触发（无 JWT） |
| GET | `/admin/workflows/:slug/triggers` | 列表 + webhook URL |
| POST | `/admin/workflows/:slug/triggers/:id/run` | 手动触发（调试） |

Publish 时从 DSL 同步 `workflow_triggers` 表；unpublish 禁用触发器。

### 10.3 E2E

compose 步骤 **76–78**（见 plan Task 9）。

---

## 参考

- 设计 spec：[`docs/superpowers/specs/2026-05-22-slice-19-workflow-engine-design.md`](superpowers/specs/2026-05-22-slice-19-workflow-engine-design.md)
- 归档 plan：[`docs/superpowers/plans/2026-05-22-slice-19-workflow-engine.md`](superpowers/plans/2026-05-22-slice-19-workflow-engine.md)
- E2E 用例：`deploy/compose/test-e2e.sh` 步骤 57–60（19a Engine）、**70–75**（19b NL Authoring）；19d 无增量 E2E（L1/L2 + 手工 §9.3）
- README Workflow section：[`README.md`](../README.md#workflow-子系统)
