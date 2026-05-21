# Private Coding Agent

私有化部署的 AI 编码 Agent 平台。

## 切片进度

- [x] 切片 1：Foundation
- [x] 切片 1.5：Foundation Hardening
- [x] 切片 2：Sandbox Runtime + DockerDriver
- [x] 切片 3：Model Gateway
- [x] 切片 4：Tool Bus + Internal MCP
- [x] 切片 5：Agent Engine
- [x] 切片 6：Session API + WebSocket
- [x] 切片 7：Memory (basic)
- [x] 切片 8：Web Frontend
- [x] 切片 9：Audit Deepening
- [x] 切片 10：Observability (OTel spans + Prometheus + structured logs)
- [x] 切片 11：Vector Memory (pgvector cosine search + 0.92 dedup)
- [x] 切片 12：Agent Skills (SKILL.md registry + Profile/Session/Run 路由 + 系统消息注入)

### P1 — 企业可用（规划已落盘，实施中）

**路线图：** [`docs/P1-ROADMAP.md`](docs/P1-ROADMAP.md)

#### MVP-P1（企业试点）— 切片 13～17

- [ ] 切片 13：Enterprise Foundation（provider 租户隔离、quota、JWT logout）
- [ ] 切片 14：Session ↔ Sandbox 强绑定（创建会话自动建沙箱）
- [ ] 切片 15：SSO (OIDC)
- [ ] 切片 16：Enterprise Web（沙箱文件浏览、Memory 注入/UI）
- [ ] 切片 17：Skills 12b（租户 Skill API + Admin UI）

#### Full P1 — 切片 18～23

- [ ] 切片 18：Sub-Agents + `agent.delegate`
- [ ] 切片 19：Workflow Engine
- [ ] 切片 20：Reflection Agent
- [ ] 切片 21：编排路由 + External MCP
- [ ] 切片 22：K8s Helm + K8sDriver + 安全深化
- [ ] 切片 23：N8N 集成（可选）

## 本地开发

```powershell
# 单元 + 集成测试 (含 dockertest 拉 PG)
go test ./...

# 集成测试 (含真 Docker)
docker build -t pca/sandbox:base ./sandbox/image
go test -tags=docker_integration ./internal/sandbox/...

# 本地直接跑
copy config\config.example.yaml config\config.yaml
go run ./cmd/server --config config\config.yaml
```

## docker-compose 启动

```powershell
docker build -t pca/sandbox:base ./sandbox/image   # 首次必须
cd deploy\compose
copy .env.example .env
# 接真实 LLM：在 .env 填入 DASHSCOPE_API_KEY=sk-...（见 deploy/compose/QWEN.md）
docker compose up -d --build
curl http://localhost:8080/healthz
```

### 阿里云 Qwen 3.6 Plus

- 迁移 `0012` 注册 provider `dashscope` → 百炼 OpenAI 兼容端点
- 对话模型：`dashscope:qwen3.6-plus`
- 详细步骤：[deploy/compose/QWEN.md](deploy/compose/QWEN.md)

## 端到端验证

```powershell
cd deploy\compose
./test-e2e.sh    # Git Bash / WSL，推荐（42 步全量）
# pwsh ./test-e2e.ps1   # 仅覆盖早期切片，完整验收请用 .sh
```

每切片完成后的增量步号与 L1/L2 命令见 [`docs/SLICE-VERIFICATION.md`](docs/SLICE-VERIFICATION.md)。  
P1 开工前完成 **Gate**：E2E 1～42 全绿 + 工作区提交（见 [`HANDOFF.md`](HANDOFF.md) §3.0）。

## 关键端点

| 方法 | 路径 | 鉴权 | 说明 |
|---|---|---|---|
| GET | /healthz | - | 健康检查 |
| GET | /readyz | - | 就绪检查 |
| POST | /auth/login | - | 登录拿 JWT |
| GET | /me | Bearer | 当前用户身份 |
| POST | /sandbox/sessions | Bearer | 创建沙箱 |
| GET | /sandbox/sessions/{id} | Bearer | 查询沙箱 |
| DELETE | /sandbox/sessions/{id} | Bearer | 销毁沙箱 |
| POST | /sandbox/sessions/{id}/exec | Bearer | 执行命令 |
| GET | /sandbox/sessions/{id}/files?path=... | Bearer | 读文件 |
| PUT | /sandbox/sessions/{id}/files?path=... | Bearer | 写文件 |
| POST | /sandbox/sessions/{id}/snapshot | Bearer | (501) |
| POST | /v1/chat/completions | Bearer | OpenAI 兼容,支持 stream |
| POST | /v1/embeddings | Bearer | OpenAI 兼容 |
| GET | /tools | Bearer | 列出 12 个 internal tools |
| POST | /tools/invoke | Bearer | 调用 tool |
| POST | /agent/run | Bearer | ReAct 循环,返回 events 数组 (非流式) |
| POST | /sessions | Bearer | 创建会话 |
| GET  | /sessions | Bearer | 列出当前用户会话 |
| GET  | /sessions/{id} | Bearer | 查询会话 |
| DELETE | /sessions/{id} | Bearer | 归档会话 |
| GET  | /sessions/{id}/messages | Bearer | 列出会话消息 |
| GET  | /sessions/{id}/ws | Bearer | WebSocket 流: 发 user_message,收 event/done/error |
| POST | /memories | Bearer | 创建一条记忆 |
| GET  | /memories | Bearer | 列出当前用户记忆,可按 ?type=&tag=&q= 过滤 |
| GET  | /memories/{id} | Bearer | 查询单条记忆 |
| PUT  | /memories/{id} | Bearer | 更新 content / tags / type |
| DELETE | /memories/{id} | Bearer | 删除一条记忆 |
| GET | /audit | Bearer (admin) | 查询审计日志,支持 action/user_id/from/to/min_status/max_status/limit/offset 过滤 |
| GET | /metrics | Bearer (admin 或 scrape token) | Prometheus exposition,`pca_*` 指标 |
| GET | / | - | SPA 首页（embed 进二进制） |
| GET | /login, /sessions/{id}, /audit | - | SPA 前端路由，由 NoRoute fallback 返回 index.html |

## 内部 MCP 工具

8 个基础工具 + 4 个记忆工具 = 12 个（通过 `GET /tools` 列出）：

- `fs.read / fs.write / fs.list / fs.glob` 沙箱内文件读写
- `grep` 沙箱内全文搜索
- `shell.exec` 沙箱内执行命令
- `llm.chat / llm.embed` 调 Model Gateway
- `memory.save / memory.search / memory.list / memory.delete` 持久化记忆（User scope）

## 记忆子系统

User-scope 持久化记忆，配套 4 个 MCP 工具与 REST 表面。

**检索路径**（`memory.search` / `POST /memories/search`）：

| `mode` | 行为 |
|---|---|
| `vector`（默认） | Gateway 算 query embedding → cosine `<=>` 排序 → 按 score 降序返回 |
| `keyword` | 退回 Slice 7 的 ILIKE + tag overlap + type 过滤 |
| 未设 + query 空 | 自动 keyword（保留 filter-only 场景） |
| 未设 + query 非空 + `embed_on_write=true` | 自动 vector |

`score` 字段仅在 vector path 出现（cosine 相似度，[-1, 1]）。

**去重**（`memory.save` / `POST /memories`）：
- Create 前先对 query embedding 做 top-1 cosine 比对
- 命中 `memory.dedup_threshold`（默认 0.92）→ 不写新行，touch 既有行 `last_used_at`
- 工具响应携带 `created` bool（false 表示 dedup hit），REST 状态码 200 vs 201

**Embedding 维度固定 1536**：迁移列 `vector(1536)` 与 `internal/memory.EmbeddingDim` 必须一致。切换不同维度的模型（如 bge-base 768）需要新 migration + 重建数据；本切片不做运行时维度切换。

**运维 kill switch**：`memory.embed_on_write=false` 时 Create 不算 embedding，Search 永远走 keyword（Slice 7 行为）。

```yaml
memory:
  embedding_model: "default-mock:text"   # 百炼: dashscope:text-embedding-v4（1536 维）
  dedup_threshold: 0.92                  # 0 关闭去重
  embed_on_write: true
```

dev / compose 默认走 mock provider 的 deterministic 1536-d 向量（sha256 → L2-normalize），同 input 必出同向量。切到真实模型后老向量与新向量不可比，需重建。

## Agent Skills

Cursor 风格的"程序化知识"子系统：每个 Skill 是一个 `SKILL.md` 文件（YAML frontmatter + Markdown body），启动期由 `internal/skills.Registry` 递归扫描 `skills.dirs` 加载，Engine 在每次 Run 之前把当次选中的 Skill body 合并成一条系统消息注入。

注入来源的优先级（高 → 低）：

1. Run 级 — `POST /agent/run` body 里的 `skill_ids`
2. Session 级 — `sessions.skill_ids` 列（POST /sessions 时设置）
3. Profile 级 — `Profile.SkillIDs`（如 `coding` 默认带 `platform-coding-standards`）
4. Config 默认 — `skills.default_skill_ids`

高优先级非空即覆盖低优先级（不合并）。每次注入受 `max_skills_per_run`（默认 5）与 `max_injected_chars`（默认 24000）双重约束，超出按顺序截断并在 metrics + audit 里标 `truncated=true`。

只读端点：

- `GET /skills` — 列出已加载的 Skill 元信息（不含 body）
- `GET /skills/:id?include=body` — 显式拿单条 body

Skills 不是工具，不走 Tool Bus；Agent 模型直接在系统消息里读到。生产部署时通过 `skills.dirs` 挂载内部知识库。`skills.enabled=false` 退化为切片 12 之前的纯 Profile 行为。

## Web Frontend

React + Vite + Tailwind + shadcn/ui SPA，源码在 `internal/webui/`，由 `go:embed` 打进 server 二进制。登录、会话列表、Chat（含 WebSocket 流式 + 工具调用折叠卡）。

### 本地开发（前后端分离）

```powershell
# 终端 1: 起后端 (:8080)
go run ./cmd/server --config config\config.yaml

# 终端 2: 起 vite dev server (:5173, 自动 proxy /api /sessions /tools /auth ... 到 :8080)
cd internal\webui
npm install
npm run dev
# 浏览器访问 http://localhost:5173
```

### 生产构建（单二进制）

```powershell
make build         # = make web + go build -o bin/server ./cmd/server
.\bin\server --config config\config.yaml
# 浏览器访问 http://localhost:8080
```

docker-compose 路径会自动跑多阶段 build：`node:20-alpine` 先 `npm run build`，再 `COPY --from=web` 进 Go build。

## 审计

两层并存：

1. **HTTP 访问层** — `audit.Middleware` 给每个请求落一行 `action=http_request`，覆盖全量访问日志。
2. **业务事件层** — 关键 handler/service 显式落领域事件，便于按动作过滤。固定 action 枚举（命名规范：`<domain>.<verb>[.<outcome>]`）：

| Action | Target | Metadata 关键字段 |
|---|---|---|
| `auth.login.success` | user email | `user_id`, `tenant_slug`, `role` |
| `auth.login.failure` | user email | `reason` (`bad_credentials` / `wrong_tenant`) |
| `sandbox.create` | sandbox_id | `image` |
| `sandbox.destroy` | sandbox_id | — |
| `session.create` | session_id | `model`, `profile` |
| `session.archive` | session_id | — |
| `session.ws.open` | session_id | — |
| `session.ws.close` | session_id | `duration_ms` |
| `tool.invoke.error` | tool_name | `error_class` |

`GET /audit` 仅对 `role=admin` 开放（`auth.RequireAdmin` 中间件 + 独立 router group），且永远按 `cl.TenantID` 过滤——不存在跨租户视图。`audit.Sink` 写盘 detached + 5s 超时，业务路径不会被审计写阻塞。

PII 最小化：metadata 不存 prompt 内容、不存 shell 命令原文、不存文件内容，仅存 ID/计数/错误类目；`auth.login.failure` 的 target 字段记 email 是为支持追失败登录所必须。

## 可观测性

三层闭环：

1. **结构化日志** — `internal/logx` 包装 `slog`，JSON handler 默认。`logx.FromCtx(ctx)` 自动注入 `request_id` / `trace_id` / `span_id` / `tenant_id` / `user_id`。日志格式与 level 由 `observability.log_format` / `observability.log_level` 控制（或 `PCA_OBSERVABILITY_LOG_FORMAT` / `_LOG_LEVEL`）。
2. **Trace** — OTel + `otelgin` 自动根 span；以下路径打了手工子 span，便于在 Jaeger 看链路：

   | Span 名 | 包 | 关键属性 |
   |---|---|---|
   | `agent.run` / `agent.step` | `internal/agent` | `agent.model`、`agent.profile`、`agent.max_steps`、`agent.step_index`、`agent.finish_reason` |
   | `tool.invoke` | `internal/toolbus` | `tool.name`、`tool.outcome`、`tool.duration_ms`、`tool.error_class` |
   | `model.chat` / `model.chat_stream` / `model.embed` | `internal/modelgw` | `model.id`、`model.prompt_tokens`、`model.completion_tokens`、`model.input_count` |
   | `sandbox.create` / `sandbox.exec` / `sandbox.destroy` | `internal/sandbox` | `sandbox.image`、`sandbox.id`、`sandbox.exec.cmd_len`、`sandbox.exec.exit_code`、`sandbox.exec.timed_out` |

   compose 启动时把 OTLP 指向 `jaeger:4317`，访问 <http://localhost:16686> 查 trace。

3. **指标 (Prometheus)** — `GET /metrics` 暴露 10 个 `pca_*` 指标：

   | 指标 | 类型 | 标签 |
   |---|---|---|
   | `pca_http_requests_total` | Counter | `method`、`route`、`status_code` |
   | `pca_http_request_duration_seconds` | Histogram | `method`、`route` |
   | `pca_tool_invocations_total` | Counter | `tool`、`outcome` |
   | `pca_tool_invocation_duration_seconds` | Histogram | `tool` |
   | `pca_model_calls_total` | Counter | `model`、`kind`、`outcome` |
   | `pca_model_call_duration_seconds` | Histogram | `model`、`kind` |
   | `pca_model_tokens_total` | Counter | `model`、`direction` (in/out) |
   | `pca_sandbox_active` | UpDownCounter | — |
   | `pca_ws_connections_active` | UpDownCounter | — |
   | `pca_sessions_created_total` | Counter | `profile` |

   `route` 来自 gin 的模板路径（带 `:id` 占位符）保证 cardinality 有界。`/metrics`、`/healthz`、`/readyz` 不进 `pca_http_*` 指标也不进 `audit_log`。

   鉴权双通道：标准 admin JWT 或 `observability.metrics_token` 静态 bearer。Prom scraper 走静态 token（JWT TTL 会让定时抓取过期）。compose 的 `prometheus.yml` 已配好；UI <http://localhost:9090>。**生产环境必须改 `PCA_OBSERVABILITY_METRICS_TOKEN` 与 `prometheus.yml` 里的 token。**

## 配置

见 `config/config.example.yaml`。所有字段可用 `PCA_<UPPER>_<UPPER>` 环境变量覆盖。
