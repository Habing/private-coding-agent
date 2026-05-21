# Slice 20 — Reflection Agent Implementation Plan

> **Goal:** 异步提议记忆、审核队列、冲突合并；E2E **61**。

**Design:** Full P1 spec §20 + 主 spec Reflection

**Depends on:** Slice 16（Memory）、16 可选 19

---

## Outline

- [ ] `internal/reflection`：worker 消费 session 结束事件
- [ ] `MemoryProposal` 表 + `POST /admin/memory-proposals` approve/reject
- [ ] 合并策略：同 type+tag 去重、confidence
- [ ] Web：审核列表
- [ ] Metrics `pca_reflection_proposals_total`
- [ ] E2E 61

**非目标：** LoRA / 行为微调（P3）
