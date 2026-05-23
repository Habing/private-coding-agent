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

- **没有 Web UI**：CRUD/发布/调用全靠 REST；Web 前端入口在 Slice 19b
- **NL→DSL 已可用**：Agent 在 `workflow-authoring` 或 `coding` profile 下直接 `tool_call workflow.create` 起草 DSL；publish 仍走 REST
- **没有 versions 表**：单行 + `version int` 单调递增；历史靠 audit + `workflow_runs.version_at_run` 还原
- **没有 `wait_event`**：事件挂起节点要等 Slice 20 Reflection 配套
- **没有 trigger**：cron/webhook/event 触发等 P2
- **没有 step-level trace 落盘**：详情靠 OTel；workflow_runs 不存每节点日志
- **表达式简化**：不支持算术 / 字符串函数 / 嵌套括号；现实负载够用

## 8. NL 建流（Slice 19b，进行中）

B+C 混合：**模板填槽（C）** + **自由 DSL（B）** → Dry-Run → 确认发布。

### 8.1 已落地（Task 1–2，库层，无 HTTP）

| 组件 | 说明 |
|------|------|
| `workflow_proposals` 表 | 迁移 `0024`；草案 + dry_run 快照 + 审批状态 |
| `ProposalService` | `Create` / `CreateFromTemplate` / `Confirm` / `Approve` / `Reject` |
| `internal/workflow/template` | 5 内置模板 + slot 校验 + render + 关键词填槽 |

**流程（程序内）：**

1. `CreateFromTemplate(template_id, slots, slug, …)` 或 `Create(dsl_yaml, …)`
2. 自动写入 draft `workflows` 行并 `Invoke(dry_run=true)`
3. admin `Confirm` → `Publish`；member `Confirm` → `pending_approval` → admin `Approve`

**审计（已实现）：** `workflow.proposal.create` / `workflow.proposal.confirm` / `workflow.proposal.reject`

### 8.2 待做（Task 3+）

- `workflow.propose` / `workflow.publish` 工具与 REST
- Web 对话确认卡片、Orchestrator 规则、E2E 70–75

计划：[`docs/superpowers/plans/2026-05-23-slice-19b-nl-workflow-authoring.md`](superpowers/plans/2026-05-23-slice-19b-nl-workflow-authoring.md)

---

## 参考

- 设计 spec：[`docs/superpowers/specs/2026-05-22-slice-19-workflow-engine-design.md`](superpowers/specs/2026-05-22-slice-19-workflow-engine-design.md)
- 归档 plan：[`docs/superpowers/plans/2026-05-22-slice-19-workflow-engine.md`](superpowers/plans/2026-05-22-slice-19-workflow-engine.md)
- E2E 用例：`deploy/compose/test-e2e.sh` 步骤 57–60
- README Workflow section：[`README.md`](../README.md#workflow-子系统)
