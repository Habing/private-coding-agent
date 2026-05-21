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
| L1 | `go test ./internal/auth/... ./internal/modelgw/...` + 新 `quota` 包 |
| L3 增量 | E2E **[43–44]**：quota 超限 429；logout 后旧 JWT 401 |

### 切片 14 — Session ↔ Sandbox

| 项 | 验证 |
|----|------|
| L1 | `go test ./internal/session/... ./internal/sandbox/... -count=1` |
| L3 增量 | E2E **[45]**：POST session 含 `sandbox_id`；WS + `fs.list` 成功 |

### 切片 15 — SSO (OIDC)

| 项 | 验证 |
|----|------|
| L1 | `go test ./internal/auth/... -run OIDC -count=1` |
| L3 增量 | E2E **[46]**：mock OIDC → JWT → `/me` 200 |

### 切片 16 — Enterprise Web

| 项 | 验证 |
|----|------|
| L2 | `cd internal/webui && npm run build` |
| L3 增量 | E2E **[47–48]**：memory 注入 audit；sandbox files list |
| 手工 | `/memories` 页 CRUD；Chat 侧栏文件树 |

### 切片 17 — Skills 12b

| 项 | 验证 |
|----|------|
| L1 | `go test ./internal/skills/... -count=1` |
| L3 增量 | E2E **[49]**：tenant skill → `skill.inject` |
| L3 全量 MVP | **[1–55]** E2E PASS |

**MVP-P1 完成：** README 勾选 13～17；HANDOFF 标 MVP 完成日期。

---

## Full P1（切片 18～23）

### 切片 18 — Sub-Agents

| 项 | 验证 |
|----|------|
| L3 增量 | E2E **[56]**：`agent.delegate` 回灌 |

### 切片 19 — Workflow Engine

| 项 | 验证 |
|----|------|
| L3 增量 | E2E **[57–60]**：发布 workflow → `workflow.<id>` invoke |

### 切片 20 — Reflection

| 项 | 验证 |
|----|------|
| L3 增量 | E2E **[61]**：proposal approve → search 命中 |

### 切片 21 — Orchestration + External MCP

| 项 | 验证 |
|----|------|
| L3 增量 | E2E **[62–63]** |

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
| MVP-P1 | `./test-e2e.sh` | 1–55 |
| Full P1 | `./test-e2e.sh` | 1–70+ |

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
| 18 | 56 |
| 19 | 57–60 |
| 20 | 61 |
| 21 | 62–63 |
| 22 | 64+ |
| 23 | 65+（可选） |
| **Full P1 全量** | **1–70+** |
