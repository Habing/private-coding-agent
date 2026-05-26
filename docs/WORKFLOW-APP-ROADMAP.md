# 路线 2：工作流应用路线图

> **上级方案**：[`MCP-WORKFLOW-PLATFORM-PLAN.md`](MCP-WORKFLOW-PLATFORM-PLAN.md)  
> **能力依赖**：路线 1 已注册的 `mcp.<域>.<tool>`  
> **技术手册**：[`WORKFLOW.md`](WORKFLOW.md) · NL 切片 [`superpowers/plans/2026-05-23-slice-19b-nl-workflow-authoring.md`](superpowers/plans/2026-05-23-slice-19b-nl-workflow-authoring.md)

---

## 1. 目标

把 **NL 下 ReAct 才能完成的重复编排**，固化为 **可发布、可复用、可调度** 的 Agent 应用：

```text
NL（建流） → DSL 节点（主要 use: mcp.*） → Dry-Run → 发布 workflow.<app>
运行：cron / webhook / 对话「跑一下 <app>」→ 不再长链 ReAct
```

**核心假设**：MCP 工具库越完善（按域、schema 稳），可生成的企业工作流越复杂，且 **运行期确定性越高**。

---

## 2. 节点模型（ReAct 拆分规则）

| ReAct 中的行为 | 工作流中的表达 |
|----------------|----------------|
| 调一个外部 API | `use: mcp.<域>.<tool>` + `args` |
| 拼变量 / 简单逻辑 | `assign:` + `${steps.*.output}` |
| 分支 | `if:` / `then:` / `else:` |
| 批量 | `foreach:` |
| 并行 | `parallel:` |
| 等待 | `wait:` |
| 调 LLM 摘要/生成文案 | `use: llm.chat`（尽量少用，优先 MCP 封装） |
| 子流程 | `use: workflow.<other_slug>` |
| 探索、一次性试错 | **留在对话 ReAct**，不强行进 DSL |

**粒度建议**：

- 一个 MCP tool 已封装好的能力 → **优先 1 节点**
- 需要组合、分支 → **多节点**
- 跨域 → 多 `mcp.*` 或子 workflow

---

## 3. NL 建流路径（PCA 已交付）

```text
用户（coding profile）
  │
  ├─ orchestrator: nl-workflow-author → hint
  │
  ├─ Path C（优先）workflow.propose { template_id, slots }
  │     slots 含 notify_tool / steps → 渲染 DSL（可填 mcp.*）
  │
  ├─ Path B（兜底）agent.delegate(workflow-authoring)
  │     → workflow.create → workflow.propose { dsl_yaml }
  │
  ▼
WorkflowProposalCard（Dry-Run 结果）
  ├─ admin: confirm → 直接 publish
  └─ member: confirm → pending_approval → admin approve
```

| 路径 | 何时用 |
|------|--------|
| **C 模板** | 需求接近内置模板（通知、HTTP 拉取、tool-chain） |
| **B 自由** | 多分支、跨域、复杂 `if` / `parallel` |
| **管理页** | 专家改 YAML、看流程图（迭代 1：概览 + 专家模式） |

内置模板（`internal/workflow/template/catalog.go`）的 `SuggestedTools` **应逐步替换**为试点域真实 `mcp.*`，减少 `llm.chat` 占位。

---

## 4. DSL 示例（多 MCP 节点）

```yaml
id: order-sync-app
name: 订单同步应用
description: 工作日拉取待同步订单，校验后写入 ERP 并通知

triggers:
  - id: weekday-morning
    cron: "0 9 * * 1-5"
    timezone: Asia/Shanghai
    inputs:
      channel: ops

inputs:
  batch_limit: { type: integer, default: 100 }

steps:
  - id: list
    use: mcp.orders.list_pending
    args:
      limit: ${inputs.batch_limit}
      status: pending

  - id: check
    assign:
      count: ${steps.list.output.count}
      has_work: ${steps.list.output.count > 0}

  - id: gate
    if: ${vars.has_work}
    then:
      - id: validate
        use: mcp.orders.validate
        args:
          items: ${steps.list.output.items}
      - id: sync
        use: mcp.orders.upsert_erp
        args:
          items: ${steps.validate.output.accepted}
      - id: notify
        use: mcp.notify.send_slack
        args:
          channel: ${inputs.channel}
          text: "同步完成：${steps.sync.output.synced} 条"
    else:
      - id: skip_notify
        use: mcp.notify.send_slack
        args:
          channel: ${inputs.channel}
          text: "无待同步订单"

outputs:
  synced: ${steps.sync.output.synced}
  skipped: ${vars.has_work == false}
```

> `mcp.orders.*` / `mcp.notify.*` 须在路线 1 注册后再 propose；否则 Dry-Run / invoke 失败。

---

## 5. 发布与运行

| 阶段 | API / 行为 |
|------|------------|
| 保存草案 | `POST /admin/workflows` 或 proposal 流 |
| Dry-Run | propose 自动 / `invoke?dry_run=true` |
| 发布 | `POST .../publish` → Bus 注册 `workflow.order-sync-app` |
| 手动跑 | `POST /tools/invoke` 或 `/admin/workflows/:slug/invoke` |
| 定时 | `triggers.cron`（Slice 24） |
| Webhook | `triggers.webhook` |
| 对话 | Agent `tool_call workflow.order-sync-app` |

**不变量**（见 `WORKFLOW.md` §6）：发布后 PUT 会 unpublish；需显式再发布。

---

## 6. 应用清单模板

| workflow slug | 名称 | 依赖 MCP | 触发 | 负责人 | 状态 |
|---------------|------|----------|------|--------|------|
| `order-sync-app` | 订单同步 | orders, notify | cron | * | 草案 |
| `e2e-mock-chain` | Mock 验证 | e2e-mock | 手动 | 平台 | P0 |

---

## 7. 与路线 1 的协作节奏

```text
Week 1–2   路线 1：mock 多 tool + 首域 MCP 设计评审
           路线 2：e2e-mock-chain propose + 发布（验证 NL）

Week 3–6   路线 1：首域 MCP 上线 + Refresh
           路线 2：2–3 条生产向 workflow + cron

Week 7+    路线 2：模板/Skill 绑定真实 mcp.*
           路线 1：第二域、工具目录维护
```

**阻塞规则**：workflow DSL 引用 `mcp.x.y` 前，必须在 `GET /tools` 中可见。

---

## 8. Web UI 与 UX（已交付 + 规划）

| 能力 | 状态 | 说明 |
|------|------|------|
| `/workflows` 三 Tab | ✅ 迭代 1 | 模板 / NL 指引 / 我的工作流 |
| 模板市场 + slot | ✅ 19c | `notify_tool` 宜选已注册 MCP |
| 流程图 + 步骤说明 | ✅ 19d + 21e | 概览 `WorkflowGraphPreview`；设计器 SWD |
| 提议收件箱 | ✅ | 草案 / 待审批 |
| **可视化设计器（选+填→YAML）** | ✅ **Slice 20 + 21e** | SWD 画布 + 右栏表单；见 [`DOC-STATUS.md`](DOC-STATUS.md) |
| 应用参数表单 | ✅ 20c | `InvokeInputsForm` + SSE 执行时间线 |
| 应用市场 | 📋 迭代 3 | 按域浏览 workflow（W6 / 阶段 B6） |

---

## 9. Skills 与 Orchestrator

| 资产 | 用途 |
|------|------|
| Skill `workflow-template-authoring` | Path C 填槽规则 |
| Skill `workflow-dsl-authoring` | Path B 复杂 DSL |
| Orchestrator `nl-workflow-author` | 识别「建自动化」→ 提议 `workflow.propose` |

**建议新增**（草案）：`skills/workflow/mcp-tool-catalog/SKILL.md`

- 列出当前租户可用 `mcp.*` 及适用场景
- 约束：propose 时只选清单内工具

---

## 10. 治理与审批

| 场景 | 流程 |
|------|------|
| 新应用上线 | propose → Dry-Run → admin confirm / member 审批 |
| 改 DSL | PUT → 自动 unpublish → 再 publish |
| 调 MCP 写操作 | 依赖 MCP `destructiveHint` + workflow Dry-Run mock |
| 审计 | `workflow.*` + `mcp.tool.invoke` + `workflow_runs` |

---

## 11. 质量门禁

| 门禁 | 标准 |
|------|------|
| L1 | `go test ./internal/workflow/...` |
| L2 | graph-preview 可解析 |
| L3 | propose `dry_run_ok=true` |
| L4 | 发布后 invoke 成功 + runs 有 outputs |
| L5 | cron 触发 1 次成功（试点） |
| 对比 | 同任务纯 NL vs workflow：见 [`NL-WORKFLOW-MCP-VALIDATION.md`](NL-WORKFLOW-MCP-VALIDATION.md) |

---

## 12. 后续切片（路线 2 专属）

| ID | 内容 | 优先级 |
|----|------|--------|
| **W0** | **Slice 20 可视化设计器** + **21e SWD 画布**（`WorkflowDesign` compile/decompile） | ✅ 已交付 |
| W1 | 域专用 workflow 模板（`order-sync` 等） | P1 |
| W2 | `mcp-tool-catalog` Skill + 建流时 tools 列表注入 | P1 |
| W3 | 应用 inputs 表单（Web） | ✅ 已交付（20c / `InvokeInputsForm`） |
| W4 | propose 时校验 DSL 中 `mcp.*` 存在性 | P2 |
| W5 | workflow 与 MCP 版本兼容矩阵 | P3 |

---

## 13. 变更日志

| 日期 | 说明 |
|------|------|
| 2026-05-24 | 初版 |
| 2026-05-25 | 新增 Slice 20 可视化设计器设计草案链接 |
| 2026-05-26 | W0/W3、§8 设计器与试运行表单标为已交付；任务总览见 [`DOC-STATUS.md`](DOC-STATUS.md) |
