# Slice 3 — Model Gateway 设计

| 字段 | 值 |
|---|---|
| 文档日期 | 2026-05-20 |
| 状态 | Draft — 待用户复核 |
| 范围 | 私有化 AI 编码 Agent — P0 第 3 个切片 |
| 前置 | Slice 1.5 已完成；Slice 2 已完成（HEAD `c4531c1`）|

---

## 1. 概述

本切片交付 **Model Gateway** —— LLM 调用的统一抽象层。对外暴露 OpenAI 兼容的 `/v1/chat/completions` 与 `/v1/embeddings`；对内通过 `Provider` 接口适配 Ollama、OpenAI、Claude 三种后端。Provider 配置存 PG 表，运维可热增；token 用量出口拦截写计费表 `model_usage`。

**核心叙事**
- **对外 OpenAI 协议** —— 调用方可直接用 OpenAI 官方 SDK 接 `http://server:8080/v1`，零适配
- **provider:model 显式前缀** —— 调用方传 `model: "claude:claude-sonnet-4-5"`，Gateway 拆前缀路由
- **SSE 端到端透传** —— 流式 chat 走 Server-Sent Events，Gateway 不缓冲
- **Claude 协议适配** —— Slice 3 含 Anthropic Messages API ↔ OpenAI Chat Completions 双向转换（含 SSE 流式状态机）
- **Token 计量** —— provider 返回的 usage 字段记录到 `model_usage` 表（不预估 / 不限额）
- **失败快返** —— 不做自动 fallback；429/4xx/5xx 各自映射

**不在 Slice 3 范围**
- Provider tenant ACL（哪些 provider 可被哪些租户用）—— P1
- 自动 fallback / retry budget —— P1
- 配额（每租户日 token 上限）—— P1
- Token 预估（tiktoken 之类）—— Slice 9 加固
- Embeddings 在 Claude 上的实现（Anthropic 无官方 API，返 `ErrUnsupportedFeature`）

## 2. 前置条件

依赖 Slice 1.5 完成的 3 项加固（已落地）：
- `db.Migrate(ctx, dsn)` 支持 ctx
- `audit.Middleware` 用 detached ctx + 5s timeout
- `ValidateJWTConfig`

依赖 Slice 2 完成的能力（可选）：本切片对 Slice 2 沙箱无依赖；但 docker-compose 与 main.go 装配会同时支撑两套服务。

**Task 0 carry-over**（来自 Slice 2 final review 遗留）：
1. `DockerDriver.Exec` 的 stdin 写入应在 stdcopy 启动后异步进行（防大 stdin 死锁）
2. `ContainerExecInspect` 错误打 log（当前静默置 ExitCode=-1）
3. 沙箱基础镜像 README 加 trivy 扫描提示

## 3. 核心需求

| 维度 | 决策 |
|---|---|
| 协议形态 | OpenAI 兼容（`/v1/chat/completions`、`/v1/embeddings`）|
| P0 provider | Ollama + OpenAI + Claude 三者 |
| Provider 配置 | PG 表 `providers`，启动期 load + 60s refresh |
| 模型选择 | 调用方传 `model: "<provider>:<model>"` 显式前缀 |
| 流式 | Chat 走 SSE；Embeddings 单次 |
| Token 计量 | 仅记 provider 返回的 usage 字段 |
| 失败策略 | 快返不 fallback |
| API key 持有 | DB 存 `api_key_env` 字段引用环境变量名；密钥不入库 |

非功能：
- 非流 chat 端到端开销（不含 provider 时间）P50 < 30 ms
- SSE 单 chunk 转发延迟 P50 < 10 ms
- 100 chunk stream 总转发 P50 < 1500 ms
- Provider 健康可用：失败仅影响该 provider，不挂整服务

## 4. 整体架构

```
+--------------------------------------------------------------+
|  HTTP 层 (gin) — 复用 Slice 1 auth + audit middleware         |
|   POST /v1/chat/completions     OpenAI 兼容,支持 SSE           |
|   POST /v1/embeddings           OpenAI 兼容,一次性             |
+--------------------------------------------------------------+
                  | uses
                  v
+--------------------------------------------------------------+
|  internal/modelgw/                                            |
|   Gateway                                                     |
|     ChatCompletion(ctx, tid, uid, req) → *ChatResponse        |
|     ChatCompletionStream(ctx, ..., yield) → error             |
|     Embeddings(ctx, tid, uid, req)  → *EmbeddingsResponse     |
|                                                               |
|   ProviderRegistry                                            |
|     启动期全量 load + 60s refresh                              |
|     Resolve("provider:model") → (Provider, model)             |
|                                                               |
|   Provider interface                                          |
|     - OpenAIProvider   (HTTP 直转 OpenAI/Ollama OpenAI-compat) |
|     - OllamaProvider   (OpenAIProvider 薄包装,base url 不同)    |
|     - ClaudeProvider   (Anthropic Messages API 双向适配)       |
|                                                               |
|   UsageRecorder (detached ctx 写入)                            |
|     Record(CallEvent)                                         |
+--------------------------------------------------------------+
                  | uses
                  v
+--------------------------------------------------------------+
|  数据层                                                       |
|   PostgreSQL                                                  |
|     - providers      (id, name, type, base_url, api_key_env)  |
|     - model_usage    (tenant_id, provider_id, model, tokens)  |
|     - audit_log      (Slice 1 已就位)                         |
|   HTTP Client (net/http)                                      |
+--------------------------------------------------------------+
```

**三个核心抽象**
1. `Provider` 接口 — 屏蔽三家协议差异；未来加新 provider 只需新实现
2. `ProviderRegistry` — DB 驱动的运行时 provider 池，避免硬编码
3. `Gateway` — 编排层：validate → Resolve → Provider 调用 → record

## 5. 接口与数据模型

### 5.1 公共类型 `internal/modelgw/types.go`

```go
type ChatRole string
const (
    RoleSystem    ChatRole = "system"
    RoleUser      ChatRole = "user"
    RoleAssistant ChatRole = "assistant"
    RoleTool      ChatRole = "tool"
)

type ChatMessage struct {
    Role       ChatRole    `json:"role"`
    Content    string      `json:"content"`
    Name       string      `json:"name,omitempty"`
    ToolCallID string      `json:"tool_call_id,omitempty"`
    ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`
}

type ToolCall struct {
    ID       string       `json:"id"`
    Type     string       `json:"type"`   // "function"
    Function ToolCallFunc `json:"function"`
}
type ToolCallFunc struct {
    Name      string `json:"name"`
    Arguments string `json:"arguments"`   // raw JSON string
}

type ChatRequest struct {
    Model       string        `json:"model"`         // "<provider>:<model>"
    Messages    []ChatMessage `json:"messages"`
    Stream      bool          `json:"stream,omitempty"`
    Temperature *float64      `json:"temperature,omitempty"`
    TopP        *float64      `json:"top_p,omitempty"`
    MaxTokens   *int          `json:"max_tokens,omitempty"`
    Tools       []ToolDef     `json:"tools,omitempty"`
    ToolChoice  any           `json:"tool_choice,omitempty"`
    Stop        []string      `json:"stop,omitempty"`
    Seed        *int          `json:"seed,omitempty"`
}
type ToolDef struct {
    Type     string          `json:"type"`   // "function"
    Function ToolDefFunction `json:"function"`
}
type ToolDefFunction struct {
    Name        string          `json:"name"`
    Description string          `json:"description,omitempty"`
    Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type ChatChoice struct {
    Index        int         `json:"index"`
    Message      ChatMessage `json:"message"`
    FinishReason string      `json:"finish_reason"`
}
type Usage struct {
    PromptTokens     int `json:"prompt_tokens"`
    CompletionTokens int `json:"completion_tokens"`
    TotalTokens      int `json:"total_tokens"`
}
type ChatResponse struct {
    ID      string       `json:"id"`
    Object  string       `json:"object"`    // "chat.completion"
    Created int64        `json:"created"`
    Model   string       `json:"model"`
    Choices []ChatChoice `json:"choices"`
    Usage   Usage        `json:"usage"`
}

type ChatStreamChunk struct {
    ID      string             `json:"id"`
    Object  string             `json:"object"`     // "chat.completion.chunk"
    Created int64              `json:"created"`
    Model   string             `json:"model"`
    Choices []ChatStreamChoice `json:"choices"`
    Usage   *Usage             `json:"usage,omitempty"`   // 末帧
}
type ChatStreamChoice struct {
    Index        int             `json:"index"`
    Delta        ChatStreamDelta `json:"delta"`
    FinishReason *string         `json:"finish_reason,omitempty"`
}
type ChatStreamDelta struct {
    Role      ChatRole   `json:"role,omitempty"`
    Content   string     `json:"content,omitempty"`
    ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

type EmbeddingsRequest struct {
    Model string   `json:"model"`
    Input []string `json:"input"`
}
type Embedding struct {
    Index     int       `json:"index"`
    Object    string    `json:"object"`    // "embedding"
    Embedding []float64 `json:"embedding"`
}
type EmbeddingsResponse struct {
    Object string      `json:"object"`     // "list"
    Data   []Embedding `json:"data"`
    Model  string      `json:"model"`
    Usage  Usage       `json:"usage"`
}

var (
    ErrModelInvalid        = errors.New("model: must be 'provider:model'")
    ErrProviderNotFound    = errors.New("provider not found")
    ErrProviderUnreachable = errors.New("provider unreachable")
    ErrProviderError       = errors.New("provider returned error")
    ErrUnsupportedFeature  = errors.New("feature not supported by this provider")
)

// ProviderError 带 status code 与原始 body 截断。
type ProviderError struct {
    StatusCode int
    Body       string   // 最多 4 KB
}
func (e *ProviderError) Error() string { return fmt.Sprintf("provider %d: %s", e.StatusCode, e.Body) }
func (e *ProviderError) Is(t error) bool { return t == ErrProviderError }

const (
    MaxMessages       = 200
    MaxMessageBytes   = 256 * 1024
    MaxEmbeddingInput = 100
    MaxEmbeddingItem  = 8 * 1024
    DefaultTimeoutSec = 120
    StreamIdleTimeout = 60 * time.Second
    MaxStreamSeconds  = 600 * time.Second
)

type CallEvent struct {
    TenantID     uuid.UUID
    UserID       uuid.UUID
    ProviderID   uuid.UUID
    ProviderType string    // "ollama" / "openai" / "claude"
    Model        string    // 裸 model
    Action       string    // "chat" / "embed"
    Stream       bool
    Status       string    // "ok" / "error"
    ErrorClass   string    // "" / "unreachable" / "provider_error" / "validation" / "stream_idle_timeout"
    InputTokens  int
    OutputTokens int
    DurationMS   int64
    OccurredAt   time.Time
}
```

### 5.2 Provider 接口

```go
type Provider interface {
    ID() uuid.UUID
    Type() string  // "ollama" / "openai" / "claude"
    Name() string  // 配置中的 slug,例如 "default-ollama"

    ChatCompletion(ctx context.Context, req ChatRequest, model string) (*ChatResponse, error)
    ChatCompletionStream(ctx context.Context, req ChatRequest, model string,
        yield func(ChatStreamChunk) error) error
    Embeddings(ctx context.Context, req EmbeddingsRequest, model string) (*EmbeddingsResponse, error)
}
```

所有方法协程安全。`model` 参数是裸 model 名（无 `provider:` 前缀）。`yield` 返 error 表示客户端断开或下游问题，provider 应立刻停止读取上游并返。

### 5.3 数据库表

#### migration 0005 `providers`

```sql
CREATE TABLE providers (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name         TEXT NOT NULL UNIQUE,
    type         TEXT NOT NULL,          -- "ollama" / "openai" / "claude"
    base_url     TEXT NOT NULL,
    api_key_env  TEXT NOT NULL DEFAULT '',
    enabled      BOOLEAN NOT NULL DEFAULT TRUE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX providers_enabled_idx ON providers(enabled) WHERE enabled = TRUE;

-- seed: docker-compose mock-provider 默认
INSERT INTO providers (name, type, base_url, api_key_env)
VALUES ('default-mock', 'openai', 'http://mock-provider:8081', '');
```

#### migration 0006 `model_usage`

```sql
CREATE TABLE model_usage (
    id              BIGSERIAL PRIMARY KEY,
    occurred_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    tenant_id       UUID NOT NULL,
    user_id         UUID NOT NULL,
    provider_id     UUID NOT NULL REFERENCES providers(id),
    provider_type   TEXT NOT NULL,
    model           TEXT NOT NULL,
    action          TEXT NOT NULL,       -- "chat" / "embed"
    stream          BOOLEAN NOT NULL,
    status          TEXT NOT NULL,       -- "ok" / "error"
    error_class     TEXT NOT NULL DEFAULT '',
    input_tokens    INT NOT NULL DEFAULT 0,
    output_tokens   INT NOT NULL DEFAULT 0,
    duration_ms     INT NOT NULL
);

CREATE INDEX model_usage_tenant_time_idx
    ON model_usage(tenant_id, occurred_at DESC);
```

`audit_log` 仍由 `audit.Middleware` 自动写一行（HTTP 层），与 `model_usage` 互补：前者通用、后者结构化便于聚合。

### 5.4 HTTP 端点

| 方法 | 路径 | 鉴权 | 协议 |
|---|---|---|---|
| POST | `/v1/chat/completions` | Bearer JWT | OpenAI Chat Completions（支持 stream） |
| POST | `/v1/embeddings` | Bearer JWT | OpenAI Embeddings |

**SSE 规范**：
- `Content-Type: text/event-stream; charset=utf-8`
- `Cache-Control: no-cache`
- `Connection: keep-alive`
- 每帧 `data: <json>\n\n`
- 末尾 `data: [DONE]\n\n`
- 错误中途：`data: {"error":{...}}\n\n` 关流（不写 `[DONE]`）

### 5.5 包结构

```
internal/modelgw/
├── types.go                公共类型 + 错误 + 上限
├── types_test.go
├── validate.go             ChatRequest / EmbeddingsRequest 校验
├── validate_test.go
├── provider.go             Provider 接口
├── registry.go             ProviderRegistry
├── registry_test.go
├── repo.go                 ProviderRepo + UsageRepo + UsageRecorder
├── repo_test.go            dockertest 集成
├── gateway.go              Gateway 编排
├── gateway_test.go         mockProvider 测
├── handler.go              HTTP handlers (含 SSE)
├── handler_test.go         mockGateway 测
├── sse.go                  SSE writer
├── sse_test.go
├── provider_openai.go      OpenAI Provider (含 Ollama OpenAI-compat 复用)
├── provider_openai_test.go httptest 测
├── provider_ollama.go      Ollama Provider (薄包装)
├── provider_ollama_test.go
├── provider_claude.go      Claude Provider
├── provider_claude_test.go httptest 测
├── claude_translate.go     Anthropic ↔ OpenAI 双向转换
├── claude_translate_test.go fixture 测
├── claude_stream.go        Anthropic SSE 流式状态机
└── claude_stream_test.go   fixture 测
```

新增迁移：
- `internal/db/migrations/0005_create_providers.{up,down}.sql`
- `internal/db/migrations/0006_create_model_usage.{up,down}.sql`

## 6. 数据流

### 6.1 非流式 Chat — POST /v1/chat/completions (stream=false)

1. Handler 验 JWT，bind 请求体
2. Gateway.ChatCompletion: validate → Registry.Resolve(`provider:model`) → provider.ChatCompletion(model) → recorder.Record（detached ctx）
3. 返 200 + JSON
4. ctx 超时（默认 120 s）取消上游 HTTP

### 6.2 流式 Chat — stream=true

1. Handler 验 JWT，bind 请求体
2. 设 SSE headers + 首次 Flush
3. Gateway.ChatCompletionStream(yield)：yield 内每帧 `fmt.Fprintf(w, "data: %s\n\n", json)` + `Flush`
4. Provider 内部解析上游 SSE / NDJSON，逐 chunk 调 yield
5. 末帧含 `Usage`，Gateway 在 stream 结束后调 recorder
6. Handler 写 `data: [DONE]\n\n`
7. 中途错误：写错误帧 + 关流 + record(status=error)
8. 客户端断开：yield 写失败 → return error → provider 停止读上游

### 6.3 Embeddings

1. Handler 验 JWT，bind 请求体
2. Gateway.Embeddings: validate（input 数 + 单条字节）→ Resolve → provider.Embeddings(model) → record
3. Claude provider 返 `ErrUnsupportedFeature` → handler 返 400 `unsupported_feature`

### 6.4 Provider Registry 启动期 + refresh

1. main.go run() 阶段：`registry.Start(ctx)` 立刻 load（含 ProviderRepo.ListEnabled）
2. 失败 → run() return error → server 不起
3. 成功后 go refresh loop（每 60 s 重 load）；refresh 失败保留旧缓存 + log
4. Resolve 用读锁查 byName map

### 6.5 UsageRecorder

`Record(e CallEvent)` 总用 `context.Background()` + 5 s timeout，不阻塞主请求。失败仅 log。

### 6.6 Claude 协议适配

#### 请求适配（OpenAI → Anthropic Messages）

| OpenAI 字段 | Anthropic 字段 |
|---|---|
| `messages[i].role=system` | 抽离到顶层 `system`（多 system 用 `\n\n` 拼接） |
| `messages[i].role=user/assistant + content` | `messages[i].content = [{type:"text", text:...}]` |
| `messages[i].role=assistant + tool_calls` | 加 `{type:"tool_use", id, name, input}` 块 |
| `messages[i].role=tool` | `messages` 中新增 user message + `{type:"tool_result", tool_use_id, content}` 块 |
| `tools[i].function.{name,description,parameters}` | `tools[i].{name, description, input_schema}` |
| `max_tokens` | 必填，缺省 4096 |

#### 响应适配（非流式）

- `content[].type=text` → 拼到 `message.content`
- `content[].type=tool_use` → 加到 `message.tool_calls`，`Arguments` 是 `json.Marshal(b.Input)`
- `stop_reason` 映射：`end_turn → stop` / `max_tokens → length` / `tool_use → tool_calls` / `stop_sequence → stop`
- `usage.input_tokens / output_tokens` → OpenAI `prompt_tokens / completion_tokens`

#### 流式适配（Anthropic SSE → OpenAI SSE）

Anthropic 事件序列：
```
message_start → content_block_start* → content_block_delta* → content_block_stop*
→ message_delta(stop_reason+usage) → message_stop
```

转换状态机：
- `message_start` → 记 `chunk_id`、`input_tokens`；不发 OpenAI chunk
- `content_block_start type=text` → 发首帧 chunk `delta:{role:"assistant"}`
- `content_block_delta type=text_delta` → chunk `delta:{content: text}`
- `content_block_start type=tool_use` → 状态登记新 tool_call slot
- `content_block_delta type=input_json_delta` → 累积 partial JSON
- `content_block_stop`（tool_use）→ 发 chunk `delta:{tool_calls:[{id,type:"function",function:{name,arguments}}]}`
- `message_delta` → 记 `output_tokens`、`stop_reason`
- `message_stop` → 发末帧 `{finish_reason, usage}`

### 6.7 横切观测

每次调用产生：
- `model_usage` 一行（永远写，含 error）
- `audit_log` 一行（HTTP middleware 自动）
- OTel span：`modelgw.chat` / `modelgw.embed`，attrs: `tenant_id, provider_id, provider_type, model, stream, status, input_tokens, output_tokens`

**不**记录 message content / stdin / stdout（防泄密）。**绝不**让 `api_key_env` 引用的 env value 出现在错误 body 中（redact 函数过滤）。

## 7. 错误处理（HTTP 出口）

所有错误用 OpenAI 错误格式：`{ "error": { "message", "type", "code" } }`。

| 故障 | HTTP | error.type | error.code |
|---|---|---|---|
| 缺/坏 JWT | 401 | auth_error | missing_token / invalid_token |
| 非法 JSON | 400 | invalid_request_error | bad_request |
| model 格式错 | 400 | invalid_request_error | model_invalid |
| provider 不存在/disabled | 400 | invalid_request_error | provider_not_found |
| messages 空 / 超 MaxMessages | 400 | invalid_request_error | validation |
| 单条 message > MaxMessageBytes | 413 | invalid_request_error | content_too_large |
| Embeddings input 越界 | 413 | invalid_request_error | content_too_large |
| Claude embeddings | 400 | invalid_request_error | unsupported_feature |
| api_key_env 引用的 env 缺失 | 500 | server_error | missing_api_key |
| Provider DNS/connect/EOF | 502 | provider_error | unreachable |
| Provider 4xx | 502 | provider_error | provider_4xx |
| Provider 5xx | 502 | provider_error | provider_5xx |
| Provider 429 | 429 | rate_limit_error | provider_rate_limit |
| Stream idle 超时 | 已 flush headers，错误帧 | provider_error | stream_idle_timeout |
| 客户端断开 | 不写错误帧；仅 record | – | – |
| 内部 panic | 500 | server_error | internal |

Stream 中途错误（headers 已发）：发一帧 `data:{"error":...}\n\n` 关流；recorder 记 status=error。

## 8. 测试策略

### 8.1 单元

| 文件 | 覆盖 |
|---|---|
| `types_test.go` | 常量与错误哨兵 |
| `validate_test.go` | ChatRequest / EmbeddingsRequest 校验边界 |
| `registry_test.go` | Resolve 各形态 + provider 缺失 |
| `claude_translate_test.go` | 双向转换：system 抽离、tool_calls/tool messages 互转、stop_reason |
| `claude_stream_test.go` | fixture：录制的 Anthropic SSE → 预期 OpenAI chunks |
| `sse_test.go` | SSE writer 格式 + flush + [DONE] + 错误帧 |
| `gateway_test.go` | mockProvider：validate fail / resolve fail / provider fail / ok；recorder 永远被调 |

### 8.2 集成（httptest 模拟外部 API）

| 文件 | 覆盖 |
|---|---|
| `provider_openai_test.go` | 非流 / 流 / embeddings；错误响应映射 |
| `provider_ollama_test.go` | 同 OpenAI，差异 base URL / 无鉴权 |
| `provider_claude_test.go` | 非流 / 流（含 tool_use）/ embeddings 返 unsupported；429 / 5xx 处理 |
| `repo_test.go` | dockertest PG：ProviderRepo + UsageRepo CRUD |
| `gateway_integration_test.go` | 起 mock provider httptest server，Gateway 完整调用链 + 写 model_usage |

### 8.3 HTTP handler

| 测试 | 内容 |
|---|---|
| `handler_test.go` | mockGateway：鉴权 / 各错误码 / SSE 流序列 / SSE 错误中途 |

### 8.4 E2E（compose）

扩展 `test-e2e.sh`：
- 复用 Slice 2 沙箱流程
- 新增章节：登录 → `curl /v1/chat/completions` 走 `default-mock` provider → 收 200 / SSE → 查 `model_usage` 表新增行

### 8.5 mock-provider

`internal/modelgw/mockserver/` 提供 ~50 行 Go HTTP server，模拟 OpenAI 兼容端点：
- `POST /v1/chat/completions` 非流返 fixed JSON、流返 5 个 chunk + usage 末帧
- `POST /v1/embeddings` 返 fixed 向量

docker-compose 新增 `mock-provider` service（基于 mockserver 二进制小镜像）。

### 8.6 性能基线（informational）

- 非流 chat 端到端（不含 provider 时间）P50 < 30 ms
- SSE 单 chunk 转发 P50 < 10 ms
- 100 chunk stream P50 < 1500 ms

## 9. Slice 3 Task 拆解（14 Task）

| Task | 内容 |
|---|---|
| 0 | Slice 2 carry-over：sandbox stdin 异步 + Exec inspect log + base 镜像 trivy 文档 |
| 1 | `internal/modelgw/types.go` + 单测 |
| 2 | migration 0005 `providers` + `ProviderRepo` + dockertest 测 |
| 3 | migration 0006 `model_usage` + `UsageRepo` + `UsageRecorder` + dockertest 测 |
| 4 | `validate.go` ChatRequest/EmbeddingsRequest 校验 + 单测 |
| 5 | `Provider` 接口 + `ProviderRegistry` 框架 + 单测（无 provider 实现） |
| 6 | `OpenAIProvider`（含 Ollama OpenAI-compat 复用） + httptest 测 |
| 7 | `OllamaProvider`（OpenAIProvider 薄包装） + httptest 测 |
| 8 | Claude 协议双向适配（非流式） + 单测 |
| 9 | Claude SSE 流式状态机 + fixture 测 |
| 10 | `ClaudeProvider` 整合 + httptest 测 |
| 11 | `Gateway` 编排 + 单测 |
| 12 | `sse.go` writer + HTTP `Handler` + mock-gateway 单测 |
| 13 | main 装配（Registry/Gateway/Recorder/routes） + compose 加 mock-provider + E2E 扩展 + README |

全部串行执行。

## 10. 验收清单

- [ ] `go test ./...` 全 PASS（不带 tag）
- [ ] `go test -tags=docker_integration ./...` 全 PASS（Slice 2 回归 + Slice 3 集成）
- [ ] `go vet ./...` 干净；`go build ./...` 干净
- [ ] `docker compose up -d --build` 后 `/healthz` 200
- [ ] `mock-provider` service 在 compose 中起来
- [ ] curl `/v1/chat/completions` 非流跑通
- [ ] curl `/v1/chat/completions` stream=true 跑通（SSE）
- [ ] curl `/v1/embeddings` 跑通
- [ ] `model_usage` 表里有 ok 与 error 两类行
- [ ] `audit_log` 表里有 `/v1/*` 路径请求
- [ ] git tree clean

## 11. 风险与开放问题

| 风险 | 缓解 |
|---|---|
| Claude 协议适配代码量大 + 维护负担 | Task 8/9/10 三步走 + fixture 守门 |
| Anthropic API 协议未来变动 | fixture 测有回归保护；每次 Anthropic 升级跑 bench 检测 |
| Ollama 用户用非 OpenAI-compat 端点 | base_url 由运维配置；Slice 3 只承诺 OpenAI 兼容路径 |
| api_key 通过 env 暴露给容器 | docker-compose 文档建议用 secrets；Slice 9 加固 |
| Stream backpressure（客户端断开但 provider 仍送） | yield 失败 → return error → provider 收 ctx 取消停 |
| 多个 system 消息拼接（Anthropic 单 system） | `\n\n` 分隔 + 测试覆盖 |
| usage 字段缺失（provider 不返） | record 0；不算错 |
| Provider 启动期不连通验证 | 首次调用才发现挂；运维通过监控察觉 |

开放问题：

1. mock-provider：纯 Go HTTP server（50 行）入仓 `internal/modelgw/mockserver/`；docker-compose 用 `build:` 指向该目录
2. P0 不做 `/admin/providers` 健康检查端点（留 P1）
3. compose seed 用 `default-mock` 指向 mock-provider；运维上线时 INSERT 真实 ollama/claude/openai 记录

## 12. ADR 摘要

| ID | 决策 | 理由 |
|---|---|---|
| ADR-21 | 对外用 OpenAI Chat Completions 协议 | 生态最广，调用方可用 OpenAI SDK |
| ADR-22 | `provider:model` 显式前缀 | 简单明确；网关不需查表 |
| ADR-23 | Provider 配置存 PG，热增不需重启 | 多环境运维友好；密钥不入 DB |
| ADR-24 | 流式仅支持 SSE | 全行业标准；ToolBus / 前端 / IDE 插件都好接 |
| ADR-25 | Token 仅记 provider usage | 简单；不预估 |
| ADR-26 | 失败快返不 fallback | P0 简单；P1 评估自动 fallback |
| ADR-27 | Claude provider 内部做协议适配 | 调用方零感知；Anthropic 升级隔离 |
| ADR-28 | api_key 走 env，DB 仅存 env 名 | 密钥不入库；与 ConfigCenter 风格一致 |
| ADR-29 | mock-provider 默认 seed | E2E 不依赖外部模型服务 |
| ADR-30 | Ollama Provider = OpenAIProvider 薄包装 | DRY；Ollama 0.4+ 已支持 OpenAI 兼容 |

## 13. 与 Spec 主文档对齐

主 spec §4.3 / §5.3 / §11 中 P0 列出：
- Model Gateway（Ollama + 1 个外部 API 兼容）✓ 本切片完成（且超额加 Claude + OpenAI）
- 内部用 OpenAI 协议 ✓
- 路由顺序：租户配置 → 模型显式指定 → 兜底默认 — Slice 3 仅做 "模型显式指定"，"租户配置 / 兜底默认" 留 P1（与现 spec §5.3 不冲突，是 P0 → P1 的合理切片）
- Token 用量出口拦截写计费/审计 ✓

未覆盖（留后续）：
- 租户路由策略（强制本地模型 only 等）
- 自动 fallback
- 配额
- Embeddings 在 Claude

---

**审核状态**：草稿，待用户复核。
