# Slice 24 — Workflow Triggers Implementation Plan

> **状态**：**已完成**（2026-05-26）  
> **Goal:** 已发布 workflow 支持 **cron + webhook** 自动触发；DSL `triggers:` 段；E2E **76–78**（compose **78/78**）。

**Design:** [`specs/2026-05-24-slice-24-workflow-triggers-design.md`](../specs/2026-05-24-slice-24-workflow-triggers-design.md)

**Depends on:** Slice 19a ✅、19b ✅

**Estimated effort:** 1.5–2 人周

**纪律：** 每 Task 完成 → 相关 `go test` → 更新 `WORKFLOW.md` §10、`SLICE-VERIFICATION.md`、本 plan 勾选 → **单独 commit**。

---

## Context

19b 模板 `cron-notify` / `webhook-forward` 曾用 `trigger_note` 占位；Slice 24 已改为真实 `triggers:` 段，并接入 **调度 + HTTP 入口**（`Service.Invoke` + `workflow_runs`）。

---

## Goal（交付清单）

- [x] `0025_workflow_triggers` migration
- [x] DSL `triggers:` parse + validate
- [x] `TriggerRepo` + publish/unpublish sync
- [x] `TriggerScheduler` 后台 cron
- [x] `POST /hooks/workflow/:token` webhook
- [x] Admin GET triggers + manual run + rotate-token
- [x] 模板 render 真实 triggers（无 `trigger_note` 占位）
- [x] Web UI 触发器摘要 + webhook URL
- [x] E2E 76–78；文档 [`WORKFLOW.md`](../../WORKFLOW.md) §10

---

## 非目标

- Event / Kafka / GitHub webhook 验签（Slice 24b 或 P2）
- 多副本 exactly-once cron
- Trigger CRUD 脱离 DSL 的 REST

---

## Task 1 — Migration + types

**Files**

- `internal/db/migrations/0025_workflow_triggers.{up,down}.sql`
- `internal/workflow/trigger_types.go`

**Do**

- 表结构见 design §4.1
- Go struct `WorkflowTrigger` + `TriggerKindCron|Webhook`

**Test**

- `go test ./internal/workflow/... -run TriggerRepo -count=1`（Task 2 一并）

**Commit:** `feat(workflow): add workflow_triggers migration and types`

---

## Task 2 — Parse + validate triggers

**Files**

- `internal/workflow/types.go` — `WorkflowDoc.Triggers []TriggerSpec`
- `internal/workflow/parse.go` / `validate.go`
- `internal/workflow/trigger_validate_test.go`

**Do**

- YAML shape：`id`, `cron`, `timezone`, `webhook`, `inputs`
- 互斥：cron vs webhook；id 唯一；cron 表达式 robfig 解析
- trigger id 不与 step id 冲突

**Test**

```bash
go test ./internal/workflow/... -run 'Trigger|Parse.*trigger' -count=1
```

**Commit:** `feat(workflow): parse and validate DSL triggers block`

---

## Task 3 — TriggerRepo + publish sync

**Files**

- `internal/workflow/trigger_repo.go`
- `internal/workflow/service.go` — `Publish`/`Unpublish` 调用 sync
- `internal/workflow/trigger_sync.go`

**Do**

- `SyncTriggersFromDoc(ctx, tenantID, workflowID, doc)` upsert/disable
- 生成 webhook token（crypto/rand 32B → base64url）
- Cron：`next_run_at` 用 robfig 算下一 fire time（UTC 或 timezone）

**Test**

- dockertest 或 repo test：publish 2 triggers → 2 rows；unpublish → enabled=false

**Commit:** `feat(workflow): sync triggers on publish and unpublish`

---

## Task 4 — TriggerScheduler

**Files**

- `internal/workflow/trigger_scheduler.go`
- `internal/workflow/trigger_scheduler_test.go`
- `cmd/server/main.go` — 启动 scheduler
- `internal/config/config.go` + `config.example.yaml`

**Do**

- Tick loop + SKIP LOCKED due cron rows
- Resolve tenant admin user for Invoke
- Update last_run / next_run / last_error
- Audit `workflow.trigger.cron`

**Test**

- 单元测试：fake repo + fake service，注入 due row → Invoke 被调一次

**Commit:** `feat(workflow): cron trigger scheduler`

---

## Task 5 — Webhook handler

**Files**

- `internal/workflow/trigger_handler.go`
- `cmd/server/main.go` — 注册 **公开** `POST /hooks/workflow/:token`（auth 组外）

**Do**

- Lookup by token；published + enabled 检查
- Merge inputs；Invoke；JSON 响应
- Audit `workflow.trigger.webhook`
- 简单 idempotency cache（memory LRU 或 PG 表 — v1 可用 sync.Map + TTL）

**Test**

- httptest：valid token → 201；bad token → 404；unpublished → 409

**Commit:** `feat(workflow): webhook trigger ingress`

---

## Task 6 — Admin REST

**Files**

- `internal/workflow/trigger_admin_handler.go`
- `handler.go` 或独立 Register

**Routes**

- `GET /admin/workflows/:slug/triggers`
- `POST /admin/workflows/:slug/triggers/:triggerId/run`
- `POST /admin/workflows/:slug/triggers/:triggerId/rotate-token`

**Test**

- `trigger_handler_test.go` / extend `handler_test.go`

**Commit:** `feat(workflow): admin trigger inspection and manual run`

---

## Task 7 — 模板 + graph（可选节点）

**Files**

- `internal/workflow/template/render.go` — cron-notify / webhook-forward 输出 `triggers:`
- `internal/workflow/graph.go` — 可选 virtual trigger nodes（连到 `__start__`）

**Commit:** `feat(workflow): template triggers and graph trigger nodes`

---

## Task 8 — Web UI

**Files**

- `internal/webui/src/pages/Workflows.tsx` — TriggersPanel
- `internal/webui/src/components/WorkflowProposalCard.tsx` — trigger 摘要
- vitest 更新

**Do**

- 展示 cron / webhook URL（admin 复制）
- publish 后刷新 triggers 列表

**Commit:** `feat(webui): workflow trigger summary and webhook URL`

---

## Task 9 — E2E 76–78 + docs

**Files**

- `deploy/compose/test-e2e.sh`
- `docs/WORKFLOW.md` §10 Triggers
- `docs/SLICE-VERIFICATION.md`
- `README.md` E2E 计数 78
- `HANDOFF.md` §2 端点 + 切片表

**Do**

- 步骤 76–78 见 design §11
- 全量 `[78/78] PASS`

**Commit:** `test(e2e): Slice 24 workflow trigger steps 76–78`

**Docs commit:** `docs: WORKFLOW triggers section and Slice 24 verification`

---

## Dependency graph

```text
Task 1 → Task 2 → Task 3 → Task 4
                    └────→ Task 5 → Task 6
Task 3 → Task 7 → Task 8
Task 4–8 → Task 9
```

---

## Verification（每 Task 后）

```bash
go test ./internal/workflow/... -count=1
go vet ./...
cd internal/webui && npm test && npm run build   # Task 8 起
cd deploy/compose && ./test-e2e.sh             # Task 9
```

---

## Acceptance checklist

- [x] 发布含 triggers 的 workflow 后 cron 自动产生 `workflow_runs`
- [x] Webhook token 可 invoke 且 unpublish 后失效
- [x] 审计含 `workflow.trigger.cron` / `workflow.trigger.webhook`
- [x] 模板无 `trigger pending Slice 24` 占位
- [x] compose E2E **78/78**
