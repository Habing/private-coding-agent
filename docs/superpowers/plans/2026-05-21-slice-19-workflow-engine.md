# Slice 19 — Workflow Engine Implementation Plan

> **Goal:** DSL 存储、DAG 执行、`workflow.<id>` 工具、Dry-Run；E2E **57–60**。

**Design:** Full P1 spec §19 + 主 spec §6 Workflow

**Depends on:** Slice 18（可选）、Tool Bus 稳定

---

## Outline (可拆 19a / 19b)

### 19a — Engine

- [ ] migrations `workflows`, `workflow_versions`, `workflow_runs`
- [ ] `internal/workflow`：parse YAML、JSON Schema 校验
- [ ] 执行器：tool / if / loop / parallel / assign
- [ ] 发布 → `toolbus.Register(workflow.<id>)`
- [ ] Dry-Run mock mutating tools

### 19b — Authoring + UI

- [ ] Profile `workflow-authoring`
- [ ] NL → DSL 草案 API（可选最小）
- [ ] Web：workflow 列表、发布按钮

- [ ] E2E 57–60

**非目标：** N8N（23）、HITL 节点（ADR 待决，默认 P2）
