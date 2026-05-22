# Slice 21 — Orchestration Router + External MCP Implementation Plan

> **Goal:** 路由策略、External MCP 注册与 ListTools；E2E **62–63**。
>
> **Status (2026-05-22):** 拆为 **21a (Router) ✅** + **21b (External MCP) ⬜**。两半互相独立（router ~500 行配置 + 一处 agent 集成；External MCP ~2000+ 行新子系统），与 19a/19b 处理方式一致。

**Design:** Full P1 spec §21 + 主 spec §4.0.1 P1+

**Depends on:** Slice 18、19

---

## 21a — Orchestration Router ✅ (2026-05-22)

**完整 plan：** `docs/superpowers/plans/2026-05-22-slice-21a-orchestration-router.md`（本仓库 plan mode 写入，已落盘到 `C:\Users\HB\.claude\plans\smooth-sprouting-pancake.md`）

### 行为（"Shadow + Hint"）

- 每次 `agent.Engine.Run` 启动时跑一遍规则引擎；规则命中 → audit `orchestrator.route` + 把建议作为 system message 注入（"Routing hint: 这看起来像 X 任务，可考虑用 workflow.Y / agent.delegate profile=Z / 工具 W"）
- LLM 看得到提示，但**不改变执行路径** —— ReAct 自己决定是否采纳；路由器不做 invoke
- 规则未命中 → 不注入，audit `outcome=no_match`
- `enabled=false` → engine.router 保持 nil，整段 short-circuit

### 不做（留给 21b 或 v2+）

- 强路由 / bypass ReAct（v2+）
- ML / embedding-based routing（P2+）
- 路由器自己 invoke workflow（v2）
- WebUI 路由器配置页（rules 走 YAML，不进 DB）
- 外部 MCP（拆到 21b）

### Outline

- [x] `internal/orchestrator` 包：`Engine` + `Rule` + `Decision` + `Router` 接口 + `PrependSystemHint` composer
- [x] Config：`orchestrator: {enabled, inject_hint, default_hint, rules: [...]}` 段；env `PCA_ORCHESTRATOR_*`
- [x] `agent.Engine.Run` 接入：可选 `WithRouter(orchestrator.Router)`，在 ContextComposer 拼装 system 之后、user 消息之前调用
- [x] Audit `orchestrator.route` (detached) + metric `pca_orchestrator_routes_total{outcome,target_type}`
- [x] Mock-provider 扩展：`ORCHESTRATOR_E2E_HINT_DELIVERED` marker → canned `"orchestrator-hint-ok"`（chat + stream）
- [x] E2E 步骤 62（agent.run 含 `E2E_ORCHESTRATOR_HINT_V1` → final + audit 双断言）
- [x] 文档：README + HANDOFF + SLICE-VERIFICATION

### 落地 commits

1. `84bf724` feat(orchestrator): rule engine + types + tests
2. `a2cc699` feat(orchestrator,agent): inject system hint in Engine.Run
3. `5462b20` feat(orchestrator,audit,metrics): wire audit + counter + mock canned + config
4. *(this commit)* test(e2e,docs): step 62 + slice 21a doc closeout

### Acceptance（已通过）

- [x] `go test ./internal/orchestrator/... -count=1` 17 个单测全 PASS
- [x] `internal/agent/engine_test.go` 加测：`WithRouter` + 命中规则 → outgoing messages 多一条 system 在用户 msg 之前
- [x] `cfg.Orchestrator.Enabled=false` 时 main.go 不调 `NewEngine`、不接入 engine
- [x] 启动期 regex 编译失败 / rule match 块空 → fail-fast
- [x] Mock-provider 看到 `ORCHESTRATOR_E2E_HINT_DELIVERED` 在 system message 返回 canned `"orchestrator-hint-ok"`（chat + stream 两路一致）
- [x] 多 system 顺序：路由器 hint 排在 skills 之后、用户消息之前
- [x] `inject_hint=false` 时只 audit 不注入；E2E 默认 `inject_hint=true`
- [x] `pca_orchestrator_routes_total` 在 /metrics 出现，标签 outcome + target_type 完整
- [x] 1 个 audit action 出现在 audit_log（即使 no_match 也发一行 + metadata.matched=false）
- [x] E2E 62/62 PASS
- [x] README + HANDOFF + SLICE-VERIFICATION 三份文档更新

---

## 21b — External MCP Manager ⬜（下一切片）

**单独 plan（待写）：** `docs/superpowers/plans/2026-05-2x-slice-21b-external-mcp.md`

### 范围

- [ ] 0020 migration `mcp_servers` 表（tenant_id + url + transport=http + auth + enabled + last_seen + tools_cache）
- [ ] `internal/mcp/` 包：JSON-RPC client（HTTP-only；stdio 推到 P2）+ Manager + heartbeat goroutine + tools schema cache
- [ ] Bus 集成：`mcp.<server>.<tool>` 注册（precedent: `workflow.<slug>`）
- [ ] Admin REST `/admin/mcp-servers` CRUD + test connection + refresh tools
- [ ] compose 加 `mock-mcp` 容器（类比 `mock-oidc`）
- [ ] WebUI `/admin/mcp-servers` 管理页
- [ ] E2E 步骤 63
- [ ] 文档：README + HANDOFF + SLICE-VERIFICATION
