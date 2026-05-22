# Slice 19 — Workflow Engine 设计 Spec

> Status: Draft — 待评审
> Author: planning session (2026-05-22)
> Related: 主 spec §6 / §11（ADR-3, ADR-4）、Full-P1 spec §19、Slice 4（Tool Bus）、Slice 18（Sub-Agents）
> Implementation plan: `docs/superpowers/plans/2026-05-22-slice-19-workflow-engine.md`
> 范围：**仅 19a（Engine）**。19b（Authoring Agent / NL→DSL / 可视化 UI）推后

## 1. 概述

P0/MVP-P1 把所有任务都交给 LLM ReAct 一步步推。对探索性任务合适；但对**步骤稳定、可重复**的流程（发布前 lint+test、巡检、数据搬移），让模型每次重新想"先 lint 还是先 test"是浪费 token + 引入不稳定性。

Slice 19 引入 **Workflow** — 把确定性流程沉淀为 YAML DSL 描述的 DAG，发布后**自身成为一个 ToolBus 工具**（`workflow.<slug>`），Agent 可像调任何普通工具一样调用它，单步即触发整段执行。

**核心叙事**

- **DSL 即 DAG**：YAML + JSON Schema；5 类节点：`tool` / `assign` / `if` / `foreach` / `parallel` / `wait`
- **发布即工具（ADR-4 兑现）**：published workflow 自动注册到 ToolBus；启动期 republish 保证 DB 与 Bus 一致
- **控制流留在 Engine（ADR-3 兑现）**：if/foreach/parallel/wait 是 Engine 原语，不是 MCP 工具
- **Dry-Run = 副作用零**：mutating 工具在 `dry_run=true` 时返回 mock JSON；通过可选 `Mutating` 接口标记
- **Subflow 自动成立**：`tool: use: workflow.<other-slug>` 即可；不需新增节点 kind

**明确不在本切片范围**

- Authoring Agent / NL→DSL / Web 列表 / 发布按钮（19b）
- 触发器（cron / webhook / event）；`wait_event` 事件挂起节点（P2，需 Reflection 配套）
- 版本历史表（用 `version int` 单调递增 + 单行替换，简化 v1）
- Fork / 可见范围（private/project/tenant）
- 步骤级 trace 落 DB（走 OTel；Slice 22 持久化）

---

## 2. 前置条件

- Slice 4 已交付（Tool Bus + `tool_invocations`）
- Slice 17 已交付（admin handler 范式 + audit.Sink 接线）
- Slice 18 已交付（Profile 注册表；workflow-authoring profile 已占位）
- ToolBus 当前缺 `Unregister` 与 `IsMutating` — 本切片补齐

---

## 3. 关键设计决策（ADR）

### ADR-69 — v1 不分 `versions` 表

`workflows` 单行最新态 + `version INT` 单调递增。

- **理由**：v1 没有"回滚到 v3"的产品诉求；客户端拿不到旧 DSL 直接降低系统复杂度
- **历史还原**：`workflow_runs.version_at_run` 记录每次执行版本；audit `workflow.admin.update/publish` 链可恢复"何时升 v"
- **替代方案**（舍弃）：独立 `workflow_versions` 表存所有历史 — 查询路径变两跳、ToolBus 注册需绑定版本号、迁移代价高、且 v1 没人会回滚

### ADR-70 — 表达式 hand-roll 而非引入 `expr-lang/expr`

200 LOC 实现 `Resolve(template, scope) any` + `EvalBool(expr, scope) bool`。

- **理由**：新增直接依赖需评审；v1 表达足够覆盖 lint/test/notify 类用例（`==`/`!=`/`<>` + `&&`/`||`/`!` + 路径解析）；hand-roll 错误信息更友好、可 fuzz
- **升级路径**：Slice 20+ 想要算术/函数时可换 expr 库，DSL 字段保持不变（仅评估实现替换）

### ADR-71 — `IsMutating()` 走可选接口；默认 non-mutating

```go
type Mutating interface { IsMutating() bool }
```

不实现该接口的 Tool 默认 `false`。

- **理由**：现有 12 个工具全部不需要改 — 不破坏 ABI；只 5 个 mutating 工具实现该方法
- **替代方案**（舍弃）：必填方法 — 12 个工具都要加 1 行 boilerplate；外部 MCP（Slice 21）适配器更难统一

### ADR-72 — Workflow 即 Tool（ADR-4 兑现）

发布 → `workflow.<slug>` 注册到 ToolBus；subflow 通过 `tool: use: workflow.<other>` 自然继承，不需要专门节点 kind。

- **嵌套调用配额自然透传**：每层 `tool` 节点都过 `Bus.Invoke` → 原 quota 中间件兜底
- **递归限深**：靠 ToolBus.Invoke 每次记一笔 `tool_invocations` + Engine.MaxNestingDepth=8 双保险

### ADR-73 — 控制流留在 Engine（ADR-3 兑现），不入 Tool Bus

`if / foreach / parallel / wait` 是 Engine 原语；不暴露为 MCP 工具。

- **理由**：控制流接 ctx / 步数计数 / vars 作用域；做成工具会泄漏 Engine 状态、且 LLM 也不该通过 tool 调控制流
- **代价**：外部系统（n8n / Argo）想复用 Workflow 引擎需要 adapter — 接受；Slice 23 N8N 是平级集成，不复用我们的引擎

### ADR-74 — Startup republish

Server 启动 → `repo.ListPublished(ctx)` → 全量重注册到 ToolBus。

- **理由**：DB 是事实来源；ToolBus 是 in-memory；不 republish 则重启后 `workflow.<slug>` 从 `GET /tools` 消失
- **失败降级**：单条 DSL validate 失败 → `log.Warn` 跳过该条 + audit `workflow.republish.skipped`；其余继续；server 仍能起
- **替代方案**（舍弃）：lazy load（首次 invoke 时拉 DB） — 列工具时少一条候选；Agent 看不见就不会选；产品语义差

### ADR-75 — Dry-Run mocks 所有 `IsMutating()==true` 工具；保守标记

5 个工具标 mutating：`fs.write` / `shell.exec` / `memory.save` / `memory.delete` / `agent.delegate`。

`agent.delegate` 保守标 true：子 Run 可能调用任何工具（含其他 mutating），dry-run 整段 mock 安全。

- mock 返回固定结构：`{"dry_run":true,"tool":"<name>","input":<args>}`
- 工具 schema 不变；调用方拿到的 JSON 类型仍合法（DSL 后续 `${steps.X.output}` 解引用不会爆栈，但语义可能不对 — 这是 dry-run 的预期）

### ADR-76 — 三层兜底：步骤超时 + MaxSteps + MaxNestingDepth

| 维度 | 缺省 | 触发后 |
|------|------|--------|
| 单步超时 | `DefaultStepTimeout=60s`，DSL 可覆盖 `timeout: 30s` | step error；`on_error: fail` 终止；`continue` 标 error 继续 |
| 总步数 | `MaxSteps=200`（叶子节点累计） | status=`max_steps`；Engine 立即返回当前 outputs |
| 嵌套深度 | `MaxNestingDepth=8`（foreach/if/parallel 嵌套） | validate 阶段拒绝（不到执行期） |
| Parallel 分支 | `MaxParallelFanout=8` | validate 阶段拒绝 |

- 三层兜底覆盖：(1) 单节点卡死 → 超时；(2) 死循环／无意义大 foreach → MaxSteps；(3) DSL 写出来病态 → 嵌套校验
- 全部硬编码默认 + DSL 可在节点级覆盖；不暴露 config（v1）

---

## 4. DSL v1（5 节点 + 2 修饰字段）

完整示例：

```yaml
id: deploy-check
name: "Deployment Pre-flight"
version: 1
description: "Lint + test gate before rolling deploy"
inputs:
  branch:     { type: string, default: "main" }
  commit_sha: { type: string }
steps:
  - id: lint
    use: shell.exec
    args: { command: "make lint" }
    timeout: 30s
    on_error: fail

  - id: store
    assign:
      lint_ok: ${steps.lint.output.exit_code == 0}

  - id: gate
    if: ${vars.lint_ok}
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
            - role: user
              content: "Lint failed: ${steps.lint.output.stderr}"

  - id: probe
    parallel:
      - - id: p1
          use: shell.exec
          args: { command: "echo a" }
      - - id: p2
          use: shell.exec
          args: { command: "echo b" }

  - id: each
    foreach: ${inputs.targets}
    as: tgt
    steps:
      - id: copy
        use: fs.write
        args:
          path: "/out/${vars.tgt}.txt"
          content: "done"

  - id: pause
    wait: 100ms

outputs:
  passed:    ${vars.lint_ok}
  test_exit: ${steps.test.output.exit_code}
```

| Kind（推断） | 必填字段 | 行为 |
|--------------|---------|------|
| `tool`（看到 `use`） | `use`, `args` | bus.Invoke；DryRun + Mutating → mock JSON |
| `assign` | `assign: {var: expr, ...}` | 表达式求值后写 vars |
| `if` | `if: expr, then: [...], else?: [...]` | bool 真假分支 |
| `foreach` | `foreach: expr, as: name, steps: [...]` | 对 list 迭代；每轮 `vars[as] = item` |
| `parallel` | `parallel: [[branch1...], [branch2...]]` | errgroup + ctx；首错取消兄弟 |
| `wait` | `wait: <duration>` | ctx-aware `time.Sleep` |

修饰字段：`timeout`（仅 tool）/ `on_error: fail|continue`（仅 tool；v1 仅这两个）。

---

## 5. 表达式语言（最小子集）

`internal/workflow/expr/expr.go`：

```go
func Resolve(template string, scope Scope) (any, error)
func EvalBool(expr string, scope Scope) (bool, error)
type Scope struct {
    Inputs map[string]any
    Vars   map[string]any
    Steps  map[string]StepResult  // {Output any, Error string}
}
```

`Resolve` 规则：
- 整串 `${expr}` → 返回底层值（保留类型，可为 map/list/int/bool/string）
- 含 ${} 段的字符串 → 每段 `fmt.Sprint(value)` 拼接

`EvalBool` 支持：
- `<path>`（truthy：bool 直接；string 非空；数 ≠0；nil/缺失 false）
- `<path> == <literal>` / `!=`（literal: string/int/float/bool）
- `<path> < N` / `<=` / `>` / `>=`（数值）
- `<lhs> && <rhs>` / `||`（短路）
- `! <path>`

不支持：嵌套括号、函数调用、算术 — v1 够用。

路径段：`inputs.foo` / `vars.bar` / `steps.<id>.output[.path...]` / `steps.<id>.error`。

---

## 6. 数据模型（迁移 0018）

```sql
CREATE TABLE workflows (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  slug         TEXT NOT NULL,
  name         TEXT NOT NULL,
  description  TEXT NOT NULL DEFAULT '',
  dsl_yaml     TEXT NOT NULL,
  version      INT  NOT NULL DEFAULT 1,
  published    BOOL NOT NULL DEFAULT FALSE,
  published_at TIMESTAMPTZ,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(tenant_id, slug)
);
CREATE INDEX workflows_tenant_published_idx ON workflows(tenant_id, published);

CREATE TABLE workflow_runs (
  id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id      UUID NOT NULL,
  user_id        UUID NOT NULL,
  workflow_id    UUID NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
  version_at_run INT  NOT NULL,
  dry_run        BOOL NOT NULL DEFAULT FALSE,
  status         TEXT NOT NULL,
  inputs_json    JSONB NOT NULL DEFAULT '{}'::jsonb,
  outputs_json   JSONB,
  error_text     TEXT,
  duration_ms    INT,
  started_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  finished_at    TIMESTAMPTZ
);
CREATE INDEX workflow_runs_workflow_idx ON workflow_runs(workflow_id, started_at DESC);
CREATE INDEX workflow_runs_tenant_user_idx ON workflow_runs(tenant_id, user_id, started_at DESC);
```

---

## 7. HTTP 表面

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | /admin/workflows | 创建（published=false） |
| GET | /admin/workflows | 列表（默认省略 dsl_yaml；`?include=dsl`） |
| GET | /admin/workflows/:slug | 详情含 DSL |
| PUT | /admin/workflows/:slug | 更新 DSL；version+=1；强制 published=false |
| DELETE | /admin/workflows/:slug | bus.Unregister + 删行 |
| POST | /admin/workflows/:slug/publish | 校验 + bus.Register |
| POST | /admin/workflows/:slug/unpublish | bus.Unregister + published=false |
| POST | /admin/workflows/:slug/invoke | 直跑 Engine；支持 `dry_run` |
| GET | /admin/workflows/:slug/runs | 最近 50 条 |

全部走 `auth.RequireAdmin`；同 Slice 17 `/admin/skills` 范式。

普通用户通过 `POST /tools/invoke {tool: "workflow.<slug>", input: {...}}` 调用已发布 workflow，走 ToolBus 通道；永远真跑（不支持 dry_run）。

---

## 8. 审计事件（7 个 action）

| Action | Target | Metadata |
|--------|--------|----------|
| `workflow.admin.create` | slug | `tenant_id, version=1` |
| `workflow.admin.update` | slug | `version_new` |
| `workflow.admin.delete` | slug | `was_published` |
| `workflow.admin.publish` | slug | `version` |
| `workflow.admin.unpublish` | slug | `version` |
| `workflow.invoke.start` | slug | `run_id, dry_run, inputs_keys` |
| `workflow.invoke.complete` | slug | `run_id, status, steps, duration_ms, dry_run` |

`invoke.start/complete` 在两条调用路径（admin invoke + bus）共用一对。

---

## 9. OTel

- `workflow.execute` 顶 span：`workflow.slug`, `workflow.version`, `workflow.dry_run`, `workflow.status`
- `workflow.step` 子 span（每节点）：`workflow.step.id`, `workflow.step.kind`, `workflow.step.dry_run`
- `tool.invoke` span 在 tool 节点路径下自然嵌套

---

## 10. 关键不变量

1. **租户隔离** — workflow 按 tenant_id 隔离；admin 路由强制 `cl.TenantID`；同 slug 不同租户互不可见
2. **`published == 在 Bus 中`** — startup republish + publish/unpublish/delete 同步维护
3. **PUT 后必须重新 publish** — 防止已发布版本被静默替换；audit 留痕
4. **Dry-Run 副作用零** — 凡 `IsMutating()==true` 的 Tool 在 dry_run 路径下不被 Invoke
5. **嵌套深度 ≤ 8 + MaxSteps 200** — validate + 运行时双重兜底
6. **Parallel 内 assign 数据竞争** — vars 加 RWMutex；文档明示不要在 parallel 内 assign 共享变量
7. **配额自然透传** — workflow 内 `tool` 节点经 bus.Invoke 走原 quota；嵌套 workflow 不会绕过
8. **错误降级而非熔断** — Engine 错误返回 `ExecutionResult{status: failed, error: ...}`；仅 ctx canceled / repo 写失败这类系统错才返回 Go error
9. **重复 slug → 409** — admin 创建唯一约束冲突直接映射 HTTP 409
10. **删除已发布 workflow 不打扰活跃 Run** — 当前 Engine.Execute 已持有 DSL 副本

---

## 11. 与后续切片的接口

- **Slice 20 Reflection** — `workflow_runs` 中 `status=failed` 的 run 是 lesson memory 素材
- **Slice 21 Orchestration Router** — 主 Agent 根据 intent 选 workflow vs delegate vs 直 tool；本切片提供候选列表 + description
- **Slice 23 N8N** — N8N workflows 通过 adapter 注册成 `n8n.<name>`；与 `workflow.<slug>` 平级
- **Slice 19b（后续）** — Authoring Agent 用 `workflow-authoring` profile + `llm.chat` 生成 DSL 草案；NL→DSL 端点

---

## 12. 风险

1. **表达式语言够不够** — v1 没有算术/字符串函数；validate 对 unknown operator 报错而非静默忽略；复杂场景可在 `llm.chat` 节点兜底
2. **published 双源同步** — startup republish + publish 写库 + bus.Register 三处需一致；publish 路径 transaction-like 顺序：DB 先 → Bus 后；失败回滚 published=false
3. **Parallel race** — vars RWMutex 保读写安全；不保证 assign 同一 key 的顺序 — 文档明示
4. **workflow_runs 大表** — 每次 invoke 一行；list 默认不返 outputs；retention 后续加 job
5. **agent.delegate IsMutating=true 影响** — 只有 workflow engine 在 dry-run 时检查；Slice 18 行为不变
