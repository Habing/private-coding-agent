# Slice 6 — Session API + WebSocket 设计

| 字段 | 值 |
|---|---|
| 文档日期 | 2026-05-20 |
| 状态 | Draft — 待用户复核 |
| 范围 | 私有化 AI 编码 Agent — P0 第 6 个切片 |
| 前置 | Slice 1.5 / 2 / 3 / 4 / 5 已完成（HEAD `85bad43`） |

---

## 1. 概述

本切片交付 **Session Orchestrator** —— 会话生命周期管理 + 消息持久化 + WebSocket 流式通道。在 Slice 5 已经实现的 `agent.Engine.Run(ctx, RunInput, yield func(Event) error) error` 之上加：
- `sessions` / `messages` 两张 PG 表，多租户隔离
- REST：`POST /sessions` / `GET /sessions` / `GET /sessions/:id` / `DELETE /sessions/:id` / `GET /sessions/:id/messages`
- WebSocket：`GET /sessions/:id/ws`，客户端实时发用户消息，服务端把 engine 事件流回 + 落库

**核心叙事**
- **会话即上下文**：一条 session 对应一段连续对话，model + profile 在创建时定，每轮发消息复用历史
- **消息即审计**：append-only `messages` 表，每个 agent.Event 都一一映射成一行（user / assistant / tool / system）
- **WS 替代 SSE**：双向需要（客户端中途发新消息、服务端推事件、心跳）；不引 SSE 单向限制
- **Engine 不变**：Slice 5 的 `agent.Engine` 直接被 Service 调用，不做改动；事件管线在 Service/WSHandler 层
- **弱绑 sandbox**：session 不持有 sandbox_id；tool 调用的 sandbox 仍由 LLM 在 tool input 里给出（保持和 slice 5 一致）

**不在 Slice 6 范围**
- 上下文压缩 / 摘要 —— Slice 7+
- Memory Loader（按 token 预算注入相关记忆）—— Slice 7
- 多 profile 切换 / Workflow Authoring Agent profile —— Slice 7+
- 会话分支 / fork —— P1
- Session 间共享 —— P2
- Sandbox 与 session 强绑定（创建 session 自动 create sandbox）—— P1
- 流式 LLM token（assistant message 整段一次性来）—— mock-provider 已支持 stream，但 Engine 当前接 non-stream，本切片不变

## 2. 前置条件

依赖 Slice 1.5 / 2 / 3 / 4 / 5 已完成；HEAD = `85bad43`。

**Carry-over**：无。Slice 5 acceptance 全部通过。

## 3. 核心需求

| 维度 | 决策 |
|---|---|
| 协议 | REST + WebSocket（同一 host，复用 JWT Bearer） |
| 持久化 | PG 两张表：`sessions` + `messages`，UUID PK，tenant 隔离 |
| Message append-only | `messages` 表只 INSERT，不 UPDATE / DELETE；session archive 也保留消息 |
| Seq 严格递增 | `(session_id, seq)` UNIQUE；server 计算 `MAX(seq)+1` |
| WS 库 | `github.com/gorilla/websocket v1.5.x`（生态最广、tested、和 gin 兼容） |
| WS 鉴权 | gin auth middleware 在 protected group 跑，握手前已校 JWT；不在 ws 帧里再嵌 token |
| WS 帧 | 文本 JSON 帧；server→client 三种 type；client→server 两种 type |
| 心跳 | gorilla ping/pong，30s read deadline、25s write ping |
| 错误处理 | Engine 错 → 写 error 帧 + close；DB 错 → 写 error 帧 + close；客户端断开 → ctx cancel → engine 自然退出 |
| 多租户 | 所有 repo 方法带 `tenant_id` 过滤；跨租户访问返 404（不暴露存在性） |
| 跨域 | `server.ws_allowed_origins []string` 配置；默认 `["*"]`（部署收紧） |

非功能：
- WS 建连 + 首事件延迟 < 100 ms（不含 LLM）
- 1000 个并发 WS 连接单实例可承载（每连 ~1 KB heap）
- Append 一条 message < 5 ms（PG 单 INSERT）

## 4. 整体架构

```
+--------------------------------------------------------------+
| HTTP 层 (gin) — 复用 Slice 1 auth + audit middleware           |
|  POST /sessions               创建 session                     |
|  GET  /sessions               列出当前 user 的 session         |
|  GET  /sessions/:id           session 详情                     |
|  DELETE /sessions/:id         archive                          |
|  GET  /sessions/:id/messages  消息历史                          |
|  GET  /sessions/:id/ws        WebSocket 升级                   |
+--------------------------------------------------------------+
                  | uses
                  v
+--------------------------------------------------------------+
| internal/session/                                            |
|  Service                                                     |
|    CreateSession(ctx, tid, uid, req) -> *Session             |
|    ListSessions(ctx, tid, uid) -> []Session                  |
|    GetSession(ctx, tid, uid, sid) -> *Session                |
|    ArchiveSession(ctx, tid, uid, sid) -> error               |
|    ListMessages(ctx, tid, uid, sid) -> []Message             |
|    SendMessage(ctx, tid, uid, sid, content,                  |
|                onEvent func(agent.Event) error) -> error     |
|                                                              |
|  SessionRepo                                                 |
|    Create / Get / List / Archive                             |
|                                                              |
|  MessageRepo                                                 |
|    Append(ctx, *Message) -> error                            |
|    List(ctx, tid, sid) -> []Message                          |
|    NextSeq(ctx, sid) -> int64                                |
|                                                              |
|  Handler   (REST)                                            |
|  WSHandler (WebSocket upgrade + per-conn loop)               |
+--------------------------------------------------------------+
                  | uses
                  v
        agent.Engine.Run (Slice 5)
                  | uses
                  v
        modelgw.Gateway (Slice 3) + toolbus.Bus (Slice 4)
```

**两个核心抽象**

1. `session.Service` — 业务编排层；handle/wshandler 都通过它访问 engine + repo
2. `session.WSHandler` — 单连接生命周期管理：upgrade → read loop → 调 Service.SendMessage → 写帧 → 心跳 → close

## 5. 接口与数据模型

### 5.1 领域类型（`internal/session/types.go`）

```go
type Session struct {
    ID          uuid.UUID `json:"id"`
    TenantID    uuid.UUID `json:"tenant_id"`
    OwnerUserID uuid.UUID `json:"owner_user_id"`
    Title       string    `json:"title"`
    Model       string    `json:"model"`
    Profile     string    `json:"profile"`
    Status      string    `json:"status"`     // active|archived
    CreatedAt   time.Time `json:"created_at"`
    UpdatedAt   time.Time `json:"updated_at"`
}

type Message struct {
    ID         uuid.UUID       `json:"id"`
    SessionID  uuid.UUID       `json:"session_id"`
    TenantID   uuid.UUID       `json:"tenant_id"`
    Seq        int64           `json:"seq"`
    Role       string          `json:"role"`           // user|assistant|tool|system
    Content    string          `json:"content"`
    ToolCallID string          `json:"tool_call_id,omitempty"`
    ToolCalls  json.RawMessage `json:"tool_calls,omitempty"`
    Metadata   json.RawMessage `json:"metadata,omitempty"`
    CreatedAt  time.Time       `json:"created_at"`
}
```

### 5.2 错误哨兵（`internal/session/errors.go`）

```go
var (
    ErrSessionNotFound    = errors.New("session not found")
    ErrSessionArchived    = errors.New("session is archived")
    ErrInvalidStatus      = errors.New("invalid session status")
    ErrEmptyContent       = errors.New("message content is empty")
)
```

### 5.3 数据库表

#### `sessions` (migration 0008)

```sql
CREATE TABLE sessions (
    id              UUID PRIMARY KEY,
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    owner_user_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title           TEXT NOT NULL DEFAULT '',
    model           TEXT NOT NULL,
    profile         TEXT NOT NULL DEFAULT 'coding',
    status          TEXT NOT NULL DEFAULT 'active',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX sessions_tenant_owner_idx
    ON sessions(tenant_id, owner_user_id, created_at DESC);
```

#### `messages` (migration 0008)

```sql
CREATE TABLE messages (
    id              UUID PRIMARY KEY,
    session_id      UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    tenant_id       UUID NOT NULL,
    seq             BIGINT NOT NULL,
    role            TEXT NOT NULL,
    content         TEXT NOT NULL DEFAULT '',
    tool_call_id    TEXT NOT NULL DEFAULT '',
    tool_calls      JSONB,
    metadata        JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (session_id, seq)
);

CREATE INDEX messages_session_seq_idx ON messages(session_id, seq);
```

**Tenant 隔离**：所有读路径都带 `WHERE tenant_id = $1`；删 session 走 CASCADE 自动删 messages。

### 5.4 REST API

| 方法 | 路径 | Body | 返 |
|---|---|---|---|
| POST | `/sessions` | `{"model":"...","profile":"coding","title":""}` | 200 `Session` JSON |
| GET | `/sessions` | – | 200 `{"sessions":[Session,...]}` |
| GET | `/sessions/:id` | – | 200 `Session` / 404 |
| DELETE | `/sessions/:id` | – | 204 / 404（status 改 archived，不真删） |
| GET | `/sessions/:id/messages` | – | 200 `{"messages":[Message,...]}` / 404 |

错误体格式沿用 `httpx`（已有的简单 `{"error":"<msg>"}`）；不沿用 modelgw 的 OpenAI 错误结构（REST 已经够用）。

### 5.5 WebSocket 协议

#### 升级

```
GET /sessions/:id/ws HTTP/1.1
Authorization: Bearer <jwt>
Connection: Upgrade
Upgrade: websocket
Sec-WebSocket-Version: 13
Sec-WebSocket-Key: ...
```

- `:id` 必须属于 `claims.TenantID` 的 session 且 `owner_user_id == claims.UserID`，否则 401 / 404
- `Origin` 校验：在 `cfg.Server.WSAllowedOrigins` 白名单内（`["*"]` 默认放行）
- 子协议：无（不用 `Sec-WebSocket-Protocol`）

#### 帧 — 客户端 → 服务端

| type | 字段 | 含义 |
|---|---|---|
| `user_message` | `content: string` | 用户发的一条 user message；触发一次 engine.Run |
| `ping` | – | 应用层 ping（gorilla 的协议层 ping/pong 已自动） |

非法 type、非 JSON、`user_message` 时 `content` 为空 → 写 error 帧 + close 1003。

#### 帧 — 服务端 → 客户端

| type | 字段 | 含义 |
|---|---|---|
| `event` | `event: agent.Event` | engine 每 yield 一个 Event 转发一帧 |
| `done` | `seq: int64` | 一次 SendMessage 调用结束（assistant 已 final），seq 是最后一条 message 的 seq |
| `error` | `message: string`, `code: string` | engine / DB 错；服务端会接着关连接 |
| `pong` | – | 对 `ping` 帧的回 |

`event.event.kind` 五种：`assistant_message` / `tool_call` / `tool_result` / `final` / `error`（沿用 slice 5 EventKind）。

#### 关闭码

| Code | 含义 |
|---|---|
| 1000 | 正常关闭（done 之后） |
| 1003 | 不支持的帧（非法 type / 非 JSON） |
| 1008 | 策略违反（auth/owner mismatch — 但通常已在 upgrade 前 401） |
| 1011 | 内部错（engine / DB 错） |
| 4001 | session 已 archived（4xxx 自定义） |

#### 心跳

- gorilla `SetReadDeadline(now + 60s)`；每收 pong 重置
- 服务端每 25 s 自动 `WriteControl(PingMessage)`；客户端 gorilla / 浏览器 ws 默认自动回 pong
- 没收到 pong → read 超时 → close

### 5.6 Service 调用流程：`SendMessage`

```
service.SendMessage(ctx, tid, uid, sid, content, onEvent)
  ├ session := sessions.Get(ctx, tid, uid, sid)
  │     如果 archived → ErrSessionArchived
  ├ history := messages.List(ctx, tid, sid)
  ├ userMsg := Message{role:"user", content, seq: NextSeq(...)}
  ├ messages.Append(ctx, userMsg)
  ├ in := agent.RunInput{
  │     TenantID: tid, UserID: uid,
  │     Model: session.Model,
  │     ProfileName: session.Profile,
  │     Messages: append(historyToChatMessages(history), {role:user, content}),
  │ }
  └ engine.Run(ctx, in, func(evt agent.Event) error {
        // 1. 落库
        msg := eventToMessage(evt, NextSeq)
        if msg != nil { messages.Append(ctx, msg) }
        // 2. 通知调用方（WSHandler 写帧）
        return onEvent(evt)
    })
```

#### `historyToChatMessages` 映射

| Message.Role | 转 ChatMessage |
|---|---|
| user / system | `{Role, Content}` |
| assistant | `{Role, Content, ToolCalls: from json}` |
| tool | `{Role, Content, ToolCallID, Name}` |

#### `eventToMessage` 映射（哪些 Event 落库）

| EventKind | 落库 |
|---|---|
| `assistant_message` | `role:assistant, content, tool_calls (json)` |
| `tool_call` | **不落库**（已在 assistant_message 的 tool_calls 里） |
| `tool_result` | `role:tool, tool_call_id, content=output, metadata={truncated,original_size,tool_error,tool_name}` |
| `final` | **不落库**（已在最后那条 assistant_message 里；这一条只是同内容的最终化通知） |
| `error` | `role:system, content=error msg, metadata={kind:"error", finish_reason}` |

这样 `messages` 表恢复成线性对话历史时直接传给 LLM 即可；中间的 `tool_call` 和 `final` 是流式提示帧，不需要落库。

### 5.7 错误映射（REST handler）

| 故障 | HTTP | 错误体 |
|---|---|---|
| 缺/坏 JWT | 401 | `{"error":"unauthorized"}` |
| 请求体非法 JSON | 400 | `{"error":"bad request: <msg>"}` |
| `model` 字段缺 | 400 | `{"error":"model required"}` |
| `ErrSessionNotFound` / 跨租户 | 404 | `{"error":"session not found"}` |
| `ErrSessionArchived` | 409 | `{"error":"session archived"}` |
| panic / 其他 | 500 | `{"error":"internal"}` |

### 5.8 错误映射（WSHandler）

| 故障 | 帧 | 关闭码 |
|---|---|---|
| session 不存在 / 跨租户 | (在 upgrade 前) HTTP 404 | – |
| session archived | `{type:"error",code:"session_archived"}` | 4001 |
| 非法 client 帧 | `{type:"error",code:"bad_frame"}` | 1003 |
| `content` 为空 | `{type:"error",code:"empty_content"}` | 1003 |
| `engine.Run` 返错 | `{type:"error",code:"engine_failed",message:...}` | 1011 |
| DB 错（append message 失败）| `{type:"error",code:"persist_failed"}` | 1011 |
| 客户端断开 | – | 自然结束 |

### 5.9 包结构

```
internal/session/
├── types.go                   Session / Message / 帧 type 常量
├── errors.go                  错误哨兵
├── errors_test.go
├── repo.go                    SessionRepo + MessageRepo
├── repo_test.go               dockertest 集成
├── service.go                 Service 编排
├── service_test.go            mockEngine + 真 repo (dockertest) 单测
├── handler.go                 REST handlers
├── handler_test.go            mockService 单测
├── wshandler.go               WS upgrade + 帧循环 + 心跳
└── wshandler_test.go          httptest + websocket.Dialer 单测

internal/db/migrations/
├── 0008_create_sessions.up.sql
└── 0008_create_sessions.down.sql
```

## 6. 数据流

### 6.1 创建 session — POST /sessions

```
handler.create
  ├ bind body + claims
  ├ validate model 非空
  └ service.CreateSession
        ├ Session{ID:uuid.New(), TenantID, OwnerUserID, Title, Model, Profile, Status:"active"}
        └ sessionRepo.Create
              └ INSERT INTO sessions
```

### 6.2 WS 流：客户端发 user_message

```
wshandler.serve
  ├ verify session 归属 (service.GetSession)
  ├ upgrader.Upgrade  → wsConn
  ├ go readLoop:
  │     for {
  │       msg := wsConn.ReadJSON
  │       switch msg.type:
  │         user_message → 启 goroutine 跑 service.SendMessage(ctx, ..., onEvent=writeFrame)
  │         ping         → writeFrame({type:"pong"})
  │         else         → writeFrame(error) + close
  │     }
  │
  ├ writeFrame(frame):
  │     mu.Lock; wsConn.WriteJSON(frame); mu.Unlock
  │
  └ 心跳: setReadDeadline + setPongHandler；25s ticker WriteControl(Ping)
```

每条 `user_message` 只允许一次并发的 `SendMessage`；同连接收到第二条时排队（简化为：第一条没完前，第二条 → error 帧 `code:"busy"`）。

### 6.3 落库与广播双管线

`service.SendMessage` 拿到 engine 的每个 Event 后：
1. 同步 `messages.Append`（DB INSERT）— 阻塞，失败则返错给 engine yield 让 Run 中断
2. 同步 `onEvent(evt)` — 由 WSHandler 写帧；如果 client 已断，`onEvent` 返 `context.Canceled` 让 engine 提早结束

如果 1 成功 2 失败：DB 已有记录，连接也确实断了，下次客户端再连可以 `GET /sessions/:id/messages` 看到历史。这是预期行为。

### 6.4 客户端断开 → engine 退出

WSHandler 用 `ctx, cancel := context.WithCancel(c.Request.Context())`：
- read loop 收到 `websocket.CloseError` → cancel
- engine.Run 内部 `gw.ChatCompletion(ctx, ...)` 会因 ctx canceled 返错
- `service.SendMessage` 把这个错向上抛
- service 退出，但落库已发生的部分仍保留

### 6.5 横切观测

- 每次 `Service.SendMessage` 一个 OTel span `session.send_message`，attrs：`session_id, tenant_id, user_id, events_count, duration_ms, status`
- 每帧不打 span（数据量太大）；engine 内部已有 span
- `audit_log` 由 HTTP middleware 自动写 `/sessions/*`（WS upgrade 也是一个 GET，会被 audit）

### 6.6 启动期装配

```go
sessionRepo := session.NewSessionRepo(pool)
messageRepo := session.NewMessageRepo(pool)
sessionSvc := session.NewService(sessionRepo, messageRepo, agentEngine)
sessionHandler := session.NewHandler(sessionSvc)
sessionWSHandler := session.NewWSHandler(sessionSvc, cfg.Server.WSAllowedOrigins)
// register 闭包追加:
sessionHandler.Register(protected)
sessionWSHandler.Register(protected)
```

## 7. 测试策略

### 7.1 单元

| 文件 | 覆盖 |
|---|---|
| `errors_test.go` | 哨兵存在 |
| `repo_test.go` | dockertest：Create/Get/List/Archive；MessageRepo Append/List/NextSeq；跨租户 |
| `service_test.go` | mockEngine + 真 repo：CreateSession/GetSession/ListSessions/ArchiveSession/ListMessages；SendMessage 落库 + onEvent；archived session 拒绝；跨租户 404 |
| `handler_test.go` | mockService：5 个 endpoint 的 200/400/404 + auth |
| `wshandler_test.go` | httptest + gorilla `Dialer`：建连、user_message→event→done、bad frame→close 1003、archived→close 4001、client disconnect→ctx cancel |

### 7.2 集成（`docker_integration` build tag）

无新增。复用 slice 5 集成测试（agent.Engine 经过 toolbus + sandbox + modelgw）；session 层作为薄编排层，单测 + E2E 已覆盖。

### 7.3 E2E

扩展 `test-e2e.sh`，把 18 步改 21 步，末尾追加：

- `[19/21]` POST /sessions 创建一个 session，断 status=active
- `[20/21]` GET /sessions 列出含上一步那个 id；GET /sessions/:id/messages 初始为空数组
- `[21/21]` WS 通过 docker `solsson/websocat`（fallback `legrandin/wscat`）连 `ws://localhost:8080/sessions/$SID/ws`，发 `{"type":"user_message","content":"hi"}`，断输出含 `"type":"event"` 和 `"kind":"final"`

最后 `E2E PASS`。

### 7.4 性能基线（informational）

- WS 建连延迟 < 50 ms
- 落库一条 message < 5 ms（PG 单 INSERT）

## 8. Task 拆解（10 Task）

| Task | 内容 |
|---|---|
| 0 | 本文 — design spec |
| 1 | migration 0008 sessions + messages + dockertest verify |
| 2 | types.go + errors.go + repo.go + repo_test.go（dockertest） |
| 3 | service.go + service_test.go（mockEngine + 真 repo） |
| 4 | handler.go + handler_test.go（mockService） |
| 5 | wshandler.go + wshandler_test.go（httptest + gorilla Dialer） |
| 6 | go.mod 添 gorilla/websocket；cmd/server/main.go 装配；config.example.yaml 加 ws_allowed_origins |
| 7 | test-e2e.sh 18→21 步 |
| 8 | README 勾选 slice 6 + 端点表追加 |
| 9 | 正式 slice plan 文件（TDD checkbox 格式）|

全部串行执行。

## 9. 验收清单

- [ ] `go test ./...` 全 PASS（不含 docker_integration）
- [ ] `go test -tags=docker_integration ./...` 全 PASS（回归）
- [ ] `go vet / build` 干净
- [ ] `docker compose up` `/healthz` 200
- [ ] `test-e2e.sh` 跑通（最后 `E2E PASS`，21 步）
- [ ] `POST /sessions` 创建后 DB 有 active 行
- [ ] WS round-trip：发 user_message 收到 ≥ 1 event 帧 + 1 done 帧
- [ ] 跨租户访问 session 返 404（单测覆盖）
- [ ] 客户端断开后 engine 调用退出（单测覆盖）
- [ ] README 勾选 + 端点表更新
- [ ] git tree clean

## 10. 风险与开放问题

| 风险 | 缓解 |
|---|---|
| gorilla/websocket 在测试里偶发 race | 写 wsConn 必须 mu 保护；测试用 `-race` 跑过 |
| WS upgrade 时 gin context cancel 的时机 | 用 `c.Request.Context()` 派生；upgrade 成功后 gin 不再 cancel |
| Origin 校验放行 `*` 默认不安全 | 文档 + config 默认 `*`，部署文档示例换成具体域 |
| 同一 session 并发两个 user_message | 简化为 busy 错；客户端可重发 |
| 消息历史无限增长 → 单次 LLM context overflow | 本切片不做压缩；超长时 LLM 自然报错；slice 7+ 解决 |
| seq 计算与并发 | 当前同一 session 同时只能有一个 SendMessage（WS 单连接 busy 排队）；多连接到同 session 不支持（owner 单一） |
| 删 session 用 archive 而非 hard delete | DELETE /sessions/:id 实际是 archive；硬删需要管理员接口（P1） |
| 缺乏分页 | List/Messages 都全返；P1 加 cursor 分页 |

### 开放问题

1. 多设备同时打开同一 session？暂不支持，第二个 owner 连入也 OK 但消息序乱（同一时刻只允许一条 in-flight SendMessage）—— P1 加锁
2. 客户端是否要能"取消正在跑的 engine"？暂不实现；断开 WS 等同取消
3. 是否要 `PATCH /sessions/:id` 改 title？暂不实现；前端 P1 加

## 11. ADR 摘要

| ID | 决策 | 理由 |
|---|---|---|
| ADR-41 | session 与 sandbox 弱绑定 | 与 slice 5 风格一致；强绑定要 toolbus/engine 联动改动大，留 P1 |
| ADR-42 | `messages` append-only | 审计 + 简化；session archive 不连带删消息 |
| ADR-43 | WS 而非 SSE | 双向（客户端中途发新消息）+ 心跳；SSE 单向不够用 |
| ADR-44 | gorilla/websocket | 生态最稳；nhooyr/websocket 也可，但 gorilla 文档 + 例子多 |
| ADR-45 | rest 错误体不沿用 OpenAI 风格 | session 不是 LLM 调用；简单 `{"error":"msg"}` 够用 |
| ADR-46 | tool_call event 不落库 | 同步信息在前面那条 assistant_message 的 tool_calls 字段里 |
| ADR-47 | final event 不落库 | 是 assistant_message 的最终态通知；同 content |
| ADR-48 | 同一 WS 连接只允许一条 in-flight SendMessage | 简化并发；序列严格 |
| ADR-49 | 不引 nats/redis 做事件总线 | 单实例已够 P0；多实例时 WS 还要 sticky session 一并解决 |
| ADR-50 | 保留 POST /agent/run | E2E 已用；当作测试/调试入口；前端走 WS |

## 12. 与 Spec 主文档对齐

主 spec §4.2 Session Orchestrator / §5.1 会话启动 / §5.2 单轮消息流 中提及：

- 主接口 `StartSession / SendMessage / Stream` → 本切片实现为 `CreateSession + SendMessage`，Stream 走 WS
- "返回 sessionId + wsURL，WS 接入二次鉴权" → POST /sessions 返 Session JSON，wsURL 由前端自构造 `/sessions/:id/ws`；二次鉴权 = Authorization header
- "每一步发事件给 Session Orchestrator，推 WS 给前端" ✓
- "工具结果 > 50 KB 截断 + 写对象存储 + LLM 摘要" → 当前只做截断（slice 5 已实现），对象存储留 slice 9
- "上下文逼近窗口时压缩" → 推迟 slice 7+
- "Memory Loader（起始注入）" → 推迟 slice 7

---

**审核状态**：草稿，待用户复核。
