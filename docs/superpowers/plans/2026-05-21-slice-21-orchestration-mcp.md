# Slice 21 — Orchestration Router + External MCP Implementation Plan

> **Goal:** 路由策略、External MCP 注册与 ListTools；E2E **62–63**。

**Design:** Full P1 spec §21 + 主 spec §4.0.1 P1+

**Depends on:** Slice 18、19

---

## Outline

- [ ] `internal/orchestrator`：规则引擎（profile/session 配置 → 优先 workflow vs react）
- [ ] `mcp_servers` 表 + 连接管理 + 心跳
- [ ] `ListTools` 合并 external（租户过滤）
- [ ] 可选 `http.get` 内置工具
- [ ] Audit `orchestrator.route`
- [ ] E2E 62–63
