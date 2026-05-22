# 项目交接文档 (HANDOFF.md)

| 字段 | 值 |
|---|---|
| 项目名 | Private Coding Agent — 私有化部署的 AI 编码 Agent 平台 |
| 项目根 | `F:\project\private-coding-agent` |
| Git module | `github.com/yourorg/private-coding-agent` |
| 当前日期 | 2026-05-22 |
| 当前 HEAD | `2ace399` *(Slice 19a + 19b 全量 push；E2E 60/60 PASS)* |
| P1 规划 | **已落盘** — [`docs/P1-ROADMAP.md`](docs/P1-ROADMAP.md) |
| 工作区状态 | MVP-P1 17 ✅；Full-P1 18 ✅, 19a ✅, 19b ✅；E2E 60/60 PASS（2026-05-22） |
| 下一阶段 | **Full P1 切片 20–23**（20 Reflection 起步） |

---

## 1. 当前已完成的工作

### 1.1 设计与规划阶段

- 主设计 spec：`docs/superpowers/specs/2026-05-18-private-ai-coding-agent-design.md`
- 每切片独立 spec + plan，全部落盘到 `docs/superpowers/{specs,plans}/`
- P0 切片 1～12 路线已覆盖；P1 见 [`docs/P1-ROADMAP.md`](docs/P1-ROADMAP.md)

### 1.2 已交付切片（P0：切片 1～12）

| 切片 | 状态 | 内容摘要 |
|---|---|---|
| 1 — Foundation | ✅ | Go 骨架 + PG 迁移 + JWT + Gin + OTel + audit_log + compose |
| 1.5 — Hardening | ✅ | ctx-aware Migrate / detached audit / JWT secret 校验 |
| 2 — Sandbox | ✅ | `SandboxRuntime` + `DockerDriver` + Redis 锁 + Reconciler |
| 3 — Model Gateway | ✅ | 多 provider + SSE + ProviderRegistry |
| 4 — Tool Bus | ✅ | 12 内置工具 + `tool_invocations` |
| 5 — Agent Engine | ✅ | ReAct + tool_calls + 上下文压缩 |
| 6 — Session API + WS | ✅ | `/sessions` REST + WebSocket + messages |
| 7 — Memory (basic) | ✅ | `/memories` REST + memory.* 工具 |
| 8 — Web Frontend | ✅ | React SPA embed |
| 9 — Audit 加固 | ✅ | admin `/audit` + 领域事件 |
| 10 — Observability | ✅ | OTel + Prometheus + 结构化日志 |
| 11 — Vector Memory | ✅ | pgvector + 0.92 dedup |
| 12 — Agent Skills (12a) | ✅ | SKILL.md 注入 + `GET /skills` + E2E 40–42 |

### 1.3 测试与验收（P0）

| 维度 | 状态 |
|---|---|
| `go test ./...` | 预期全 PASS |
| `go vet ./...` | 干净 |
| `go build ./...` | 干净 |
| E2E `test-e2e.sh` | 全量 **60 步**（发版 / Gate 前必跑；P0 1–42 + MVP-P1 43–49 + Slice 18 50 + Slice 19a 57–60） |

### 1.4 Gate G1 收口（2026-05-21）

P0 至 Slice 12 全部 push 之后，工作区仍堆了一批跨切片的杂活；本次 Gate 收口拆成 6 个 Conventional Commits 顺序 push：

| Commit | 内容 |
|---|---|
| `6fadd41` | `feat(agent,modelgw,session)`: 流式 Agent 循环 + WS 断连不杀 in-flight run（`context.WithoutCancel`） |
| `70e8cdf` | `feat(db,compose)`: DashScope provider 注册（0012/0013 迁移）+ Qwen env 透传 |
| `7b1dec6` | `feat(webui)`: `assistant_delta` 实时气泡 + 默认模型切到 `dashscope:qwen3.6-plus` |
| `a014d26` | `docs(p1)`: 路线图 + MVP/Full spec + slice 13–23 plan + HANDOFF 刷新 |
| `5690ca9` | `chore`: ignore `.claude/` 与 `deploy/compose/*.json` 临时载荷 |
| `bd21e6d` | `test(e2e)`: 启动期 TRUNCATE 让 vector dedup（步骤 39）跨 run 幂等 |

**Gate 状态**：G1–G4 全部 ✅，`main` 与 `origin/main` 同步，`test-e2e.sh` 42/42 PASS。

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

### 2.2 内部 MCP 工具（17 个）

- `fs.read / fs.write / fs.list / fs.glob / grep / shell.exec`（沙箱内）
- `llm.chat / llm.embed`（Model Gateway 透传）
- `memory.save / memory.search / memory.list / memory.delete`（User scope；Slice 11 后 `memory.search` 支持 `mode=vector|keyword`，`memory.save` 响应携带 `created` bool）
- `agent.delegate`（仅 coding profile；委派子 Run；详见 Slice 18）
- `workflow.create / workflow.update / workflow.list / workflow.get`（admin-only；Agent 在会话里起草 / 改 DSL；publish/delete/invoke 仍只在 admin REST。`coding` + `workflow-authoring` profile 持有）

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

## 3. P1 执行计划（已批准）

**总路线图**：[`docs/P1-ROADMAP.md`](docs/P1-ROADMAP.md)  
**MVP Spec**：[`docs/superpowers/specs/2026-05-21-p1-mvp-enterprise-design.md`](docs/superpowers/specs/2026-05-21-p1-mvp-enterprise-design.md)  
**Full Spec**：[`docs/superpowers/specs/2026-05-21-p1-full-enterprise-design.md`](docs/superpowers/specs/2026-05-21-p1-full-enterprise-design.md)  
**验收**：[`docs/SLICE-VERIFICATION.md`](docs/SLICE-VERIFICATION.md)

### 3.0 Gate（开工 Slice 13 前）

| ID | 项 | 状态 |
|----|-----|------|
| G1 | 工作区未提交改动 commit | ✅ `bd21e6d` 已 push |
| G2 | 本 HANDOFF 与 HEAD 一致 | ✅ |
| G3 | E2E **42/42** + `go test ./...` | ✅（2026-05-21 验证） |
| G4 | P0 缺口归属确认（文件浏览→16，自动沙箱→14） | ✅ 已写入 spec |

### 3.1 MVP-P1（切片 13～17）— 企业试点

| 切片 | 状态 | E2E | Plan |
|------|------|-----|------|
| 13 Enterprise Foundation | ✅ | 43–44 | `plans/2026-05-21-slice-13-enterprise-foundation.md` |
| 14 Session↔Sandbox | ✅ | 45 | `plans/2026-05-21-slice-14-session-sandbox-binding.md` |
| 15 SSO (OIDC) | ✅ | 46 | `plans/2026-05-21-slice-15-sso-oidc.md` |
| 16 Enterprise Web | ✅ | 47–48 | `plans/2026-05-21-slice-16-enterprise-web.md` |
| 17 Skills 12b | ✅ | 49 | `plans/2026-05-21-slice-17-skills-12b.md` |

**MVP 完成后**：E2E **55** 步；可对外「企业试点」。

### 3.2 Full P1（切片 18～23）— 主 spec §11 字面

| 切片 | 状态 | E2E | 说明 |
|------|------|-----|------|
| 18 Sub-Agents + delegate | ✅ | 50 | review/research/workflow-authoring profile + `agent.delegate` + ctx-based RunCtx |
| 19a Workflow Engine | ✅ | 57–60 | YAML DSL + DAG executor (tool/assign/if/foreach/parallel/wait) + `workflow.<slug>` 注册到 ToolBus + Dry-Run mock mutating tools |
| 19b Workflows & Tools Web UI | ✅ | — | `/workflows` admin（Monaco YAML 编辑器 + CRUD + publish/invoke/dry_run + runs 抽屉）+ `/toolbox`（只读工具列表 + Mutating 徽标）；`GET /tools` 暴露 `mutating bool` |
| 20 Reflection | ⬜ | 61 | |
| 21 Orchestration + External MCP | ⬜ | 62–63 | |
| 22 K8s + 安全深化 | ⬜ | 64+ | seccomp、trivy、Snapshot |
| 23 N8N（可选） | ⬜ | 65+ | 需法务确认 |

### 3.3 技术债 ↔ 切片映射

| 项 | 归入 |
|----|------|
| `providers.tenant_id`、quota、rate limit、JWT logout、HTTP 超时 | **13** |
| session 自动建沙箱 | **14** |
| Memory 自动注入、Memory UI、沙箱文件浏览 | **16** |
| Tenant Skills DB | **17** |
| seccomp、trivy、Snapshot、Helm、audit hash chain | **22** |
| Reflection、Workflow、delegate、N8N | **18–23** |
| Hybrid 检索、Project/Tenant memory | **P2** /  backlog |
| 历史 re-embed admin | backlog |

---

## 4. 当前遇到的问题

### 4.1 阻塞性问题

- **Slice 17** 已交付（租户 Skill `skills`/`tenant_profile_skills` 表 + `DBRepo` + `/admin/skills` CRUD + `/admin/profiles/:name/skills` 绑定 + Resolver 合并 FS/DB + `/admin/skills` Web UI + E2E 49）。MVP-P1 完成。
- WebUI 仍无独立沙箱入口；聊天经会话绑定沙箱，工具可继续显式传 `sandbox_id` 覆盖。
- **ivfflat 召回兜底**：`internal/db.Connect` 每条连接 `SET ivfflat.probes = 100`（默认 1 在 E2E 小数据集上漏召 → 步骤 47 `memory.inject` 偶发失败）。生产 lists 调大时需同步上调 probes。

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
- E2E（60 步含切片 13–19a；Full-P1 进行中）~3-8 分钟（首次 build 镜像更久）

---

## 5. 下一步建议

### 5.1 立即（Slice 20 启动前自检）

```bash
cd F:/project/private-coding-agent
go test ./... -count=1
go vet ./...
cd deploy/compose && ./test-e2e.sh   # 期望 60/60 E2E PASS（含切片 19a workflow 链路）
```

### 5.2 已完成

1. ~~**Slice 13** — Foundation~~ ✅
2. ~~**Slice 14** — Session↔Sandbox~~ ✅
3. ~~**Slice 15** — OIDC~~ ✅
4. ~~**Slice 16** — Enterprise Web~~ ✅
5. ~~**Slice 17** — Skills 12b~~ ✅（MVP-P1 收口）
6. ~~**Slice 18** — Sub-Agents + delegate~~ ✅（Full-P1 起步）
7. ~~**Slice 19a** — Workflow Engine~~ ✅（YAML DSL + Bus.Register workflow.<slug> + Dry-Run）
8. ~~**Slice 19b** — Workflows & Tools Web UI~~ ✅（`/workflows` Monaco 编辑 + `/toolbox` 工具列表 + `GET /tools` mutating 标志）

每切片：读 plan → 实现 → 更新 `SLICE-VERIFICATION.md` + E2E 步号 → README 勾选。

### 5.3 Full P1 剩余

按 [`docs/P1-ROADMAP.md`](docs/P1-ROADMAP.md)：**20 → 21**；**22** 视交付压力可提前；**23** 可选。Slice 19a workflow_runs `failed` 行已经成为 Slice 20 Reflection worker 的素材；workflow descriptions 列表也供 Slice 21 Orchestration Router 候选用。

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
