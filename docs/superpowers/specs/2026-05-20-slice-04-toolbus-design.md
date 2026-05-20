# Slice 4 — Tool Bus + Internal MCP 设计

| 字段 | 值 |
|---|---|
| 文档日期 | 2026-05-20 |
| 状态 | Draft — 待用户复核 |
| 范围 | 私有化 AI 编码 Agent — P0 第 4 个切片 |
| 前置 | Slice 1.5 / Slice 2 / Slice 3 已完成（HEAD `58a96ee`） |

---

## 1. 概述

本切片交付 **Tool Bus** —— Agent 与 Workflow 共用的工具调度层。对内提供 `Tool` Go 接口，内置 8 个 MCP-style tools（fs.read/write/list/glob、grep、shell.exec、llm.chat、llm.embed）；对外暴露 `POST /tools/invoke` 与 `GET /tools` HTTP API。所有 fs/shell 操作经 `sandbox.Runtime`（Slice 2），llm 操作经 `modelgw.Gateway`（Slice 3）。

**核心叙事**
- **统一调用入口**：未来 Agent / Workflow 都通过 ToolBus.Invoke 调工具，单点审计 + 统一错误格式 + JSON Schema 校验
- **in-process Go 接口**：Internal tools 是 Go struct 直接函数调用；不走 MCP JSON-RPC（避免不必要的序列化开销）
- **External MCP 留口**：未来 Slice 7+ 加 stdio/SSE adapter 接入第三方 MCP server 时，复用 Tool 接口
- **工具即叶子**：Slice 4 的工具都不互相调用；递归 / 编排留给 Slice 5 Agent
- **沙箱-tools 弱关联**：fs/shell 工具的 input 含 `sandbox_id`，调用方负责传对应沙箱；ToolBus 本身不持有沙箱状态

**不在 Slice 4 范围**
- External MCP server（stdio / SSE adapter）—— Slice 7+
- Per-tenant Tool ACL（哪些 tool 哪些租户可用）—— P1
- Tool 间编排 / pipeline —— Slice 5 Agent / Slice 6+ Workflow
- 流式工具调用（llm.chat 流式）—— Agent 自己直接调 modelgw.Gateway 走流式
- `http.*` / `vector.*` / `memory.*` / `workflow.*` 等其他工具 —— 未来切片

## 2. 前置条件

依赖 Slice 1.5 / 2 / 3 已完成；HEAD = `58a96ee`。

**Task 0 carry-over**（Slice 3 final review 留的）：
- `ProviderError.Body` 扫描 redact 已知 env value（30 行代码 + 测试）

## 3. 核心需求

| 维度 | 决策 |
|---|---|
| 协议 | HTTP `POST /tools/invoke` + `GET /tools`（OpenAI 兼容错误体） |
| 工具数（P0） | 8 个：fs.read/write/list/glob、grep、shell.exec、llm.chat、llm.embed |
| 内部调用 | in-process Go 接口（不走 MCP JSON-RPC） |
| fs/shell 实施 | 经 `sandbox.Runtime`（每会话独立容器） |
| llm.* 实施 | 经 `modelgw.Gateway` |
| sandbox 关联 | input args 里传 `sandbox_id`（调用方负责） |
| 工具 schema | 手写 JSON Schema（OpenAI tool calling 兼容） |
| schema 校验库 | `github.com/santhosh-tekuri/jsonschema/v6` |
| glob 匹配 | `github.com/bmatcuk/doublestar/v4`（Go side） |
| input/output 审计 | 只存 sha256，不存内容 |
| 多租户 | 通过下游（sandbox/modelgw）的 tenant 隔离；ToolBus 本身不做 ACL |

非功能：
- Bus.Invoke 调度开销（不含 tool 自身耗时）P50 < 5 ms
- Schema 校验 P50 < 1 ms（编译缓存）
- 100 并发 invoke 单实例可承载

## 4. 整体架构

```
+------------------------------------------------------------+
| HTTP 层 (gin) — 复用 Slice 1 auth + audit middleware        |
|  GET  /tools                  列出 tool                     |
|  POST /tools/invoke           调用 tool                     |
+------------------------------------------------------------+
                  | uses
                  v
+------------------------------------------------------------+
| internal/toolbus/                                          |
|  Bus                                                       |
|    ListTools(ctx, tenantID) -> []ToolDef                   |
|    Invoke(ctx, tenantID, userID, toolName, input) -> out   |
|                                                            |
|  Registry                                                  |
|    Register(Tool) / Get(name) / List()                     |
|                                                            |
|  Tool interface                                            |
|    Name / Description / Schema / Invoke                    |
|                                                            |
|  Schema (santhosh-tekuri/jsonschema/v6 编译缓存)            |
|                                                            |
|  InvocationRecorder (detached ctx 写库)                    |
|                                                            |
|  tools/  (子包,避免循环依赖)                                 |
|    fs.go         fs.read / fs.write / fs.list / fs.glob    |
|    grep.go       grep                                      |
|    shell.go      shell.exec                                |
|    llm.go        llm.chat / llm.embed                      |
+------------------------------------------------------------+
                  | uses                          | uses
                  v                                v
        sandbox.Runtime (Slice 2)        modelgw.Gateway (Slice 3)
        +------+                              +------+
        | PG   |                              | PG   |
        | model_usage, audit_log              |
        | tool_invocations <- toolbus 新增    |
        +------+                              +------+
```

**三个核心抽象**
1. `Tool` 接口 — 屏蔽具体工具实现；in-process Go 调用
2. `Registry` — 启动期注册全部 internal tools，运行时按 name 查找
3. `Bus` — 编排层：取 tool → schema 校验 → sha256 → invoke → record

## 5. 接口与数据模型

### 5.1 `Tool` 接口（`internal/toolbus/tool.go`）

```go
type Tool interface {
    Name() string
    Description() string
    Schema() json.RawMessage  // OpenAI tool calling 兼容 parameters
    Invoke(ctx context.Context, tenantID, userID uuid.UUID,
        input json.RawMessage) (json.RawMessage, error)
}

type ToolDef struct {
    Name        string          `json:"name"`
    Description string          `json:"description"`
    Parameters  json.RawMessage `json:"parameters"`
}
```

### 5.2 错误哨兵

```go
var (
    ErrToolNotFound      = errors.New("tool not found")
    ErrInvalidArguments  = errors.New("invalid arguments")
    ErrSandboxIDRequired = errors.New("sandbox_id required in args")
    ErrToolFailed        = errors.New("tool execution failed")
)
```

### 5.3 `Bus` 与 `Registry`

```go
type Registry struct { /* sync.RWMutex + map[string]Tool */ }
func NewRegistry() *Registry
func (r *Registry) Register(t Tool) error  // 重名返错
func (r *Registry) Get(name string) (Tool, bool)
func (r *Registry) List() []Tool  // 按 name 排序

type Bus struct {
    reg      *Registry
    recorder *InvocationRecorder
    schemas  map[string]*jsonschema.Schema  // 启动期编译
}
func NewBus(reg *Registry, rec *InvocationRecorder) (*Bus, error)  // schema 编译失败返错
func (b *Bus) ListTools(ctx context.Context, tenantID uuid.UUID) []ToolDef
func (b *Bus) Invoke(ctx context.Context, tenantID, userID uuid.UUID,
    toolName string, input json.RawMessage) (json.RawMessage, error)
```

### 5.4 数据库表

#### `tool_invocations` (migration 0007)

```sql
CREATE TABLE tool_invocations (
    id              BIGSERIAL PRIMARY KEY,
    occurred_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    tenant_id       UUID NOT NULL,
    user_id         UUID NOT NULL,
    tool_name       TEXT NOT NULL,
    status          TEXT NOT NULL,           -- "ok" / "error"
    error_class     TEXT NOT NULL DEFAULT '',
    duration_ms     INT NOT NULL,
    input_sha256    TEXT NOT NULL DEFAULT '',
    output_sha256   TEXT NOT NULL DEFAULT ''
);

CREATE INDEX tool_invocations_tenant_time_idx
    ON tool_invocations(tenant_id, occurred_at DESC);
```

### 5.5 HTTP API

| 方法 | 路径 | 鉴权 | Body | 返 |
|---|---|---|---|---|
| GET | `/tools` | Bearer | – | 200 `{"tools": [{name, description, parameters}, ...]}` |
| POST | `/tools/invoke` | Bearer | `{"tool": "<name>", "input": {...}}` | 200 `{"output": {...}}` / 4xx / 5xx OpenAI 风格错 |

### 5.6 八个 internal tools

#### `fs.read`

- **Description**: `Read a UTF-8 text file from the sandbox workspace. Path is relative to /workspace.`
- **Input**: `{sandbox_id: uuid, path: string}`
- **Output**: `{content: string, size: int}`
- **实现**: `sandbox.Runtime.ReadFile`。非法 UTF-8 字节用 `�` 替换。

#### `fs.write`

- **Description**: `Write content to a file in the sandbox workspace. Creates intermediate directories. Overwrites if exists.`
- **Input**: `{sandbox_id: uuid, path: string, content: string}`
- **Output**: `{bytes_written: int}`
- **实现**: `sandbox.Runtime.WriteFile`。

#### `fs.list`

- **Description**: `List files and directories under a sandbox path. Non-recursive.`
- **Input**: `{sandbox_id: uuid, path?: string}`（path 默认 `.`）
- **Output**: `{entries: [{name, type: "file|dir", size?: int}]}`
- **实现**: `sandbox.Runtime.Exec` 跑 `find <path> -mindepth 1 -maxdepth 1 -printf '%f\t%y\t%s\n'`，Go side 解析。

#### `fs.glob`

- **Description**: `Find files in the sandbox matching a glob pattern (e.g. '**/*.go', 'src/**/*.test.ts').`
- **Input**: `{sandbox_id: uuid, pattern: string, path?: string}`（path 默认 `/workspace`）
- **Output**: `{matches: [<relative path>, ...]}`
- **实现**: `sandbox.Runtime.Exec` 跑 `find <root> -type f`，结果用 `doublestar.Match` 在 Go side 过滤。

#### `grep`

- **Description**: `Search file contents in the sandbox using regex. Returns lines matching the pattern with file:line context.`
- **Input**: `{sandbox_id: uuid, pattern: string, path?: string, case_insensitive?: bool, max_results?: int}`
- **Output**: `{matches: [{path, line, text}]}`
- **实现**: `sandbox.Runtime.Exec` 跑 `rg --json -n [-i] <pattern> <path>`（pca/sandbox:base 已装 ripgrep）。解析 `--json` 输出。`max_results` 默认 100，上限 1000，Go side 截断。

#### `shell.exec`

- **Description**: `Run a shell command inside the sandbox. Returns exit code, stdout, stderr.`
- **Input**: `{sandbox_id: uuid, cmd: string[], working_dir?: string, timeout_sec?: int}`
- **Output**: `{exit_code, stdout, stderr, truncated, duration_ms, timed_out}`
- **实现**: 透传 `sandbox.Runtime.Exec`。

#### `llm.chat`

- **Description**: `Send a Chat Completion request to the configured LLM provider. Returns the assistant message.`
- **Input**: `{model: string, messages: [{role, content}], temperature?: number, max_tokens?: int}`
- **Output**: `{content: string, usage: {prompt_tokens, completion_tokens, total_tokens}}`
- **实现**: `modelgw.Gateway.ChatCompletion`（非流式）。**不**透传 tools / tool_choice / seed。

#### `llm.embed`

- **Description**: `Compute embedding vectors for one or more text strings.`
- **Input**: `{model: string, input: [string, ...]}`（minItems=1, maxItems=100）
- **Output**: `{vectors: [[float, ...], ...], usage: {prompt_tokens}}`
- **实现**: `modelgw.Gateway.Embeddings`。

### 5.7 错误映射（handler）

| 故障 | HTTP | type | code |
|---|---|---|---|
| 缺/坏 JWT | 401 | auth_error | missing_token / invalid_token |
| 请求体非法 JSON | 400 | invalid_request_error | bad_request |
| `tool` 字段缺/空 | 400 | invalid_request_error | tool_required |
| Tool 不存在 | 404 | invalid_request_error | tool_not_found |
| schema 校验失败 | 400 | invalid_request_error | invalid_arguments |
| `ErrSandboxIDRequired` | 400 | invalid_request_error | sandbox_id_required |
| `sandbox.ErrSandboxNotFound` | 404 | invalid_request_error | sandbox_not_found |
| `sandbox.ErrSandboxNotReady` | 409 | invalid_request_error | sandbox_not_ready |
| `sandbox.ErrTooLarge` | 413 | invalid_request_error | payload_too_large |
| `sandbox.ErrPathOutsideWorkspace` | 400 | invalid_request_error | path_outside_workspace |
| `modelgw.ErrProviderUnreachable` | 502 | provider_error | provider_unreachable |
| `modelgw.ErrProviderError` | 502 | provider_error | provider_error |
| `*modelgw.ProviderError` StatusCode==429 | 429 | rate_limit_error | provider_rate_limit |
| `modelgw.ErrUnsupportedFeature` | 400 | invalid_request_error | unsupported_feature |
| panic / 其他 | 500 | server_error | internal |

### 5.8 包结构

```
internal/toolbus/
├── tool.go              Tool 接口 + ToolDef
├── errors.go            错误哨兵
├── registry.go          Registry
├── registry_test.go
├── schema.go            JSON Schema 编译/校验 helper
├── schema_test.go
├── repo.go              InvocationRepo
├── recorder.go          InvocationRecorder
├── repo_test.go         dockertest 集成
├── recorder_test.go
├── bus.go               Bus 编排
├── bus_test.go          mockTool 单测
├── handler.go           HTTP handlers + 错误映射
├── handler_test.go      mockBus 单测
└── tools/
    ├── fs.go            fs.read / fs.write / fs.list / fs.glob
    ├── fs_test.go
    ├── fs_integration_test.go  (docker_integration)
    ├── grep.go
    ├── grep_test.go
    ├── shell.go
    ├── shell_test.go
    ├── shell_integration_test.go
    └── llm.go           llm.chat / llm.embed
    └── llm_test.go
    └── llm_integration_test.go

internal/db/migrations/
├── 0007_create_tool_invocations.up.sql
└── 0007_create_tool_invocations.down.sql

internal/modelgw/
└── redact.go            (新增, Task 0)
```

## 6. 数据流

### 6.1 List Tools — GET /tools

handler.list → bus.ListTools(ctx, tid) → reg.List() → 8 Tool → 转 []ToolDef → JSON 200。无 PG 写入。

### 6.2 Invoke Tool — POST /tools/invoke

```
handler.invoke
  ├ bind body + claims
  └ bus.Invoke(ctx, tid, uid, toolName, input)
        ├ reg.Get(toolName) → 不存在 ErrToolNotFound
        ├ schema.Validate(input) → 失败 ErrInvalidArguments
        ├ inputSHA := sha256(input)
        ├ tool.Invoke(ctx, tid, uid, input) ──► 下游 (sandbox.Runtime / modelgw.Gateway)
        ├ outputSHA := sha256(output)（错误 path 留空）
        ├ recorder.Record(detached ctx + 5s timeout)
        └ return output, err
```

handler 根据 err 类型映射 HTTP code（见 5.7）。

### 6.3 三表互补审计

| 表 | 写入主体 | 用途 |
|---|---|---|
| `audit_log` | `audit.Middleware`（HTTP 层） | 通用流量审计 |
| `model_usage` | `modelgw.UsageRecorder` | LLM 调用按 provider/model 计费 |
| `tool_invocations` | `toolbus.InvocationRecorder` | 工具调用按 tool 聚合 |

`llm.chat` 工具调用会同时触发三表写入（HTTP audit + tool record + model usage）；`fs.read` 仅触发两表（audit + tool record）。

### 6.4 Slice 3 redact carry-over（Task 0）

新增 `internal/modelgw/redact.go`：

```go
func redact(s string, envNames []string) string {
    for _, name := range envNames {
        v := os.Getenv(name)
        if v != "" && len(v) >= 8 {
            s = strings.ReplaceAll(s, v, "[REDACTED]")
        }
    }
    return s
}
```

调用点：每个 Provider 构造 `ProviderError` 时把自己 `apiKeyEnv` 传进 redact：
- `provider_openai.go` 非流 + 流式两处
- `provider_claude.go` 非流 + 流式两处

只 redact `ProviderError.Body`；不做 generic-secrets 扫描（不扫 `sk-` 等 pattern，避免误伤）。

### 6.5 横切观测

每次 `Bus.Invoke` 产生：
- `tool_invocations` 一行（永远写）
- `audit_log` 一行（HTTP middleware 自动）
- OTel span `toolbus.invoke`，attrs：`tool_name, status, error_class, duration_ms, input_sha256, output_sha256, tenant_id`

**不**记录 input/output 内容（仅 sha256）。

### 6.6 启动期装配

```go
toolRegistry := toolbus.NewRegistry()
_ = toolRegistry.Register(tools.NewFSRead(sandboxDriver))
_ = toolRegistry.Register(tools.NewFSWrite(sandboxDriver))
_ = toolRegistry.Register(tools.NewFSList(sandboxDriver))
_ = toolRegistry.Register(tools.NewFSGlob(sandboxDriver))
_ = toolRegistry.Register(tools.NewGrep(sandboxDriver))
_ = toolRegistry.Register(tools.NewShellExec(sandboxDriver))
_ = toolRegistry.Register(tools.NewLLMChat(modelGateway))
_ = toolRegistry.Register(tools.NewLLMEmbed(modelGateway))

invocationRecorder := toolbus.NewInvocationRecorder(
    toolbus.NewInvocationRepo(pool),
    func(err error) { log.Printf("tool invocation record: %v", err) })

toolBus, err := toolbus.NewBus(toolRegistry, invocationRecorder)
if err != nil {
    return fmt.Errorf("toolbus: %w", err)
}
toolHandler := toolbus.NewHandler(toolBus)
// register 闭包追加 toolHandler.Register(protected)
```

## 7. 测试策略

### 7.1 单元

| 文件 | 覆盖 |
|---|---|
| `registry_test.go` | 注册 / 查找 / 排序 / 重名报错 |
| `schema_test.go` | 编译缓存 + 校验典型 |
| `errors_test.go` | 哨兵存在 |
| `bus_test.go` | mockTool：成功 / schema fail / tool fail / recorder 永远写 / sha256 |
| `repo_test.go` | dockertest PG：InvocationRepo Insert + CountByTenant |
| `recorder_test.go` | dockertest PG：detached ctx + onErr |
| `tools/fs_test.go` | mockRuntime：4 个 fs tool 各自的 input → 下游调用、output 映射 |
| `tools/grep_test.go` | mockRuntime：rg --json 解析 |
| `tools/shell_test.go` | mockRuntime：透传语义 |
| `tools/llm_test.go` | mockGateway：chat / embed input → output |
| `handler_test.go` | mockBus：HTTP 鉴权 / 错误映射 / 成功 |

### 7.2 集成（`docker_integration` build tag）

- `tools/fs_integration_test.go`：起真沙箱跑 fs.read/write/list/glob 端到端
- `tools/shell_integration_test.go`：shell.exec 真容器 echo
- `tools/llm_integration_test.go`：起 httptest mock-provider 跑 llm.chat / llm.embed

### 7.3 E2E

扩展 `test-e2e.sh` 增加 `[13/16]` - `[16/16]`：
- `[13]` GET /tools 列 8 个 tool
- `[14]` fs.write + fs.read round-trip
- `[15]` shell.exec ls 含 hello.txt
- `[16]` llm.chat → "hello from mock" + 验证 `tool_invocations` 行

最后 `E2E PASS`。

### 7.4 性能基线（informational）

- Bus.Invoke 调度开销 P50 < 5 ms
- Schema 校验 P50 < 1 ms
- 100 并发 invoke 单实例可承载

## 8. Task 拆解（14 Task）

| Task | 内容 |
|---|---|
| 0 | Slice 3 carry-over：`modelgw/redact.go` + 4 处 ProviderError 构造点改写 + 测试 |
| 1 | `toolbus/tool.go` + `errors.go` + 单测 |
| 2 | `toolbus/registry.go` + 单测 |
| 3 | `toolbus/schema.go` JSON Schema 编译/校验 + 单测 |
| 4 | migration 0007 + `repo.go` + `recorder.go` + dockertest 测 |
| 5 | `toolbus/bus.go` Bus 编排 + mockTool 单测 |
| 6 | `tools/fs.go` 4 个 fs tool + mockRuntime 单测 |
| 7 | `tools/grep.go` + 单测 |
| 8 | `tools/shell.go` + 单测 |
| 9 | `tools/llm.go` chat + embed + mockGateway 单测 |
| 10 | `toolbus/handler.go` HTTP handlers + 错误映射 + mockBus 测 |
| 11 | main 装配 + go build/vet/test |
| 12 | `docker_integration` 测试（fs + shell + llm）|
| 13 | E2E 扩展 + README 更新 |

全部串行执行。

## 9. 验收清单

- [ ] `go test ./...` 全 PASS（不含 docker_integration tag）
- [ ] `go test -tags=docker_integration ./...` 全 PASS（Slice 2/3 回归 + Slice 4 集成）
- [ ] `go vet / build` 干净
- [ ] `docker compose up` `/healthz` 200
- [ ] `test-e2e.sh` 跑通（最后 `E2E PASS`，16 步）
- [ ] `tool_invocations` 表有 status=ok 行
- [ ] `audit_log` 含 `/tools/*` 路径
- [ ] GET /tools 返 8 个 tool
- [ ] redact 生效：`provider_openai_test.go` / `provider_claude_test.go` 有 redact 用例 PASS
- [ ] git tree clean

## 10. 风险与开放问题

| 风险 | 缓解 |
|---|---|
| Task 6 装 4 个 fs tool 偏大 | 同模式（mockRuntime）批量产；若 review 期间觉得超大可拆 6a/6b |
| santhosh-tekuri/jsonschema/v6 学习成本 | API 简洁；只用 `Compile + Validate`；编译错误清晰 |
| doublestar 与 sandbox 内 find 的语义差异 | 文档化 `**` / `?` / `[...]` 支持范围；`?(...)` 等 extglob 不支持 |
| llm.chat 不支持流式 | Agent 直接调 modelgw.Gateway；llm.chat 给 workflow 节点用 |
| tool input 含 sandbox_id 用户传错 | sandbox.Runtime 内已校验 tenant；越界 → 404 sandbox_not_found |
| 工具调用循环 | Slice 4 工具都是叶子；递归留 Agent 控制 |
| redact 字符串替换性能 | 单次 strings.ReplaceAll 在 4KB body 上微秒级 |
| schema 校验库引入 indirect deps | 选 santhosh-tekuri/jsonschema 引入约 5 个 indirect（与 viper 比小） |

### 开放问题

1. Slice 4 不实现 `/admin/tools` 管理 endpoint（启停 tool） —— P1
2. 不实现 tool dry-run（仅校验 schema 不执行） —— Workflow 阶段需要时再加
3. tool 间互调禁止 —— 显式不支持

## 11. ADR 摘要

| ID | 决策 | 理由 |
|---|---|---|
| ADR-31 | Tool 接口 in-process Go 调用 | 避免 JSON-RPC 序列化开销；External MCP 用 adapter 接入 |
| ADR-32 | Tool 不持有 session/sandbox 状态 | sandbox_id 作为 input args；Bus 是 stateless |
| ADR-33 | 手写 JSON Schema 与 OpenAI 兼容 | 直接拿给 Agent 用作 tool_def；reflect 生成不可靠 |
| ADR-34 | input/output 仅存 sha256 | 防泄密；按 sha 关联 audit_log 调试 |
| ADR-35 | llm.chat 仅支持非流式 | Agent 直接调 modelgw.Gateway 走流式 |
| ADR-36 | 工具都是叶子 | 简化；递归由 Agent 控制 |
| ADR-37 | doublestar 在 Go side 做 glob | 简单可控；不依赖容器内 shell 通配规则 |
| ADR-38 | santhosh-tekuri/jsonschema/v6 | 纯 Go、性能好、draft-7 完整支持 |
| ADR-39 | Provider redact 只查已知 env value | 避免 generic-secrets pattern 误伤 |
| ADR-40 | tool_invocations 与 model_usage 互补不重复 | 一次 llm.chat 同时写两表（不同维度） |

## 12. 与 Spec 主文档对齐

主 spec §4.3 Tool Bus / §5.2 单轮消息流 / §6 数据流 中提及：
- "Internal MCP servers (builtin)" ✓ 8 个 tool
- "External MCP servers (用户/租户接入)" ⏭ Slice 7+
- "fs.* / shell.* / llm.* / http.* / memory.* / workflow.*" — Slice 4 完成前 3 类（fs/shell/llm）；后续切片补 http/memory/workflow
- "Workflow 节点 = MCP 工具调用" — Slice 4 提供调度层；Workflow Engine 是 Slice 6+ 主题
- "审计记录 tool name + tenant + status + duration，不记内容" ✓

---

**审核状态**：草稿，待用户复核。
