# 项目交接文档 (HANDOFF.md)

| 字段 | 值 |
|---|---|
| 项目名 | Private Coding Agent — 私有化部署的 AI 编码 Agent 平台 |
| 项目根 | `F:\project\private-coding-agent` |
| Git module | `github.com/yourorg/private-coding-agent` |
| 当前日期 | 2026-05-21 |
| 当前 HEAD | `b2eb6bb` (Slice 10 已完成并 push) |
| 累计 commits | 139 |
| 工作区状态 | Slice 11 代码 + 测试 + e2e + 文档已完成，**未 commit**（30 个修改文件 + 6 个新文件） |

---

## 1. 当前已完成的工作

### 1.1 设计与规划阶段

- 主设计 spec：`docs/superpowers/specs/2026-05-18-private-ai-coding-agent-design.md`
- 每切片独立 spec + plan，全部落盘到 `docs/superpowers/{specs,plans}/`
- 11 切片切分路线已全部覆盖（Foundation → … → Vector Memory）

### 1.2 已交付切片（10 个 + 1 个未 commit）

| 切片 | 状态 | HEAD | 内容摘要 |
|---|---|---|---|
| 1 — Foundation | ✅ | `8175d94` | Go 骨架 + PG 迁移 + JWT + Gin + OTel + audit_log + compose |
| 1.5 — Hardening | ✅ | `535b2ad` | ctx-aware Migrate / detached audit / JWT secret 校验 |
| 2 — Sandbox | ✅ | `c4531c1` | `SandboxRuntime` + `DockerDriver` + Redis 锁 + Reconciler |
| 3 — Model Gateway | ✅ | `58a96ee` | 3 provider（OpenAI/Ollama/Claude）+ SSE + ProviderRegistry |
| 4 — Tool Bus | ✅ | — | `Tool` 接口 + JSON Schema 校验 + 8 个内置工具 + `tool_invocations` |
| 5 — Agent Engine | ✅ | — | ReAct 循环 + tool_calls + 上下文压缩 |
| 6 — Session API + WS | ✅ | `0360877` | `/sessions` REST + WebSocket 流式 + `sessions/messages` 持久化 |
| 7 — Memory (basic) | ✅ | `53a451d` | `/memories` REST + 4 个 memory.* 工具（ILIKE 关键字） |
| 8 — Web Frontend | ✅ | `57955d1` | React+Vite+Tailwind+shadcn SPA，embed 进 Go 二进制 |
| 9 — Audit 加固 | ✅ | `3d3e0a2` | admin 查询 + 领域事件落库（auth/sandbox/session/...） |
| 10 — Observability | ✅ | `b2eb6bb` | OTel spans + Prometheus + 结构化日志 + Jaeger/Prom compose |
| 11 — Vector Memory | 🟡 代码完工，**未 commit** | — | pgvector cosine 检索 + 0.92 dedup |

### 1.3 测试与验收

| 维度 | 状态 |
|---|---|
| `go test ./...` | 全 PASS（含 memory / modelgw / toolbus 等 dockertest 包） |
| `go vet ./...` | 干净 |
| `go build ./...` | 干净 |
| E2E `test-e2e.sh` | Slice 10 截止 35 步全过；Slice 11 扩到 **39 步**（尚未运行验证） |

### 1.4 Slice 11 工作区状态（待 commit）

工作树相对于 `b2eb6bb` 的改动：

**新增（6 个）**：
```
internal/db/migrations/0010_memories_embedding.up.sql
internal/db/migrations/0010_memories_embedding.down.sql
internal/memory/embedder.go
internal/memory/embedder_test.go
docs/superpowers/specs/2026-05-21-slice-11-vector-memory-design.md
docs/superpowers/plans/2026-05-21-slice-11-vector-memory.md
```

**修改（30 个）**：
- 核心：`internal/memory/{types,repo,service,handler,errors}.go` + 各 `_test.go`
- 工具：`internal/toolbus/tools/memory.go` + `_test.go`
- 嵌入通道：`internal/modelgw/mockserver/main.go`（deterministic 1536-d）
- 配置：`internal/config/config.go`、`config/config.example.yaml`、`cmd/server/main.go`
- DB：`internal/db/db.go`（pgvector pgx codec 注册 `AfterConnect`）
- 镜像：`deploy/compose/docker-compose.yml`（postgres → pgvector/pgvector:pg16）
- 9 个 `*_test.go` 的 dockertest 镜像同步替换
- E2E：`deploy/compose/test-e2e.sh`（35 → 39 步）
- 文档：`README.md`（切片进度 + 记忆子系统小节）
- 依赖：`go.mod` / `go.sum`（新增 `github.com/pgvector/pgvector-go` + `pgvector-go/pgx`）

**推荐 commit 切分（4 个 Conventional Commits）**：
1. `feat(db,memory): migration 0010 + pgvector codec + Embedder interface + Repo`
2. `feat(memory): Service dedup + Search mode dispatch + handler + tool schema`
3. `feat(mockserver,compose): deterministic 1536-d embed + pgvector compose image`
4. `feat(config,e2e,docs): config wiring + e2e 39 steps + README + slice 11 specs`

---

## 2. 系统能力快照（用户视角）

### 2.1 端点表

| 方法 | 路径 | 鉴权 | 说明 |
|---|---|---|---|
| GET | /healthz, /readyz | - | 健康检查 |
| POST | /auth/login | - | 登录拿 JWT |
| GET | /me | Bearer | 当前身份 |
| POST/GET/DELETE | /sandbox/sessions[/{id}] | Bearer | 沙箱生命周期 |
| POST | /sandbox/sessions/{id}/exec | Bearer | 沙箱内命令 |
| GET/PUT | /sandbox/sessions/{id}/files | Bearer | 沙箱内读写 |
| POST | /v1/chat/completions, /v1/embeddings | Bearer | OpenAI 兼容（支持 SSE） |
| GET | /tools | Bearer | 列出 12 个内置工具 |
| POST | /tools/invoke | Bearer | 调用工具 |
| POST | /agent/run | Bearer | ReAct 循环（非流） |
| POST/GET/DELETE | /sessions[/{id}] | Bearer | 会话生命周期 |
| GET | /sessions/{id}/messages | Bearer | 历史 |
| GET | /sessions/{id}/ws | Bearer (URL token shim) | WebSocket 流式 |
| POST/GET/PUT/DELETE | /memories[/{id}] | Bearer | 记忆 CRUD |
| GET | /audit | Bearer (admin) | 审计日志查询 |
| GET | /metrics | Bearer (admin 或 scrape token) | Prometheus exposition |
| GET | /, /login, /sessions/{id}, /audit | - | SPA shell（NoRoute fallback） |

### 2.2 内部 MCP 工具（12 个）

- `fs.read / fs.write / fs.list / fs.glob / grep / shell.exec`（沙箱内）
- `llm.chat / llm.embed`（Model Gateway 透传）
- `memory.save / memory.search / memory.list / memory.delete`（User scope；Slice 11 后 `memory.search` 支持 `mode=vector|keyword`，`memory.save` 响应携带 `created` bool）

### 2.3 持久化表（10 个迁移）

```
0001 tenants
0002 users
0003 audit_log
0004 sandbox_sessions
0005 providers
0006 model_usage
0007 tool_invocations
0008 sessions + messages
0009 memories
0010 memories.embedding (vector(1536)) + ivfflat partial cosine index   ← Slice 11
```

### 2.4 配置（`config/config.example.yaml`）

```yaml
server:        port / mode / ws_allowed_origins
db:            dsn
redis:         addr
auth:          jwt_secret / jwt_ttl
telemetry:     service_name / otlp_endpoint
observability: log_format / log_level / metrics_token
memory:        embedding_model / dedup_threshold / embed_on_write   ← Slice 11
```

env 覆盖：`PCA_<SECTION>_<FIELD>`，例如 `PCA_MEMORY_DEDUP_THRESHOLD=0.92`

---

## 3. 尚未完成的任务

### 3.1 立即可做（Slice 11 收尾）

1. **跑 e2e 验证**：`cd deploy/compose && ./test-e2e.sh`（期望 39/39 PASS）
2. **commit** 按上述 4 段切分
3. **push** 到 `origin/main`

### 3.2 后续切片（spec/plan 均未开始）

| 候选切片 | 内容 | 触发条件 |
|---|---|---|
| Reflection / Agent-driven memory | confidence 衰减 + 自动 propose + 异步 worker | Slice 11 稳定后 |
| Project / Tenant scope memory | 共享记忆层 + ACL | 多用户协作场景显现 |
| 会话起始自动注入 | session WS handler 在 first user msg 前 prepend 相关 memory | UX 反馈"Agent 忘事"出现 |
| Hybrid 检索 | vector + keyword RRF 融合排序 | vector 召回率不够 |
| Workflow Engine | n8n / 自研控制流原语；spec §11 ADR-3/4 | 多 step 任务编排需求显现 |
| Snapshot 实现化 | sandbox 状态快照 → MinIO | 长时会话/恢复场景 |
| Rate limit | 每租户 LLM 调用配额 | 上线前合规清账 |

### 3.3 累积技术债（按优先级，**不**阻塞 Slice 11 收尾）

#### 待修（建议专题加固期）
- HTTP per-tenant rate limit（防 ToolBus 放大 LLM 流量）
- `providers` 表加 `tenant_id` + Registry 按 tenant 过滤（目前所有租户共享 provider）
- server 容器以 root + 挂 `docker.sock` 的根本风险面（rootless docker / sock-proxy）
- 自定义 seccomp profile（沙箱二级隔离）
- 沙箱镜像 trivy 漏洞扫描接入 CI
- Snapshot 实现化（接 MinIO）
- JWT 吊销列表 / logout
- Audit log hash chain（不可篡改）
- HTTP IdleTimeout / WriteTimeout
- 历史无 embedding 行的一次性 re-embed admin endpoint（Slice 11 显式推后）

---

## 4. 当前遇到的问题

### 4.1 阻塞性问题：无

Slice 11 代码 / 单测 / 集成测试 / 构建 / vet 全过；e2e 待运行。

### 4.2 环境注意事项

| 项 | 说明 |
|---|---|
| Go | Windows 全局 PATH 内可直接 `go version`；项目使用 Go 1.25+ 语法（pgvector-go/pgx 依赖） |
| GOPROXY | `https://goproxy.cn,direct` |
| Docker Desktop | 必须在跑（dockertest、E2E、compose 都依赖） |
| Postgres 镜像 | **Slice 11 起 postgres → `pgvector/pgvector:pg16`**（compose + 9 个 dockertest 文件）。数据卷格式兼容（pgvector 镜像基于 postgres:16），无需迁移 |
| Redis (本地) | dockertest 不自动起；跑 sandbox docker_integration 测试前手动 `docker run -d --rm --name pca-redis-test -p 6379:6379 redis:7-alpine` |
| Docker socket | server 容器以 user 0:0 挂 `/var/run/docker.sock`；distroless nonroot 妥协 |

### 4.3 Windows 特定坑

| 问题 | 规避 |
|---|---|
| PowerShell 5.1 `2>&1` 把 stderr 包成 `NativeCommandError` | E2E 用 `test-e2e.sh`（Git Bash） |
| 主机无 `jq` | E2E 内部已通过 docker run jq 绕过 |
| `LF will be replaced by CRLF` | 正常，可忽略 |

### 4.4 性能 / 资源

- 首次跑 dockertest 启 PG ~10-20s（pgvector 镜像比 postgres:16-alpine 大 ~130MB，首次 pull 慢一些）
- 全包测试（不带 docker_integration tag）~25-60s
- 全包测试 + docker_integration ~3-5 分钟
- E2E（39 步）~3-5 分钟（首次 build 镜像更久）

---

## 5. 下一步建议

### 5.1 立即（Slice 11 收尾）

```bash
cd F:/project/private-coding-agent/deploy/compose
./test-e2e.sh          # 期望 39/39 PASS

cd ..
# 按 §1.4 推荐的 4 段切 commit
git add internal/db/migrations/0010_*.sql internal/db/db.go \
        internal/memory/embedder.go internal/memory/embedder_test.go \
        internal/memory/repo.go internal/memory/repo_test.go \
        internal/memory/errors.go internal/memory/types.go \
        <9 个 dockertest *_test.go> go.mod go.sum
git commit -m "feat(db,memory): ..."
# ... 其余 3 段
git push
```

### 5.2 中期方向（优先级建议）

1. **Reflection Agent / Memory 衰减** — 把 Slice 11 留的 hook（confidence / 自动 propose / re-embed）补上，让记忆系统真正"自我维护"
2. **会话起始自动注入** — UX 影响最直接的小改动（session WS start hook 查 top-K memory prepend）
3. **多租户隔离收紧** — `providers.tenant_id`、per-tenant rate limit；上线前必须做
4. **Workflow Engine** — spec §11 ADR-3 描绘的最后一块大拼图

### 5.3 已知"未做"的设计决策（留给后续）

- Embedding 维度切换工具链（换模型 = 清表 / 重新生成）
- 记忆 confidence / 衰减 / 重排（Reflection 切片）
- Project / Tenant scope memory（多用户共享层）
- Hybrid（vector + keyword RRF）检索融合
- Long-content chunking（当前假设 memory 内容短）

---

## 6. 重要设计决策（ADR 摘录）

### 6.1 架构层面

| ADR | 决策 | 出处 |
|---|---|---|
| ADR-1 | 模块化单体起步，渐进拆分微服务 | 主 spec §11 |
| ADR-2 | Tool Bus 统一抽象，内置能力也 MCP 化 | 主 spec §11 |
| ADR-3 | 控制流原语保留在 Workflow Engine 而非 MCP | 主 spec §11 |
| ADR-4 | Workflow 发布后自动注册为 MCP 工具 | 主 spec §11 |
| ADR-7 | N8N 作为对等服务集成 | 主 spec §11 |
| ADR-53 | pgvector 推后到 Slice 11（已兑现） | Slice 7 spec |
| ADR-58 | Embedding 维度硬编码 1536 | Slice 11 spec |
| ADR-59 | ivfflat lists=100 + partial WHERE embedding IS NOT NULL | Slice 11 spec |
| ADR-60 | Search default vector，`mode=keyword` 显式退回 | Slice 11 spec |
| ADR-61 | Create 0.92 dedup hit → touch + 返回原 id（200，不是 201） | Slice 11 spec |
| ADR-62 | Embedder 同步、失败拒绝；不静默落库无 embedding | Slice 11 spec |
| ADR-63 | Mock embedding sha256+L2-normalize 出 deterministic 1536-d | Slice 11 spec |

### 6.2 哲学层

| 原则 | 实践 |
|---|---|
| 安全先行 | 沙箱 cap_drop + 内网 + 资源限；token 不入库；audit 仅记 sha256 |
| 隔离明确 | `SandboxRuntime` / `Provider` / `Tool` / `Embedder` 接口屏蔽具体实现 |
| 不重复造轮子 | 复用 OpenAI / Anthropic / MCP / pgvector 协议 |
| 测试金字塔 | ~70% 单元 / ~20% 集成（dockertest+httptest） / ~8% E2E / ~2% 在线 |
| Detached ctx 写库 | audit / model_usage / tool_invocations 都用 5s detached ctx |
| Embedder 失败拒绝 | 不静默落库无 embedding 行（避免"看似入库但 vector search 不可见"的隐形分裂） |

---

## 7. 运行和测试命令

### 7.1 一次性环境准备

```powershell
go version            # 应显示 go1.25+
docker --version      # 应显示 Docker 28+
docker compose version
git --version
```

### 7.2 单元 + 集成测试

```bash
cd F:/project/private-coding-agent

# 全部测试（不含真 Docker 集成）
go test ./... -count=1

# vet / build
go vet ./...
go build ./...

# 真 Docker 集成测试
docker run -d --rm --name pca-redis-test -p 6379:6379 redis:7-alpine
go test -tags=docker_integration ./internal/sandbox/... -count=1 -timeout=180s
docker stop pca-redis-test
```

### 7.3 端到端（39 步）

```bash
cd F:/project/private-coding-agent/deploy/compose
docker compose down 2>&1 | tail -1
./test-e2e.sh
# 期望最后输出: E2E PASS
```

39 步覆盖：
- [1-8] 沙箱生命周期
- [9-12] model gateway（chat 非流/流 + embeddings + usage 校验）
- [13-18] tool bus + agent
- [19-21] sessions + WebSocket
- [22-25] memory CRUD + MCP round-trip
- [26-28] SPA fallback
- [29-32] audit
- [33-35] metrics
- [36-39] **Slice 11**：vector ranking / keyword 退回 / dedup hit / dedup miss

### 7.4 本地直接跑

```powershell
Copy-Item config\config.example.yaml config\config.yaml -Force
$env:PCA_AUTH_JWT_SECRET = "dev-only-replace-in-prod-7Hk2wQpL3xRnF8tEsCvBmAyZ"
go run ./cmd/server --config config\config.yaml
```

### 7.5 跑 compose 后手工调端点

```bash
TOK=$(curl -s -X POST http://localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"tenant":"default","email":"demo@example.com","password":"demo123"}' | jq -r .token)

# 创建一条记忆（自动算 embedding）
curl -X POST http://localhost:8080/memories \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"type":"preference","content":"user loves golang generics"}'

# 向量检索（default mode = vector）
curl -X POST http://localhost:8080/tools/invoke \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"tool":"memory.search","input":{"query":"golang"}}'
```

### 7.6 查看持久化数据

```bash
cd F:/project/private-coding-agent/deploy/compose

# 验证 pgvector extension
docker compose exec postgres psql -U app -d app -c "\dx vector"

# memories 表 + embedding 列
docker compose exec postgres psql -U app -d app -c "\d memories"

# 看 LLM 用量（含 embeddings）
docker compose exec postgres psql -U app -d app -c \
  "SELECT provider_type, model, action, status, input_tokens, output_tokens \
   FROM model_usage ORDER BY occurred_at DESC LIMIT 10;"
```

### 7.7 git 状态总览

```bash
cd F:/project/private-coding-agent
git log --oneline | wc -l          # 应显示 139
git status --short                  # Slice 11 改动列表
git diff --stat HEAD                # 累计改动
```

---

## 附录：最近 30 条 commit

```
b2eb6bb docs(slice-10): observability section + plan archive
dec7c54 feat(compose): wire jaeger + prometheus + e2e 35 steps
ec179b8 feat(observability): OTel spans + Prometheus metrics
ed0b7ab feat(observability): structured logging + request-id middleware
3d3e0a2 feat(audit): slice 9 — admin audit query + domain event instrumentation
f626093 test(e2e): step 28 uses GET to assert API content-type
c064f09 fix(httpx): SPA fallback treats HEAD as GET
57955d1 docs: mark slice 8 complete and document web frontend
5a3b8c4 feat(server): embed web UI and serve SPA from Go binary
f041887 feat(webui): WebSocket chat streaming with tool-call cards
d2eac49 feat(web): chat page scaffold with history fetch and Composer
25cbb7e feat(web): session list with create/delete + Home shell
ca4625c feat(web): login page with auth flow
c21e0d8 feat(web): types, api client, auth store, router shell
cfc450a feat(web): Vite+React+TS+Tailwind+shadcn scaffold
fa07f5f feat(server): WS ?token= shim + SPA fallback for embedded web UI
9e3a7a4 docs(web): slice 8 design spec + formal plan
384f590 docs(memory): formal slice 7 implementation plan
53a451d docs: mark slice 7 complete and document /memories endpoints + memory tools
8d2a5fe test(e2e): extend to 25 steps with /memories CRUD + MCP round-trip
440a9a8 feat(memory): wire Service + REST handler + 4 MCP tools into main
82617e3 feat(memory): internal MCP tools memory.{save,search,list,delete}
a2a622f feat(memory): REST CRUD handler for /memories endpoints
cf27aa8 feat(memory): Service layer with validation + cross-tenant safety
3a43206 feat(memory): Repo with dockertest coverage (CRUD + search + last_used_at touch)
5235647 feat(db): migration 0009 memories table
e3623f2 docs(memory): design spec for slice 7 (basic memory)
0360877 fix(e2e): route websocat via compose network
5ddf401 docs(plan): slice 6 formal implementation plan
a5fd48b docs: mark slice 6 complete in README and add /sessions endpoints
```

完整历史：`git log --oneline | head -139`
