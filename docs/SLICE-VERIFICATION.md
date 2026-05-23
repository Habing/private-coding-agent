# 切片验收清单（每切片完成后执行）

每完成一个切片，按 **三层验证** 收口，再勾选 README 切片进度。

| 层级 | 命令 / 动作 | 通过标准 |
|------|-------------|----------|
| **L1 单元/集成** | `go test ./... -count=1` | 全 PASS |
| **L2 静态/构建** | `go vet ./...`；`go build -o bin/server ./cmd/server`（或 `make build`） | 无报错 |
| **L3 端到端** | 见下文「增量 E2E」 | 输出 `E2E PASS` |

**E2E 前置（每次 L3）：**

```powershell
docker build -t pca/sandbox:base ./sandbox/image   # 首次或沙箱镜像变更后
cd deploy/compose
# .env 不存在时脚本会自动 cp .env.example
./test-e2e.sh    # Git Bash / WSL；Windows 推荐，避免 PS 5.1 stderr 问题
```

脚本会 `docker compose up --build`，结束 `trap` 自动 `compose down`。要保留容器调试时，注释脚本顶部 `trap cleanup EXIT`。

**增量 E2E 原则：** 完成切片 N 后，至少跑到该切片对应的 **最后一步**；P0 发版跑 **1～42**；MVP-P1 跑到 **55**；Full P1 跑到 **70+**（以 `test-e2e.sh` 实际步数为准）。

**P1 路线图：** [`docs/P1-ROADMAP.md`](P1-ROADMAP.md)

---

## 各切片验收范围

### 切片 1 — Foundation

| 项 | 验证 |
|----|------|
| L1 | `go test ./internal/auth/... ./internal/db/... ./internal/httpx/... -count=1` |
| L3 增量 | E2E **[1–3]**：compose 起、demo 用户、登录 JWT |
| 手工 | `curl http://localhost:8080/healthz` → 200；`curl http://localhost:8080/readyz` → 200 |

### 切片 1.5 — Foundation Hardening

| 项 | 验证 |
|----|------|
| L1 | 含 `internal/auth` JWT 校验相关测试 |
| 手工 | 弱 JWT secret 启动应失败；`audit` 写库不拖垮请求（见 slice 1.5 plan） |

### 切片 2 — Sandbox

| 项 | 验证 |
|----|------|
| L1 | `go test ./internal/sandbox/... -count=1` |
| L3 可选 | `go test -tags=docker_integration ./internal/sandbox/... -count=1`（需 Docker + 可选 Redis） |
| L3 增量 | E2E **[4–8]**：建沙箱、写文件、exec、销毁、销毁后 404 |

### 切片 3 — Model Gateway

| 项 | 验证 |
|----|------|
| L1 | `go test ./internal/modelgw/... -count=1` |
| L3 增量 | E2E **[9–12]**：chat 非流/流、embeddings、`model_usage` 有 ok 行 |

### 切片 4 — Tool Bus

| 项 | 验证 |
|----|------|
| L1 | `go test ./internal/toolbus/... -count=1` |
| L3 增量 | E2E **[13–16]**：12 工具列表、fs/shell round-trip、`llm.chat`、`tool_invocations` 有记录 |

### 切片 5 — Agent Engine

| 项 | 验证 |
|----|------|
| L1 | `go test ./internal/agent/... -count=1` |
| L3 增量 | E2E **[17–18]**：`agent.run` final；含 tool_call + tool_result 链 |

### 切片 6 — Session API + WebSocket

| 项 | 验证 |
|----|------|
| L1 | `go test ./internal/session/... -count=1` |
| L3 增量 | E2E **[19–21]**：POST/GET sessions、WS 事件、messages 持久化 ≥2 条 |

### 切片 7 — Memory (basic)

| 项 | 验证 |
|----|------|
| L1 | `go test ./internal/memory/... -count=1` |
| L3 增量 | E2E **[22–25]**：REST CRUD、过滤、`memory.save/search` 工具、DELETE 404 |

### 切片 8 — Web Frontend

| 项 | 验证 |
|----|------|
| L2 | `make build` 或 `cd internal/webui && npm run build` + `go build` |
| L3 增量 | E2E **[26–28]**：SPA `/`、`/login` fallback、`GET /sessions` 为 JSON |
| 手工 | 浏览器 `http://localhost:8080` 登录 demo@example.com / demo123 |

### 切片 9 — Audit

| 项 | 验证 |
|----|------|
| L1 | `go test ./internal/audit/... -count=1` |
| L3 增量 | E2E **[29–32]**：admin `/audit`、按 action 过滤、member 403 |

### 切片 10 — Observability

| 项 | 验证 |
|----|------|
| L1 | 全包 `go test ./...` |
| L3 增量 | E2E **[33–35]**：`/metrics` admin JWT、`pca_*`、scrape token、无鉴权 401 |
| 手工 | Jaeger `http://localhost:16686`、Prometheus `http://localhost:9090`（compose 全栈时） |

### 切片 11 — Vector Memory

| 项 | 验证 |
|----|------|
| L1 | `go test ./internal/memory/... -count=1`（含 dockertest + pgvector 镜像） |
| L3 增量 | E2E **[36–39]**：vector top-1 + score、keyword、dedup `created=false`、新内容 `created=true` |
| 手工 | `docker compose exec postgres psql -U app -d app -c "\\d memories"` 含 `embedding` 列 |

### 切片 12 — Agent Skills (12a)

| 项 | 验证 |
|----|------|
| L1 | `go test ./internal/skills/... ./internal/agent/... -count=1` |
| L3 增量 | E2E **[40–42]**：GET `/skills` 含 `e2e-marker` 与 `platform-coding-standards`；`?include=body`；`agent.run` + `skill_ids` → `skill-marker-ok` |
| 手工 | 配置 `skills.dirs` 指向 `./skills`；检查 system 注入不撑爆 token |

**切片 12 完成后 README：** 勾选「切片 12」；更新 HANDOFF HEAD / commits。

---

## Gate（P1 开工前）

| 项 | 验证 |
|----|------|
| G1 | 工作区无大块未提交 P0/P1 规划外功能 |
| G3 | E2E **[1–42]** 全 PASS |

---

## MVP-P1（切片 13～17）

### 切片 13 — Enterprise Foundation

| 项 | 验证 |
|----|------|
| L1 | `go test ./internal/auth/... ./internal/modelgw/... ./internal/quota/... -count=1` |
| L3 增量 | E2E **[43–44]**：43 沙箱配额 429 + `quota_exceeded`；44 `POST /auth/logout` 后旧 JWT 401 |
| compose | `PCA_QUOTA_SANDBOX_MAX_ACTIVE=1`（默认）供步骤 43；见 `deploy/compose/docker-compose.yml` |

### 切片 14 — Session ↔ Sandbox

| 项 | 验证 |
|----|------|
| L1 | `go test ./internal/session/... ./internal/agent/... -count=1` |
| L3 增量 | E2E **[45]**：POST/GET session 含 `sandbox_id`；WS「list files in workspace」→ `fs.list` + tool 持久化 |
| 注意 | 步骤 21 后 `DELETE /sessions/:id` 释放沙箱，避免与步骤 43 配额冲突 |

### 切片 15 — SSO (OIDC)

| 项 | 验证 |
|----|------|
| L1 | `go test ./internal/auth/... -count=1`（含 `TestOIDC_*`） |
| L3 增量 | E2E **[46]**：`curl -L /auth/oidc/login` → JWT → `GET /me` 200 |
| compose | `mock-oidc` :8082；见 [`deploy/compose/OIDC.md`](../deploy/compose/OIDC.md) |

### 切片 16 — Enterprise Web

| 项 | 验证 |
|----|------|
| L1 | `go test ./internal/memory/... ./internal/session/... ./internal/sandbox/... -count=1` |
| L2 | `cd internal/webui && npm run build` |
| L3 增量 | E2E **[47–48]**：47 首条 user 触发 `memory.inject` audit（含 memory_ids）；48 session sandbox 写文件后 `?list=1` 返回根目录 entries |
| 注意 | pgvector `ivfflat.probes` 在 `internal/db.Connect` 设为 100；E2E 小数据集下默认 1 会漏召 |
| 手工 | `/memories` 页 CRUD；Chat 侧栏文件树 |

### 切片 17 — Skills 12b

| 项 | 验证 |
|----|------|
| L1 | `go test ./internal/skills/... -count=1`（含 `TestDBRepo_*`、`TestAdminHandler_*`、`TestResolver_DB*`） |
| L2 | `cd internal/webui && npm run build` 触达 `/admin/skills` 页 |
| L3 增量 | E2E **[49]**：`POST /admin/skills` 写租户 Skill → `agent.run skill_ids=[...]` 回 `tenant-skill-marker-ok`；profile binding round-trip OK |
| 隔离 | 跨租户 GET `/admin/skills/:key` 返回 404；列表为空 |
| 审计 | `skill.admin.{create,update,delete,profile_bind}` 进 `audit_log` |
| L3 全量 MVP | **[1–49]** E2E PASS |

**MVP-P1 完成：** README 勾选 13～17；HANDOFF 标 MVP 完成日期。

---

## Full P1（切片 18～23）

### 切片 18 — Sub-Agents

| 项 | 验证 |
|----|------|
| L1 | `go test ./internal/agent/... -count=1`（含 `TestDelegateTool_*`、`TestRunCtx*`、`TestHandler_ListProfiles_*`） |
| L2 | `cd internal/webui && npm run build`（Home 页 profile 下拉） |
| L3 增量 | E2E **[50]**：`GET /agent/profiles` 4 项；`agent.run` 用 `E2E_DELEGATE_PARENT_V1` 触发 `agent.delegate` → review 子 Run → `delegate-parent-final: delegate-sub-marker-ok`；audit 含 `agent.delegate.start` + `agent.delegate.complete`（含 `sub_profile=review`） |
| 不变量 | coding profile 工具列表含 `agent.delegate`；review/research/workflow-authoring 都**不含** `agent.delegate`；MaxDelegateDepth=1（ctx 计数 + 子 profile 白名单双保险） |

### 切片 19a — Workflow Engine

| 项 | 验证 |
|----|------|
| L1 | `go test ./internal/workflow/... ./internal/toolbus/... -count=1` |
| L2 | `go build ./...`；migration 0018 up/down 干净 |
| L3 增量 | E2E **[57–60]**：57 CRUD + publish → `GET /tools` 含 `workflow.e2e-demo`；58 `/tools/invoke {tool:"workflow.e2e-demo"}` outputs 含 `said:"hello E2E"` + `workflow_runs` 写 ok 行；59 `agent.run` 用 `E2E_WORKFLOW_V1` 触发 tool_call workflow + audit `workflow.invoke.{start,complete}`；60 `?dry_run=true` 时 shell.exec 节点返 mock JSON `{"dry_run":true,"tool":"shell.exec",...}` + `workflow_runs.dry_run=true` |
| 不变量 | published == 在 Bus 中（startup republish 兜底）；PUT 强制 unpublish；跨租户隔离；MaxSteps=200 / MaxParallelFanout=8 / MaxNestingDepth=8 |
| 审计 | `workflow.admin.{create,update,delete,publish,unpublish}` + `workflow.invoke.{start,complete}` 共 7 个 action |

### 切片 19b — Workflows & Tools Web UI

| 项 | 验证 |
|----|------|
| L1 | `go test ./internal/toolbus/... -count=1`（含 `TestBus_ListTools_MutatingFlag` 断言 mutating bool 字段） |
| L2 | `cd internal/webui && npm install && npm test && npm run build`（产物 ~290 KB main + Monaco 通过 CDN worker 不进 bundle） |
| L3 手工 | (a) admin 访问 `/workflows` → 列表见 `e2e-demo`；进编辑 → Monaco YAML 高亮；改 DSL → 保存（version+=1 + 自动 unpublish）→ 重新 publish；点 ▶ invoke（normal 出 outputs，dry_run on 出 mock JSON）；runs 抽屉显示最近 20 次；删除前确认。 (b) 任意登录用户访问 `/toolbox` → 卡片列出全部 internal 工具；`fs.write` / `shell.exec` / `memory.save` / `memory.delete` / `agent.delegate` 有红色 Mutating 徽标；点 "JSON Schema" 折叠面板展开 schema。 (c) 硬刷新 `/workflows` 与 `/toolbox` 返回 200 SPA（不被 backend 401 抢走）。 (d) 非 admin 访问 `/workflows` → AdminGuard 跳转 `/`；`/toolbox` 仍可见。 |
| 不变量 | `GET /tools` 响应每个 tool 含 `mutating bool`，5 个 mutating 工具值为 true（`fs.write` / `shell.exec` / `memory.save` / `memory.delete` / `agent.delegate`），其余为 false；前端不硬编码工具名清单 |
| 前端路由 | 避开后端已占的 `/admin/workflows`、`/tools`：前端用 `/workflows` 与 `/toolbox`（empty backend → SPA fallback） |

### 切片 20 — Reflection

| 项 | 验证 |
|----|------|
| L1 | `go test ./internal/reflection/... ./internal/session/... ./internal/config/... ./internal/modelgw/... -count=1` |
| L2 | `go build ./...`；migration 0019 up/down 干净；`cd internal/webui && npm test && npm run build`（产物 ~295 KB） |
| L3 增量 | E2E **[61]**：建会话→WS 发 user message→DELETE 归档触发异步 Reflector→`GET /admin/memory-proposals?status=pending` 5–10s 内出现一行（confidence=0.5，mock canned）→`POST /admin/memory-proposals/{id}/approve` 返回 `status=approved` 且 `memory_id` 非空→`/tools/invoke memory.search query="golang generics"` 命中 ≥1 行 |
| 不变量 | (a) `cfg.Reflection.Enabled=false` 时 main.go 不构造 worker/admin handler；(b) channel 满 → `outcome=dropped` 计数 + `ArchiveSession` 不阻塞；(c) confidence ≥ `auto_approve_threshold`（默认 0.85）→ `status=auto_approved` + 同步 memory.Service.Create；< 阈值 → `status=pending` 入审核队列；(d) approve 走 `memory.Service.Create(Source=reflection)`，复用既有 0.92 cosine dedup（`dedup_hit=true/false` 入 audit metadata）；reject 时 `memory_id=NULL`；(e) admin handler 所有路径按 `cl.TenantID` 过滤，跨租户返回 404 |
| 审计 | 5 个 action：`reflection.session.{complete,failed}`、`memory.proposal.{create,approve,reject}`；metric `pca_reflection_proposals_total{outcome=created\|auto_approved\|approved\|rejected\|dropped\|llm_failed}` |
| Mock 协议 | Reflector 系统 prompt 拼接 `REFLECTION_TASK_V1`；mock-provider 看到该 token 返回固定 JSON 数组 `[{"type":"preference","content":"E2E test prefers golang generics","tags":["golang","e2e"],"confidence":0.5}]`，chat + stream 两路一致 |

### 切片 21a — Orchestration Router (Shadow + Hint)

| 项 | 验证 |
|----|------|
| L1 | `go test ./internal/orchestrator/... ./internal/agent/... ./internal/config/... ./internal/modelgw/... -count=1` |
| L2 | `go build ./...`；`go vet ./...` 干净；mock-provider canned 分支无新警告 |
| L3 增量 | E2E **[62]**：`agent.run` 含 `E2E_ORCHESTRATOR_HINT_V1` → mock 返回 `"orchestrator-hint-ok"`（证明 `ORCHESTRATOR_E2E_HINT_DELIVERED` system msg 注入）；`/audit?action=orchestrator.route` 5–10s 内出现一行 `rule_name=e2e-orchestrator-marker` + `matched=true` |
| 不变量 | (a) `cfg.Orchestrator.Enabled=false` 时 main.go 不构造 `NewEngine`，`engine.router` 保持 nil，`Route` 整段 short-circuit；(b) 启动期任意 rule `content_regex` 编译失败 → server boot fail-fast；(c) rule `match` 块空（`profile=[]` + `content_regex=""` + `content_contains=""`）→ `NewEngine` 返回 error；(d) 命中规则时 hint 排在 skills/memory system block 之后、用户消息之前（保留 skills 优先级）；(e) `inject_hint=false` 仅 audit + counter，不修改 messages；(f) no_match 也发 audit + counter（`matched=false, outcome=no_match`），便于影子分析；(g) `Engine.Route` 是纯函数（不发 audit / metric），由 `agent.Engine.recordOrchestratorRoute` 统一发送；(h) 规则按 YAML 顺序首匹胜出，未命中且 `default_hint` 非空 → 注入合成 default 规则 |
| 审计 | 1 个 action：`orchestrator.route`（target=命中规则的 `suggest.target` 或 `""`，metadata `{matched, rule_name, type, matched_on, profile}`）；metric `pca_orchestrator_routes_total{outcome=hit\|no_match\|disabled\|error, target_type=tool\|workflow\|sub_agent\|skill\|""}` |
| Mock 协议 | mock-provider chat + stream 两路看到任意 system message 含 `ORCHESTRATOR_E2E_HINT_DELIVERED` → 直接返回 final `"orchestrator-hint-ok"`，优先级 0a（早于 reflection 0b）；marker 必须在 system message（user message 不触发） |

### 切片 21b — External MCP Manager

| 项 | 验证 |
|----|------|
| L1 | `go test ./internal/mcp/... ./internal/config/... -count=1`（含 client httptest mock、repo dockertest CRUD + tenant 隔离 + tools_cache JSONB round-trip、manager boot republish/refresh/heartbeat/test、mcpTool tenant mismatch + IsMutating annotations 判定、handler 全套 admin REST + token redact + cross-tenant 404 + slug 409 + 503 disabled） |
| L2 | `go build ./...`；`go vet ./...` 干净；`docker build -f internal/mcp/mockserver/Dockerfile .` 通；`cd internal/webui && npm run build` 干净 |
| L3 增量 | E2E **[63]**：admin POST `/admin/mcp-servers` slug=`e2e-mock` url=`http://mock-mcp:8083` → `tools_cache>=1`；GET `/tools` 含 `mcp.e2e-mock.echo`；POST `/tools/invoke` tool=`mcp.e2e-mock.echo` input=`{text:"hi"}` → `output.content[0].text == "echo: hi"`；`/audit?action=mcp.admin.create`、`mcp.tool.invoke` 各 ≥1 行；DELETE 收尾保持幂等 |
| 不变量 | (a) `cfg.MCP.Enabled=false` 时 main.go 不构造 `mcp.NewManager`，但 `AdminHandler` 仍挂载（每条路由返回 503 `mcp_disabled`）；(b) `Manager.Start` boot 期从 `tools_cache` 直接 republish，不调远端 `tools/list`（启动快、远端宕机不阻塞）；(c) 每个 `mcpTool.Invoke` 检查 `runCtx.TenantID == t.tenantID`，跨租户调用直接拒绝（防御性二保险，配合 workflow 同款 Unregister-then-Register 占位竞争）；(d) `auth_token` 在 GET/list/update 响应统一 redact 为 `"***"`；audit metadata 只记 `sha256[:8]` 指纹；(e) `IsMutating()` 读 `Annotations["destructiveHint"]`，缺省保守为 `true`（前端可显示红色徽标）；(f) heartbeat goroutine 60s 一次 `Ping`（即 initialize），失败仅写 `last_error` + counter，不阻塞其他 server；(g) `RefreshTools` 显式触发，更新 `tools_cache` 并重建 Bus prefix `mcp.<slug>.`；`enabled=false` server `RefreshTools` 拒绝 |
| 审计 | 6 个 action：`mcp.admin.{create,update,delete,refresh,enable,disable}` + `mcp.tool.invoke`；3 个 metric：`pca_mcp_invocations_total{server,tool,outcome}`、`pca_mcp_invocation_duration_seconds{server,tool}`、`pca_mcp_heartbeat_total{server,outcome}` |
| Mock 协议 | `internal/mcp/mockserver/main.go` 监听 `:8083`，JSON-RPC 接 `initialize`（返回 `protocolVersion=2024-11-05` + 单工具 `echo` 元信息）/ `tools/list`（返 `[{name:"echo",inputSchema:{type:"object",properties:{text:{type:"string"}},required:["text"]}}]`）/ `tools/call`（参数 `arguments.text="hi"` → `{content:[{type:"text",text:"echo: hi"}]}`）；`/healthz` 供 compose `service_healthy` 等待 |

### 切片 22a — Audit Hash Chain

| 项 | 验证 |
|----|------|
| L1 | `go test ./internal/audit/... -count=1`（含 hash.go 决定性/key-order/RS 分隔/字段敏感/nil UUID/empty metadata/prev_hash 长度兜底 9 测；repo dockertest genesis/chain/concurrent 20×10；verify dockertest clean/tampered metadata/tampered prev_hash/from_id suffix/pre-chain skip/empty table 6 测；handler httptest ok/from_id/bad input/tampered passthrough/member 403/anonymous 401/repo 500 7 测） |
| L2 | `go build ./...`；`go vet ./...` 干净；migration 0021 up/down 通过 dockertest 启动期自动跑通 |
| L3 增量 | E2E **[64]**：`GET /audit/verify` 返回 `ok=true`；`UPDATE audit_log SET metadata` 篡改任意行 → 再 verify → `ok=false, first_broken_id=<id>, reason=entry_hash_mismatch`；恢复原 metadata → verify 再次 `ok=true`；无 token 调 `/audit/verify` → 401 |
| 不变量 | (a) `Repo.Append` 在 `BeginTx → pg_advisory_xact_lock(hashtext('audit_log')) → SELECT prev → INSERT → COMMIT` 序列内完成，跨 goroutine / 跨副本（K8s 多 pod）链不分叉；(b) `occurred_at` 写入前 `Truncate(time.Microsecond)`，确保 hash 输入与 PG timestamptz 存储字节一致；(c) Genesis 行 `prev_hash = 32 零字节`；pre-chain 行（迁移前已存在）`prev_hash + entry_hash` 都是零字节，verify 跳过它们并把首个非零行设为 `chain_start_id`；(d) `Verify(fromID=0)` 强制首链行 prev_hash 必须等于 ZeroHash；`Verify(fromID>0)` 信任首行 prev_hash 做种子，只校验 suffix 内部一致性；(e) `/audit/verify` 自身不入 audit_log，避免递归；admin-only（`auth.Middleware + RequireAdmin`），member→403、无 token→401；(f) 哈希算法硬编码 SHA-256，canonical 编码 = `prev || RS || occurred_at_rfc3339nano_utc || RS || tenant_id || RS || user_id || RS || action || RS || target || RS || method || RS || path || RS || status || RS || duration_ms || RS || canonical_metadata_json`（map[string]any json.Marshal 按 key 升序，nil UUID 编码为空串） |

### 切片 22b — Snapshot → MinIO

| 项 | 验证 |
|----|------|
| L1 | `go test ./internal/objstore/... ./internal/sandbox/... ./internal/config/... -count=1`（含 objstore `New` 配置映射 + 空 bucket 校验；snapshot_repo dockertest Insert/Get round-trip / tenant 隔离 / session 删除后 `session_id` NULL 行仍在 / List `session_id` 过滤；handler httptest Snapshot_Create_OK 201 + DTO、Disabled→503 snapshot_disabled、NotReady→409、NotFound→404、SnapshotList_DisabledNoRepo→503、SnapshotGet_DisabledNoRepo→503、SnapshotList_NoAuth→401；docker_driver `Snapshot_DisabledWithoutDeps`→ErrSnapshotDisabled；config_test 覆盖默认值 + `PCA_SNAPSHOT_*` env 解析） |
| L2 | `go build ./...`；`go vet ./...` 干净；migration 0022 up/down 通过 dockertest 启动期自动跑通；`docker compose -f deploy/compose/docker-compose.yml config` 校验 yaml |
| L3 增量 | E2E **[65]**：(a) 建沙箱 → (b) PUT `/workspace/marker.txt` 内容 `hello-22b` → (c) POST `/sandbox/sessions/$SID/snapshot` 返回 201 + `{id,object_key,size_bytes,image_ref}`；`object_key` 前缀含 `tenant_id/session_id/`、`size_bytes>1000`、`image_ref` 形如 `pca-snapshot-*` → (d) GET `/sandbox/snapshots?session_id=$SID` items 含该 snapshot → (e) DELETE 沙箱 → (f) GET `/sandbox/snapshots/$SNAP_ID` 仍 200，`session_id=null`（FK ON DELETE SET NULL）；audit `?action=sandbox.snapshot.create` 含 target=$SNAP_ID 一行 |
| 不变量 | (a) `cfg.Snapshot.Enabled=false` 时 main.go 不构造 objstore；`docker_driver.Snapshot` 返回 `ErrSnapshotDisabled` → handler 映射 503 `snapshot_disabled`；3 条路由（POST snapshot / GET snapshots / GET snapshots/:id）始终注册（对齐 21b MCP 行为）；(b) 对象 key 布局固定为 `{prefix?}/{tenant_id}/{session_id}/{rfc3339nano}.tar` — tenant 必须为第一段（未来 IAM scoped policy 直接按前缀切），即便 DB `session_id` 被 NULL 也保留原 session 段；(c) tar 端到端流式 — `ImageSave` 返 `io.ReadCloser` 直传 minio-go `PutObject(objectSize=-1, PartSize=64MiB)`，服务端 RSS 与镜像大小无关；(d) `runtime.Snapshot` 流程内 release pgx 连接再做上传，最后再 Acquire 写 DB，防止慢上传长时占用连接池；(e) `docker save` 成功后 `ImageRemove(force=false)` 清理临时镜像，`PCA_SNAPSHOT_KEEP_LOCAL_IMAGE=true` 时保留；`ImageRemove` 失败仅 WARN，不算请求失败；(f) FK `session_id REFERENCES sandbox_sessions(id) ON DELETE SET NULL`：沙箱销毁后快照行保留、`session_id=null`；(g) tenant-scoped 查询：`SnapshotRepo.Get/List WHERE tenant_id=$1` 跨租户 404；(h) handler 列表分页 `limit` 默认 50、上限 200；(i) 启动期 `objstore.EnsureBucket` 幂等（已存在则 NoOp）；MinIO 默认 7 天 multipart abort lifecycle 处理崩溃 orphan parts |
| 审计 | 3 个 action：`sandbox.snapshot.{create,list,get}` 均 `audit.Detached`（5s ctx，不阻塞 hash chain append）；`create` metadata `{object_key,size_bytes,image_ref,session_id}`；`list` metadata `{count,session_id_filter?}`、target 空串；`get` target=snapshot_id |
| 配置/部署 | 新增 `SnapshotConfig{Enabled,Endpoint,Bucket,AccessKey,SecretKey,Region,UseSSL,Prefix,KeepLocalImage}` + `PCA_SNAPSHOT_*` env；compose `minio` service pin `RELEASE.2025-04-08T15-41-24Z`、端口 9000/9001、healthcheck、命名卷 `miniodata`；server `depends_on: minio: service_healthy`；启动期 log `objstore: bucket pca-snapshots ready` |

### 切片 22c — seccomp + trivy CI

| 项 | 验证 |
|----|------|
| L1 | `go test ./internal/sandbox/... ./internal/config/... -count=1`：`LoadSeccompProfile` 返回有效 JSON 字符串且 `defaultAction==SCMP_ACT_ERRNO`；`seccompAllowedSyscalls()` 拍平后断言 `mount/umount/umount2/pivot_root/name_to_handle_at/open_by_handle_at/ptrace/process_vm_readv/process_vm_writev/process_madvise/keyctl/add_key/request_key/bpf/init_module/finit_module/delete_module/kexec_load/kexec_file_load/userfaultfd/perf_event_open` 等 21 个高危 syscall **不在** allow 名单（drift detection）；同时断言 `read/write/openat/close/execve/clone/clone3/fork/exit/exit_group/brk/mmap/munmap/mprotect/socket/connect/accept4/poll/epoll_wait/wait4/getpid/getuid/getgid/setns/unshare/futex` 等 26 个常用 syscall **在** allow 名单（over-trim detection）；`TestSandboxConfig_Defaults` 断言 `Sandbox.SeccompEnabled==true`；`TestSandboxConfig_EnvDisable` 断言 `PCA_SANDBOX_SECCOMP_ENABLED=false` 后 `false` |
| L2 | `go build ./...`；`go vet ./...` 干净；`docker build -t pca/sandbox:base ./sandbox/image` 烟测（trivy workflow 中跑）；二进制内嵌 seccomp.json ~22KB 增量 |
| L3 增量 | E2E **[66]**：(a) 建沙箱（默认 `PCA_SANDBOX_SECCOMP_ENABLED=true`）→ (b) `POST /sandbox/sessions/$SID/exec` cmd=`["mount","-t","tmpfs","none","/tmp/seccomp-probe"]` 期望 `exit_code != 0` 且 `stderr_base64` 解码后匹配 `operation not permitted|permission denied|not permitted`（seccomp 在 syscall 层返回 EPERM）→ (c) 回归保护：`exec sh -c "echo ok > /workspace/seccomp-probe && cat /workspace/seccomp-probe"` 期望 `exit_code==0` 且 stdout 含 `ok`（防止 profile 删过头）→ (d) `DELETE /sandbox/sessions/$SID` 收尾 |
| L3 trivy CI | `.github/workflows/security.yml` 在 PR 修改 `sandbox/image/**` / `.github/workflows/security.yml` / `.trivyignore` 或 push to main 时触发；流程：checkout → `docker build pca/sandbox:base ./sandbox/image` → `aquasecurity/trivy-action@master` CRITICAL（exit-code=1 → 阻塞 merge）→ HIGH（`if: always()`, exit-code=0 → 仅 table）。本次 22c PR 应全绿（debian:12-slim 当前无 CRITICAL CVE） |
| 不变量 | (a) profile 派生自 Docker `moby/v25.0.5/profiles/seccomp/default.json`，沿用 `defaultAction: SCMP_ACT_ERRNO`（默认拒）+ 显式 allowlist 范式；(b) 从 allow 名单 **物理删除** 16 类约 40 个危险 syscall 名（不是新增 `SCMP_ACT_ERRNO` 块），让它们 fall through 到顶层 default deny —— 比"允许后再拒"更不易被绕过；(c) 保留 `setns/unshare/clone3` 因为现代 glibc/Node.js 启动期需要（删了沙箱直接进不去）；(d) `//go:embed seccomp.json` 编译进二进制，零外挂依赖（compose / kubelet 不挂 volume）；启动期 `LoadSeccompProfile` 一次解析并校验，profile 损坏 → `slog.Warn` 后回退（带 fallback 路径但实际 boot 期发现错误也只是不注入 seccomp，不阻 server 启动）；(e) `securityOpts(profile string)` helper：`["no-new-privileges:true"]` 是 baseline；profile 非空时追加 `"seccomp="+profile`；空字符串等价于禁用 seccomp（不退化为 Docker default）；(f) `SandboxConfig.SeccompEnabled` 默认 true，`v.SetDefault("sandbox.seccomp_enabled", true)` 必须在 `ReadInConfig` 前注册以让 viper.AutomaticEnv 绑定 `PCA_SANDBOX_SECCOMP_ENABLED`；(g) 与 `CapDrop ALL` 是 defense-in-depth 双层 —— 即便 `CapAdd` 配置错误塞回 `SYS_ADMIN`，sandbox 内 `mount` 仍被 seccomp 在 syscall 层拒 |
| 审计 | 无新增 audit action（seccomp 是底层 enforcement，不产生应用层事件）。trivy run 失败通过 GitHub Actions check 反馈，不写入 audit_log |
| 配置/部署 | 新增 `SandboxConfig{SeccompEnabled bool}` 顶层段（首次新增 `sandbox.*` 配置树）；`config.example.yaml` 加 `sandbox: { seccomp_enabled: true }` 段附说明；`cmd/server/main.go` boot 期 `if cfg.Sandbox.SeccompEnabled { seccompJSON, err := sandbox.LoadSeccompProfile(); ... }` → 传入 `DockerDriverConfig.SeccompProfile`；新增 `.github/workflows/security.yml`（仓库首个 GitHub Actions workflow）+ `.trivyignore` placeholder；SECURITY-SANDBOX.md §1/§2/§9/§11 改写 |

### 切片 22d1 — K8sDriver Runtime + fake-client L1

| 项 | 验证 |
|----|------|
| L1 | `go test ./internal/sandbox/... ./internal/config/... -count=1`：13 个 `k8s_driver_test.go` fake-clientset 用例覆盖 — pod 元数据 + securityContext 全字段断言 + seccomp 三态（cfg 留空→`RuntimeDefault` / 指定路径→`Localhost`+ `LocalhostProfile` 填、`Unconfined` 显式） + Guaranteed QoS（requests==limits，cpu/memory Quantity canonical 形式 `"2"` / `"1Gi"`） + waitForPodReady 超时（fake reactor 让 pod 永 Pending，Create 在 PodReadyTimeoutSec 后报 deadline + reaper 调 Pods.Delete 回收）+ Get 跨租户 → `ErrSandboxNotFound` + Destroy 二次幂等 + Pods.Delete reactor 计数 + DetachSession 仍被调（SnapshotRepo 类型断言钩通）+ Snapshot tenant scope check 优先 → `ErrSandboxNotFound`，scope 通过 → `ErrSnapshotDisabled` + NetworkMode 三态打到 `pca.network` label + DNSPolicy `ClusterFirst`/`None` 切换；`TestSandboxConfig_DefaultDriver` 断言 `Sandbox.Driver=="docker"`；`TestSandboxConfig_EnvDriverK8s` 断言 `PCA_SANDBOX_DRIVER=k8s` 生效；`TestSandboxConfig_InvalidDriverFailsFast` 断言 `PCA_SANDBOX_DRIVER=podman` 让 `Load()` 报错且 message 含 `sandbox.driver`；`TestHealthz_InfoMerged` 断言 /healthz body 含 `"sandbox"` 顶层 key 且 `driver` 等于注入值 |
| L2 | `go build ./...`；`go vet ./...` 干净；`go mod tidy && git diff --exit-code go.mod go.sum`；K8sDriver 在编译期满足 `sandbox.Runtime`（`var _ sandbox.Runtime = (*K8sDriver)(nil)`）；client-go v0.32.0 vendored 增量 ~30MB go.sum / ~10MB binary |
| L3 增量 | E2E **[67]**：boot 后 `curl -fsS http://localhost:8080/healthz \| jq -r '.sandbox.driver'` 必须返回 `docker`（compose 默认）。K8s 真实跑（in-cluster exec/files/destroy）由 22d2 kind nightly 覆盖 |
| 不变量 | (a) `SandboxConfig.Driver` 默认 `"docker"`，合法值 `"docker"` \| `"k8s"`，非法值 `Load()` fail-fast；(b) `cmd/server/main.go` boot 期 switch — docker → 走老 DockerDriver + Reconciler；k8s → `buildK8sRestConfig`（InCluster=true `rest.InClusterConfig` / false `clientcmd.NewDefaultClientConfigLoadingRules`+ ExplicitPath）→ `kubernetes.NewForConfig` → `NewK8sDriver`；Reconciler 在非 docker driver 下跳过（K8s 模式 reconciliation 留 22d-v2）；(c) `dockerCli` 构造仅在 docker driver 下执行，K8s 模式不依赖 docker socket；(d) `SetSnapshotDeps` 改类型断言保护，K8sDriver 暴露 `SetSnapshotRepo` 让 Destroy 仍能 DetachSession（Snapshot 本身在 K8s 模式直接 `ErrSnapshotDisabled`）；(e) K8sDriver.buildPod 全字段对齐 DockerDriver 硬化矩阵（见 SECURITY-SANDBOX §2.1 等价表）；(f) Pod 名 `pca-sb-<sandbox-uuid 前 12 hex>`，存入 `sandbox_sessions.container_id`（无 schema 变更）；(g) emptyDir{medium:Memory,sizeLimit:1Gi} 给 /workspace + /tmp 模拟 DockerDriver tmpfs；(h) requests==limits → Guaranteed QoS（沙箱不能 noisy-neighbor）；(i) PidsLimit 在 22d1 不写 Pod spec（K8s 1.31+ alpha gate，留 22d-v2，DockerDriver 路径不受影响）；(j) NetworkMode 在 22d1 只打 `pca.network` label + DNSPolicy 切换；真实 egress 隔离要等 22d2 chart 内 NetworkPolicy YAML |
| 审计 | 无新增 audit action（driver 切换是 boot 期决策，沙箱生命周期 audit 走原 `sandbox.create/destroy/exec/snapshot.*` 与 driver 无关） |
| 配置/部署 | 新增 `SandboxConfig.Driver` + `SandboxK8sConfig{Namespace,InCluster,Kubeconfig,ServiceAccount,SeccompLocalhostProfile,PodReadyTimeoutSec}`；`config.example.yaml` 加 `sandbox.driver` + `sandbox.k8s.*` 段附说明；`PCA_SANDBOX_DRIVER` / `PCA_SANDBOX_K8S_*` env 绑定；`httpx.Deps.Info` 新增 map（boot 期 server 注入 `{"sandbox":{"driver":...}}`，/healthz body 合并）；`go.mod` 加 `k8s.io/{api,apimachinery,client-go} v0.32.0` |

### 切片 22d2 — Helm chart + kind nightly + DEPLOY-K8S.md

| 项 | 验证 |
|----|------|
| L1 | `deploy/helm/pca/templates/_helpers.tpl` 的 `pca.assertions` 在 helm 渲染期硬拦截：(a) `secrets.jwtSecret` 空且 `secrets.existing` 空 → fail；(b) `secrets.jwtSecret` 非空且 < 32 字符 → fail；(c) `config.sandbox.k8s.namespace != rbac.sandboxNamespace` → fail；(d) `sandbox.network` 不在 `internal\|bridge\|none` → fail；(e) `config.sandbox.driver` 不在 `docker\|k8s` → fail；本地无 helm 也可 `bash -n deploy/helm/pca/test/kind-e2e.sh` 校验脚本语法 |
| L2 | `helm lint ./deploy/helm/pca` 干净；`helm template ./deploy/helm/pca -f ./deploy/helm/pca/values-kind.yaml \| kubectl apply --dry-run=client -f -` 全部 manifest schema-valid；server Deployment env 段必含 `PCA_DB_PASSWORD`（valueFrom secretKeyRef）+ `PCA_DB_DSN`（值含 `$(PCA_DB_PASSWORD)`）+ `PCA_REDIS_ADDR` + `PCA_AUTH_JWT_SECRET` |
| L3 | `.github/workflows/kind-nightly.yml` 在 `workflow_dispatch` 手动触发 + 03:17 UTC 定时下绿通；`deploy/helm/pca/test/kind-e2e.sh` 六步全 PASS — 1) psql exec bootstrap demo user / 2) port-forward `svc/pca-server :18080` 起来 / 3) 登录 + create sandbox → status=running 且 `kubectl -n pca-sandboxes get pods` 非空 / 4) PUT /files + POST /exec via SPDY 来回 `hello kind` / 5) NetworkPolicy=internal 让 `curl https://1.1.1.1` exit != 0（外网拒）/ 6) DELETE session 后再 exec 必 404 |
| 不变量 | (a) Helm chart `values.config.sandbox.k8s.namespace` 必须 == `values.rbac.sandboxNamespace`（_helpers.tpl assert）；(b) `templates/rbac.yaml` Role 只含 `pods{create,get,list,delete}` + `pods/exec{create}` + `pods/log{get}`，scope 限定 `rbac.sandboxNamespace`；server SA 在自己的 release ns 无任何 K8s API 权限；(c) `db.dsn` / `redis.addr` 不出现在 ConfigMap（viper 不解析 `$(VAR)`）；server 通过 Pod-spec env `$(PCA_DB_PASSWORD)` 在 admission 期由 K8s 展开拼 `PCA_DB_DSN`，viper.AutomaticEnv 让 env 覆盖 config.db.dsn；(d) NetworkPolicy `pca-sandbox-internal` 通过 podSelector `pca.network=internal` 拦截，对 K8sDriver 在 buildPod 打的同名 label 生效；`pca-sandbox-none` 是 `ingress: []` + `egress: []` deny-all；server NP 出站允许 DNS(53) + kube-apiserver(443/6443) + chart-managed PG/Redis(5432/6379)，公网由 `networkPolicy.allowExternalEgress` 控制 |
| 审计 | 无新增 audit action；K8s 部署形态下的沙箱生命周期 audit 仍走原 `sandbox.create/destroy/exec/snapshot.*` |
| 配置/部署 | 新增 `deploy/helm/pca` chart（13 模板 + Chart.yaml + values.yaml + values-kind.yaml + README.md）；新增 `.github/workflows/kind-nightly.yml` 单 job + `deploy/helm/pca/test/kind-config.yaml` + `deploy/helm/pca/test/kind-e2e.sh`（+x）；`docs/DEPLOY-K8S.md` 新增（10 段：部署形态选择 / 前置 / 镜像 / 快速开始 / values 速查 / 生产 checklist / 升级 / 回滚 / Troubleshooting / kind 本地实验）；`docs/DEPLOY.md` §1 形态表加 K8s/Helm 行；`docs/SECURITY-SANDBOX.md` §3.1 注明 K8s + chart RBAC 已替换 docker.sock 妥协路径 |

### 切片 19b — NL Workflow Authoring（B+C，进行中）

| 项 | 验证 |
|----|------|
| L1 Task 1 | 迁移 `0024_workflow_proposals` migrate 成功；`go test ./internal/workflow/... -run Proposal -count=1` PASS |
| L1 Task 3–4 | `workflow.propose` / `workflow.publish` 注册；`GET /agent/workflow/templates`；proposal confirm/approve handler 单测 PASS |
| L2 | `go test ./internal/workflow/... ./internal/agent/... -count=1` |
| L3 | E2E 70–75 **待 Task 5–7** |
| 状态 | **Task 1–4 ✅**（2026-05-23）；Task 5+ Web/orchestrator/E2E 未做 |

### 切片 23 — N8N（可选）

| 项 | 验证 |
|----|------|
| L3 | E2E **[68+]**（若未 skip） |

### Compose Pilot — 单实例运维收口（P2 tech-debt #11–#15）

> 计划：[`docs/P2-COMPOSE-PILOT.md`](P2-COMPOSE-PILOT.md) · 实施：[`superpowers/plans/2026-05-22-compose-pilot-tech-debt.md`](superpowers/plans/2026-05-22-compose-pilot-tech-debt.md)  
> **纪律：每项 Task 改完必跑下表 L1 + compose E2E 69/69，再勾选 plan。**

| 项 | 验证 |
|----|------|
| L1 #11 | `bash -n deploy/compose/backup/*.sh`；Redis `CONFIG GET appendonly` → `yes`；`backup.sh` 产出 `pca-pg-*.dump` |
| L1 #14 | `go test ./internal/workflow/... -run DeleteRunsOlderThan`；server 日志 `workflow retention` |
| L1 #15 | `go test ./internal/reflection/...` 含 `job_repo_test`；迁移 `0023_reflection_jobs` migrate 成功 |
| L1 #12 | `go test ./internal/memory/...`；E2E 步 69 `POST /admin/memories/re-embed` |
| L1 #13 | `go test ./internal/sandbox/...`；E2E 步 68 `POST /sandbox/snapshots/restore/:id` → 读 `marker.txt` |
| L2 | `go test ./... -count=1` + `go vet ./...` |
| L3 | compose `./test-e2e.sh` **69/69** |
| 配置 | `config.example.yaml` + `docs/DEPLOY.md` §8 re-embed / §9 备份 |
| 状态 | **#11–#15 全部 ✅** |

**Full P1 完成：** E2E **≥70**；主 spec §11 核心项 ✅。

---

## 全量回归

| 里程碑 | 命令 | E2E 步号 |
|--------|------|----------|
| P0 / Gate | `./test-e2e.sh` | 1–42 |
| MVP-P1 | `./test-e2e.sh` | 1–49 |
| Full P1（含 18） | `./test-e2e.sh` | 1–50（slice 18 完成后） |
| Full P1（含 19a） | `./test-e2e.sh` | 1–60（slice 19a 完成后） |
| Full P1（含 20） | `./test-e2e.sh` | 1–61（slice 20 完成后） |
| Full P1（含 21a） | `./test-e2e.sh` | 1–62（slice 21a 完成后） |
| Full P1（含 21b） | `./test-e2e.sh` | 1–63（slice 21b 完成后） |
| Full P1（含 22a） | `./test-e2e.sh` | 1–64（slice 22a 完成后） |
| Full P1（含 22b） | `./test-e2e.sh` | 1–65（slice 22b 完成后） |
| Full P1（含 22c） | `./test-e2e.sh` | 1–66（slice 22c 完成后） |
| Full P1（含 22d1） | `./test-e2e.sh` | 1–67（slice 22d1 完成后） |
| Full P1（含 22d2） | `./test-e2e.sh` + nightly `kind-e2e.sh` | compose 1–67 不变；kind 6 步独立 PASS |
| Compose Pilot (#11–#15) | `./test-e2e.sh` + 上表 L1 | compose **69/69** |

```powershell
go test ./... -count=1
go vet ./...
go build -o bin/server ./cmd/server
cd deploy/compose
./test-e2e.sh
```

---

## 常见问题

| 现象 | 处理 |
|------|------|
| Prometheus 镜像拉取失败 | 仅起核心服务：`docker compose up -d postgres redis mock-provider server` |
| E2E 沙箱 not running | 先 `docker build -t pca/sandbox:base ./sandbox/image` |
| 首次 PG 慢 | dockertest / compose 拉 `pgvector/pgvector:pg16` 等待完成 |
| PowerShell 跑 e2e 中断 | 用 Git Bash 执行 `test-e2e.sh` |

---

## 切片 ↔ E2E 步号速查

| 切片 | E2E 步号 |
|------|----------|
| 1 | 1–3 |
| 2 | 4–8 |
| 3 | 9–12 |
| 4 | 13–16 |
| 5 | 17–18 |
| 6 | 19–21 |
| 7 | 22–25 |
| 8 | 26–28 |
| 9 | 29–32 |
| 10 | 33–35 |
| 11 | 36–39 |
| 12 | 40–42 |
| **P0 全量** | **1–42** |
| 13 | 43–44 |
| 14 | 45 |
| 15 | 46 |
| 16 | 47–48 |
| 17 | 49 |
| **MVP-P1 全量** | **1–55** |
| 18 | 50 |
| 19a | 57–60 |
| 19b | — (纯前端 + L1/L2，无 E2E 步号) |
| 20 | 61 |
| 21a | 62 |
| 21b | 63 |
| 22a | 64 |
| 22b | 65 |
| 22c | 66 |
| 22d1 | 67 |
| 22d2 | — (Helm chart + kind nightly；compose 步号不增) |
| 23 | 68+（可选） |
| **Full P1 全量** | **1–70+** |
