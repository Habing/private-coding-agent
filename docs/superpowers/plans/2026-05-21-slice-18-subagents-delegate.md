# Slice 18 — Sub-Agents + agent.delegate Implementation Plan

> **Goal:** 多 Profile、`agent.delegate` 工具、Session profile 切换；E2E **56**。

**Design:** [`../specs/2026-05-21-p1-full-enterprise-design.md`](../specs/2026-05-21-p1-full-enterprise-design.md) §18

**Depends on:** MVP-P1 完成

---

## Outline

- [ ] `Profile` 注册表扩展：`review`、`research`（system + tool allowlist）
- [ ] `tools/agent_delegate.go`：子 `Engine.Run`，max_steps 上限
- [ ] 主 Engine：delegate 结果作为 tool message
- [ ] `PATCH /sessions/:id` profile（若 16 未做）
- [ ] Web：profile 下拉
- [ ] E2E 56：delegate → tool_result → final
- [ ] Audit `agent.delegate.start` / `complete`

**非目标：** Orchestration Router（21）
