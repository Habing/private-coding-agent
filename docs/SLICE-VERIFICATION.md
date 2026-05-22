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

### 切片 21b — External MCP Manager（待启动）

| 项 | 验证 |
|----|------|
| L3 增量 | E2E **[63]** |

### 切片 22 — K8s + 生产安全

| 项 | 验证 |
|----|------|
| L3 | E2E **[64+]** 或 kind nightly |
| 手工 | `helm install` 烟测 |

### 切片 23 — N8N（可选）

| 项 | 验证 |
|----|------|
| L3 | E2E **[65+]**（若未 skip） |

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
| 22 | 64+ |
| 23 | 65+（可选） |
| **Full P1 全量** | **1–70+** |
