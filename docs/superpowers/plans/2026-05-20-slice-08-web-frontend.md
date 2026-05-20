# Slice 8 — Web Frontend (Chat + WS Streaming) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 交付最小可用 Web SPA：用户能登录、看会话列表、打开会话页与 Agent 实时聊天。Vite + React 18 + TypeScript + Tailwind + shadcn/ui；Go embed 静态产物单二进制部署；后端补两段：(a) WS `?token=` 降级 (b) SPA fallback。spec §3.2 / §11.P0 "会话 + 流式渲染" 在本切片完工。

**Architecture:** 单仓 monorepo —
1. `web/` 子目录承载 React SPA；`npm run build` → `web/dist/`
2. `cmd/server` 用 `//go:embed all:web/dist` 把产物打进二进制；NoRoute fallback 返 `index.html`
3. `internal/auth` 新增一个**仅作用于 WS 路由的** `WSTokenFromQuery` 中间件：缺 `Authorization` header 时把 `?token=` 提升成 `Authorization: Bearer <t>`，再交给现有 `auth.Middleware`
4. SPA 内：TanStack Query 管 server state（sessions / messages）、zustand 管 auth、`useChatSocket` hook 封装 WS 生命周期

依赖方向：`web/` 不进 Go module；`cmd/server` 用 stdlib `embed`；`internal/auth` 不依赖任何新包。新增前端依赖见 §Tech Stack。

**Tech Stack:**
- Go 1.26（已有）+ stdlib `embed` / `io/fs`
- Frontend：Vite 5、React 18、TypeScript 5、Tailwind CSS 3、shadcn/ui（按需 copy）、react-router-dom v6、@tanstack/react-query v5、zustand v4、react-markdown 9、lucide-react、clsx
- 测试：Vitest 1.x、@testing-library/react、mock-socket、msw 2.x
- 不引：Redux、axios、socket.io、styled-components、emotion、material-ui

---

## 前置条件

依赖 Slice 1.5 / 2 / 3 / 4 / 5 / 6 / 7 已完工（HEAD = `384f590` "docs(memory): formal slice 7 implementation plan"，已推 origin/main）。

E2E 25/25 在 compose 环境实测通过。

读 `docs/superpowers/specs/2026-05-20-slice-08-web-frontend-design.md` 全篇，重点 §5（接口与数据模型）、§5.4（WS token-in-query）、§5.5（embed + fallback）、§11（ADR-58 ~ ADR-65）。

## 本切片边界

**在本切片**：
- 后端：`internal/auth.WSTokenFromQuery` 中间件 + 单测；`cmd/server/main.go` 挂到 ws 路由前
- 后端：`web/dist` placeholder + `go:embed` 装配 + SPA fallback handler（GET 未命中 → index.html）+ 单测
- 前端：`web/` 完整 scaffold（Vite + TS + Tailwind + shadcn init + eslint）
- 前端：types / api 客户端 / auth store / queryClient / Router shell
- 前端：`<Login>` 表单 + 集成测试
- 前端：`<Home>` + `<SessionList>` + 创建/删除会话
- 前端：`<Chat>` 页 = 历史 + Composer + WS hook + MessageList（含 tool_call / tool_result 折叠卡）
- E2E：25 → 28 步（root html / login fallback / API 不受 fallback 干扰）
- README：勾选切片 8 + 添加"Web Frontend"章节 + dev 模式说明

**不在本切片**：
- 文件浏览器 UI（沙箱 `/files`）
- Memory Management UI
- Settings 页（model / profile / API key）
- 多 session 同时打开 WS / 工作台标签
- i18n / dark mode 切换器
- 移动端响应式精细化
- Playwright E2E

## File Structure

```
web/                                  Task 2: 全新 scaffold
  package.json
  vite.config.ts
  tsconfig.json
  tailwind.config.ts
  postcss.config.js
  index.html
  .eslintrc.cjs
  .gitignore
  src/
    main.tsx                          Task 2
    App.tsx                           Task 3
    index.css                         Task 2 (tailwind directives)
    types/api.ts                      Task 3
    lib/
      api.ts                          Task 3: fetch wrapper
      ws.ts                           Task 3: wsURL helper
      utils.ts                        Task 2 (shadcn cn() helper)
    stores/auth.ts                    Task 3: zustand + localStorage
    queryClient.ts                    Task 3
    components/
      ui/                             Task 2: shadcn copy (button/input/card/...)
      ProtectedShell.tsx              Task 3
      TopBar.tsx                      Task 5
      SessionList.tsx                 Task 5
      MessageList.tsx                 Task 7
      Composer.tsx                    Task 6
      ToolCallCard.tsx                Task 7
    hooks/
      useChatSocket.ts                Task 7
    pages/
      Login.tsx                       Task 4
      Home.tsx                        Task 5
      Chat.tsx                        Task 6
      NotFound.tsx                    Task 3
    __tests__/                        分散在各 task
  dist/                               Task 8: npm run build 产物
    .gitkeep                          Task 1: 占位以让 go:embed 不空

internal/auth/
  ws_token.go                         Task 1
  ws_token_test.go                    Task 1

internal/httpx/
  spa.go                              Task 1
  spa_test.go                         Task 1

cmd/server/
  main.go                             Task 8: import embed + 装配 fallback + 挂 WSTokenFromQuery
  webembed.go                         Task 8: //go:embed all:web/dist + staticFS()

deploy/compose/test-e2e.sh            Task 8: 25 -> 28 步

README.md                             Task 9
docs/superpowers/specs/...            (Task 0: 已写)
docs/superpowers/plans/...            (Task 9: 本文件)

.gitignore                            Task 2: 新增 web/node_modules、web/dist 但保留 .gitkeep
Makefile (新增)                        Task 8: make web + make build
```

---

## Task 0: 写 design spec

已完成（`docs/superpowers/specs/2026-05-20-slice-08-web-frontend-design.md`），并已在主线对话与用户对齐。本文件 Task 1 起为实施步骤。

- [x] **Step 1: spec 已写并被用户复核。**

---

## Task 1: 后端 WS token 降级 + SPA fallback (TDD)

**Files:**
- Create: `internal/auth/ws_token.go`
- Create: `internal/auth/ws_token_test.go`
- Create: `internal/httpx/spa.go`
- Create: `internal/httpx/spa_test.go`
- Create: `web/dist/.gitkeep`

> 本 Task 先做的原因：后端两段是阻塞前端 dev 体验的（WS 鉴权）和 E2E 验收的（fallback）。`web/dist/.gitkeep` 是为了让 Task 8 的 `//go:embed all:web/dist` 在还没真正 build 出 index.html 前也能编过。

- [ ] **Step 1: 写 `internal/auth/ws_token_test.go`** —— 测一个返回 `gin.HandlerFunc` 的工厂 `WSTokenFromQuery()`：
  - case A: 请求带 `Authorization: Bearer x` → 中间件直接 `c.Next()`，不动 header
  - case B: 请求无 Authorization 但 `?token=abc` → 中间件给 `c.Request.Header` 注入 `Authorization: Bearer abc` 后 `c.Next()`
  - case C: 既无 header 也无 `?token=` → 中间件不阻断（让下一个 `auth.Middleware` 自己 401）
  - case D: `?token=` 含特殊字符（如 `+/=`）原样透传，不二次编码
  - 用 `httptest.NewRecorder` + `gin.New()` 串中间件链 `WSTokenFromQuery()` → 一个 echo handler 直接返回 `c.GetHeader("Authorization")`，断言注入结果

- [ ] **Step 2: 跑测试确认失败**（`go test ./internal/auth/... -run TestWSTokenFromQuery -count=1`）。

- [ ] **Step 3: 写 `internal/auth/ws_token.go`**：
  ```go
  // WSTokenFromQuery is a narrow shim that lets browser WebSocket clients pass
  // the JWT in ?token= because the WS constructor cannot set custom headers.
  // Mount this BEFORE auth.Middleware on the WS upgrade route only.
  // The query parameter is consumed silently; the URL is not rewritten so the
  // token does not leak into downstream loggers expecting RawQuery.
  func WSTokenFromQuery() gin.HandlerFunc {
      return func(c *gin.Context) {
          if c.GetHeader("Authorization") == "" {
              if tok := c.Query("token"); tok != "" {
                  c.Request.Header.Set("Authorization", "Bearer "+tok)
              }
          }
          c.Next()
      }
  }
  ```
  注释里强调 "WS upgrade route only" — REST 不要挂。

- [ ] **Step 4: 跑测试全 PASS。**

- [ ] **Step 5: 写 `internal/httpx/spa_test.go`** —— 测 SPA fallback：
  - 构造一个 `embed.FS` mock：用 `fstest.MapFS{"index.html": {Data: []byte("<html>root</html>")}, "assets/app.js": {Data: []byte("js")}}`
  - 调 `httpx.RegisterSPAFallback(engine, mockFS)` 后：
    - case A: GET `/` → 200 text/html `<html>root</html>`
    - case B: GET `/login`（无对应静态文件） → 200 text/html index.html（SPA route）
    - case C: GET `/assets/app.js` → 200 with js body
    - case D: GET `/sessions/xyz`（不在静态 fs 内） → 200 index.html（前端 router 接管）
    - case E: POST `/unknown` → 404 JSON（不走 fallback）
    - case F: 已注册的 API 路由（提前 `engine.GET("/api/ping", ...)`） → 不被 fallback 拦截，仍返 API 响应
  - 注意：fallback 必须挂在 `engine.NoRoute` 上（gin 唯一一次注册），所以测试要确保挂的顺序正确

- [ ] **Step 6: 跑测试确认失败**。

- [ ] **Step 7: 写 `internal/httpx/spa.go`**：
  ```go
  // RegisterSPAFallback serves static files from fsys and falls back to
  // index.html for any GET request that does not match an existing route or
  // static asset. Non-GET requests fall through to gin's default 404.
  func RegisterSPAFallback(r *gin.Engine, fsys fs.FS) error {
      idx, err := fs.ReadFile(fsys, "index.html")
      if err != nil {
          return fmt.Errorf("spa: read index.html: %w", err)
      }
      fileServer := http.FileServer(http.FS(fsys))
      r.NoRoute(func(c *gin.Context) {
          if c.Request.Method != http.MethodGet {
              c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error":"not_found"})
              return
          }
          // 1. Try to serve a real file (assets/*, favicon.ico, ...).
          //    Probe via fs.Stat to avoid a 404 from FileServer leaking through.
          p := strings.TrimPrefix(c.Request.URL.Path, "/")
          if p != "" {
              if _, err := fs.Stat(fsys, p); err == nil {
                  fileServer.ServeHTTP(c.Writer, c.Request)
                  return
              }
          }
          // 2. SPA fallback: serve index.html with 200.
          c.Data(http.StatusOK, "text/html; charset=utf-8", idx)
      })
      return nil
  }
  ```

- [ ] **Step 8: 跑测试全 PASS。`go vet ./internal/auth/... ./internal/httpx/...` 干净。**

- [ ] **Step 9: 写 `web/dist/.gitkeep`** —— 空文件，仅为让 git 跟踪目录，避免 `//go:embed all:web/dist` 在 build 时空目录报错。同时需要一个最小 `web/dist/index.html` placeholder 以让 SPA fallback 的 `ReadFile` 不返错。先放：
  ```html
  <!doctype html><html><body><div id="root">build pending</div></body></html>
  ```
  路径：`web/dist/index.html`，承诺 Task 8 会被真实产物覆盖。

- [ ] **Step 10: commit**
  ```
  git add internal/auth/ws_token*.go internal/httpx/spa*.go web/dist/.gitkeep web/dist/index.html
  git commit -m "feat(server): WS ?token= shim + SPA fallback for embedded web UI"
  ```

---

## Task 2: 前端 scaffolding

**Files:**
- Create: `web/package.json`
- Create: `web/vite.config.ts`
- Create: `web/tsconfig.json` + `web/tsconfig.node.json`
- Create: `web/tailwind.config.ts`
- Create: `web/postcss.config.js`
- Create: `web/index.html`
- Create: `web/.eslintrc.cjs`
- Create: `web/.gitignore`
- Create: `web/src/main.tsx`
- Create: `web/src/index.css`（tailwind directives + shadcn 主题变量）
- Create: `web/src/lib/utils.ts`（shadcn 必备 `cn()`）
- Create: `web/src/components/ui/button.tsx` 等若干 shadcn 组件
- Modify: `.gitignore`（根）

- [ ] **Step 1: 初始化 vite 项目结构。** 不跑 `npm create vite@latest`（避免交互），直接手写最小 `package.json`：
  ```json
  {
    "name": "private-coding-agent-web",
    "private": true,
    "version": "0.0.0",
    "type": "module",
    "scripts": {
      "dev": "vite",
      "build": "tsc -b && vite build",
      "preview": "vite preview",
      "test": "vitest run",
      "test:watch": "vitest",
      "lint": "eslint . --ext .ts,.tsx --max-warnings 0"
    },
    "dependencies": {
      "react": "^18.3.1",
      "react-dom": "^18.3.1",
      "react-router-dom": "^6.26.2",
      "@tanstack/react-query": "^5.59.0",
      "zustand": "^4.5.5",
      "react-markdown": "^9.0.1",
      "lucide-react": "^0.453.0",
      "clsx": "^2.1.1",
      "tailwind-merge": "^2.5.4",
      "class-variance-authority": "^0.7.0"
    },
    "devDependencies": {
      "@types/react": "^18.3.10",
      "@types/react-dom": "^18.3.0",
      "@vitejs/plugin-react": "^4.3.1",
      "typescript": "^5.6.2",
      "vite": "^5.4.8",
      "tailwindcss": "^3.4.13",
      "postcss": "^8.4.47",
      "autoprefixer": "^10.4.20",
      "vitest": "^2.1.2",
      "jsdom": "^25.0.1",
      "@testing-library/react": "^16.0.1",
      "@testing-library/jest-dom": "^6.5.0",
      "@testing-library/user-event": "^14.5.2",
      "mock-socket": "^9.3.1",
      "msw": "^2.4.9",
      "eslint": "^8.57.0",
      "@typescript-eslint/parser": "^7.18.0",
      "@typescript-eslint/eslint-plugin": "^7.18.0",
      "eslint-plugin-react-hooks": "^4.6.2",
      "eslint-plugin-react-refresh": "^0.4.12"
    }
  }
  ```

- [ ] **Step 2: 写 `vite.config.ts`**：
  - `plugins: [react()]`
  - `server.proxy`：把 `/auth`、`/me`、`/sessions`、`/tools`、`/agent`、`/v1`、`/memories`、`/sandbox`、`/healthz`、`/readyz` 全 proxy 到 `http://localhost:8080`，`ws: true`
  - `test`: `{ environment: 'jsdom', globals: true, setupFiles: ['./src/test/setup.ts'] }`

- [ ] **Step 3: 写 `tsconfig.json`** —— vite 标准模板（`target: ES2022`, `strict: true`, `jsx: react-jsx`, `paths: { "@/*": ["src/*"] }`）+ `tsconfig.node.json` 给 vite.config 用。

- [ ] **Step 4: 写 tailwind.config.ts + postcss.config.js**。tailwind config 的 `content: ['./index.html', './src/**/*.{ts,tsx}']`，extend 加 shadcn 主题色变量（`hsl(var(--background))` 等）。

- [ ] **Step 5: 写 `index.html`**：minimal HTML5 + `<div id="root"></div>` + `<script type="module" src="/src/main.tsx">`。

- [ ] **Step 6: 写 `src/index.css`** —— `@tailwind base/components/utilities` + shadcn 默认主题变量（light only，dark 注释保留）+ body { font-family: system-ui }。

- [ ] **Step 7: 写 `src/main.tsx`** —— `ReactDOM.createRoot(...).render(<App />)`；`<App />` 先放占位 "Hello"，Task 3 会改。

- [ ] **Step 8: 写 `src/lib/utils.ts`**：
  ```ts
  import { type ClassValue, clsx } from 'clsx'
  import { twMerge } from 'tailwind-merge'
  export function cn(...inputs: ClassValue[]) { return twMerge(clsx(inputs)) }
  ```

- [ ] **Step 9: copy 进 6 个 shadcn 组件** —— 不跑 `npx shadcn-ui init`，直接手写：
  - `src/components/ui/button.tsx`
  - `src/components/ui/input.tsx`
  - `src/components/ui/card.tsx`
  - `src/components/ui/label.tsx`
  - `src/components/ui/scroll-area.tsx`
  - `src/components/ui/separator.tsx`
  - 参照 shadcn-ui 官方源（v0 latest），主题用 `cn(...)` + cva。这一步可在实现时由 agent 从 shadcn 文档拷贝；这里只列出文件名。

- [ ] **Step 10: 写 `.eslintrc.cjs`** —— vite-react-ts 默认。

- [ ] **Step 11: 写 `web/.gitignore`**：
  ```
  node_modules/
  dist/*
  !dist/.gitkeep
  ```

- [ ] **Step 12: 改根 `.gitignore`** —— 追加 `web/node_modules/`。

- [ ] **Step 13: `cd web && npm install`**（仅本地；CI 跑 `npm ci`）。验证 `npm run build` 能产出 `dist/index.html`。**这一步覆盖了 Task 1 写的 placeholder。**

- [ ] **Step 14: commit**
  ```
  git add web/package.json web/package-lock.json web/vite.config.ts web/tsconfig*.json \
          web/tailwind.config.ts web/postcss.config.js web/index.html web/.eslintrc.cjs \
          web/.gitignore web/src/main.tsx web/src/index.css web/src/lib/utils.ts \
          web/src/components/ui/*.tsx .gitignore
  git commit -m "feat(web): Vite+React+TS+Tailwind+shadcn scaffold"
  ```

  **不要** commit `web/node_modules/` 或 `web/dist/*`（除 `.gitkeep`）。

---

## Task 3: 前端基础设施 (types / api / store / shell)

**Files:**
- Create: `web/src/types/api.ts`
- Create: `web/src/lib/api.ts`
- Create: `web/src/lib/ws.ts`
- Create: `web/src/stores/auth.ts`
- Create: `web/src/queryClient.ts`
- Create: `web/src/components/ProtectedShell.tsx`
- Create: `web/src/pages/NotFound.tsx`
- Create: `web/src/App.tsx`（替换 Task 2 的占位）
- Create: `web/src/test/setup.ts`
- Create: `web/src/lib/api.test.ts`
- Create: `web/src/stores/auth.test.ts`

- [ ] **Step 1: 写 `src/types/api.ts`** —— 见 design spec §5.1，全部 TS 类型。

- [ ] **Step 2: 写 `src/lib/api.ts`** —— `fetch` 封装：
  ```ts
  export async function api<T>(path: string, opts: RequestInit & { token?: string } = {}): Promise<T> {
    const headers = new Headers(opts.headers)
    if (opts.token) headers.set('Authorization', `Bearer ${opts.token}`)
    if (opts.body && !headers.has('Content-Type')) headers.set('Content-Type', 'application/json')
    const r = await fetch(path, { ...opts, headers })
    if (!r.ok) throw new ApiError(r.status, await r.text().catch(() => ''))
    if (r.status === 204) return undefined as T
    return r.json() as Promise<T>
  }
  export class ApiError extends Error { constructor(public status: number, body: string) { super(body || `HTTP ${status}`) } }
  ```

- [ ] **Step 3: 写 `src/lib/ws.ts`** —— `wsURL(sessionId, token)`：
  ```ts
  export function wsURL(sessionId: string, token: string): string {
    const scheme = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const host = window.location.host
    return `${scheme}//${host}/sessions/${encodeURIComponent(sessionId)}/ws?token=${encodeURIComponent(token)}`
  }
  ```

- [ ] **Step 4: 写 `src/stores/auth.ts`** —— zustand + persist middleware：
  ```ts
  interface AuthState {
    token: string | null
    user: User | null
    setAuth(token: string, user: User): void
    clear(): void
  }
  export const useAuthStore = create<AuthState>()(
    persist(
      (set) => ({
        token: null,
        user: null,
        setAuth: (token, user) => set({ token, user }),
        clear: () => set({ token: null, user: null }),
      }),
      { name: 'pca-auth' }
    )
  )
  ```

- [ ] **Step 5: 写 `src/queryClient.ts`** —— `new QueryClient({ defaultOptions: { queries: { retry: 1, staleTime: 30_000 } } })`。

- [ ] **Step 6: 写 `src/components/ProtectedShell.tsx`** —— 检查 `useAuthStore(s => s.token)`，无则 `<Navigate to="/login" replace />`；有则渲染 `<TopBar /> <aside><SessionList /></aside> <main><Outlet/></main>` 二栏 layout（TopBar / SessionList 在 Task 5 实现，先用 placeholder div）。

- [ ] **Step 7: 写 `src/pages/NotFound.tsx`** —— 简单 "404 + 回首页"。

- [ ] **Step 8: 写 `src/App.tsx`** —— Router shell：
  ```tsx
  <QueryClientProvider client={queryClient}>
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
  ```
  `<Login>` / `<Home>` / `<Chat>` 此 Task 用空占位组件，后续 Task 替换。

- [ ] **Step 9: 写 `src/test/setup.ts`** —— `import '@testing-library/jest-dom/vitest'`。

- [ ] **Step 10: 写 `src/lib/api.test.ts`** + `src/stores/auth.test.ts` —— 单测 api 错误包装、auth store set/clear/persist 回读。

- [ ] **Step 11: 跑 `cd web && npm test` 全 PASS。`npm run build` 仍能编过。**

- [ ] **Step 12: commit**
  ```
  git add web/src/types web/src/lib web/src/stores web/src/queryClient.ts \
          web/src/components/ProtectedShell.tsx web/src/pages/NotFound.tsx \
          web/src/App.tsx web/src/test
  git commit -m "feat(web): types, api client, auth store, router shell"
  ```

---

## Task 4: 登录页

**Files:**
- Create: `web/src/pages/Login.tsx`
- Create: `web/src/pages/Login.test.tsx`
- Create: `web/src/test/mswServer.ts`（msw handlers 共享）

- [ ] **Step 1: 写 `src/test/mswServer.ts`** —— msw `setupServer()`，导出 `server`；handlers 默认空，各测试再 `server.use(...)` 添加。setup.ts 里挂 `beforeAll(() => server.listen())` / `afterEach(() => server.resetHandlers())` / `afterAll(() => server.close())`。

- [ ] **Step 2: 写 `src/pages/Login.test.tsx`** —— RTL 集成测试：
  - 渲染 `<MemoryRouter><Login /></MemoryRouter>`
  - msw 拦 `POST /auth/login` 返 `{token: "jwt.demo", user: {...}}`
  - userEvent 填 tenant=`default` / email=`demo@example.com` / password=`demo123` → 点 Submit
  - 断言：`useAuthStore.getState().token === "jwt.demo"`，导航到 `/`
  - case 错误：msw 返 401 → 显示 "登录失败" 错误消息

- [ ] **Step 3: 跑测试确认失败**。

- [ ] **Step 4: 写 `src/pages/Login.tsx`**：
  - 受控表单：tenant（默认 "default"）/ email / password
  - useMutation 调 `api<LoginResp>('/auth/login', { method:'POST', body:JSON.stringify(...)} )`
  - 成功 → `setAuth(token, user)` + `navigate('/')`
  - 失败 → 红色错误条
  - 用 shadcn Card / Input / Button / Label 拼

- [ ] **Step 5: 跑测试全 PASS。`npm run lint` 干净。**

- [ ] **Step 6: commit**
  ```
  git add web/src/pages/Login.tsx web/src/pages/Login.test.tsx web/src/test/mswServer.ts
  git commit -m "feat(web): login page with auth flow"
  ```

---

## Task 5: 会话列表 + Home

**Files:**
- Create: `web/src/components/SessionList.tsx`
- Create: `web/src/components/SessionList.test.tsx`
- Create: `web/src/components/TopBar.tsx`
- Create: `web/src/pages/Home.tsx`
- Create: `web/src/pages/Home.test.tsx`

- [ ] **Step 1: 写 `SessionList.test.tsx`** —— msw 拦 `GET /sessions` 返三个 session；渲染 `<SessionList />`：
  - 断言三行渲染、当前 `:id` 路由命中时高亮
  - 点击 "New session" 按钮 → msw 拦 `POST /sessions` → 断言导航到新 id
  - 点击删除按钮 → msw 拦 `DELETE /sessions/:id` → 列表移除该行

- [ ] **Step 2: 写 `TopBar.tsx`** —— 左侧 logo "Private Coding Agent" + 右侧当前 user.email + Logout button（清 store + navigate `/login`）。

- [ ] **Step 3: 写 `SessionList.tsx`**：
  - `useQuery(['sessions'], () => api<{sessions: Session[]}>('/sessions', { token }))`
  - 渲染列表 + 新建按钮 + 每行 hover 时显示删除 icon
  - `useMutation` 实现 create / delete，成功后 `queryClient.invalidateQueries(['sessions'])`

- [ ] **Step 4: 跑 SessionList 测试 PASS**。

- [ ] **Step 5: 写 `Home.test.tsx`** —— 路由 `/` 时：
  - case 0 session → 自动导航到新建（POST /sessions 拦截后跳 `/sessions/:newId`）
  - case ≥1 session → 渲染 placeholder "Select a session"
  - 已登录但 token 错（msw 返 401）→ 跳回 `/login`

- [ ] **Step 6: 写 `Home.tsx`** —— 右侧 main 区域：根据 `sessions` query 结果决定 placeholder 或 auto-redirect。

- [ ] **Step 7: 跑全部测试 PASS。**

- [ ] **Step 8: commit**
  ```
  git add web/src/components/SessionList.tsx web/src/components/SessionList.test.tsx \
          web/src/components/TopBar.tsx web/src/pages/Home.tsx web/src/pages/Home.test.tsx
  git commit -m "feat(web): session list with create/delete + Home shell"
  ```

---

## Task 6: 聊天页骨架 + Composer

**Files:**
- Create: `web/src/components/Composer.tsx`
- Create: `web/src/components/Composer.test.tsx`
- Create: `web/src/pages/Chat.tsx`（先不含 WS，纯历史 + Composer 占位）
- Create: `web/src/pages/Chat.test.tsx`

- [ ] **Step 1: 写 `Composer.test.tsx`** —— RTL + userEvent：
  - 渲染 `<Composer onSend={fn} disabled={false} />`
  - 输入 "hello"，按 Enter → fn 被调用且 textarea 清空
  - 按 Shift+Enter → 不发送、文本插入换行
  - `disabled={true}` → 按钮 + textarea 都 disabled，Enter 不发送
  - 发送中态：`onSend` 返一个 pending Promise 时按钮 spinner

- [ ] **Step 2: 写 `Composer.tsx`** —— shadcn Textarea + Button + lucide `Send` icon；用 `useState` 管 value、`useTransition` 管 pending。

- [ ] **Step 3: 写 `Chat.test.tsx`** —— msw 拦 `GET /sessions/:id/messages` 返三条历史 → 渲染三个气泡（user / assistant / tool）；WS 部分 mock 成无操作（Task 7 引入真测）。

- [ ] **Step 4: 写 `Chat.tsx`**：
  - `useParams<{id: string}>` 拿 sid
  - `useQuery(['messages', sid], () => api(`/sessions/${sid}/messages`, ...))`
  - 渲染 `<MessageList>`（Task 7 实现，本 Task 用临时简版：把 messages 直接 map 成 `<div>`）
  - 底部挂 `<Composer onSend={...placeholder...}>`，本 Task 的 onSend 仅 `console.log`（Task 7 接 WS）

- [ ] **Step 5: 跑测试全 PASS。`npm run build` 干净。**

- [ ] **Step 6: commit**
  ```
  git add web/src/components/Composer.tsx web/src/components/Composer.test.tsx \
          web/src/pages/Chat.tsx web/src/pages/Chat.test.tsx
  git commit -m "feat(web): chat page scaffold with history fetch and Composer"
  ```

---

## Task 7: WS hook + MessageList + ToolCallCard

**Files:**
- Create: `web/src/hooks/useChatSocket.ts`
- Create: `web/src/hooks/useChatSocket.test.ts`
- Create: `web/src/components/MessageList.tsx`
- Create: `web/src/components/MessageList.test.tsx`
- Create: `web/src/components/ToolCallCard.tsx`
- Modify: `web/src/pages/Chat.tsx`（接入 WS hook + 真 MessageList）

- [ ] **Step 1: 写 `useChatSocket.test.ts`** —— 用 `mock-socket` 的 `Server`：
  - 启 mock WS server 接受 `ws://localhost/sessions/:id/ws?token=...`
  - renderHook `useChatSocket(sid)`，断言初始 `status === 'connecting'`
  - server 发 open → status 转 'open'
  - hook.sendUserMessage('hi') → server 收到 `{"type":"user_message","content":"hi"}`
  - server 发 `{"type":"event","event":{"kind":"assistant_message","text":"hi back"}}` → `events` 数组追加一条
  - server close 1011 → status 转 'error'
  - server 正常 close 1000 → status 'closed'，不重连
  - server 异常 close 1006 → 触发重连，二次 open → status 回 'open'（验证退避：用 `vi.useFakeTimers()` 推进 1000ms）

- [ ] **Step 2: 写 `useChatSocket.ts`**：
  - useRef 持 `WebSocket | null`
  - useState `events: AgentEvent[]`、`status`
  - useEffect on (sessionId, token) → 建连 + bind 4 个事件回调
  - 重连：`reconnectAttempts` ref，序列 `[1000, 3000, 9000]`，超出标 error 不再重连
  - cleanup: `ws.close(1000, 'unmount')`
  - `sendUserMessage(content)`：optimistic 把 `{kind:'user', text: content}` 推进 events（design spec §6.3）

- [ ] **Step 3: 跑 hook 测试全 PASS。**

- [ ] **Step 4: 写 `ToolCallCard.tsx`** —— shadcn Card + `<details>` 展开，title 是 `🔧 {tool.function.name}`，body pre-format 输入/输出 JSON。

- [ ] **Step 5: 写 `MessageList.test.tsx`** —— 给定 messages + events 混合数组，断言：
  - role=user → primary 气泡（class 含 `bg-primary` 等）
  - role=assistant text → muted 气泡 + markdown 渲染（断言 `**bold**` 渲染为 `<strong>`）
  - role=assistant with tool_calls → 渲染 `<ToolCallCard>`
  - role=tool → 缩进 + tool_call_id 在 caption
  - role=system → 居中标签
  - 滚动行为：mock `scrollTo`，新消息追加且用户在底部 → 自动滚；上滚 100px 后追加 → 不自动滚

- [ ] **Step 6: 写 `MessageList.tsx`**：
  - props: `messages: Message[], pendingEvents: AgentEvent[]`
  - 合并策略：messages 是已持久化（来自 react-query），pendingEvents 是 WS 流（来自 useChatSocket）；merge by seq（如果 server 帧带 seq）或 append 顺序（如果不带）
  - 渲染 6 种气泡
  - `useEffect` + `scrollIntoView` 实现 sticky-to-bottom
  - `react-markdown` 渲染 assistant text

- [ ] **Step 7: 改 `Chat.tsx`** —— 接 `useChatSocket(sid)`，把 `events` 传给 `<MessageList>`，`<Composer onSend={hook.sendUserMessage} disabled={hook.status !== 'open'}>`；done 帧到时 `queryClient.invalidateQueries(['messages', sid])`。

- [ ] **Step 8: 跑全部测试 PASS。`npm run build` + `npm run lint` 干净。**

- [ ] **Step 9: commit**
  ```
  git add web/src/hooks/useChatSocket.ts web/src/hooks/useChatSocket.test.ts \
          web/src/components/MessageList.tsx web/src/components/MessageList.test.tsx \
          web/src/components/ToolCallCard.tsx web/src/pages/Chat.tsx
  git commit -m "feat(web): WS hook + MessageList + tool call rendering"
  ```

---

## Task 8: Go embed 装配 + E2E 扩到 28 步

**Files:**
- Create: `cmd/server/webembed.go`
- Modify: `cmd/server/main.go`
- Modify: `internal/httpx/server.go`（增 `EnableSPA bool` 给 Deps，可选）
- Create: `Makefile`
- Modify: `deploy/compose/test-e2e.sh`
- Modify: `deploy/compose/Dockerfile`（Go 服务的 Dockerfile，多 stage 加 node build）

- [ ] **Step 1: 跑 `cd web && npm run build`，产出 `web/dist/index.html` + `web/dist/assets/*`。** 提交前确认 `git status` 显示 `web/dist/` 被覆盖；如果根 `.gitignore` ignore 了 dist，需要 `git add -f web/dist/` 强制加，**或者** 改策略：不 commit dist，CI / Makefile 每次现 build。
    - **决定**：不 commit `web/dist/`（除 `.gitkeep` + 占位 index.html）；构建产物在 Docker build 多 stage 阶段产生；本地 dev 由 vite dev server 接管。

- [ ] **Step 2: 写 `cmd/server/webembed.go`**：
  ```go
  //go:build !nowebui
  package main

  import (
      "embed"
      "io/fs"
  )

  //go:embed all:../../web/dist
  var webDistFS embed.FS

  func webStaticFS() (fs.FS, error) {
      return fs.Sub(webDistFS, "../../web/dist")
  }
  ```
  ⚠️ 注意：`//go:embed` 的路径相对于当前 Go 文件所在目录。`cmd/server/` 下访问 `web/dist` 必须穿两层；如果跨目录 embed 不行，把 `webembed.go` 放到仓库根的小包 `internal/webui/embed.go` 里，由 main.go import。**优先方案：放 `internal/webui/embed.go`**：
  ```go
  package webui
  import ("embed"; "io/fs")
  //go:embed all:dist
  var distFS embed.FS
  func FS() (fs.FS, error) { return fs.Sub(distFS, "dist") }
  ```
  然后把 `web/dist/` 改名 / 软链 / 复制到 `internal/webui/dist/`。
  **最干净方案**：把前端整个移到 `internal/webui/` 下；`internal/webui/dist/.gitkeep`、`internal/webui/package.json` 等。design spec 用 `/web` 是惯例，但 Go embed 路径限制让 `internal/webui` 更顺。**这里 plan 决策：使用 `internal/webui/` 而非 `web/`**。Task 2-7 涉及的所有 `web/` 路径相应改为 `internal/webui/`，本 Task 改名。

  > **plan 提示**：实施时把 Task 2 之前的所有 `web/` 改成 `internal/webui/`。这一条 ADR 在 spec ADR-61 里没明说，本 plan 内补充：使用 `internal/webui/` 子目录是为绕过 Go embed 跨目录限制。如果嫌 Task 2 已写完再改名麻烦，可以在 Task 2 开始就用 `internal/webui/`。

- [ ] **Step 3: 改 `cmd/server/main.go`**：
  - import `internal/webui`
  - 在 `register` 函数末尾、所有路由注册之后，加：
    ```go
    if fsys, err := webui.FS(); err == nil {
        if err := httpx.RegisterSPAFallback(r, fsys); err != nil {
            log.Printf("spa fallback: %v", err)
        }
    }
    ```
  - 在 WS 路由前挂 `auth.WSTokenFromQuery()`：当前 `sessionWSHandler.Register(protected)` 内部已用 `protected` group 的 `auth.Middleware`；需要在该 group 之前另起一个 middleware 链。
    **最小改法**：把 WS 路由从 `protected` 移到一个专门的 group：
    ```go
    wsGroup := r.Group("/")
    wsGroup.Use(audit.Middleware(...))      // 沿用
    wsGroup.Use(auth.WSTokenFromQuery())    // 新
    wsGroup.Use(auth.Middleware(jwtSvc))    // 沿用
    sessionWSHandler.Register(wsGroup)
    ```
    然后从 `protected` group 里**移除** `sessionWSHandler.Register(protected)` 那一行（如果存在）。

- [ ] **Step 4: 写 `Makefile`**（仓库根）：
  ```makefile
  .PHONY: web build test

  web:
  	cd internal/webui && npm ci && npm run build

  build: web
  	go build -o bin/server ./cmd/server

  test:
  	go test ./...
  	cd internal/webui && npm test

  clean:
  	rm -rf internal/webui/dist/* internal/webui/node_modules bin/
  ```

- [ ] **Step 5: 改 `deploy/compose/Dockerfile`**（Go 服务 Dockerfile）—— 多 stage：
  ```dockerfile
  # ---- stage 1: web ----
  FROM node:20-alpine AS web
  WORKDIR /src
  COPY internal/webui/package.json internal/webui/package-lock.json ./
  RUN npm ci
  COPY internal/webui/ ./
  RUN npm run build

  # ---- stage 2: go ----
  FROM golang:1.26-alpine AS go
  WORKDIR /src
  COPY go.mod go.sum ./
  RUN go mod download
  COPY . .
  COPY --from=web /src/dist ./internal/webui/dist
  RUN CGO_ENABLED=0 go build -o /bin/server ./cmd/server

  FROM alpine:3.20
  COPY --from=go /bin/server /usr/local/bin/server
  ENTRYPOINT ["/usr/local/bin/server"]
  ```

- [ ] **Step 6: `go build ./...` 干净。** 起本地服务，浏览器访问 `http://localhost:8080/` 验证看到 `<div id="root">build pending</div>` 或真实页面。

- [ ] **Step 7: 扩 `deploy/compose/test-e2e.sh` 到 28 步**：
  - 所有 `[N/25]` → `[N/28]`（sed）
  - 追加：
    ```bash
    echo "[26/28] GET / returns html ..."
    HTML=$(curl -fsS http://localhost:8080/)
    echo "$HTML" | grep -q 'id="root"' || { echo "root html missing"; exit 1; }
    CTYPE=$(curl -sI http://localhost:8080/ | tr -d '\r' | awk '/^Content-Type:/{print $2}')
    [[ "$CTYPE" == "text/html;" || "$CTYPE" == "text/html" || "$CTYPE" == text/html* ]] || { echo "ctype: $CTYPE"; exit 1; }

    echo "[27/28] GET /login (SPA fallback) returns same index.html ..."
    HTML2=$(curl -fsS http://localhost:8080/login)
    echo "$HTML2" | grep -q 'id="root"' || { echo "spa fallback failed"; exit 1; }

    echo "[28/28] API not shadowed by fallback: GET /sessions returns JSON ..."
    CT=$(curl -sI -H "Authorization: Bearer $TOK" http://localhost:8080/sessions | tr -d '\r' | awk '/^Content-Type:/{print $2}')
    [[ "$CT" == application/json* ]] || { echo "API content-type: $CT"; exit 1; }
    ```

- [ ] **Step 8: `bash -n deploy/compose/test-e2e.sh` 语法清。** E2E 真跑由人或 CI 执行（含 docker build web stage，会拉 node 镜像）。

- [ ] **Step 9: commit**
  ```
  git add internal/webui/dist/.gitkeep internal/webui/dist/index.html \
          internal/webui/embed.go cmd/server/main.go Makefile \
          deploy/compose/Dockerfile deploy/compose/test-e2e.sh
  git commit -m "feat(server): embed web UI + Dockerfile multi-stage + e2e 28 steps"
  ```
  ⚠️ 如果在 Task 2 时已经把 `web/` 改成 `internal/webui/`，此 commit 就不会出现路径搬迁；否则把搬迁也放进这个 commit。

---

## Task 9: README + 写正式 slice plan 收尾

**Files:** Modify `README.md`

> 本 Task 文件（`docs/superpowers/plans/2026-05-20-slice-08-web-frontend.md`）在 Task 1 开始前就已写完并 commit；本 Task 不再单独 commit plan。

- [ ] **Step 1: 切片 8 复选框打 `[x]`：**
  ```
  - [x] 切片 8：Web Frontend (Chat + WS Streaming)
  ```

- [ ] **Step 2: 新增 "Web Frontend" 章节** —— 放在 "内部 MCP 工具" 之后：
  ```
  ## Web Frontend

  React + Vite SPA，源码在 `internal/webui/`，由 `go:embed` 打进 server 二进制。

  ### 本地开发（前后端分离）

  ```powershell
  # 起后端 (:8080)
  go run ./cmd/server --config config\config.yaml
  # 另开终端起 vite dev server (:5173, 自动 proxy 到 :8080)
  cd internal/webui
  npm install
  npm run dev
  # 浏览器访问 http://localhost:5173
  ```

  ### 生产构建（单二进制）

  ```powershell
  make build         # = make web + go build
  ./bin/server --config config/config.yaml
  # 浏览器访问 http://localhost:8080
  ```

  Docker compose 路径会自动跑 multi-stage build。
  ```

- [ ] **Step 3: 端点表追加：**
  ```
  | GET | / | - | SPA: 首页 (登录后 redirect 到 /sessions/:id) |
  | GET | /login, /sessions/:id | - | SPA: 由前端路由接管，后端返回 index.html |
  ```

- [ ] **Step 4: commit**
  ```
  git add README.md
  git commit -m "docs: mark slice 8 complete and document web frontend"
  ```

---

## 验收 Checklist

- [ ] `docs/superpowers/specs/2026-05-20-slice-08-web-frontend-design.md` 存在
- [ ] `docs/superpowers/plans/2026-05-20-slice-08-web-frontend.md` 存在
- [ ] `internal/auth/ws_token.go` + test 全 PASS
- [ ] `internal/httpx/spa.go` + test 全 PASS
- [ ] `internal/webui/` scaffold 完整：`npm install && npm run build && npm test && npm run lint` 全过
- [ ] `internal/webui/dist/` 含 `index.html` + `assets/*`
- [ ] `go test ./... -count=1` 全 PASS
- [ ] `go build ./...` 干净，binary 含 embed 产物
- [ ] 手测：`./bin/server` 启动后浏览器访问 `http://localhost:8080/`：能登录、看会话、发消息、看到 tool_call 折叠卡和 final 气泡
- [ ] E2E 28 步 syntax check 通过
- [ ] E2E 真跑 28 步全过（compose docker build 含 node + go 两阶段）
- [ ] README 切片 8 勾选 + Web Frontend 章节 + 端点表更新
- [ ] git tree clean，commit 按 Conventional Commits 切分

## 关键不变量

1. **WS token 双通道，路由级隔离** —— `WSTokenFromQuery` 仅挂在 `/sessions/:id/ws` 所在 group，REST 一律走 `Authorization: Bearer`，避免 token 落进 access log。
2. **SPA fallback 仅 GET** —— 任何 method != GET 的 NoRoute 命中仍返 404 JSON，不退化成 200 index.html。
3. **API 路由优先于 fallback** —— `engine.NoRoute` 在所有 API 路由注册之后挂，gin 路由树命中时优先；fallback 只接 unmatched。
4. **dist 是 build artifact，不进 git** —— `internal/webui/dist/` 由 Makefile / Dockerfile 现 build；仅 `.gitkeep` + 占位 `index.html` 入版本控制，前者维持目录、后者满足 `embed.ReadFile("index.html")` 不报错。
5. **token 不入 URL log** —— `WSTokenFromQuery` 注入 header 后**不**改写 `c.Request.URL`，但 gin access log 默认会记 RawQuery 含 token。部署文档需提醒：把 token 字段从 nginx / 反向代理日志屏蔽。
6. **optimistic + invalidate** —— Composer 发送时本地追加 user 气泡；done 帧到达后 `invalidateQueries(['messages', sid])` 用持久化版本 rebase 兜底。
7. **WS 重连只在异常 close** —— `code === 1000`（正常关闭，如 unmount）不重连；`1006` / `1011` / 其他异常按 `[1s, 3s, 9s]` 退避三次后标 error。
