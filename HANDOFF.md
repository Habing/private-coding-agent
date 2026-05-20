# 项目交接文档 (HANDOFF.md)

| 字段 | 值 |
|---|---|
| 项目名 | Private Coding Agent — 私有化部署的 AI 编码 Agent 平台 |
| 项目根 | `D:\IdeaProjects\private-coding-agent` |
| Git module | `github.com/yourorg/private-coding-agent` |
| 当前日期 | 2026-05-20 |
| 当前 HEAD | `3e9b4ce` (Slice 4 plan 已落盘,实施未开始) |
| 累计 commits | 76 |

---

## 1. 当前已完成的工作

### 1.1 设计与规划阶段（全部完成）

- 主设计 spec：`docs/superpowers/specs/2026-05-18-private-ai-coding-agent-design.md`（含 §1-§13 ADR 30+）
- Slice 1 / 2 / 3 / 4 各自的 spec + plan，全部落盘
- 整体 9 切片切分路线确定（Foundation → Sandbox → Model Gateway → Tool Bus → Agent → Session → Memory → Web → Integration）

### 1.2 已交付切片（4 个，可独立运行）

#### Slice 1 — Foundation（HEAD `8175d94`）
- Go 项目骨架 + PG 迁移 + JWT 登录 + Gin HTTP + OpenTelemetry + audit_log + docker-compose
- 表：`tenants` `users` `audit_log` + `schema_migrations`
- 端点：`/healthz` `/readyz` `/auth/login` `/me`
- 主服务镜像 + distroless 运行时

#### Slice 1.5 — Foundation Hardening（HEAD `535b2ad`）
- `db.Migrate(ctx, dsn)` 接收 context
- `audit.Middleware` 用 detached ctx + 5s timeout 防请求 ctx 取消
- `ValidateJWTConfig` 启动期拒绝默认 / 弱 JWT secret（最小 32 字节）
- `NewJWT` 内置二次防御 panic

#### Slice 2 — Sandbox Runtime + DockerDriver（HEAD `c4531c1`）
- `internal/sandbox` 包 + `SandboxRuntime` 接口 + `DockerDriver` 实现
- 沙箱基础镜像 `pca/sandbox:base`（1.13 GB，含 Go/Node/Python/git/rg/jq）
- 表：`sandbox_sessions`
- 端点：`POST/GET/DELETE /sandbox/sessions[/{id}]` + `/exec` + `/files` + `/snapshot`(501 stub)
- 安全配置：`ReadonlyRootfs` + tmpfs + `CapDrop ALL` + `no-new-privileges` + PIDsLimit + 默认 `--internal` 网络
- Redis 分布式锁（按沙箱 ID 锁定 destroy）
- 启动期 Reconciler 清理脏数据

#### Slice 3 — Model Gateway（HEAD `58a96ee`）
- `internal/modelgw` 包 + `Provider` 接口 + `ProviderRegistry`（DB 驱动，60s refresh）
- 3 provider 实现：`OpenAIProvider` / `OllamaProvider`（薄包装） / `ClaudeProvider`（含 Anthropic 协议双向适配 + SSE 流式状态机）
- 表：`providers` `model_usage`
- 端点：`POST /v1/chat/completions`（支持 SSE 流式） + `POST /v1/embeddings`
- `mock-provider` 服务（compose 内置，无外部依赖即可跑 E2E）
- Token 计量从 provider usage 字段读

### 1.3 测试与验收

| 维度 | 状态 |
|---|---|
| `go test ./... -count=1` | 全 PASS（11 包） |
| `go test -tags=docker_integration ./...` | PASS（含真 Docker 沙箱 + httptest provider） |
| `go vet / build` | 干净 |
| E2E 脚本 `test-e2e.sh` | 12 步全过：登录 → 沙箱建/写/exec/销 → /v1/chat/* 非流 + 流 + /v1/embeddings → model_usage 校验 |
| 提交粒度 | 76 commits，每 Task 1-2 commit，git tree clean |

### 1.4 设计 + 实施 plan 已就绪但**未开始实施**

- Slice 4 spec：`docs/superpowers/specs/2026-05-20-slice-04-toolbus-design.md`（510 行）
- Slice 4 plan：`docs/superpowers/plans/2026-05-20-slice-04-toolbus.md`（3082 行，14 Task）

---

## 2. 修改过的文件（按目录组织）

### 2.1 项目根

```
.gitignore               # 标准 Go + IDE + docker 忽略
.dockerignore            # 含 sandbox/image 排除
README.md                # 切片进度 + 端点表 + 启动指引
Dockerfile               # 主服务镜像（多阶段 + distroless）
go.mod / go.sum          # 直接依赖见 2.5
HANDOFF.md               # 本文件
```

### 2.2 cmd/server/

```
main.go                  # 装配:config/db/auth/audit/otel/sandbox/modelgw
                         # 用 run() error 模式;所有失败路径都 defer 执行
```

### 2.3 config/

```
config.example.yaml      # server/db/redis/auth/telemetry 五段配置
```

### 2.4 deploy/compose/

```
docker-compose.yml       # 4 服务:postgres / redis / mock-provider / server
                         # server 容器挂 /var/run/docker.sock 调主机 daemon
.env.example             # 含 32 字符 dev JWT secret
test-e2e.sh              # 12 步 bash E2E(主用)
test-e2e.ps1             # PowerShell 版(Windows 备用)
```

### 2.5 internal/

```
audit/                   # 审计中间件 + InvocationRepo + Recorder
auth/                    # JWT + middleware + handler + config(ValidateJWTConfig)
config/                  # viper YAML+env 加载,字段:Server/DB/Redis/Auth/Telemetry
db/                      # pgxpool + embed.FS + golang-migrate
  migrations/            # 6 个迁移:
    0001_create_tenants
    0002_create_users
    0003_create_audit_log
    0004_create_sandbox_sessions
    0005_create_providers
    0006_create_model_usage
httpx/                   # Gin engine + healthz/readyz + me + Recovery
modelgw/                 # ~2000 行,Slice 3 最大块
  types/validate/provider.go/registry.go/repo.go/recorder.go
  gateway.go / sse.go / handler.go
  provider_openai/ollama/claude.go
  claude_translate.go / claude_stream.go
  mockserver/             # 50 行 OpenAI 兼容 mock(compose build)
sandbox/                 # ~1400 行,Slice 2
  types/validate/path/runtime.go/sessionrepo.go
  docker_driver.go / docker_driver_exec.go / docker_driver_fs.go
  reconciler.go / handler.go
telemetry/               # OpenTelemetry trace + metrics provider
tenant/                  # tenant 模型 + repo + Lookup adapter
user/                    # user 模型 + repo + bcrypt service
```

### 2.6 sandbox/image/

```
Dockerfile               # debian:12-slim + git/curl/jq/tree/rg/Go/Node/Python
README.md                # 工具清单 + 大小说明 + trivy 提示
```

### 2.7 docs/superpowers/

```
specs/
  2026-05-18-private-ai-coding-agent-design.md       # 主 spec
  2026-05-18-private-ai-coding-agent-design.md       # Foundation
  2026-05-18-slice-02-sandbox-design.md
  2026-05-20-slice-03-model-gateway-design.md
  2026-05-20-slice-04-toolbus-design.md              # ← Slice 4

plans/
  2026-05-18-slice-01-foundation.md
  2026-05-18-slice-1-5-hardening.md
  2026-05-18-slice-02-sandbox.md
  2026-05-20-slice-03-model-gateway.md
  2026-05-20-slice-04-toolbus.md                     # ← Slice 4(待实施)
```

---

## 3. 尚未完成的任务

### 3.1 立即可开工：Slice 4 实施（14 Task）

Plan 位置：`docs/superpowers/plans/2026-05-20-slice-04-toolbus.md`

| Task | 内容 |
|---|---|
| 0 | Slice 3 carry-over：`internal/modelgw/redact.go` + 4 处 ProviderError 改写 |
| 1 | `internal/toolbus/tool.go` + `errors.go`：Tool 接口 + 4 个错误哨兵 |
| 2 | `internal/toolbus/registry.go`：Registry 注册器（线程安全 + 排序 List） |
| 3 | `internal/toolbus/schema.go`：JSON Schema 编译 + 校验（santhosh-tekuri/jsonschema/v6） |
| 4 | migration 0007 `tool_invocations` + `InvocationRepo` + `InvocationRecorder` + dockertest |
| 5 | `internal/toolbus/bus.go`：Bus 编排（取 tool → schema 校验 → sha256 → invoke → record） |
| 6 | `internal/toolbus/tools/fs.go`：fs.read / fs.write / fs.list / fs.glob 共 4 个工具 |
| 7 | `internal/toolbus/tools/grep.go`：grep（ripgrep --json 解析） |
| 8 | `internal/toolbus/tools/shell.go`：shell.exec（sandbox.Runtime.Exec 透传） |
| 9 | `internal/toolbus/tools/llm.go`：llm.chat / llm.embed（modelgw.Gateway 透传） |
| 10 | `internal/toolbus/handler.go`：HTTP `/tools` + `/tools/invoke` + 错误映射 |
| 11 | `cmd/server/main.go` 装配 + 注册 8 个 tool |
| 12 | integration tests（docker_integration tag）— fs / shell / llm 真链路 |
| 13 | E2E 扩展（[13/16]-[16/16] 4 步）+ README 勾选 |

执行方式：subagent-driven（每 Task 用 implementer subagent + spec/quality review），已在 Slice 1-3 用得很顺。

### 3.2 后续切片（spec/plan 都未开始）

| 切片 | 内容 | 状态 |
|---|---|---|
| Slice 5 | Agent Engine（ReAct 循环 + tool calling + 上下文压缩 + 流式事件） | 未规划 |
| Slice 6 | Session API + WebSocket（前端真聊起来的入口） | 未规划 |
| Slice 7 | Memory（user/project profile + 上下文注入 + pgvector） | 未规划 |
| Slice 8 | Web Frontend（React UI） | 未规划 |
| Slice 9 | Integration & Audit 加固 | 未规划 |

### 3.3 累积技术债（按优先级，**不**阻塞 Slice 4）

#### 已修
- Slice 1 review 17 条 → Slice 1.5 修了 3 条最关键的
- Slice 2 review → Slice 3 Task 0 修了 sandbox stdin/inspect/trivy 共 3 条
- Slice 3 review → Slice 4 Task 0 将修 ProviderError redact 1 条

#### 待修（建议 Slice 9 收尾或专题加固期）
- HTTP per-tenant rate limit（防 ToolBus 放大 LLM 流量）
- providers 表加 `tenant_id` 列 + Registry 按 tenant 过滤
- server 容器以 root + 挂 docker.sock 的根本风险面（考虑 rootless docker 或 sock-proxy）
- 自定义 seccomp profile（沙箱二级隔离）
- 沙箱镜像 trivy 漏洞扫描接入 CI
- Snapshot 实现化（接 MinIO，Slice 7 起做）
- JWT 吊销列表 / logout
- Audit log hash chain（不可篡改）
- HTTP IdleTimeout / WriteTimeout

---

## 4. 当前遇到的问题

### 4.1 阻塞性问题：无

项目目前一切正常：测试全过、E2E 通、git tree clean、依赖未变。

### 4.2 环境注意事项

| 项 | 说明 |
|---|---|
| Go 1.26.3 | 安装在 `D:\tools\go`，PATH 已写入用户级注册表；新开 PowerShell 应能直接 `go version` |
| GOPROXY | 已设 `https://goproxy.cn,direct`（国内网络） |
| GOSUMDB | 已设 `sum.golang.org` |
| Docker Desktop | 必须在跑（dockertest、Slice 2/3 集成测试、E2E、compose 都依赖） |
| Redis (本地) | dockertest 不自动起；跑 sandbox docker_integration 测试前手动 `docker run -d --rm --name pca-redis-test -p 6379:6379 redis:7-alpine` |
| Docker socket | server 容器以 user 0:0 挂 `/var/run/docker.sock`；distroless nonroot + sock 权限冲突的妥协；Slice 9 重新设计 |

### 4.3 Windows 特定坑

| 问题 | 规避 |
|---|---|
| PowerShell 5.1 `2>&1` 把 stderr 包成 `NativeCommandError` 导致 `ErrorActionPreference=Stop` 中断 | E2E 用 `test-e2e.sh`（Git Bash）跑；ps1 版只是参考 |
| `pwsh` 不在 PATH（仅 Windows PowerShell 5.1） | 用 `& ./test-e2e.ps1` 或直接 sh 版 |
| 主机无 jq | `test-e2e.sh` 内已通过 docker run jq 镜像绕过 |
| Bash 子 shell 偶发外网超时（goproxy 不在 PATH） | implementer subagent 已学会用 PowerShell 装依赖 |
| `LF will be replaced by CRLF` git 警告 | 正常，可忽略 |

### 4.4 性能 / 资源

- 首次跑 dockertest 启 PG ~10-20s（拉镜像）
- 首次跑 sandbox integration 启沙箱容器 ~6s/个
- 全包测试（不带 docker_integration tag）~25-40s
- 全包测试 + docker_integration ~3-5 分钟

---

## 5. 下一步建议

### 5.1 优先推进 Slice 4（已就绪）

```bash
# 复用既有 subagent-driven 流程
# 每个 Task 一个 implementer + 二阶段 review
# 14 Task 估计耗时 ~3-5 小时(连续推进,不停)
```

按 Slice 1-3 的节奏：每个 Task 我（控制层）派一个 implementer subagent，完成后派 spec reviewer + code quality reviewer，有问题派 fix subagent，无问题进下一 Task。

### 5.2 Slice 4 完成后建议的顺序

1. **Slice 5（Agent Engine）** — 这是产品价值真正显现的切片；Tool Bus 是它的基础
2. **Slice 6（Session WebSocket）** — Agent 跑起来必须有流式 UI 通道
3. **Slice 8（Web Frontend）** — 让你能用浏览器直接聊
4. **Slice 7（Memory）** — 长期记忆
5. **Slice 9（Integration & Audit 加固）** — 上线前清账

> 这个顺序优先"看得见的能力"；如果是企业 IT 推内部使用，Slice 7 可提前到 Slice 5/6 之间。

### 5.3 立即可做的小决策

- 是否接受 Slice 4 plan 给 8 个 tool 的内容；要不要增删？现在改 plan 比实施完改便宜
- Slice 5 Agent 用什么 Agent 框架？倾向 vanilla Go 自研（与 `modelgw.Gateway` 直接对接走 tool_calls），不引 LangChain Go / eino，但你拍板

---

## 6. 重要设计决策（ADR 摘录）

### 6.1 架构层面

| ADR | 决策 | 出处 |
|---|---|---|
| ADR-1 | 模块化单体起步，渐进拆分微服务 | 主 spec §11 |
| ADR-2 | Tool Bus 统一抽象，内置能力也 MCP 化 | 主 spec §11 |
| ADR-3 | 控制流原语保留在 Workflow Engine 而非 MCP | 主 spec §11 |
| ADR-4 | Workflow 发布后自动注册为 MCP 工具 | 主 spec §11 |
| ADR-7 | N8N 作为对等服务集成，不进我们的进程 | 主 spec §11 |

### 6.2 已落地切片的决策

| 决策 | 位置 |
|---|---|
| 多租户 schema 从 day 1 加 tenant_id，P0 默认单租户部署 | Slice 1 |
| 默认本地 Ollama + 可选 API 双路 | Slice 1 spec |
| Sandbox 接口（`Runtime`）屏蔽 Docker / K8s，DockerDriver 先做 | Slice 2 |
| `Tool Bus` 统一所有调用入口，内置能力也按 MCP 协议 | Slice 4 |
| Sandbox 默认 `--internal` 网络（无外网）+ ReadonlyRootfs + tmpfs | Slice 2 |
| 销毁 sandbox 用 Redis owner-tagged 锁 + Lua 释放 | Slice 2 |
| 内部协议用 OpenAI Chat Completions（Claude 内部做协议转换） | Slice 3 |
| `provider:model` 显式前缀路由（不查表） | Slice 3 |
| API key 走 `api_key_env` 环境变量名引用，DB 不存密钥 | Slice 3 |
| Token 计量只读 provider usage 字段 | Slice 3 |
| `ProviderError.Body` 截 4KB；env value redact（Slice 4 Task 0） | Slice 3+4 |
| Tool 接口 in-process Go 调用（不走 MCP JSON-RPC） | Slice 4 |
| Tool input/output 仅记 sha256，不存内容 | Slice 4 |
| JSON Schema 手写（OpenAI tool calling 兼容） | Slice 4 |
| `sandbox_id` 作为 input args 一员（Tool 不持 session 状态） | Slice 4 |

### 6.3 哲学层

| 原则 | 实践 |
|---|---|
| 安全先行 | 沙箱 cap_drop + 内网 + 资源限；token 不入库；audit 仅记 sha |
| 隔离明确 | `SandboxRuntime` / `Provider` / `Tool` 三个接口屏蔽具体实现 |
| 不重复造轮子 | 大量复用 OpenAI 协议 / Anthropic 官方协议 / MCP 设计；Slice 5+ 才考虑自研 |
| 测试金字塔 | 70% 单元 / 20% 集成（dockertest+httptest）/ 8% E2E / 2% 在线 |
| Detached ctx 写库 | audit / model_usage / tool_invocations 都用 5s detached ctx |

---

## 7. 运行和测试命令

### 7.1 一次性环境准备

```powershell
# Windows PowerShell（管理员或普通都行）
$env:Path = [Environment]::GetEnvironmentVariable('Path','Machine') + ';' + [Environment]::GetEnvironmentVariable('Path','User')
go version            # 应显示 go1.26.3 windows/amd64
docker --version      # 应显示 Docker 28+
docker compose version
git --version
```

如 Go 找不到：
- 查 `D:\tools\go\bin\go.exe` 是否存在
- 重启 PowerShell 窗口让 PATH 生效

### 7.2 单元 + 集成测试

```bash
cd D:/IdeaProjects/private-coding-agent

# 全部测试（不含真 Docker 集成）
PATH="/d/tools/go/bin:$PATH" go test ./... -count=1

# vet / build
PATH="/d/tools/go/bin:$PATH" go vet ./...
PATH="/d/tools/go/bin:$PATH" go build ./...

# 真 Docker 集成测试（需 Docker Desktop + Redis 本地容器）
docker run -d --rm --name pca-redis-test -p 6379:6379 redis:7-alpine
PATH="/d/tools/go/bin:$PATH" go test -tags=docker_integration ./internal/sandbox/... -count=1 -timeout=180s
PATH="/d/tools/go/bin:$PATH" go test -tags=docker_integration ./internal/modelgw/... -count=1
# 跑完后清理
docker stop pca-redis-test
```

### 7.3 端到端（最有说服力的演示）

```bash
cd D:/IdeaProjects/private-coding-agent/deploy/compose
docker compose down 2>&1 | tail -1   # 清旧状态
./test-e2e.sh                        # Git Bash 跑
# 期望最后输出: E2E PASS
```

12 步链路：
1. compose up（postgres + redis + mock-provider + server 4 容器）
2. 建 demo 用户
3. 登录拿 JWT
4-7. 沙箱：建 / 写文件 / exec cat / 销
8. 验证已销毁沙箱返 404
9. `/v1/chat/completions` 非流式
10. `/v1/chat/completions` 流式（SSE）
11. `/v1/embeddings`
12. 校验 `model_usage` 表有 status=ok 行

### 7.4 本地直接跑（不用 compose）

```powershell
# 假设 postgres 起在 localhost:5432
Copy-Item config\config.example.yaml config\config.yaml -Force
$env:PCA_AUTH_JWT_SECRET = "dev-only-replace-in-prod-7Hk2wQpL3xRnF8tEsCvBmAyZ"
go run ./cmd/server --config config\config.yaml
```

### 7.5 跑 compose 后手工调端点

```powershell
# 登录
$body = '{"tenant":"default","email":"demo@example.com","password":"demo123"}'
$tok = (Invoke-RestMethod -Method POST -Uri http://localhost:8080/auth/login `
        -ContentType application/json -Body $body).token
$H = @{ Authorization = "Bearer $tok" }

# 用 OpenAI SDK 兼容方式调
Invoke-RestMethod -Method POST -Uri http://localhost:8080/v1/chat/completions `
    -Headers $H -ContentType application/json -Body @'
{"model":"default-mock:gpt-4o","messages":[{"role":"user","content":"hi"}]}
'@
```

### 7.6 查看持久化数据

```powershell
cd D:\IdeaProjects\private-coding-agent\deploy\compose

# 表清单
docker compose exec postgres psql -U app -d app -c "\dt"
# 期望:tenants users audit_log sandbox_sessions providers model_usage schema_migrations

# 最近审计
docker compose exec postgres psql -U app -d app -c "SELECT method, path, status FROM audit_log ORDER BY occurred_at DESC LIMIT 5;"

# LLM 用量
docker compose exec postgres psql -U app -d app -c "SELECT provider_type, model, action, stream, status, input_tokens, output_tokens FROM model_usage ORDER BY occurred_at DESC LIMIT 5;"
```

### 7.7 git 状态总览

```bash
cd D:/IdeaProjects/private-coding-agent
git log --oneline | wc -l                    # 总 commits
git log --oneline | head -20                 # 最近
git diff --stat 210cba3..HEAD | tail -1      # 累计改动
git status                                    # 应 clean
```

### 7.8 启动 Slice 4 实施（subagent-driven）

```
[控制层 = 你 / 我]
1. 读 docs/superpowers/plans/2026-05-20-slice-04-toolbus.md 取 Task 0 全文
2. 派 implementer subagent 实现 Task 0
3. implementer 报告 DONE → 派 spec reviewer
4. spec reviewer 通过 → 派 code quality reviewer
5. quality reviewer 通过 → 标 Task 0 完成
6. 重复 Task 1-13
7. 全部完成派 final reviewer 总评
```

如果想恢复推进，告诉我「继续 Slice 4」即可。

---

## 附录：完整 git 历史摘要（最近 30 条）

```
3e9b4ce docs: slice 4 tool bus implementation plan
53822a5 docs: slice 4 tool bus design spec
58a96ee feat(modelgw): main wiring + mock-provider + E2E + README       ← Slice 3 完成
5ca12f5 feat(modelgw): SSE writer + HTTP handlers
108ab39 feat(modelgw): Gateway orchestration
85aa8f0 feat(modelgw): ClaudeProvider over Anthropic Messages API
5723579 feat(modelgw): Anthropic SSE → OpenAI chunks state machine
030b30f feat(modelgw): Anthropic ↔ OpenAI translate
4b673bc feat(modelgw): OllamaProvider as thin OpenAIProvider wrapper
7f8e882 feat(modelgw): OpenAIProvider (chat / stream / embeddings)
5e0239d feat(modelgw): Provider interface + ProviderRegistry
6b54664 feat(modelgw): validate ChatRequest/EmbeddingsRequest
3d70479 feat(modelgw): model_usage migration + UsageRepo + UsageRecorder
361c2a6 feat(modelgw): providers migration + ProviderRepo
d2e6f77 feat(modelgw): types, limits, error sentinels
d1c7e1c chore: slice 2 carry-over
97a3bd5 docs: slice 3 model gateway implementation plan
d4d0b29 docs: slice 3 model gateway design spec
c4531c1 fix(sandbox): destroyed sandbox returns NotFound; bash e2e       ← Slice 2 完成
e4c77b7 docs: README + e2e script for slice 2
792627c feat(sandbox): startup reconciler
a65ff9a deploy: docker.sock + redis healthcheck + sandbox builder
fc3f7bc feat(cmd): wire sandbox driver, Docker client, Redis
3180246 feat(sandbox): HTTP handlers for sandbox lifecycle
b74bb6f feat(sandbox): Snapshot stub
ef507cc fix(sandbox): tar -- separator
089fc6f feat(sandbox): ReadFile/WriteFile via tar streams
2a97495 feat(sandbox): DockerDriver.Exec
6b09c23 fix(sandbox): Destroy uses owner-tagged Redis lock
d7561f5 feat(sandbox): DockerDriver Get + Destroy
```

完整历史可通过 `git log --oneline | head -76` 查看。
