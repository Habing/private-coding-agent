# NL + Workflow + MCP 优势验证方案

> **目的**：用可重复实验说明「MCP 为工作流提供能力 + NL 拆节点发布」相对 **纯 NL ReAct** 的优势。  
> **总方案**：[`MCP-WORKFLOW-PLATFORM-PLAN.md`](MCP-WORKFLOW-PLATFORM-PLAN.md)

---

## 1. 验证假设

| ID | 假设 | 若成立 |
|----|------|--------|
| H1 | 发布后执行步骤稳定 | 同 DSL 多次 run 调用相同 `mcp.*` 序列 |
| H2 | 运行成本低于纯 NL | C 组 token/时长显著低于 A 组 |
| H3 | NL 主要用于建流而非执行 | 生产触发以 `workflow.*` / cron 为主 |
| H4 | MCP 节点优于 llm 占位 | 结构化 `steps.*.output` 可编程引用 |
| H5 | 可审计可审批 | propose + runs 可追溯 |

---

## 2. 实验环境

> **说明**：本节为**手工实验前置清单**（按你的 compose 环境勾选），不代表产品功能未实现。

```bash
cd deploy/compose
docker compose up -d --build
# 完成 PILOT-RUNBOOK §0 用户 bootstrap
```

前置（实验前自检）：

- [x] `mock-mcp` 已扩展为 ≥3 tool（见 [`MCP-TOOL-ROADMAP.md`](MCP-TOOL-ROADMAP.md) §8）
- [ ] 本环境已注册 MCP `slug=e2e-mock` 且 Refresh
- [ ] 本环境 `GET /tools` 含 `mcp.e2e-mock.*`

---

## 3. 实验 1：三路径对比（核心）

**统一业务描述（示例）**：

> 先查询系统状态；若有异常则记录事件并 echo 通知运维；否则只 echo 正常。

### 3.1 A 组 — 纯 NL（基线）

1. 新建 `coding` 会话。
2. 用户消息：完整描述上述流程（不提及 workflow）。
3. 记录：对话轮数、总 token（若可查）、是否调对工具、第二次重复同一需求是否行为一致。

### 3.2 B 组 — NL 建流（不重复执行）

1. 新会话，发送：「请用 workflow.propose 和 tool-chain 模板，工具都用 mcp.e2e-mock，做一个状态巡检工作流，slug=inspect-app」
2. 确认卡片 `dry_run_ok`。
3. Admin confirm → 发布。
4. 记录：propose 轮数、dry_run 结果、DSL 中 `mcp.*` 列表。

### 3.3 C 组 — 已发布应用（验证复用）

1. 对话：「请执行 workflow.inspect-app，inputs 用默认值」
2. 再测：`POST /admin/workflows/inspect-app/invoke` body `{"inputs":{}}`
3. 可选：配置 cron 触发 1 次。
4. 记录：invoke 时长、token（应接近 0 对话）、`workflow_runs` 的 outputs。

### 3.4 记录表

| 指标 | A 纯 NL | B NL 建流 | C 已发布 |
|------|---------|-----------|----------|
| 首次完成耗时 (s) | | | |
| 估算 token | | | |
| 工具调用序列一致（Y/N） | | | |
| 第二次同任务一致（Y/N） | | N/A | |
| 审计/run 记录（Y/N） | 部分 | proposal | runs |

**通过建议**：C 的第二次执行 **一致性 = Y** 且 **token ≤ A 的 30%**。

---

## 4. 实验 2：审批链（H5）

1. Member 账号 propose 含 `mcp.e2e-mock.record_event`（写）的 workflow。
2. Member confirm → `pending_approval`。
3. Admin 在 `/admin/workflow-proposals` 批准。
4. 确认 Bus 有 `workflow.<slug>`。

---

## 5. 实验 3：Dry-Run 与 destructiveHint

| tool | destructiveHint | Dry-Run 预期 |
|------|-----------------|--------------|
| `fetch_status` | false | 可能真执行 |
| `record_event` | true | mock JSON |

检查 proposal 卡片与 `workflow_runs` 中 `dry_run=true` 行。

---

## 6. 实验 4：首域真实 MCP（P1 后）

将实验 1 业务句换成首域（如 `orders` / `ops-alert`）真实描述，重复 A/B/C。  
**额外指标**：ERP/告警 API 调用成功率、业务方主观验收。

---

## 7. 交付物

| 交付物 | 负责人 |
|--------|--------|
| 填写完成的记录表（§3.4） | 试点负责人 |
| 1 条已发布 workflow + runs 截图 | 路线 2 |
| 1 份 MCP tool 清单 | 路线 1 |
| 结论：H1–H5 是否成立 | 平台/architect |

---

## 8. 变更日志

| 日期 | 说明 |
|------|------|
| 2026-05-24 | 初版 |
