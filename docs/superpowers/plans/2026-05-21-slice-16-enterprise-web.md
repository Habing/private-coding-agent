# Slice 16 — Enterprise Web Implementation Plan

> **Goal:** 沙箱文件侧栏、Memory Loader、Memory 管理页、最小 Settings；E2E **47–48**。

**Design:** MVP-P1 spec § Slice 16

**Depends on:** Slice 14（sandbox_id）

---

## Task 1 — Memory Loader (backend)

- [ ] `internal/memory/loader.go`：`LoadForSession(ctx, tenant, user, budgetTokens)`
- [ ] `session.Service.SendMessage`：首条 user 前调用；结果进 `ContextComposer`
- [ ] `agent.composer`：Skills 后插入 `## Relevant memories`
- [ ] Audit `memory.inject`（ids, chars, truncated）

## Task 2 — Memory UI

- [ ] `pages/Memories.tsx`：列表、编辑、删除
- [ ] 路由 `/memories`；ProtectedShell 导航链接
- [ ] 单测 + 可选 vitest

## Task 3 — Sandbox file browser

- [ ] `components/SandboxFiles.tsx`：递归 list（深度限制 3）
- [ ] `GET /sandbox/sessions/{sandboxId}/files?path=`
- [ ] 预览：文本 ≤256KB base64 解码

## Task 4 — Settings (minimal)

- [ ] Chat 或 Shell 展示 model、profile（来自 session GET）

## Task 5 — E2E 47–48

- [ ] 47：POST memory → 新 session WS → 断言 audit/memory 注入
- [ ] 48：session 有 sandbox → files list 非空根响应

**非目标：** 沙箱 logs 流（Full/16b）、在线编辑文件
