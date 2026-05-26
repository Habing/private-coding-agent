# MCP + 工作流双路线平台方案

> **状态**：草案（2026-05-24）  
> **定位**：在 PCA 已交付能力之上，建设「企业 MCP 能力目录 + NL 工作流应用」两条主线。  
> **子文档**：[`MCP-TOOL-ROADMAP.md`](MCP-TOOL-ROADMAP.md) · [`WORKFLOW-APP-ROADMAP.md`](WORKFLOW-APP-ROADMAP.md) · [`NL-WORKFLOW-MCP-VALIDATION.md`](NL-WORKFLOW-MCP-VALIDATION.md)

---

## 1. 目标与一句话

**目标**：用私有化 Agent 平台，把企业业务能力封装为 **MCP 工具**，再用 **自然语言 + 工作流** 编排成可发布、可调度、可审计的 **Agent 应用**，支撑重复性业务自动化。

**公式**：

```text
路线 1（能力层）  MCP 工具开发     →  mcp.<域>.<tool> 进入 Tool Bus
路线 2（应用层）  工作流生成与发布  →  workflow.<应用> 编排 MCP 节点
契约             运行期只调已注册工具；NL 主要用于「建流」，不替代生产执行
```

---

## 2. 战略架构

```text
                    ┌─────────────────────────────────────┐
                    │  用户 / 运维 / 业务分析              │
                    └──────────────┬──────────────────────┘
                                   │
          ┌────────────────────────┼────────────────────────┐
          ▼                        ▼                        ▼
   ┌──────────────┐        ┌──────────────┐        ┌──────────────┐
   │ 对话（NL）    │        │ /workflows   │        │ cron/webhook │
   │ propose/确认  │        │ 模板市场      │        │ 触发器        │
   └──────┬───────┘        └──────┬───────┘        └──────┬───────┘
          │                       │                       │
          └───────────────────────┼───────────────────────┘
                                  ▼
                    ┌─────────────────────────────────────┐
                    │  Workflow Engine（YAML DAG）          │
                    │  节点: use: mcp.* / assign / if / …   │
                    └──────────────┬──────────────────────┘
                                   ▼
                    ┌─────────────────────────────────────┐
                    │  Tool Bus（统一执行 + 审计 + 配额）    │
                    └──────────────┬──────────────────────┘
                                   ▼
          ┌────────────────────────┴────────────────────────┐
          ▼                        ▼                        ▼
   ┌──────────────┐        ┌──────────────┐        ┌──────────────┐
   │ 内置工具      │        │ mcp.<域>.*   │        │ workflow.*   │
   │ fs/shell/…   │        │ 外部 MCP 服务 │        │ 子工作流      │
   └──────────────┘        └──────────────┘        └──────────────┘
```

**与纯 ReAct 的差异**：探索型任务仍用对话 ReAct；**企业生产**优先「已发布工作流 + MCP 节点」，把推理不确定性关在 **提议 / Dry-Run / 审批** 阶段。

---

## 3. 两条路线分工

| | **路线 1：MCP 工具开发** | **路线 2：工作流应用** |
|--|--------------------------|------------------------|
| **回答的问题** | 企业能调用哪些稳定 API？ | 何时、按什么顺序、在什么条件下调用？ |
| **主要产出** | HTTP MCP 服务、Docker 部署、Bus 工具名 | DSL、proposal、发布、`workflow_runs` |
| **主要用户** | 领域后端、集成工程师 | 平台组、业务运维、管理员 |
| **PCA 入口** | `/admin/mcp-servers`、连接器 | `/workflows`、`workflow.propose`、审批 |
| **不负责** | DAG、定时、审批发布 | 实现 ERP/Slack 等具体 API |

详见子路线图。

---

## 4. 接口契约（两线必须共同遵守）

| # | 契约 | 说明 |
|---|------|------|
| C1 | 命名 | Bus：`mcp.<slug>.<tool>`；`slug`=业务域（kebab-case），`tool`=动词短语（snake_case） |
| C2 | 发现 | 建流前工具必须已注册；DSL 禁止引用不存在的 `mcp.*` |
| C3 | Schema | 每个 tool 有完整 `inputSchema` + 中文 `description`；写操作 `destructiveHint: true` |
| C4 | 刷新 | MCP 发版后必须在 PCA **Refresh** 工具列表 |
| C5 | 版本 | MCP 实现版本与 workflow 版本解耦；破坏性变更需迁移指南 |
| C6 | 租户 | MCP server 行绑定 `tenant_id`；workflow 跨租户不可调用（已有） |
| C7 | 执行 | Workflow `tool` 节点 → `bus.Invoke`；审计 `mcp.tool.invoke` |

---

## 5. 阶段规划（建议 12 周试点）

| 阶段 | 周 | 路线 1 | 路线 2 | 里程碑 |
|------|-----|--------|--------|--------|
| **P0 打通** | 1–2 | 扩展 `mock-mcp` 为 ≥3 tool；注册 `e2e-mock` | `tool-chain` proposal 全用 `mcp.e2e-mock.*`；发布 + invoke | 链路验证：[`NL-WORKFLOW-MCP-VALIDATION.md`](NL-WORKFLOW-MCP-VALIDATION.md) 实验 0–1 |
| **P1 单域** | 3–6 | 公共域 **`data-prep`**（文件 inbox → MCP **AI** 降噪 → outbox） | `data-prep-pipeline` 发布 + invoke | 见 [`domains/data-prep.md`](domains/data-prep.md) |
| **P2 扩展** | 7–9 | 第二域 MCP；工具目录文档化 | 模板/Skill 对齐域；子 workflow | 工具被引用率、run 成功率 KPI |
| **P3 工厂** | 10–12 | CI：代码 → 镜像 → 部署 → 自动 Refresh（可选） | NL 分类 + 应用市场 UX（迭代 2/3） | 从沙箱到 MCP 上线 SOP |

**原则**：先 **公共域 `data-prep` 工具库够厚**，再 **业务域工作流**；P0 仍用 `e2e-mock` 打通 NL+Workflow 链路。

---

## 6. 域 ↔ MCP ↔ 工作流 对照表（模板）

试点时维护一张表（可放在 Wiki 或 `docs/domains/`）：

| 类型 | 业务域 | MCP slug | 核心 tools（示例） | 工作流 slug | 触发 | 阶段 |
|------|--------|----------|-------------------|-------------|------|------|
| **公共** | AI 数据降噪 | `data-prep` | `load_file`, `ai_denoise_records`, `summarize_run`, `write_file` | `data-prep-pipeline` | invoke / cron | **P1** |
| 业务 | 订单同步 | `orders` | `list_pending`, `upsert_erp`, … | `order-sync-app` | cron | P2 暂缓 |
| 业务 | 告警运维 | `ops-alert` | `list_open_alerts`, … | `alert-digest-app` | cron | P2 暂缓 |
| 开发 | 链路验证 | `e2e-mock` | `echo`, `fetch_status`, `record_event` | `e2e-mock-chain` | 手动 | P0 |

---

## 7. 角色与 RACI（简）

| 活动 | 领域开发 | 平台/SRE | 业务运维 | 安全/合规 |
|------|----------|----------|----------|-----------|
| MCP 实现与部署 | R | A | C | C |
| MCP 注册与 Refresh | C | R/A | I | I |
| NL 建流 / 模板 | C | R | R | I |
| 工作流审批发布 | I | A | C | R |
| 生产监控 runs | C | R | R | I |

R=负责，A=批准，C=协商，I=知会。

---

## 8. KPI（试点末验收）

| 指标 | 路线 1 | 路线 2 |
|------|--------|--------|
| 业务域 MCP 数 | ≥1 生产域 | — |
| 每域 tool 数 | ≥8 | — |
| 已发布工作流 | — | ≥3 |
| workflow run 成功率 | — | ≥95%（试点环境） |
| 相对纯 NL token 节省 | — | 同任务 C 路径比 A 路径 ≥30%（见验证 doc） |
| 审批覆盖 | — | 变更流 100% 经 propose（member 场景） |

---

## 9. 风险与边界

| 风险 | 缓解 |
|------|------|
| 工具堆砌、NL 选型混乱 | 按域一个 MCP；tool 清单评审 |
| MCP 写操作 Dry-Run 仅 mock | 联调环境真跑；生产靠审批 + 灰度 |
| NL 建流超时 | 优先模板 Path C；复杂流拆子 workflow |
| 沙箱内 docker build | 工厂走 CI，不走沙箱 socket |
| K8s 无 Snapshot | MCP 部署独立于沙箱快照 |

**非目标（本方案阶段）**：替代 GitLab/GitHub 全自动 PR；替代 Dify 全民画布；N8N 集成（Slice 23 已跳过）。

---

## 10. 相关文档

| 文档 | 内容 |
|------|------|
| [`MCP-TOOL-ROADMAP.md`](MCP-TOOL-ROADMAP.md) | 路线 1：开发规范、目录结构、发布 SOP |
| [`WORKFLOW-APP-ROADMAP.md`](WORKFLOW-APP-ROADMAP.md) | 路线 2：NL 路径、模板、发布与触发 |
| [`NL-WORKFLOW-MCP-VALIDATION.md`](NL-WORKFLOW-MCP-VALIDATION.md) | 优势验证实验与记录表 |
| [`WORKFLOW.md`](WORKFLOW.md) | DSL / API / Dry-Run 技术说明 |
| [`CONNECTORS.md`](CONNECTORS.md) | MCP 注册与 Slack/GitHub 示例 |
| [`PILOT-RUNBOOK.md`](PILOT-RUNBOOK.md) | Compose 试点运维 |

---

## 11. 变更日志

| 日期 | 说明 |
|------|------|
| 2026-05-24 | 初版：双路线总方案 + 子文档索引 |
| 2026-05-24 | P1 首域：`data-prep`（文件 → MCP **AI** 清洗） |
