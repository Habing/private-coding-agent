# Slice 19b — NL Workflow Authoring (B+C) Design

> **Status:** Approved for implementation  
> **Depends on:** Slice 19a (Engine), Slice 18 (profiles + delegate), Slice 21 (orchestrator)  
> **Parent spec:** [`2026-05-18-private-ai-coding-agent-design.md`](2026-05-18-private-ai-coding-agent-design.md) §6  
> **Plan:** [`../plans/2026-05-23-slice-19b-nl-workflow-authoring.md`](../plans/2026-05-23-slice-19b-nl-workflow-authoring.md)

---

## 1. Problem

P1 主 spec 承诺 **NL → Workflow 全流程**，但 19a 只交付 Engine + admin REST。当前缺口：

| 用户期望 | 现状 |
|----------|------|
| 全程自然语言建流 | Agent 能 `workflow.create`，用户仍须打开 Web/REST 发布 |
| 业务同学可用 | 需理解 YAML / slug / 工具名 |
| 高可靠常见场景 | 自由生成 DSL 易犯 `args`/`with`、`shell`/`shell.exec` 等错误 |

**19b 目标：** 用户**只对话**；系统内部走 **模板填槽（C）** 或 **自由生成（B）**；**Dry-Run 预览**；**对话内一键确认发布**（admin）或 **转交 admin 审批**（member）。

---

## 2. B+C 双层策略

```text
用户自然语言
      │
      ▼
┌─────────────────┐
│ Intent Router   │  orchestrator 规则 + authoring 分类
└────────┬────────┘
         │
    ┌────┴────┐
    ▼         ▼
 Path C      Path B
 模板填槽    自由 DSL
 (高可靠)    (workflow-authoring + Skill)
    │         │
    └────┬────┘
         ▼
   validate + dry_run
         ▼
   Draft Proposal（会话内卡片）
         ▼
   confirm → publish → workflow.<slug>
```

### Path C — 模板填槽（优先）

- 内置 **5–8 个**高频模板（cron 通知、Webhook 转发、PR 事件、HTTP 拉取+通知、简单))
- NL 提取 **slots**（schedule、channel、message、tool 名等）
- 渲染为合法 DSL → **几乎零语法错误**
- 模板 catalog 对 Agent 可见（Skill + `GET /agent/workflow/templates`）

### Path B — 自由生成（兜底）

- 无匹配模板 / 用户明确「自定义」→ `agent.delegate(profile=workflow-authoring)`
- 沿用 `workflow-dsl-authoring` Skill + `workflow.create`
- validate 失败 → 自动重试 1 轮（错误回灌 LLM）

**路由原则：** 能匹配模板 **≥ 0.7 置信** 走 C；否则 B。用户可说「不要用模板，自定义」强制 B。

---

## 3. 确认发布模型（HITL in chat）

发布仍改变 Bus 全局状态，**不能** silent auto-publish。

| 角色 | 行为 |
|------|------|
| **admin** | 对话卡片「预览 + Dry-Run 结果」→ 点 **确认发布** → `workflow.publish` |
| **member** | 同上卡片 → **提交审批** → admin 收到通知 → admin 对话/REST 确认 |

**不变量：**

1. `workflow.publish` 工具 **仅 admin** 可调（与 create/update 同 gate）
2. Publish 前 **必须** 有一次 `dry_run` 成功记录绑到 proposal
3. PUT 后 proposal 失效，需重新 Dry-Run
4. 审计：`workflow.proposal.create` / `workflow.proposal.confirm` / 现有 `workflow.admin.publish`

---

## 4. 数据模型

### `workflow_proposals` 表

| 列 | 说明 |
|----|------|
| id | UUID PK |
| tenant_id | 租户 |
| session_id |  originating 会话（可空） |
| created_by | user_id |
| slug / name / description | 目标 workflow 元数据 |
| dsl_yaml | 渲染后 DSL |
| source | `template:<id>` \| `freeform` |
| template_id / slots_json | C 路径溯源 |
| dry_run_ok | bool |
| dry_run_output_json | 最近一次 dry_run outputs |
| status | `draft` \| `pending_approval` \| `confirmed` \| `published` \| `rejected` |
| published_at | nullable |

Proposal 生命周期短（7 天 TTL job 清理），**不**替代 `workflows` 表。

---

## 5. API 面

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/agent/workflow/templates` | 模板 catalog（name、description、slots schema） |
| POST | `/agent/workflow/proposals` | 从 NL 或显式 `{template_id, slots}` / `{dsl_yaml}` 创建 proposal + 自动 dry_run |
| GET | `/agent/workflow/proposals/:id` | 详情（含 dry_run 摘要） |
| POST | `/agent/workflow/proposals/:id/confirm` | admin：publish；member：→ pending_approval |
| POST | `/admin/workflow/proposals/:id/approve` | admin 批准 member 提交的 proposal |

Tool Bus 新增：

- `workflow.publish` — input `{slug}` 或 `{proposal_id}`；admin only
- `workflow.propose` — input `{template_id?, slots?, dsl_yaml?, slug, name}`；创建 proposal + dry_run，返回 `{proposal_id, summary, dry_run_ok}`

---

## 6. 对话 UI

Web 聊天流内渲染 **WorkflowProposalCard**：

- 中文摘要（步骤列表、触发方式占位文案）
- Dry-Run 结果折叠区
- 按钮：**确认发布**（admin）/ **提交审批**（member）/ **修改需求**（回对话）

不强制用户打开「工作流」YAML 页；高级用户仍可在 Workflows 页手工编辑。

---

## 7. Orchestrator 集成

新增规则（config YAML）：

```yaml
- name: nl-workflow-author
  match:
    profile: [coding]
    content_regex: '(工作流|自动化|定时|每周|webhook|流程编排)'
  suggest:
    type: sub_agent
    target: workflow-authoring
    hint: |
      用户似乎在描述可复用自动化。优先匹配 workflow 模板 catalog；
      匹配则 workflow.propose(template_id=...)；否则 delegate 自由起草。
      完成后展示 proposal 摘要，引导 Dry-Run 与确认发布。
```

---

## 8. 模板 Catalog v1

| template_id | 场景 | 主要 slots |
|-------------|------|------------|
| `cron-notify` | 定时通知 | `schedule_cron`, `message`, `notify_tool`, `notify_args` |
| `webhook-forward` | Webhook 入站转发 | `webhook_path`, `forward_tool`, `forward_args` |
| `http-fetch-notify` | HTTP 拉取 + 通知 | `url`, `method`, `notify_tool`, `notify_args` |
| `llm-summarize-notify` | LLM 摘要 + 通知 | `prompt`, `model`, `notify_tool`, `notify_args` |
| `tool-chain` | 顺序调用 2–3 工具 | `steps_json`（简化数组） |

**触发器：** v1 模板 DSL **不含** `triggers:` 段（Slice 24 再补）；卡片上显示「触发器：手动 / 对话调用（cron 即将支持）」。

---

## 9. 非目标（19b 出栈）

- cron/webhook 触发器实现（Slice 24）
- MCP 连接器 catalog UI（Slice 25）
- 可视化流程图编辑器
- 模板市场 / fork / 跨租户共享
- member 直接 publish（必须审批链）
- `agent.run` workflow 节点

---

## 10. 风险

| 风险 | 缓解 |
|------|------|
| NL 填槽错误 | 模板 slot schema 校验 + Dry-Run + 卡片人工确认 |
| 自由 DSL 质量 | Skill 硬规则 + validate 错误回灌 + 模板优先 |
| proposal 堆积 | TTL job + status 机清晰 |
| admin 审批延迟 | 审计 + Web 通知（v1 仅 audit 行，IM 推 P2） |

---

## 11. 验收

- E2E **70–75** 步 PASS（模板路径 + 自由路径 + admin confirm + member pending）
- `go test ./internal/workflow/...` 全绿
- Web UI：`WorkflowProposalCard` 单测 + 手工冒烟
- 文档：`docs/WORKFLOW.md` §「自然语言建流」
