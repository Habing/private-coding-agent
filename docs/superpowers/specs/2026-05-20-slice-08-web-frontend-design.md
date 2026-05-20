# Slice 8 — Web Frontend (Chat + WS Streaming) 设计

| 字段 | 值 |
|---|---|
| 文档日期 | 2026-05-20 |
| 状态 | Draft — 待用户复核 |
| 范围 | 私有化 AI 编码 Agent — P0 第 8 个切片 |
| 前置 | Slice 1.5 / 2 / 3 / 4 / 5 / 6 / 7 已完成（HEAD `384f590`） |

---

## 1. 概述

本切片交付 **最小可用 Web Frontend**：一个 React SPA，让用户能登录、看会话列表、打开会话页、和 Agent 实时对话。这是项目历来 9 个切片里第一次有"用户能用鼠标点开浏览器看到效果"的产出。

**这一刀只做"聊天主路径"**：
- 登录页 → 拿 JWT
- 会话列表页 → 复用 `GET /sessions`
- 聊天页 → 复用 `GET /sessions/:id/messages`（拉历史）+ `GET /sessions/:id/ws`（实时流）
- WS 帧渲染：`user` / `assistant` / `tool_call` / `tool_result` / `final` / `error` 六类事件 → 聊天气泡 + 可展开的工具调用块
- 部署：`go:embed web/dist` 静态产物，单一二进制（dev 时 vite proxy 到 :8080）

**核心叙事**
- **后端零改动**：所有 REST + WS endpoint 已在 slice 6/7 就绪；本切片只消费
- **SPA 单页**：React Router 客户端路由；后端命中未知路径回 `index.html`（SPA fallback）
- **状态最小化**：TanStack Query 管 server state（sessions / messages）+ 一个轻量 zustand store 管 auth token & 当前 ws 连接；不引 Redux
- **WS 是消费侧的事件源**：每条 server→client 帧映射成一条 message 行 push 进列表；用 server 的 `seq` 字段确保顺序
- **不做工具表单**：工具调用就显示 `name` + 折叠的 `input/output` JSON；不为每个工具写专门 UI

**不在 Slice 8 范围**
- 文件浏览器（沙箱 `/files` 端点）—— Slice 9 或后切片
- 沙箱 logs 实时流面板 —— Slice 9
- Memory Management UI（增删改查 `/memories`） —— 切到 8b 或后续
- Settings 页（model / profile 切换、API key 管理） —— 后续
- 多 tab / 多 session 同时打开 WS —— 本切片单 session 单 ws
- i18n —— 默认中文，硬编码
- Dark mode 切换器（默认 light，shadcn dark 主题预埋但不暴露开关）
- 移动端响应式精细化 —— 桌面优先，移动可滚动看就行
- 服务端推 push 通知 / unread badge

## 2. 前置条件

依赖 Slice 1.5 / 2 / 3 / 4 / 5 / 6 / 7 已完成；HEAD = `384f590`。

**Carry-over**：无。Slice 7 acceptance 全通过，E2E 25/25 实测通过。

**后端依赖**（全部已就绪）：
- `POST /auth/login` → `{token}`
- `GET /me`
- `POST/GET/GET:id/DELETE:id /sessions`
- `GET /sessions/:id/messages`
- `GET /sessions/:id/ws`（WebSocket，Bearer 鉴权）

新增配置 / 后端改动（最小）：
- `cmd/server/main.go` 增加 `web/dist` 的 `go:embed`，未命中 API 的路径回 `index.html`
- `internal/httpx/engine.go` 增加 SPA fallback handler（404 → index.html，限定 GET）

## 3. 核心需求

| 维度 | 决策 |
|---|---|
| 框架 | Vite 5 + React 18 + TypeScript 5 |
| UI 库 | shadcn/ui（按需 copy）+ Tailwind CSS 3 + lucide-react 图标 |
| 路由 | react-router-dom v6 (client-side) |
| 数据获取 | @tanstack/react-query v5 |
| 客户端状态 | zustand v4 (auth token 持久化到 localStorage) |
| HTTP 客户端 | 原生 `fetch` 封装（无 axios）|
| WS 客户端 | 原生 `WebSocket` API（无 socket.io / gorilla 客户端） |
| 包管理 | npm（不引 pnpm / yarn，CI 最少环境） |
| 构建产物 | `web/dist/`，Go embed 进 binary |
| 字体 | system-ui fallback（不引 web font） |
| 测试 | Vitest（unit）+ React Testing Library；本切片不做 e2e Playwright |
| Lint / Format | eslint + prettier（vite 默认模板配置即可）|

非功能：
- 首屏 < 200 KB gzipped（Vite tree-shake + shadcn copy-paste 不打整库）
- 登录到聊天页 < 800 ms（含 token 缓存命中）
- WS 重连：断网后退避 1s / 3s / 9s，三次失败提示用户手动刷新

## 4. 整体架构

```
浏览器
+-----------------------------------------------------------+
|  React SPA  (/web)                                        |
|                                                           |
|  Router                                                   |
|    /login        -> <Login>                               |
|    /             -> <SessionList> (重定向到首个/新建)      |
|    /sessions/:id -> <Chat>                                |
|                                                           |
|  zustand store (持久化)                                    |
|    auth: { token, user }                                  |
|                                                           |
|  React Query                                              |
|    sessions      GET /sessions                            |
|    messages(id)  GET /sessions/:id/messages               |
|    createSession POST /sessions                           |
|    deleteSession DELETE /sessions/:id                     |
|                                                           |
|  useChatSocket(sid)  自定义 hook                           |
|    new WebSocket("ws://.../sessions/:id/ws", token)       |
|    收 server frame -> 写入本地 messages 列表 + react-query |
|    导出 sendUserMessage(content)                          |
+-----------------------------------------------------------+
                |
                v
+-----------------------------------------------------------+
|  Go server (cmd/server)                                   |
|    /healthz /readyz                                       |
|    /auth/* /me                                            |
|    /sessions/* /sessions/:id/messages /sessions/:id/ws    |
|    /tools /agent /memories ...                            |
|    NEW: GET 静态 -> embed.FS /web/dist                    |
|         GET 未匹配 -> index.html (SPA fallback)            |
+-----------------------------------------------------------+
```

**关键决策：单仓 monorepo，不拆 Go / Web 两 repo**
- Go 侧：`web/` 子目录承载所有前端代码；`web/dist/` 由 vite 产出，被 `go:embed` 进 binary
- CI：`make web` → `cd web && npm ci && npm run build` 在 Go test 之前
- 开发：`cd web && npm run dev` 起 vite (5173)，配 proxy `/api`、`/auth`、`/sessions`、`/tools`、`/v1`、`/me`、`/memories`、`/agent` 都到 `localhost:8080`

## 5. 接口与数据模型

### 5.1 前端类型（`web/src/types/api.ts`）

```ts
export type Role = 'user' | 'assistant' | 'tool' | 'system'

export interface User {
  id: string
  tenant_id: string
  email: string
  name: string
  role: string
}

export interface Session {
  id: string
  tenant_id: string
  owner_user_id: string
  title: string
  model: string
  profile: string
  status: 'active' | 'archived'
  created_at: string
  updated_at: string
}

export interface Message {
  id: string
  session_id: string
  seq: number
  role: Role
  content: string
  tool_call_id?: string
  tool_calls?: ToolCall[]
  metadata?: Record<string, unknown>
  created_at: string
}

export interface ToolCall {
  id: string
  type: 'function'
  function: { name: string; arguments: string }
}

// WS server -> client frame
export type ServerFrame =
  | { type: 'event'; event: AgentEvent }
  | { type: 'done'; seq: number }
  | { type: 'error'; message: string }
  | { type: 'pong' }

export interface AgentEvent {
  kind: 'assistant_message' | 'tool_call' | 'tool_result' | 'final' | 'error'
  step?: number
  text?: string
  tool?: string
  tool_call_id?: string
  input?: unknown
  output?: unknown
  error?: string
}

// WS client -> server frame
export type ClientFrame =
  | { type: 'user_message'; content: string }
  | { type: 'ping' }
```

### 5.2 路由 / 页面

| Path | Component | 说明 |
|---|---|---|
| `/login` | `<Login>` | 表单：tenant slug（默认 `default`）+ email + password；提交后存 token，跳 `/` |
| `/` | `<Home>` | `useSessions()`；若 0 个 session，自动跳新建；否则展示左右两栏的 shell（左：list，右：empty placeholder）|
| `/sessions/:id` | `<Chat>` | 与 `<Home>` 同 shell，右侧渲染 `<MessageList> + <Composer>`，挂 `useChatSocket(id)` |
| `*` | `<NotFound>` | 仅 404 提示 + 回首页链接 |

未鉴权访问受保护路由 → 跳 `/login`。

### 5.3 组件树

```
<App>
  <QueryClientProvider>
    <BrowserRouter>
      <Routes>
        <Route path="/login" element={<Login />} />
        <Route element={<ProtectedShell />}>
          <Route path="/" element={<Home />} />
          <Route path="/sessions/:id" element={<Chat />} />
        </Route>
        <Route path="*" element={<NotFound />} />
      </Routes>
    </BrowserRouter>
  </QueryClientProvider>
</App>
```

`<ProtectedShell>`：
- 检查 `useAuthStore().token`，无则 `<Navigate to="/login" replace />`
- 渲染 `<TopBar>` + `<aside>` (`<SessionList>`) + `<main><Outlet/></main>`

`<MessageList>`：
- 受控于 `messages` 数组（react-query cache + ws 增量 push）
- 按 `role` / `kind` 分气泡：
  - `user` → 右侧 primary 气泡
  - `assistant` (text) → 左侧 muted 气泡（markdown 渲染 via react-markdown）
  - `assistant` (tool_calls) → 左侧 outline 卡片 `🔧 tool.name` + `<details>` 展开 input JSON
  - `tool` → 缩进 muted 卡片 `↩ tool_call_id` + `<details>` 展开 output JSON
  - `system` → 居中 muted 标签（如 "session started" / "error: ..."）
- 自动 scroll-to-bottom（受控：只有用户在底部才自动滚；中途上滚则停）

`<Composer>`：
- Textarea + Send 按钮
- Enter 发送，Shift+Enter 换行
- 发送中禁用，连 WS 断了禁用 + 红点提示
- 发送时即在 UI 追加 user 气泡（optimistic），等 server 回 `event` 帧再确认

### 5.4 WS Hook（`web/src/hooks/useChatSocket.ts`）

```ts
export function useChatSocket(sessionId: string) {
  const token = useAuthStore(s => s.token)
  const [status, setStatus] = useState<'connecting'|'open'|'closed'|'error'>('connecting')
  const [events, setEvents] = useState<AgentEvent[]>([])
  const wsRef = useRef<WebSocket | null>(null)

  useEffect(() => {
    if (!token) return
    const url = wsURL(sessionId)
    const ws = new WebSocket(url, ['bearer', token]) // 见下方鉴权
    ...
    ws.onmessage = ev => {
      const frame = JSON.parse(ev.data) as ServerFrame
      if (frame.type === 'event') setEvents(es => [...es, frame.event])
      else if (frame.type === 'done') {/* 触发 invalidate messages */}
      else if (frame.type === 'error') setStatus('error')
    }
    wsRef.current = ws
    return () => ws.close(1000, 'unmount')
  }, [sessionId, token])

  const sendUserMessage = useCallback((content: string) => {
    wsRef.current?.send(JSON.stringify({ type: 'user_message', content }))
  }, [])

  return { status, events, sendUserMessage }
}
```

**鉴权细节**：浏览器 `WebSocket` 构造器不支持自定义 header，因此 token 走 query string：

```
ws://host/sessions/:id/ws?token=<JWT>
```

后端 slice 6 的 `WSHandler` 当前用 `auth.Middleware`（读 `Authorization: Bearer`）。本切片需要后端补一个适配：在 WS upgrade 之前，如果 `Authorization` header 缺失但 `?token=` 存在，把它合成成 `Authorization: Bearer <token>`，再交给 middleware。

> **ADR-58（见 §11）**：WS 鉴权双通道。Authorization header 优先；缺失则降级到 `?token=`。`?token=` 路径仅 ws upgrade 端点接受，REST 一律拒绝（避免 token 落进 access log）。

### 5.5 静态资源 embed（后端微改）

`cmd/server/main.go`：

```go
//go:embed all:web/dist
var webDist embed.FS

func staticFS() http.FileSystem {
    sub, _ := fs.Sub(webDist, "web/dist")
    return http.FS(sub)
}
```

`internal/httpx/engine.go`：
- 在所有 API 路由注册之后，挂 `NoRoute` handler：
  - 若 `c.Request.URL.Path` 命中 dist 里实际文件 → 返回该文件
  - 否则若 method == GET → 返回 `index.html`（status 200）
  - 否则 → 404 JSON

dev 模式 (`PCA_WEB_DEV=1`)：跳过 embed，由 vite dev server 处理；后端只服务 API。

## 6. 数据流

### 6.1 登录

```
用户填 form -> POST /auth/login -> { token }
  -> zustand.setAuth({token, user(decoded)}) + localStorage.setItem
  -> useNavigate('/')
```

### 6.2 打开会话

```
<Chat> mount
  -> useQuery(['messages', sid]) GET /sessions/:id/messages  (拉历史)
  -> useChatSocket(sid)                                       (建 WS)
       onopen -> setStatus('open')
  -> 渲染历史 + (WS 在线就 enable Composer)
```

### 6.3 发送消息

```
用户输 "ls /workspace" -> Composer.onSubmit
  -> useChatSocket.sendUserMessage(content)
       ws.send({type:"user_message", content})
       本地 events.push({kind:'user', text: content}) (optimistic)
  -> 后端 SendMessage:
       Append(user) -> seq=N
       engine.Run yield ->
         Append(assistant tool_call) -> ws.send({type:"event", event:{kind:"tool_call",...}})
         Append(tool result)         -> ws.send({type:"event", event:{kind:"tool_result",...}})
         Append(assistant final)     -> ws.send({type:"event", event:{kind:"final",...}})
       ws.send({type:"done", seq})
  -> 前端每收一帧 -> events.push -> <MessageList> 重渲染
  -> done 帧 -> queryClient.invalidateQueries(['messages', sid]) (rebase 用最终持久化版本)
```

### 6.4 断线重连

```
ws.onclose (code != 1000)
  -> retry after [1s, 3s, 9s]
  -> 三次都失败 -> setStatus('error') + toast "WS 断开，请刷新"
```

## 7. 测试策略

| 层 | 工具 | 覆盖点 |
|---|---|---|
| 单元（util / store） | Vitest | wsURL 构造、auth store 持久化、optimistic merge |
| 组件 | Vitest + React Testing Library | Composer Enter/Shift+Enter、MessageList 滚动、ToolCard 折叠 |
| Hook | Vitest + `mock-socket` | useChatSocket 收发帧、重连、状态机 |
| 集成 | Vitest jsdom + msw 拦 REST | Login → 跳首页流程；Chat 拉历史 + WS push 合流 |
| 后端 SPA fallback | Go test | `GET /unknown` 返 index.html；`GET /sessions/:id/messages` 不受 fallback 干扰 |
| E2E | Bash extends test-e2e.sh | 编译产物存在 + `curl -I /` 返 200 text/html + `curl /sessions` 仍 JSON |

**不在测试范围**：
- 真浏览器 Playwright（推后到 slice 9 之后）
- 视觉回归
- 性能 / Lighthouse 自动跑分

## 8. Task 拆解

详见 `docs/superpowers/plans/2026-05-20-slice-08-web-frontend.md`。10 个 Task：

0. 写本 design spec（你正在读的文件）
1. 后端：WS `?token=` 降级 + SPA fallback + go:embed 占位
2. 前端 scaffolding：vite + react + ts + tailwind + shadcn init
3. 前端基础设施：types / api 客户端 / auth store / queryClient / Router shell
4. 登录页 `<Login>` + 集成测试
5. 会话列表 + 创建 / 删除 `<Home>` + `<SessionList>`
6. 聊天页：messages 历史 + Composer 组件
7. WS hook + MessageList 事件渲染（含 tool_call / tool_result 折叠卡）
8. Go embed 装配 + dev/prod 双模式 + E2E 26-28 步
9. README + 写正式 slice plan

## 9. 验收

- [ ] `docs/superpowers/specs/2026-05-20-slice-08-web-frontend-design.md` 存在并被用户复核
- [ ] `web/` 子目录 + `web/dist/` 由 `npm run build` 产出
- [ ] `cd web && npm test` 全 PASS（vitest + RTL）
- [ ] `go test ./internal/httpx/... ./cmd/server/...` 含 SPA fallback test 全 PASS
- [ ] `go build ./...` 产出单一 binary（含 web/dist embed）
- [ ] 浏览器访问 `http://localhost:8080/`：
  - 自动跳 `/login`
  - 登录后跳 `/`，能看到 session 列表
  - 点开一个 session 能聊天、发消息、看到 tool_call 折叠卡 + final assistant 气泡
- [ ] E2E：`curl -fsS http://localhost:8080/` 返 `text/html`、含 `id="root"`
- [ ] E2E：`curl -fsS http://localhost:8080/login` 也返 index.html（SPA fallback）
- [ ] E2E：`curl -fsS http://localhost:8080/sessions -H "Authorization: Bearer …"` 仍返 JSON（API 不受 fallback 干扰）
- [ ] README 切片 8 勾选 + Web 部分章节 + 浏览器截图（占位 OK）

## 10. 风险与回退

| 风险 | 影响 | 缓解 |
|---|---|---|
| Go embed 把 dist 打进 binary 后体积膨胀 | binary > 30 MB | shadcn 按需 copy，主依赖 react/react-dom/react-router/tanstack-query/zustand 总 <250KB gz |
| WS `?token=` 让 JWT 进了 nginx access log | token 泄露 | (a) 部署文档显式提醒 log 屏蔽 token；(b) header path 是默认，仅 ws upgrade 兜底 query |
| dev 模式下 vite proxy WS 转发不稳 | 开发体验差 | vite.config.ts 显式 `ws: true`；fallback 是直连 ws://localhost:8080 |
| React strict mode 双调 useEffect 导致 WS 重连 | dev 时报 ghost 连接 | 用 ref 标记 mounted；strict-mode 兼容 |
| 没有 e2e 浏览器测试 → 视觉问题靠人眼 | 回归风险 | 切片 9 引入 Playwright；本切片靠手测 + 用户验收 |
| 全部前端依赖外部 npm registry，私有化部署遇国内网络抖动 | npm ci 失败 | 部署文档建议起 verdaccio / npm 镜像；本切片暂走公网 |

## 11. ADR

| ID | 决策 | 理由 |
|---|---|---|
| ADR-58 | WS 鉴权双通道：Authorization header 优先 + `?token=` 降级 | 浏览器 WS API 无法自定义 header；query 是 W3C 推荐的兜底 |
| ADR-59 | Vite + React + TypeScript + Tailwind + shadcn/ui | 生态最广、TS 一等公民、shadcn 复制粘贴避免组件库锁定 |
| ADR-60 | TanStack Query 管 server state，zustand 管 client state，不引 Redux | 90% 状态是 fetch，Query 已足够；纯 client state 极少，zustand 18KB 够用 |
| ADR-61 | 单仓 `/web` 子目录 + `go:embed web/dist`，不分 repo | 一次 `go build` 出整套；私有化部署最简 |
| ADR-62 | 不引专门 WS 客户端库 | 原生 WebSocket API + 一个 hook 够用；省去 socket.io 协议适配 |
| ADR-63 | 不实现工具调用专门表单 | tool 多变化快，统一展示 JSON + 折叠 |
| ADR-64 | optimistic user message + done 后 invalidate | 体感流畅；invalidate 用持久化版本 rebase 兜底 |
| ADR-65 | SPA fallback 仅 GET，不影响 API 4xx 路径 | 后端 NoRoute 仅处理 GET 未命中 → index.html；非 GET 仍 405/404 JSON |

## 12. Spec 对齐

本切片对齐 design spec §3.2 表："Web Frontend — 会话、项目、工作流市场、记忆管理、流式渲染"。本切片只交付**会话 + 流式渲染**两项，对齐 §11.P0 的 "Web UI（会话、流式、文件浏览、设置）" 中的前两个。文件浏览 / 设置 / 项目 / 工作流市场 / 记忆管理 UI 推后。

---

**审核状态**：草稿，待用户复核。
