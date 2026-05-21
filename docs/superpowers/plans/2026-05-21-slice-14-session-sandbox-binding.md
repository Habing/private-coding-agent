# Slice 14 — Session ↔ Sandbox Binding Implementation Plan

> **Goal:** `POST /sessions` 自动创建沙箱；`sessions.sandbox_id`；销毁联动；Agent 注入 sandbox_id；E2E **45**。

**Design:** MVP-P1 spec § Slice 14

**Depends on:** Slice 13（sandbox 配额）

---

## Task 1 — Schema

- [ ] `0015_sessions_sandbox_id.up.sql`：`sandbox_id UUID NULL` → 逐步 NOT NULL（新 session 必填）
- [ ] FK → `sandbox_sessions(id)` ON DELETE SET NULL 或 RESTRICT（实现时选定）

## Task 2 — Session service

- [ ] `CreateSession`：调用 `sandbox.Runtime.Create`；失败则不回滚已插入 session 行（spec 默认 503 整体失败）
- [ ] `ArchiveSession`：`Sandbox.Destroy`
- [ ] GET session 响应含 `sandbox_id`

## Task 3 — Agent / Composer

- [ ] `ComposeInput` 增加 `SandboxID`
- [ ] system 行：`Current sandbox_id: …`（仅当非空）
- [ ] 单测：mock engine 收到 sandbox id

## Task 4 — Web UI（最小）

- [ ] `api.ts` Session 类型加 `sandbox_id`（可选隐藏，调试可展示）

## Task 5 — E2E 45

- [ ] 建 session → 断言 sandbox_id
- [ ] WS 发「list files in workspace」→ tool_call `fs.list` + 成功 result

**非目标：** 文件树 UI（16）、K8sDriver（22）
