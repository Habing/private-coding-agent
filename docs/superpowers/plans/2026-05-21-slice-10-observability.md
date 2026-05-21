# Slice 10 — Observability (OTel Spans + Prometheus + Structured Logging)

## Context

切片 9 完成后系统具备完整的"业务事件"审计层，但 **运行时可观测性** 仍存在三个明显缺口：

1. **没有手工 trace span**——`otelgin.Middleware` 已挂在 `internal/httpx/server.go`，每个 HTTP 请求自动产生根 span，但 agent ReAct 循环、tool invoke、model gateway 调用、sandbox docker 操作都**没有子 span**。线上一旦出现"为什么这次 agent.run 花了 30s"或"哪个 tool 卡住了"的问题，没有 trace 链路可看。
2. **没有指标暴露**——`telemetry.Setup` 已经 wire 了 OTLP metric 导出，但没有应用层指标（请求数/时延/token 数/沙箱并发数），也没有 `/metrics` Prometheus 抓取端点。线上无法用 Grafana 做 dashboard 或 alert。
3. **日志非结构化**——19 处 `log.Printf` 散落在 `cmd/server/main.go` (6)、`internal/modelgw/registry.go` (3)、`internal/sandbox/docker_driver.go` (3)、`internal/sandbox/docker_driver_exec.go` (1)、`internal/sandbox/reconciler.go` (3)、以及 main 里 model usage / tool invocation 的两个闭包。没有 request_id / trace_id 关联，没有 JSON 输出，无法在 Loki 之类的日志系统按字段过滤。

本切片闭环 P0 可观测性：手工 span 埋点 + Prometheus metrics + slog 全量迁移，并把 compose 栈一次性补齐（Jaeger 看 trace，Prometheus 抓 metrics）让本地 dev 立刻能用。

**用户已确认本切片边界**：
1. 结构化日志：**全量迁移**——所有 `log.Printf` 改写成 slog + ctx-aware 字段
2. compose 栈：**Jaeger + Prometheus 都加**（本地开箱可用）
3. `/metrics` 鉴权：**admin-only**——通过 JWT + `auth.RequireAdmin` 守护；Prom scraper 走 **静态 metrics token 旁路**（避免 JWT TTL 导致 scrape 失败）

## Goal

- 新建 `internal/logx` 包，封装 slog JSON handler + `FromCtx` / `WithCtx` 把 `request_id` / `trace_id` / `tenant_id` / `user_id` 自动注入每条日志
- 新建 `internal/httpx/requestid.go` 中间件：读取 / 生成 `X-Request-ID`，写入 ctx + response header
- 将 19 处 `log.Printf` 全量迁移到 slog（保留行为不变，仅替换 IO 通路 + 字段化）
- `internal/agent/engine.go` `Run`、`internal/toolbus/bus.go` `Invoke`、`internal/modelgw/gateway.go` `Chat`/`ChatStream`/`Embeddings`、`internal/sandbox/docker_driver.go` `Create`/`Exec`/`Destroy` 全部包子 span，带语义化属性
- 新建 `internal/metrics` 包，定义 10 个核心指标（HTTP 请求数+时延、tool 调用数+时延、model 调用数+时延+token、sandbox 并发数、WS 连接数、session 创建数）
- 新增 `GET /metrics`（Prometheus exposition format），admin JWT 或 `Authorization: Bearer <metrics_token>` 静态 token 任一可通过
- `telemetry.Setup` 扩展支持 Prometheus exporter 与 OTLP 并存
- `deploy/compose/docker-compose.yml` 加 `jaeger`（all-in-one）+ `prometheus`（带 scrape 配置）
- E2E 32 → 35 步：admin 调 `GET /metrics` 看到 `pca_` 前缀指标；metrics_token 也能调通；未鉴权 401/403
- `config/config.example.yaml` 加 `observability.metrics_token` / `observability.log_format` / `observability.log_level`

## 非目标（明确出栈）

- Grafana dashboard JSON 仓库化 → P1（compose 里**不**起 Grafana，避免过早承诺 dashboard 维护）
- 日志采集器（Loki/Vector/Promtail）→ P1，本切片只做应用侧 JSON 输出，落盘到 stdout
- 完整 RED/USE 指标全集 → 本切片只挑核心 10 个，其余按需加
- Tracing 采样率配置 → 本切片始终 AlwaysSample，prod 配置推到 P1
- Frontend 把 X-Request-ID 显示在错误 toast 上 → 简单加，但不强求样式
- Alert rules / SLO → P1
- 把 `audit_log` 表也喂进结构化日志 → 已有审计 API，不重复
- 把 Prometheus token rotate / Vault 集成 → 本切片只支持配置文件静态 token

## Architecture

```
HTTP 入口栈 (按挂载顺序):
  RequestIDMiddleware  -> 注入/生成 X-Request-ID 到 ctx
  otelgin.Middleware   -> 自动根 span (已有)
  auth.Middleware      -> Claims -> ctx
  audit.Middleware     -> http_request 行 (已有)
  metricsMiddleware    -> 记录 pca_http_requests_total / pca_http_request_duration_seconds
                           (新增, 复用 promhttp 风格 wrapper)

日志:
  logx.New(cfg)        -> *slog.Logger (JSON handler, level 可配)
  logx.FromCtx(ctx)    -> 自动附加 request_id / trace_id / span_id / tenant_id / user_id
  logx.WithCtx(ctx, l) -> 把 logger 塞回 ctx
  全局默认 logger 由 main 安装到 slog.SetDefault

手工 span 埋点 (每个埋点都用 `defer span.End()` + 错误时 SetStatus):
  agent.Engine.Run                  span="agent.run"       attrs: model, profile, max_steps
    └─ each ReAct step              span="agent.step"      attrs: step_index, kind
  toolbus.Bus.Invoke                span="tool.invoke"     attrs: tool_name, latency_ms
  modelgw.Gateway.Chat              span="model.chat"      attrs: model, prompt_tokens, completion_tokens
  modelgw.Gateway.ChatStream        span="model.chat_stream"
  modelgw.Gateway.Embeddings        span="model.embed"     attrs: model, input_count
  sandbox.DockerDriver.Create       span="sandbox.create"  attrs: image
  sandbox.DockerDriver.Exec         span="sandbox.exec"    attrs: cmd_len
  sandbox.DockerDriver.Destroy      span="sandbox.destroy"

指标 (全部 pca_ 前缀, otel meter API):
  pca_http_requests_total           Counter   labels: method, route, status_code
  pca_http_request_duration_seconds Histogram labels: method, route
  pca_tool_invocations_total        Counter   labels: tool, outcome
  pca_tool_invocation_duration_seconds Histogram labels: tool
  pca_model_calls_total             Counter   labels: model, kind (chat/stream/embed), outcome
  pca_model_call_duration_seconds   Histogram labels: model, kind
  pca_model_tokens_total            Counter   labels: model, direction (in/out)
  pca_sandbox_active                Gauge     (Create++ / Destroy--)
  pca_ws_connections_active         Gauge     (open++ / close--)
  pca_sessions_created_total        Counter   labels: profile

/metrics 鉴权 (两种凭证任一通过):
  - 标准 Bearer JWT + role=admin (auth.Middleware + RequireAdmin)
  - 静态 token: Authorization: Bearer <cfg.Observability.MetricsToken>
  实现: 自定义 metricsAuth 中间件, 优先 match 静态 token (constant-time compare),
        否则降级到标准 JWT 链.

compose 服务:
  jaeger      jaegertracing/all-in-one  ports: 16686 (UI), 4317 (OTLP gRPC)
              -> server 通过 PCA_TELEMETRY_OTLP_ENDPOINT=jaeger:4317 自动连
  prometheus  prom/prometheus           ports: 9090
              scrape config: target=server:8080/metrics, bearer_token=<metrics_token>,
                             scrape_interval=15s
```

## 关键不变量

1. **日志写入永远不阻塞业务** —— slog handler 用 `os.Stdout`；不引入异步 buffer，避免崩溃丢日志的复杂权衡
2. **span 创建零成本兜底** —— `OTLPEndpoint` 为空时 `otel.Tracer` 返回 no-op tracer，所有 `StartSpan` 仍可调用但不上报
3. **request_id 全链路** —— middleware 始终生成或透传，**绝不**只在某些路由生成；前端拿不到时也能在后端日志里关联
4. **静态 metrics token 不写入 audit_log** —— `/metrics` 命中 audit middleware 会产生大量噪声行；在 audit middleware 加路径前缀过滤跳过 `/metrics` 和 `/healthz` / `/readyz`
5. **指标基数控制** —— `route` label 用 gin 的 `c.FullPath()`（带 `:id` 占位符），不用原始 URL；`status_code` 数字化（不 group by 范围）
6. **日志字段命名稳定** —— `request_id` / `trace_id` / `span_id` / `tenant_id` / `user_id` / `action` 是事实合同，迁移后只能加不能删/改名
7. **结构化迁移不改语义** —— `log.Printf("foo: %v", err)` → `slog.Error("foo", "err", err)` 行为对等；不顺手做 retry/recovery 行为重构

## Tech Stack

复用：
- `internal/telemetry/otel.go` 的 OTLP wiring 模板（要扩展支持 prom exporter）
- `internal/auth/{middleware,require_admin}.go` JWT + admin 链
- `internal/audit/middleware.go`（小改加路径过滤）
- `internal/httpx/server.go` middleware 注册位置
- gin context API `c.FullPath()` / `c.Request.Context()`

新增依赖：
- `go.opentelemetry.io/otel/exporters/prometheus`（指标 Prom exporter）
- `github.com/prometheus/client_golang/prometheus/promhttp`（暴露 `/metrics`）
- `github.com/google/uuid` 已有
- slog 标准库（Go 1.21+，go.mod 已是 1.22+）

## 工作分解

### Task 1 — `internal/logx` + request id middleware

**新增文件：**
- `internal/logx/logger.go`
- `internal/logx/context.go`（`FromCtx` / `WithCtx` / ctx key）
- `internal/logx/logger_test.go`
- `internal/httpx/requestid.go`
- `internal/httpx/requestid_test.go`

接口：
```go
// logx/logger.go
type Config struct {
    Format string // "json" (default) or "text"
    Level  string // "debug" / "info" / "warn" / "error"
}
func New(cfg Config) *slog.Logger
func Install(l *slog.Logger) // slog.SetDefault + 包级 default

// logx/context.go
type ctxKey int
const loggerKey ctxKey = iota
func WithCtx(ctx context.Context, l *slog.Logger) context.Context
func FromCtx(ctx context.Context) *slog.Logger // 从 ctx 取 logger,
                                                 // 自动 With(request_id, trace_id, span_id, tenant_id, user_id)
                                                 // 若 ctx 没 logger, 返回 default

// httpx/requestid.go
const HeaderRequestID = "X-Request-ID"
type ctxKeyRequestID int
func RequestIDMiddleware() gin.HandlerFunc // 读 header 或 uuid.NewString(), set on ctx + response
func RequestIDFromCtx(ctx context.Context) string
```

`FromCtx` 内部从 ctx 取 `trace.SpanContextFromContext` 拿 trace_id/span_id（otelgin 已经塞好了），从 `auth.FromCtx` 拿 tenant_id/user_id（避免硬耦合 auth 包 → logx 注入函数指针的方式，或反过来 auth 包提供 `EnrichLog(ctx, l) *slog.Logger` 辅助函数让 logx 调）。最简方案：logx 直接 `import auth`（auth 是叶子包），不构成环。

测试：JSON 输出包含期望字段、level 过滤生效、ctx 字段自动补齐。

### Task 2 — 全量迁移 19 处 `log.Printf`

**改动文件：**
- `cmd/server/main.go` — 6 处 + 2 个闭包（model_usage、tool_invocation 落库失败）
- `internal/modelgw/registry.go` — 3 处
- `internal/sandbox/docker_driver.go` — 3 处
- `internal/sandbox/docker_driver_exec.go` — 1 处
- `internal/sandbox/reconciler.go` — 3 处
- `internal/modelgw/mockserver/main.go` — 1 处（独立 binary，照样迁移以统一风格）

每处转换规则：
```go
// before
log.Printf("docker create failed: %v", err)
// after
logx.FromCtx(ctx).Error("docker create failed", "err", err.Error())
```

启动期日志（main 早期、没有 ctx）用 `slog.Default().Info(...)`。

加单测：随机抽 3 个迁移点做 capture-handler 测试，确认字段命名正确（不重复测每一处）。

### Task 3 — agent / tool / model / sandbox 手工 span

**改动文件：**
- `internal/agent/engine.go` — `Run`：start `agent.run` span；每个 step start `agent.step` 子 span；err 时 `span.RecordError` + `SetStatus(codes.Error)`
- `internal/toolbus/bus.go` — `Invoke`：start `tool.invoke`；attrs：`tool.name`、`tool.outcome`
- `internal/modelgw/gateway.go` — `Chat`/`ChatStream`/`Embeddings`：start 对应 span；attrs：`model.id`、`model.prompt_tokens`、`model.completion_tokens`（usage 拿到后 SetAttribute）
- `internal/sandbox/docker_driver.go` — `Create`/`Exec`/`Destroy`：start span；attrs：`sandbox.image`、`sandbox.id`

tracer 单例：每个包内 `var tracer = otel.Tracer("internal/agent")` 这种形式（otel API 推荐，避免每次调用 `otel.Tracer()` 解析）。

测试：用 `sdktrace.NewTracerProvider(WithSpanProcessor(spanRecorder))` 在测试中替换全局 tracer，验证 Run 触发期望的 span 树结构。每个包加一个集成测试就够，不每个 span 单测。

### Task 4 — `internal/metrics` 包 + 各埋点处 record

**新增文件：**
- `internal/metrics/metrics.go`
- `internal/metrics/metrics_test.go`

接口：
```go
package metrics

var (
    HTTPRequestsTotal       metric.Int64Counter
    HTTPRequestDuration     metric.Float64Histogram
    ToolInvocationsTotal    metric.Int64Counter
    ToolInvocationDuration  metric.Float64Histogram
    ModelCallsTotal         metric.Int64Counter
    ModelCallDuration       metric.Float64Histogram
    ModelTokensTotal        metric.Int64Counter
    SandboxActive           metric.Int64UpDownCounter
    WSConnectionsActive     metric.Int64UpDownCounter
    SessionsCreatedTotal    metric.Int64Counter
)

// Init must be called after telemetry.Setup so meter provider is wired.
func Init() error
```

每个指标在 `Init` 里 lazy create。在已有埋点处补 record 调用：
- `internal/httpx/server.go` 加一个 `metricsMiddleware`（最外层挂）：startTime → defer record HTTP 两个指标
- `internal/toolbus/bus.go` `Invoke` 末尾 record tool 两个指标
- `internal/modelgw/gateway.go` 三个方法 record model 三个指标
- `internal/sandbox/docker_driver.go` Create/Destroy 调 `SandboxActive.Add(ctx, +1/-1)`
- `internal/session/wshandler.go` serve 入口 +1 / defer -1
- `internal/session/service.go` `CreateSession` 调 `SessionsCreatedTotal.Add(...)`

### Task 5 — `/metrics` endpoint + Prometheus exporter

**新增文件：**
- `internal/metrics/handler.go` — `Register(r *gin.RouterGroup, cfg AuthConfig)`
- `internal/metrics/auth.go` — `MetricsAuth(jwtSvc, staticToken)` gin middleware
- `internal/metrics/auth_test.go`

**改动：**
- `internal/telemetry/otel.go` — `Setup` 同时返回 prom registry，或新增 `SetupWithPrometheus`
- `cmd/server/main.go` — 装配 `/metrics` 路由（**注意**：不挂在 protected group 下，独立 group，自带 auth）
- `internal/audit/middleware.go` — 加 `skipPaths = ["/metrics", "/healthz", "/readyz"]`，命中直接 next

`MetricsAuth` 中间件逻辑：
```go
1. 取 Authorization: Bearer <token>
2. if subtle.ConstantTimeCompare(token, cfg.StaticToken) == 1 -> next
3. else 走 auth.Middleware + RequireAdmin 链（手工触发或重用现有 handler 组合）
4. 都不通过 -> 401 或 403
```

测试覆盖：静态 token 通过、JWT admin 通过、JWT member 403、无 header 401、错 token 401。

### Task 6 — telemetry config 扩展

**改动文件：**
- `internal/config/config.go` — 加 `Observability` block：
  ```go
  type ObservabilityConfig struct {
      LogFormat        string // json / text (default json)
      LogLevel         string // info / debug / warn / error (default info)
      MetricsToken     string // 静态 prom scrape token, 空则禁用静态 token 通道
  }
  ```
- `config/config.example.yaml` — 加 `observability:` 段
- `cmd/server/main.go` — 装配顺序：logx.New → telemetry.Setup → metrics.Init → middleware/route 注册

`PCA_OBSERVABILITY_LOG_LEVEL` / `PCA_OBSERVABILITY_METRICS_TOKEN` 等环境变量自动可用（viper 现有逻辑）。

### Task 7 — docker-compose Jaeger + Prometheus

**改动文件：**
- `deploy/compose/docker-compose.yml`
- `deploy/compose/.env.example` — 加 `PCA_OBSERVABILITY_METRICS_TOKEN=dev-scrape-token-change-me`
- `deploy/compose/prometheus.yml`（新增）

新增 services：
```yaml
jaeger:
  image: jaegertracing/all-in-one:1.55
  environment:
    COLLECTOR_OTLP_ENABLED: "true"
  ports:
    - "16686:16686"  # UI
    - "4317:4317"    # OTLP gRPC

prometheus:
  image: prom/prometheus:v2.51.2
  volumes:
    - ./prometheus.yml:/etc/prometheus/prometheus.yml:ro
  ports:
    - "9090:9090"
  depends_on: [server]
```

server 服务追加：
```yaml
environment:
  PCA_TELEMETRY_OTLP_ENDPOINT: "jaeger:4317"
  PCA_OBSERVABILITY_METRICS_TOKEN: ${PCA_OBSERVABILITY_METRICS_TOKEN}
  PCA_OBSERVABILITY_LOG_FORMAT: json
  PCA_OBSERVABILITY_LOG_LEVEL: info
```

`prometheus.yml`：
```yaml
global:
  scrape_interval: 15s
scrape_configs:
  - job_name: pca-server
    metrics_path: /metrics
    authorization:
      type: Bearer
      credentials_file: /etc/prometheus/token   # or inline credentials
    static_configs:
      - targets: ['server:8080']
```

> 直接用 `credentials: ${PCA_OBSERVABILITY_METRICS_TOKEN}` 行内（prom 不支持 env interpolation，最简方案是 compose 启动前 envsubst 生成 prom 配置；或退一步把 token 硬编码到 `.env.example` + `prometheus.yml` 两边对齐，文档强调"开发用，prod 改"）。**选择后者**：`.env.example` 和 `prometheus.yml` 都用同一个 dev token 常量，README 显式标注 prod 必改。

### Task 8 — E2E 32 → 35 步

**改动文件：** `deploy/compose/test-e2e.sh`

- 把 `/32]` 全量 sed 成 `/35]`
- 追加：
  - [33/35] `GET /metrics` with admin JWT → 200，body 包含 `pca_http_requests_total`
  - [34/35] `GET /metrics` with metrics_token → 200
  - [35/35] `GET /metrics` 无 auth → 401

### Task 9 — README + 配置文档 + spec 归档

- `README.md`：`- [x] 切片 10：Observability`、端点表加 `/metrics`、新增"可观测性"小节说明 Jaeger UI / Prom UI 端口、metrics token 用途、span 命名表
- `config/config.example.yaml` 注释里说明 `observability.metrics_token` 配在哪
- `docs/superpowers/plans/2026-05-21-slice-10-observability.md` 归档本 plan

## 关键文件清单

**新增（13 个）：**
- `internal/logx/logger.go` + `context.go` + `logger_test.go`
- `internal/httpx/requestid.go` + `_test.go`
- `internal/metrics/metrics.go` + `handler.go` + `auth.go` + `metrics_test.go` + `auth_test.go`
- `deploy/compose/prometheus.yml`
- `docs/superpowers/plans/2026-05-21-slice-10-observability.md`

**修改（约 18 个）：**
- `cmd/server/main.go`（logger 安装、metrics.Init、新 /metrics 路由、6 处 log.Printf）
- `internal/telemetry/otel.go`（prom exporter 选项）
- `internal/config/config.go` + `config/config.example.yaml`
- `internal/httpx/server.go`（挂 RequestIDMiddleware + metricsMiddleware）
- `internal/audit/middleware.go`（skip /metrics, /healthz, /readyz）
- `internal/agent/engine.go`（span）
- `internal/toolbus/bus.go`（span + tool metrics）
- `internal/modelgw/gateway.go`（span + model metrics）
- `internal/modelgw/registry.go`（3 处 log.Printf）
- `internal/modelgw/mockserver/main.go`（1 处）
- `internal/sandbox/docker_driver.go`（span + 3 处 log.Printf + SandboxActive）
- `internal/sandbox/docker_driver_exec.go`（1 处）
- `internal/sandbox/reconciler.go`（3 处）
- `internal/session/service.go`（SessionsCreatedTotal）
- `internal/session/wshandler.go`（WSConnectionsActive）
- `deploy/compose/docker-compose.yml` + `.env.example`
- `deploy/compose/test-e2e.sh`
- `README.md`

## 验证

```bash
go vet ./...
go test ./internal/logx/... ./internal/httpx/... ./internal/metrics/... -count=1 -v
go test ./... -count=1

cd internal/webui && npm test -- --run && npm run lint && npm run build

cd deploy/compose && ./test-e2e.sh   # 期望 35/35 PASS

# 手动 smoke (compose up 之后):
curl http://localhost:16686            # Jaeger UI
curl http://localhost:9090/targets     # Prom up=1
curl -H "Authorization: Bearer dev-scrape-token-change-me" http://localhost:8080/metrics | grep pca_
```

## Acceptance

- [ ] `logx.New` 单测过；JSON 输出含 level/msg/request_id/trace_id 字段
- [ ] `RequestIDMiddleware` 单测过；缺失时生成、存在时透传、response header 总有
- [ ] 19 处 `log.Printf` 全部迁移；`grep -rn 'log.Printf' --include='*.go'` 返回空（mockserver 也算）
- [ ] agent.run / tool.invoke / model.chat / sandbox.create 手工 span 单测过（用 spanRecorder）
- [ ] `/metrics` 返回 prom 格式 text，包含 10 个 `pca_` 指标
- [ ] 静态 token、admin JWT 都能调通 `/metrics`；member JWT 403；无 auth 401
- [ ] compose up 后 jaeger UI 能看到至少一个根 span（先发个 `agent.run` 触发）
- [ ] compose up 后 prom `up{job="pca-server"} == 1`
- [ ] E2E 35 步全过
- [ ] README + 端点表更新；observability config 段示例完整
- [ ] git tree clean，commit 按 Conventional Commits 切分（建议 4-5 个 commit：logx、span、metrics、compose、docs）

## 风险与折衷

1. **slog 在 Go 1.22 是 std**——若 `go.mod` 是 1.21 早期可能 import 失败。已确认 go.mod 是 1.22+，无风险。
2. **静态 metrics token 是 trade-off**——理论上违反"凭证统一"原则；但 Prom scrape 用 JWT TTL 会过期，标准做法就是静态 token 或 mTLS。文档强调 prod 改。
3. **slog 迁移可能漏字段**——比如 `log.Printf("model %s failed: %v", id, err)` 变 `slog.Error("model failed", "model", id, "err", err)`，但模型 ID 在不同位置叫 `model`/`model_id` 风险不一致。统一规则：标识用 `<domain>_id`、错误用 `err`（不带后缀）、耗时用 `duration_ms`。
4. **span 创建成本**——每个 invoke 新增 ~1µs；可忽略。
5. **Prom 客户端基数风险**——`route` 用 `c.FullPath()`（gin 模板路径）避免每个 `:id` 实例化一行；`tool` 标签固定 12 个枚举值；`model` 标签由配置决定，通常 <10 个。
