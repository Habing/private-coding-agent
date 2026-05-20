# Slice 6 — Session API + WebSocket Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 交付 `internal/session` 包：会话生命周期 + append-only 消息持久化 + REST CRUD + WebSocket 流式接入。WS 把 `agent.Engine` (slice 5) 在每一步 yield 出的 Event 同时落到 `messages` 表与推回客户端。Spec §4.2 / §5.1 / §5.2 的 "StartSession / SendMessage / Stream" 三接口在本切片完工。

**Architecture:** 三层依赖 —
1. `SessionRepo` / `MessageRepo` (pgxpool) 持久化；
2. `Service` 编排：CreateSession、SendMessage(组装 RunInput + 调用 engine + yield 时落库 + 回调 onEvent)；
3. `Handler` (REST) 与 `WSHandler` (WebSocket) 各自挂到 protected RouterGroup，共享同一个 `Service`。
WSHandler 一条连接独占一个读循环：每条 client 帧 = JSON；user_message 触发一次 SendMessage（在 goroutine 中执行，避免阻塞读循环对客户端断开的探测）；engine 的每个 Event → `{"type":"event","event":...}` 写回（write mutex 串行化）；engine 结束 → `done` 或 `error` 帧。

**Tech Stack:**
- Go 1.26、gin、pgx v5、testify、google/uuid（沿用既有）
- **新增**：`github.com/gorilla/websocket v1.5.x`
- 标准库 `context`、`encoding/json`、`sync`、`time`、`net/http`

---

## 前置条件

依赖 Slice 1.5 / 2 / 3 / 4 / 5 已完成。Slice 5 `agent.Engine.Run(ctx, RunInput, yield func(Event) error) error` 是本切片唯一外部强依赖。

## 本切片边界

**在本切片**：
- `sessions` 表 (id, tenant_id, owner_user_id, title, model, profile, status, created_at, updated_at)
- `messages` 表（append-only，含 role/content/tool_call_id/tool_calls JSONB/metadata JSONB/UNIQUE(session_id,seq)）
- REST: `POST /sessions`、`GET /sessions`、`GET /sessions/:id`、`DELETE /sessions/:id` (archive)、`GET /sessions/:id/messages`
- WS: `GET /sessions/:id/ws`，JWT Bearer 握手鉴权；帧协议见 design spec
- engine event → message 映射：`assistant_message`/`tool_result`/`error` 落库；`tool_call` 和 `final` 跳过（前者是 `assistant_message.tool_calls` 的重复，后者是最后一条 `assistant_message` 的重复）
- 跨租户访问 session / messages 返回 404，无存在性泄露
- 每条 WS 连接只允许一次 SendMessage 在飞（"busy" 错误帧拒绝并发）
- 客户端断开 → ctx cancel → engine 调用栈退出（ReadMessage 返 err 触发）
- 保留 `POST /agent/run`（slice 5 非流式入口），新端点不取代它
- E2E: 18 → 21 步

**不在本切片**：
- 上下文压缩 / 摘要 / token 预算（slice 7+）
- session 与 sandbox 自动绑定（保持 weak binding，sandbox_id 仍由 tool input 指定）
- 多 profile 切换 UI（slice 7+）
- WS 子协议协商 / 二进制帧（一律 TextMessage + JSON）

## File Structure

```
internal/db/migrations/
  0008_create_sessions.up.sql      Task 1: sessions + messages 表
  0008_create_sessions.down.sql    Task 1

internal/session/
  types.go                         Task 2: Session/Message/CreateRequest + 帧 type 常量
  errors.go                        Task 2: 错误哨兵
  errors_test.go                   Task 2
  repo.go                          Task 2: SessionRepo + MessageRepo
  repo_test.go                     Task 2: dockertest, fixtures(tenant+user)
  service.go                       Task 3: Service + AgentEngine interface
  service_test.go                  Task 3: mockEngine + 真 repo (dockertest)
  handler.go                       Task 4: REST handler + HandlerService interface
  handler_test.go                  Task 4: mock service
  wshandler.go                     Task 5: WSHandler + WSSendService interface
  wshandler_test.go                Task 5: httptest + websocket.Dialer

cmd/server/main.go                 Task 6: 装配 session.Service + 两个 handler
internal/config/config.go          Task 6: ServerConfig.WSAllowedOrigins
config/config.example.yaml         Task 6: ws_allowed_origins: ["*"]

deploy/compose/test-e2e.sh         Task 7: 18 → 21 步 (POST /sessions, list+messages, WS round-trip)

README.md                          Task 8: 进度勾选 + 6 个端点
docs/superpowers/specs/...         Task 0 (本切片专属 design spec, 先于实现写)
```

---

## Task 0: 写 design spec

**Files:**
- Create: `docs/superpowers/specs/2026-05-20-slice-06-session-design.md`

按 slice 2/3/4 design spec 模板（概述 / 前置 / 核心需求 / 整体架构 / 接口与数据模型 / 数据流 / 测试策略 / Task 拆解 / 验收 / 风险 / ADR 摘要 / Spec 对齐）写完整。重点：

- WS 协议表：5 种帧类型（user_message/ping client→server；event/done/error/pong server→client）
- 关闭码：1000/1003/1008/1011/4001 语义
- engine event ↔ message 映射表（含哪些跳过 + 理由）
- ADR-41 ~ ADR-50（弱绑定、append-only、WS over SSE、gorilla、tool_call 不落库、final 不落库、one in-flight、等）

- [ ] **Step 1: 写 spec 文件，与 slice 4 design spec 同结构。**
- [ ] **Step 2: `git add docs/superpowers/specs/2026-05-20-slice-06-session-design.md && git commit -m "docs(session): design spec for slice 6"`**

---

## Task 1: DB 迁移

**Files:**
- Create: `internal/db/migrations/0008_create_sessions.up.sql`
- Create: `internal/db/migrations/0008_create_sessions.down.sql`

- [ ] **Step 1: 写 up.sql** —— 见 design spec §数据模型；要点：
  - `sessions`: FK 到 `tenants` 与 `users`，`status` DEFAULT 'active'，索引 `(tenant_id, owner_user_id, created_at DESC)`
  - `messages`: FK 到 `sessions` ON DELETE CASCADE，`tool_call_id TEXT NOT NULL DEFAULT ''`，`tool_calls`/`metadata` JSONB nullable，`UNIQUE (session_id, seq)`，索引 `(session_id, seq)`

- [ ] **Step 2: 写 down.sql** —— `DROP TABLE IF EXISTS messages; DROP TABLE IF EXISTS sessions;`

- [ ] **Step 3: 跑 `go test ./internal/db/... -count=1`**，确认 migration 应用成功（既有 dockertest 测试自动覆盖新增 migration）。

- [ ] **Step 4: commit**
  ```
  git add internal/db/migrations/0008_create_sessions.*.sql
  git commit -m "feat(db): migration 0008 sessions + messages tables"
  ```

---

## Task 2: 类型 + 错误 + repo

**Files:**
- Create: `internal/session/types.go`
- Create: `internal/session/errors.go`
- Create: `internal/session/errors_test.go`
- Create: `internal/session/repo.go`
- Create: `internal/session/repo_test.go`

- [ ] **Step 1: 写 `types.go`** —— `Session`, `Message`, `CreateRequest`，Status/Role 常量，DefaultProfile = "coding"，WS 帧 type 常量。

- [ ] **Step 2: 写 `errors.go` + `errors_test.go`** —— 哨兵：`ErrSessionNotFound`, `ErrSessionArchived`, `ErrEmptyContent`, `ErrModelRequired`。测试断言 non-nil + non-empty Error()。

- [ ] **Step 3: 写 `repo_test.go`** —— 复用 sandbox 包的 dockertest 模式：
  - `TestMain` 起 postgres:16，跑 `db.Migrate`，全文件共享 DSN
  - `fixtures(t, pg)` helper 每次 insert 新 tenant + user（FK 要求）
  - `newPool(t)` helper
  - 测试用例：CreateGetList / GetNotFound 跨租户跨 owner / Archive (含未找到) / Message AppendList / SeqUnique / Message List 跨租户返空 / 错误哨兵

- [ ] **Step 4: 跑测试确认失败**（repo 未实现）。

- [ ] **Step 5: 写 `repo.go`**：
  - `SessionRepo.Create/Get/List/Archive`，Get 用 `WHERE id=$1 AND tenant_id=$2 AND owner_user_id=$3`，无行返 `ErrSessionNotFound`
  - `MessageRepo.Append`，JSONB nil 处理（`var v any; if len(m.X) > 0 { v = []byte(m.X) }`）
  - `MessageRepo.List(tenantID, sessionID)`，`ORDER BY seq ASC`
  - `MessageRepo.NextSeq` = `SELECT COALESCE(MAX(seq), 0) + 1 FROM messages WHERE session_id=$1`，注释说明非原子，由 service 层串行 + UNIQUE 约束保护

- [ ] **Step 6: 跑测试全 PASS**（`go test ./internal/session/... -count=1`）。

- [ ] **Step 7: commit**
  ```
  git add internal/session/{types,errors,errors_test,repo,repo_test}.go
  git commit -m "feat(session): SessionRepo + MessageRepo with dockertest coverage"
  ```

---

## Task 3: Service 层

**Files:**
- Create: `internal/session/service.go`
- Create: `internal/session/service_test.go`

- [ ] **Step 1: 写 `service_test.go`** —— 用 `mockEngine` (sync.Mutex 包裹 lastInIn) + 真 repo (dockertest)：
  - `newService(t)` helper 用 `fixtures(t, pg)` 拿 tenant+user
  - CreateSession happy / ModelRequired / 跨租户 Get 返 404 / ListSessions / ArchiveSession
  - SendMessage HappyPath（验证 user msg + assistant msg 持久化，final 跳过）
  - SendMessage ToolChain（验证 tool_call 跳过，tool_result 落库，最终 4 条消息）
  - SendMessage EmptyContent / Archived / NotFound 跨租户 / onEvent abort 错误透传 / 第二轮 RehydratesHistory（engine 看到 [user1, assistant1, user2]）
  - ListMessages 跨租户返 404

- [ ] **Step 2: 跑测试确认失败**（service 未实现）。

- [ ] **Step 3: 写 `service.go`**：
  - `AgentEngine` 本地 interface（仅 `Run(ctx, RunInput, yield) error`），便于 mockEngine
  - `Service` 字段 `sessions/messages/engine`
  - `CreateSession`：profile 缺省 "coding"，Create 后 round-trip Get 拿时间戳
  - `SendMessage`：验空 → Get session 验状态 → List history → Append user msg → 构造 RunInput → engine.Run 的 yield 闭包：`eventToMessage` 决定是否落库（NextSeq + Append），然后调 `onEvent(evt)`
  - `historyToChatMessages`：role=assistant 时 unmarshal `ToolCalls` JSONB 回 `[]modelgw.ToolCall`；role=tool 时设 `ToolCallID`
  - `eventToMessage`：assistant_message → role=assistant + ToolCalls JSONB + metadata{kind,step,finish_reason}；tool_result → role=tool + ToolCallID + content=ToolOutput 或 ToolError；error → role=system + content=evt.Text；其余 (tool_call, final) 跳过

- [ ] **Step 4: 跑测试全 PASS**。

- [ ] **Step 5: commit**
  ```
  git add internal/session/service*.go
  git commit -m "feat(session): Service orchestrator over engine+repos with mockEngine tests"
  ```

---

## Task 4: REST Handler

**Files:**
- Create: `internal/session/handler.go`
- Create: `internal/session/handler_test.go`

- [ ] **Step 1: 写 `handler_test.go`** —— `mockHandlerSvc` (函数字段) + `auth.NewJWT` 签发测试 token + `httptest.NewRecorder`：
  - Create OK (201 + 校验 model echo) / Create ModelRequired (400 + "model_required") / Create Unauthorized (401)
  - List OK (200 + `.sessions` 数组)
  - Get OK / Get NotFound (404) / Get BadID (400)
  - Archive OK (204) / Archive NotFound (404)
  - ListMessages OK / ListMessages NotFound (404)

- [ ] **Step 2: 跑测试确认失败**。

- [ ] **Step 3: 写 `handler.go`**：
  - 本地 `HandlerService` interface (context.Context-based 方法签名，Service 自动满足)
  - `Handler.Register(rg)` 挂 5 个路由到 `rg.Group("/sessions")`
  - `claims(c)` helper 从 `auth.FromCtx` 取，无则 401
  - `parseID(c)` helper 解析 `:id` UUID，失败 400 "validation: id"
  - 错误映射：`ErrModelRequired` → 400 "model_required"，`ErrSessionNotFound` → 404 "not_found"，其余 → 500 "internal"
  - 响应 envelope：`{"sessions":[...]}` / `{"messages":[...]}`

- [ ] **Step 4: 跑测试全 PASS。`go vet ./internal/session/...` 干净。**

- [ ] **Step 5: commit**
  ```
  git add internal/session/handler*.go
  git commit -m "feat(session): REST CRUD handler for /sessions endpoints"
  ```

---

## Task 5: WebSocket Handler

**Files:**
- Add dep: `go get github.com/gorilla/websocket@v1.5.3`
- Create: `internal/session/wshandler.go`
- Create: `internal/session/wshandler_test.go`

- [ ] **Step 1: 写 `wshandler_test.go`** —— `httptest.NewServer` + `gorilla/websocket.DefaultDialer.Dial`：
  - HappyPath（发 user_message，依次收 event×2 + done）
  - PingPong（client 发 `{"type":"ping"}` 收 pong）
  - NotFoundBeforeUpgrade（GetSession 返 ErrSessionNotFound → 握手返 404，不升级）
  - Unauthorized（无 Bearer → 401，不升级）
  - EngineError_WritesErrorFrame（engine 返错 → `{"type":"error","message":"..."}`）
  - Archived_WritesErrorFrame（验 `code:"archived"`）
  - ClientDisconnect_CancelsEngine（mock SendMessage 阻塞 `<-ctx.Done()`，client `c.Close()` 后断言 ctx 被 cancel）
  - UnknownFrame_ClosesConn（`{"type":"gibberish"}` → close 1003）
  - InvalidJSON_ClosesConn

- [ ] **Step 2: 跑测试确认失败**。

- [ ] **Step 3: 写 `wshandler.go`**：
  - 本地 `WSSendService` interface (`GetSession` + `SendMessage`)
  - `NewWSHandler(svc, allowedOrigins)` 配 `websocket.Upgrader.CheckOrigin`：`["*"]` 放行所有；其余按字符串匹配；缺 Origin 头放行（非浏览器客户端）
  - `Register` 挂 `GET /sessions/:id/ws`
  - `serve`：claims → parseID → **预升级 GetSession 验存在/归属**（404 不暴露存在性）→ Upgrade → 设 ReadDeadline (70s) + PongHandler 续期 → 创建 `wsConn` → goroutine 跑 `pingLoop` (25s 间隔) → 主 goroutine 跑 `readLoop`
  - `wsConn.writeJSON` / `writeClose` 用 `sync.Mutex` 串行化所有写入
  - `readLoop`：每条 ReadMessage → 解析 type → ping 回 pong；user_message 先 `tryAcquire()`（互斥 + busy bool）；命中 busy 写 error 帧；否则 **在 goroutine 内** 跑 `handleUserMessage`，结束 `release()`
  - `handleUserMessage`：调 SendMessage，onEvent 把每个 Event 包成 `{"type":"event","event":evt}` 写出；如果写失败记 writeErr 并返回（中止后续）；engine 结束：error → `{"type":"error","code":"...","message":...}`；成功 → `{"type":"done"}`

- [ ] **Step 4: 跑测试全 PASS。客户端断开测试可能首次失败 → 把 SendMessage 放进 goroutine 让 readLoop 持续读到 close。**

- [ ] **Step 5: commit**
  ```
  git add internal/session/wshandler*.go go.mod go.sum
  git commit -m "feat(session): WebSocket handler streaming agent events to client"
  ```

---

## Task 6: 装配进 main.go + 配置项

**Files:**
- Modify: `internal/config/config.go` — `ServerConfig.WSAllowedOrigins []string`
- Modify: `config/config.example.yaml` — `ws_allowed_origins: ["*"]`
- Modify: `cmd/server/main.go`

- [ ] **Step 1: 加配置字段** + example yaml 默认值。

- [ ] **Step 2: 装配 session 三件套**（在 agentEngine 之后）：
  ```go
  sessionService := session.NewService(
      session.NewSessionRepo(pool),
      session.NewMessageRepo(pool),
      agentEngine,
  )
  sessionHandler := session.NewHandler(sessionService)
  wsAllowed := cfg.Server.WSAllowedOrigins
  if len(wsAllowed) == 0 { wsAllowed = []string{"*"} }
  sessionWSHandler := session.NewWSHandler(sessionService, wsAllowed)
  ```
  在 protected group 内挂 `sessionHandler.Register(protected)` + `sessionWSHandler.Register(protected)`。

- [ ] **Step 3: `go build ./... && go vet ./...` 全清。**

- [ ] **Step 4: commit**
  ```
  git add cmd/server/main.go internal/config/config.go config/config.example.yaml
  git commit -m "feat(session): wire Session service + REST + WS handlers into main"
  ```

---

## Task 7: E2E 扩到 21 步

**Files:** Modify `deploy/compose/test-e2e.sh`

- [ ] **Step 1: 把所有 `[N/18]` 改 `[N/21]`**（`sed -i 's|\[\([0-9]\+\)/18\]|[\1/21]|g'`）。

- [ ] **Step 2: 在 [18/21] 之后追加：**
  - `[19/21] POST /sessions` 创建 session，捕获 `SID`
  - `[20/21] GET /sessions` 校验包含该 SID
  - `[21/21] WS round-trip`：`docker run --rm -i --network host solsson/websocat -H="Authorization: Bearer $TOK" -n1 -t "ws://localhost:8080/sessions/$SID/ws" <<<'{"type":"user_message","content":"hi"}' | head -n5 | grep -q '"type":"event"'`；然后 `GET /sessions/$SID/messages` 校验消息条数 ≥ 2

- [ ] **Step 3: `bash -n deploy/compose/test-e2e.sh` 语法清。E2E 真跑由人或 CI 执行。**

- [ ] **Step 4: commit**
  ```
  git add deploy/compose/test-e2e.sh
  git commit -m "test(e2e): add slice 6 sessions REST + WS round-trip to e2e (21 steps)"
  ```

---

## Task 8: README

**Files:** Modify `README.md`

- [ ] **Step 1: 切片 6 复选框打 `[x]`。**
- [ ] **Step 2: 端点表追加 6 行：POST/GET/GET/DELETE /sessions/...，GET .../messages，GET .../ws。**
- [ ] **Step 3: commit**
  ```
  git add README.md
  git commit -m "docs: mark slice 6 complete in README and add /sessions endpoints"
  ```

---

## 验收 Checklist

- [ ] `docs/superpowers/specs/2026-05-20-slice-06-session-design.md` 已存在
- [ ] `internal/db/migrations/0008_*.sql` up + down 在 dockertest 跑通
- [ ] `go test ./internal/session/... -count=1` 全 PASS（含 dockertest）
- [ ] `go vet ./...` 干净，`go build ./...` 干净
- [ ] 跨租户/跨 owner 访问 session / messages 返 404（service_test + handler_test 覆盖）
- [ ] WS 客户端断开 → ctx cancel → SendMessage 返 ctx.Err（wshandler_test 覆盖）
- [ ] one-in-flight：第二条 user_message 在第一条未结束时收到 "busy" error 帧
- [ ] E2E 21/21 在 compose 环境跑通
- [ ] README 切片 6 勾选 + 6 个端点入表
- [ ] Conventional Commits：每 Task 一个 feat / docs / test 提交，git tree clean

## 关键不变量

1. **跨租户 404，不泄露存在性** —— 所有 repo SELECT 都 `WHERE tenant_id=$X AND owner_user_id=$Y`；WS 升级前必须先 GetSession。
2. **消息 append-only + UNIQUE(session_id, seq)** —— NextSeq 非原子，但 service 串行化保证；并发 SendMessage 走 WS 的 busy 机制拒绝。
3. **engine event 取舍** —— tool_call、final 不落库（重复信息）；其他全部入 `messages` 行，metadata JSONB 保留 kind/step/finish_reason/tool_name/truncated/original_size。
4. **WS 单 goroutine 写** —— `wsConn.wmu` 串行所有 WriteMessage；read loop + ping loop + handler goroutine 三方都走 writeJSON 才安全。
5. **客户端断开必须取消 engine** —— SendMessage 必须在独立 goroutine 内执行，让 readLoop 持续可达 ReadMessage 探测断开；ctx cancel 通过 `c.Request.Context()` 传播到 engine。
