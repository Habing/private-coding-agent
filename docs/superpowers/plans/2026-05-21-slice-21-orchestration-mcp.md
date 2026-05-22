# Slice 21 — Orchestration Router + External MCP Implementation Plan

> **Goal:** 路由策略、External MCP 注册与 ListTools；E2E **62–63**。
>
> **Status (2026-05-22):** 拆为 **21a (Router) ✅** + **21b (External MCP) ✅**。两半互相独立（router ~500 行配置 + 一处 agent 集成；External MCP ~2000+ 行新子系统），与 19a/19b 处理方式一致。

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

## 21b — External MCP Manager ✅ (2026-05-22)

**完整 plan：** `C:\Users\HB\.claude\plans\smooth-sprouting-pancake.md`（plan mode 写入；本仓库内联在 git commit 信息 + 本文件下方 Outline）

### 行为

- admin 通过 `/admin/mcp-servers` 注册一个 HTTP MCP server（URL + Bearer + 自定义 header），保存时 Manager 即跑 `Initialize+ListTools`，把每个 tool 以 `mcp.<slug>.<tool>` 注册到 ToolBus
- Agent 调 `mcp.<slug>.<tool>` 透明转发到外部 server 的 `tools/call`，结果原样回 ReAct
- 启动期从 `tools_cache` JSONB 列直接 republish（远端宕机不阻塞启动）；admin 显式 refresh 才重新 `tools/list`
- 60s 心跳 goroutine 仅做 liveness ping，更新 `last_seen_at` / `last_error`，不刷工具列表
- 每次 invoke 走 `Initialize → tools/call` 短连接（stateless，简化错误恢复）
- 跨租户保护：`mcpTool.Invoke` 检查 `runCtx.TenantID == server.TenantID`（防御性二保险，配合 workflow 同款 Unregister-then-Register 占位竞争）

### 不做（推 P2 或 v2）

- stdio transport（HTTP-only；stdio 需要进程管理）
- OAuth flow / token 自动旋转（静态 Bearer 足够）
- Prompts / Resources / Sampling（只做 tools）
- WebSocket / 长连接 session（每次 invoke 都重新 initialize）
- 工具粒度授权（v1 = server 级 enabled 开关 + tenant 隔离 + admin only）
- 跨租户共享 server（每条行强绑 tenant）

### Outline

- [x] 0020 migration `mcp_servers`（tenant_id + slug + url + transport=http + auth_type + auth_token + headers JSONB + enabled + last_seen + last_error + tools_cache JSONB + unique (tenant_id,slug)）
- [x] `internal/mcp/types.go` + `client.go`：2024-11-05 JSON-RPC client（Initialize / ListTools / CallTool / Ping），httptest 覆盖网络超时 / unsupported method / JSON-RPC error code
- [x] `internal/mcp/repo.go`：pgx tenant-scoped CRUD + `UpdateToolsCache` / `UpdateLastSeen` / `UpdateLastError`；dockertest tenant 隔离 + JSONB round-trip
- [x] `internal/mcp/manager.go` + `mcp_tool.go`：Start (boot republish) / RegisterServer / UnregisterServer / RefreshTools / TestConnection / 60s heartbeat goroutine；`mcpTool` 实现 `toolbus.Tool` 并在 Invoke 时做 tenant 校验
- [x] `internal/mcp/handler.go` + `_test.go`：admin REST 9 路由 + token redact + slug 冲突 409 + cross-tenant 404 + 503 disabled
- [x] `internal/mcp/mockserver/main.go` + Dockerfile：单工具 `echo`，JSON-RPC initialize/tools/list/tools/call + `/healthz`
- [x] `internal/config/config.go` `MCPConfig` + `applySlice21bDefaults`（60s heartbeat / 30s invoke / 10s list_tools）
- [x] `cmd/server/main.go`：构造 Manager（Enabled=true 时）+ 总是挂载 AdminHandler（nil mgr 返 503）
- [x] compose 加 `mock-mcp:8083` + healthcheck + server depends_on
- [x] WebUI：`/admin/mcp-servers` 列表 + 表单 + refresh / test / enable / disable / delete + TopBar admin link
- [x] 6 audit action：`mcp.admin.{create,update,delete,refresh,enable,disable}` + `mcp.tool.invoke`
- [x] 3 metric：`pca_mcp_invocations_total` / `pca_mcp_invocation_duration_seconds` / `pca_mcp_heartbeat_total`
- [x] E2E 步骤 63（register → tools/list → /tools 包含 mcp.e2e-mock.echo → invoke → audit 双断言 → cleanup）
- [x] 文档：README + HANDOFF + SLICE-VERIFICATION

### 落地 commits

1. `3897b0e` feat(mcp): types + http json-rpc client + tests
2. `7fb9792` feat(db,mcp): 0020 mcp_servers migration + repo + tests
3. `be8ff85` feat(mcp,metrics): Manager + Bus-side Tool adapter + mock MCP container
4. `7fcb5f4` feat(mcp,api,webui): admin REST + config + compose wiring + WebUI page
5. *(this commit)* test(e2e,docs): step 63 + slice 21b doc closeout

### Acceptance（已通过）

- [x] `go test ./internal/mcp/... -count=1` 全 PASS（client httptest mock / repo dockertest CRUD + JSONB round-trip / manager boot republish + refresh + heartbeat / mcpTool tenant mismatch + IsMutating annotations 判定 / handler 全套 admin REST + token redact + cross-tenant 404 + slug 409 + 503）
- [x] 0020 migration up/down 干净（pgx 自动跑通）
- [x] `cfg.MCP.Enabled=false` 时 Manager 不构造，但 AdminHandler 仍挂载 → 每条路由返回 503 `mcp_disabled`
- [x] 启动期 boot republish 失败（远端宕机）→ 写 `last_error`，不阻塞启动；单个 server initialize 失败不影响其他 server
- [x] `auth_token` 在 GET/list/update 响应统一 redact 为 `"***"`；audit metadata 只记 `sha256[:8]` 指纹
- [x] heartbeat goroutine 60s 跑一次；server 不可达时 `last_error` 更新 + `pca_mcp_heartbeat_total{outcome=fail}` 增计
- [x] WebUI `/admin/mcp-servers` 列表 + 表单 + refresh + test + delete 可用；非 admin 跳转 `/`
- [x] mock-mcp 容器 compose up 健康；JSON-RPC initialize / tools/list / tools/call 都通
- [x] `mcp.<slug>.<tool>` 出现在 `GET /tools` 列表（含 `mutating` flag）
- [x] 跨租户：tenant A 创建的 mcp tool，tenant B 调用 → tenant_mismatch + audit + metric counter
- [x] E2E 63/63 PASS（待 compose 启动后跑验证）
- [x] README + HANDOFF + SLICE-VERIFICATION 三份文档更新

### 与原 plan 的偏差

- **未新增 `toolbus.Tool.OwnerTenant()` 接口**：原 plan 提议给 toolbus 加 owner tenant tag + `HasPrefix` / `UnregisterPrefix`。落地时复用了 workflow 已有的"Unregister-then-Register 后写者胜出 + Invoke 时校验 tenant"模式，零接口改动。代价：跨租户 slug 冲突走"先到先得 + 后到者覆盖"，由 Invoke 时 tenant 校验兜底，与 workflow 行为对齐。
