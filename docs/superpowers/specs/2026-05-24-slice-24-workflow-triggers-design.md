# Slice 24 — Workflow Triggers Design

> **Status:** Approved for implementation  
> **Depends on:** Slice 19a (Engine), 19b NL Authoring, 19d Visualization (optional UI)  
> **Parent spec:** [`2026-05-18-private-ai-coding-agent-design.md`](2026-05-18-private-ai-coding-agent-design.md) §6 / §11  
> **Plan:** [`../plans/2026-05-24-slice-24-workflow-triggers.md`](../plans/2026-05-24-slice-24-workflow-triggers.md)

---

## 1. Problem

Workflow Engine + NL 建流 + 只读流程图已交付，但 **执行入口仍只有**：

- Admin REST `POST /admin/workflows/:slug/invoke`
- ToolBus `POST /tools/invoke` / Agent `tool_call workflow.<slug>`

`cron-notify`、`webhook-forward` 等模板在 DSL 里用 `trigger_note` **占位**，用户无法真正「定时跑」或「HTTP 打进来就跑」。主 spec P1 自动化叙事缺最后一块。

**24 目标：** 为 **已发布** workflow 配置 **cron** 与 **webhook** 触发器；调度器 / HTTP 入口调用现有 `Service.Invoke`；审计与 `workflow_runs` 与手动 invoke 一致。

---

## 2. Scope (v1)

| In | Out (defer) |
|----|-------------|
| DSL `triggers:` 段（parse + validate） | `wait_event` 挂起节点 |
| DB 表 `workflow_triggers` + 与 publish 联动 | 通用 event bus（GitHub PR、Kafka） |
| 进程内 cron scheduler（robfig/cron/v3） | 分布式 cron leader（多副本 exactly-once） |
| Webhook `POST /hooks/workflow/:token` | 每租户自定义域名 |
| Admin REST CRUD + enable/disable | 可视化 trigger 编辑器（19d 只读标注即可） |
| 模板 `cron-notify` / `webhook-forward` 渲染真实 `triggers:` | Slice 25 连接器替换 notify 占位 |
| E2E **76–78** | Event trigger v2 |

---

## 3. DSL extension

在 `WorkflowDoc` 顶层增加可选块（与 `inputs` / `steps` 同级）：

```yaml
id: weekly-summary
name: Weekly Summary
triggers:
  - id: weekday-morning
    cron: "0 9 * * 1-5"
    timezone: UTC          # optional, default UTC
    inputs:                # optional static inputs merged into invoke
      channel: "team"
  - id: inbound-hook
    webhook: {}            # empty object = enable; server assigns token
    inputs:
      payload: {}
steps:
  - id: notify
    use: llm.chat
    args: { message: "${inputs.payload}" }
```

**规则：**

- `triggers[].id`：slug 内唯一，`^[a-z][a-z0-9-]{0,62}$`
- 每条 trigger **恰好一种**：`cron` **或** `webhook`（与 step kind 互斥同理）
- `cron`：标准 5-field cron（robfig 语法）；validate 时试解析
- `webhook`：YAML 为 `{}` 或 `{ "enabled": true }`；**secret/token 不出现在 DSL**（服务端生成）
- Validate 时：trigger id 不与 step id 冲突
- **Publish 时** 将 DSL triggers 同步到 `workflow_triggers` 表；unpublish 将行 `enabled=false`（保留配置）

---

## 4. Data model

### 4.1 Migration `0025_workflow_triggers`

```sql
CREATE TABLE workflow_triggers (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id       UUID NOT NULL REFERENCES tenants(id),
  workflow_id     UUID NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
  trigger_id      TEXT NOT NULL,          -- DSL triggers[].id
  kind            TEXT NOT NULL,          -- cron | webhook
  cron_expr       TEXT,
  timezone        TEXT NOT NULL DEFAULT 'UTC',
  webhook_token   TEXT,                   -- high-entropy URL token (unique)
  default_inputs  JSONB NOT NULL DEFAULT '{}',
  enabled         BOOLEAN NOT NULL DEFAULT true,
  next_run_at     TIMESTAMPTZ,            -- cron only
  last_run_at     TIMESTAMPTZ,
  last_status     TEXT,
  last_error      TEXT,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (workflow_id, trigger_id),
  UNIQUE (webhook_token)
);
CREATE INDEX workflow_triggers_due ON workflow_triggers (enabled, kind, next_run_at)
  WHERE kind = 'cron' AND enabled = true;
```

### 4.2 与 publish 联动

| 事件 | 行为 |
|------|------|
| `Publish` | Upsert triggers from parsed DSL；cron 计算 `next_run_at`；webhook 无 token 则生成 |
| `Unpublish` | `UPDATE enabled=false`（不删行，便于再发布恢复） |
| `PUT` workflow（强制 unpublish） | 同上 disable |
| `DELETE` workflow | CASCADE 删 trigger 行 |

---

## 5. Scheduler

**`internal/workflow/trigger_scheduler.go`**

- 启动：`StartTriggerScheduler(ctx, svc, repo, cfg)`（`main.go` 与 retention 并列）
- 默认 tick **30s**；`config.workflow.trigger_poll_interval`
- 每 tick：
  1. `SELECT ... WHERE kind=cron AND enabled AND next_run_at <= now() ORDER BY next_run_at LIMIT 32 FOR UPDATE SKIP LOCKED`
  2. 对每行：`Service.Invoke(ctx, tenantID, systemUserID, slug, default_inputs, dryRun=false)`
  3. 更新 `last_run_at` / `last_status` / `last_error`；重算 `next_run_at`
- **system user**：使用该 tenant 的首个 `role=admin` 用户 ID（启动 cache）；找不到则 skip + slog.Warn
- **Missed runs**：启动时若 `next_run_at` 远在过去，**只补跑 1 次**（不堆历史 backlog）
- 多副本：**SKIP LOCKED** 降低双跑概率；不承诺 exactly-once（v1 文档化）

---

## 6. Webhook ingress

**路由：** `POST /hooks/workflow/:token`（**无 JWT**）

| 项 | 行为 |
|----|------|
| Body | JSON object → merge 进 invoke `inputs`（`default_inputs` 打底，body 覆盖同 key） |
| Auth | token 查表；constant-time compare；无效 → 404（不泄露存在性） |
| 前置条件 | trigger `enabled` + parent workflow `published` |
| 限流 | 复用 tenant rate limit 或 per-token 60/min（config） |
| Idempotency | 可选 header `Idempotency-Key` → 5min 内同 key 返回同一 run 摘要 |
| 响应 | 201 `{ "run_id", "status", "outputs" }` 或 409 若 workflow 未发布 |

审计：`workflow.trigger.webhook`（target=workflow slug，metadata `{trigger_id, run_id}`）

Cron 触发审计：`workflow.trigger.cron`

---

## 7. Admin REST

挂载于现有 admin group（`auth.RequireAdmin`）：

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/admin/workflows/:slug/triggers` | 列表（含 webhook URL 前缀 + token 末 4 位） |
| POST | `/admin/workflows/:slug/triggers/:triggerId/run` | 手动 fire 一次（调试） |
| POST | `/admin/workflows/:slug/triggers/:triggerId/rotate-token` | webhook 轮换 token |

**注意：** trigger 定义仍以 **DSL `triggers:` + publish** 为主；REST 不提供「脱离 DSL 建 trigger」（避免双源真相）。PUT workflow 后需 re-publish 同步。

---

## 8. UI / 模板 / 可视化

|  surface | v1 |
|---------|-----|
| `/workflows` 编辑页 | 侧栏「触发器」只读列表（来自 GET triggers）；publish 后显示 webhook URL 复制 |
| `WorkflowProposalCard` | 展示解析出的 trigger 摘要（cron 表达式 / webhook 已启用） |
| `cron-notify` / `webhook-forward` 模板 | 渲染真实 `triggers:`，去掉 `trigger_note` 占位 |
| 19d 流程图 | 可选：trigger 虚拟节点连到 `__start__`（Task 8） |

---

## 9. Config

```yaml
workflow:
  runs_retention_days: 90
  trigger_poll_interval: 30s
  trigger_webhook_rate_per_minute: 60
  trigger_max_due_per_tick: 32
```

env：`PCA_WORKFLOW_TRIGGER_*`

---

## 10. Security

- Webhook token ≥ 32 byte random，base64url；DB 存明文 token（与 JWT 不同，可 rotate；仅 admin 可见完整 URL 一次）
- Webhook 路由不走 SPA；不记 prompt/ body 全文入 audit（仅 size + run_id）
- Cron invoke 使用 tenant admin 身份，仍走 quota / rate limit
- 未发布 workflow 的 webhook 返回 404

---

## 11. E2E（76–78）

| 步 | 场景 |
|----|------|
| 76 | 发布带 `triggers: [{cron: "* * * * *"}]` 的 workflow（测试用每分钟）；等待 ≤90s；`workflow_runs` 新增 `trigger_kind=cron` 行 |
| 77 | 同 workflow webhook token `POST /hooks/workflow/:token` body `{"payload":"hi"}` → 200/201 + outputs |
| 78 | unpublish → cron 不再新增 run；webhook 404/409 |

Mock：可用极短 cron + scheduler tick 30s；或 test hook 暴露 `POST /admin/workflows/:slug/triggers/:id/run`。

---

## 12. 非目标

- Event trigger（GitHub、SQS）
- 跨 workflow 触发链
- Trigger 级 dry-run（用手动 invoke dry_run 代替）
- Helm 专用 trigger Ingress（compose 8080 即可）

---

## 13. 验收

- [x] DSL parse/validate triggers
- [x] Publish/unpublish 同步 DB
- [x] Cron scheduler 产生 run + audit
- [x] Webhook invoke + rotate token
- [x] 模板 cron-notify / webhook-forward 无占位
- [x] E2E 76–78 PASS；compose 全量 **78/78**
