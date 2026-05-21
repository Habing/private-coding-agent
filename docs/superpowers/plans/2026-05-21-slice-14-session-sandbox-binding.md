# Slice 14 — Session ↔ Sandbox Binding Implementation Plan

> **Goal:** `POST /sessions` 自动创建沙箱；`sessions.sandbox_id`；销毁联动；Agent 注入 sandbox_id；E2E **45**。

**Design:** MVP-P1 spec § Slice 14

**Depends on:** Slice 13（sandbox 配额）

---

## Task 1 — Schema

- [x] `0015_sessions_sandbox_id.up.sql`：`sandbox_id UUID NULL` → 逐步 NOT NULL（新 session 必填）
- [x] FK → `sandbox_sessions(id)` ON DELETE SET NULL

## Task 2 — Session service

- [x] `CreateSession`：先 `Sandbox.Create`，失败 503；DB 失败则 Destroy 沙箱
- [x] `ArchiveSession`：`Sandbox.Destroy`
- [x] GET session 响应含 `sandbox_id`

## Task 3 — Agent / Composer

- [x] `RunInput.SandboxID` + engine system 行 `Current sandbox_id: …`
- [x] 单测：`TestEngine_SandboxIDSystemPrefix`、`SendMessage` 传递 sandbox id

## Task 4 — Web UI（最小）

- [x] `api.ts` Session 类型加 `sandbox_id`

## Task 5 — E2E 45

- [x] 建 session → 断言 sandbox_id
- [x] WS 发「list files in workspace」→ tool_call `fs.list` + 成功 result

**非目标：** 文件树 UI（16）、K8sDriver（22）
