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
docker compose up -d --build
curl http://localhost:8080/healthz
```

## 端到端验证

```powershell
cd deploy\compose
pwsh ./test-e2e.ps1
```

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
| GET | / | - | SPA 首页（embed 进二进制） |
| GET | /login, /sessions/{id}, /audit | - | SPA 前端路由，由 NoRoute fallback 返回 index.html |

## 内部 MCP 工具

8 个基础工具 + 4 个记忆工具 = 12 个（通过 `GET /tools` 列出）：

- `fs.read / fs.write / fs.list / fs.glob` 沙箱内文件读写
- `grep` 沙箱内全文搜索
- `shell.exec` 沙箱内执行命令
- `llm.chat / llm.embed` 调 Model Gateway
- `memory.save / memory.search / memory.list / memory.delete` 持久化记忆（User scope）

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

## 配置

见 `config/config.example.yaml`。所有字段可用 `PCA_<UPPER>_<UPPER>` 环境变量覆盖。
