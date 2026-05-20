# Slice 7 — Memory (basic) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 交付 `internal/memory` 包 + 4 个内部 MCP 工具：让 user / agent 能把短句记忆持久化、按 type / tag / 关键字搜回。仅 User scope；不做 pgvector，不做自动注入，不做 Reflection。Spec §7 的 "Memory 作为内部 MCP" 在本切片完工。

**Architecture:** 单层依赖 —
1. `Repo` (pgxpool) 持久化；多租户 scoped SELECT；
2. `Service` 编排：validation + 调 repo + Search 命中后 touch `last_used_at`；
3. `Handler` (REST `/memories`) 与 4 个 `tools/memory_*.go` 各自挂到 protected 路由 / Tool Registry，共享一个 `*Service`。

依赖方向：`internal/memory` 仅 import `auth`、`pgx`、`uuid`；不 import session/agent。Tool 适配层在 `internal/toolbus/tools/memory.go`。

**Tech Stack:**
- Go 1.26、gin、pgx v5、testify、google/uuid（沿用）
- 新增依赖：无
- DB 特性：`TEXT[]` + GIN 索引 + ILIKE

---

## 前置条件

依赖 Slice 1.5 / 2 / 3 / 4 / 5 / 6 已完工（HEAD = `0360877` "fix(e2e): route websocat via compose network"）。

`internal/toolbus` 的 Tool 接口、`tools.Registry`、`Bus.Invoke` 在本切片完全沿用。

## 本切片边界

**在本切片**：
- migration 0009：单表 `memories`，含 GIN(tags) + (tenant_id, owner_user_id, created_at DESC) 索引
- `Memory / CreateRequest / UpdateRequest / ListFilter / SearchRequest` 类型
- REST: `POST /memories`、`GET /memories`、`GET /memories/:id`、`PUT /memories/:id`、`DELETE /memories/:id`
- 4 个 MCP 工具：`memory.save / memory.search / memory.list / memory.delete`
- Search 命中后 `last_used_at = now()`（未来 LRU 归档预埋）
- Search 至少要求 query / type / tags 之一非空，否则 `ErrEmptySearch` → 400
- 跨租户 / 跨 owner 访问返 404，无存在性泄露
- E2E: 21 → 25 步

**不在本切片**：
- Project / Tenant scope（推后到 slice 8/9）
- pgvector 向量相似度、confidence 衰减、0.92 去重
- 自动会话起始注入
- Reflection Agent
- UI 隐私控制（导出/一键清空）

## File Structure

```
internal/db/migrations/
  0009_create_memories.up.sql       Task 1
  0009_create_memories.down.sql     Task 1

internal/memory/
  types.go                          Task 2: Memory/CreateRequest/UpdateRequest/SearchRequest + 常量
  errors.go                         Task 2: 错误哨兵
  errors_test.go                    Task 2
  repo.go                           Task 2: Repo (Insert/Get/List/Update/Delete/Search)
  repo_test.go                      Task 2: dockertest, fixtures(tenant+user)
  service.go                        Task 3: Service + validation
  service_test.go                   Task 3: 真 repo + dockertest
  handler.go                        Task 4: REST handler + HandlerService interface
  handler_test.go                   Task 4: mock service

internal/toolbus/tools/
  memory.go                         Task 5: 4 个 Tool + MemoryService interface
  memory_test.go                    Task 5: mock service

cmd/server/main.go                  Task 6: 装配 memory.Service + handler + 4 个 tool

deploy/compose/test-e2e.sh          Task 7: 22-25 步 + step 13 tool 列表更新

README.md                           Task 8: 进度勾选 + /memories 端点表 + tools 说明
docs/superpowers/specs/...          Task 0 (设计稿,先于实现写)
docs/superpowers/plans/...          Task 9 (本文件)
```

---

## Task 0: 写 design spec

**Files:**
- Create: `docs/superpowers/specs/2026-05-20-slice-07-memory-design.md`

按 slice 6 design spec 模板（概述 / 前置 / 核心需求 / 整体架构 / 接口与数据模型 / 数据流 / 测试策略 / Task 拆解 / 验收 / 风险 / ADR / Spec 对齐）写完整。重点：

- 与 spec §7 差距明示推迟表
- ADR-51 单表 + type 列、ADR-52 仅 User scope、ADR-53 ILIKE+tag 不上 pgvector、ADR-54 REST + MCP 双暴露、ADR-55 不自动注入不 Reflection、ADR-56 search touch last_used_at、ADR-57 search 至少一个过滤条件

- [ ] **Step 1: 写 spec 文件。**
- [ ] **Step 2: `git add docs/superpowers/specs/2026-05-20-slice-07-memory-design.md && git commit -m "docs(memory): design spec for slice 7 (basic memory)"`**

---

## Task 1: DB 迁移

**Files:**
- Create: `internal/db/migrations/0009_create_memories.up.sql`
- Create: `internal/db/migrations/0009_create_memories.down.sql`

- [ ] **Step 1: 写 up.sql** —— design spec §5.1 完整 schema。要点：
  - FK 到 `tenants` / `users`
  - `type` CHECK in `('profile','preference','knowledge','lesson')`
  - `tags TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[]`
  - `last_used_at / created_at / updated_at` 三个 TIMESTAMPTZ
  - 索引 `(tenant_id, owner_user_id, created_at DESC)` + GIN(tags)

- [ ] **Step 2: 写 down.sql** —— `DROP TABLE IF EXISTS memories;`

- [ ] **Step 3: 跑 `go test ./internal/session/... -count=1 -timeout 180s`** 确认全部 migration（含 0009）跑通。

- [ ] **Step 4: commit**
  ```
  git add internal/db/migrations/0009_create_memories.*.sql
  git commit -m "feat(db): migration 0009 memories table"
  ```

---

## Task 2: 类型 + 错误 + repo

**Files:**
- Create: `internal/memory/types.go`
- Create: `internal/memory/errors.go`
- Create: `internal/memory/errors_test.go`
- Create: `internal/memory/repo.go`
- Create: `internal/memory/repo_test.go`

- [ ] **Step 1: 写 `types.go`** —— `Memory`, `CreateRequest`, `UpdateRequest`（含 `TagsSet bool` 区分"未提供"vs"清空"）, `ListFilter`, `SearchRequest`；`Type*`、`Source*`、`DefaultListLimit=20`、`MaxListLimit=100` 常量。

- [ ] **Step 2: 写 `errors.go` + `errors_test.go`** —— 哨兵：`ErrMemoryNotFound`, `ErrEmptyContent`, `ErrInvalidType`, `ErrEmptySearch`。测试断言非空。

- [ ] **Step 3: 写 `repo_test.go`** —— 复用 slice 6 的 dockertest 模板：
  - `TestMain` 起 postgres:16，跑 `db.Migrate`，全文件共享 DSN
  - `fixtures(t, pg)` 每次 insert 新 tenant + user
  - 测试用例：InsertGetList / GetNotFound_CrossTenant（含 cross-tenant + cross-owner + missing） / List filter by type/tag/query / List limit+offset / Update partial（仅 content / 仅 tags=[]） / Update cross-tenant 返 ErrMemoryNotFound / Delete + double-delete 返 ErrMemoryNotFound / Search 关键字 / Search tags && / Search type / Search 组合 / Search 命中后 last_used_at 推进 / Search 跨租户返 0

- [ ] **Step 4: 跑测试确认失败**（repo 未实现）。

- [ ] **Step 5: 写 `repo.go`**：
  - `Repo.Insert`：handle nil tags → `[]string{}`（CHECK NOT NULL）；round-trip Get 拿权威时间戳
  - `Repo.Get`：`WHERE id=$1 AND tenant_id=$2 AND owner_user_id=$3`，无行返 `ErrMemoryNotFound`
  - `Repo.List`：动态 WHERE + limit/offset，参数索引用 `len(args)+1`
  - `Repo.Update`：partial set + RowsAffected==0 → `ErrMemoryNotFound`；`TagsSet=true && Tags==nil` → 用 `[]string{}` 清空
  - `Repo.Delete`：scoped DELETE + RowsAffected==0 → `ErrMemoryNotFound`
  - `Repo.Search`：构造 WHERE + LIMIT；命中后 `UPDATE memories SET last_used_at=now() WHERE id = ANY($ids) AND tenant_id=$x AND owner_user_id=$y`

- [ ] **Step 6: 跑测试全 PASS**（`go test ./internal/memory/... -count=1 -timeout 180s`）。

- [ ] **Step 7: commit**
  ```
  git add internal/memory/{types,errors,errors_test,repo,repo_test}.go
  git commit -m "feat(memory): Repo with dockertest coverage (CRUD + search + last_used_at touch)"
  ```

---

## Task 3: Service 层

**Files:**
- Create: `internal/memory/service.go`
- Create: `internal/memory/service_test.go`

- [ ] **Step 1: 写 `service_test.go`** —— 用真 repo + dockertest：
  - `newService(t)` helper（newPool + fixtures + NewService）
  - Create happy / Create EmptyContent / Create InvalidType
  - List InvalidType / Update InvalidType / Update EmptyContent / Search EmptyParams / Search InvalidType
  - Search HappyRoundTrip
  - CrossTenant404（Get/Delete/Update 任一跨租户返 `ErrMemoryNotFound`）

- [ ] **Step 2: 跑测试确认失败**。

- [ ] **Step 3: 写 `service.go`**：
  - `Service` 字段 `repo *Repo`
  - `Create`：trimspace 空内容 → `ErrEmptyContent`；type 不在白名单 → `ErrInvalidType`；source 缺省 "user"
  - `List`：Type 非空必须合法
  - `Update`：Type / Content 各自校验
  - `Search`：query/type/tags 全空 → `ErrEmptySearch`；Type 非空校验
  - `isValidType` 私有 helper

- [ ] **Step 4: 跑测试全 PASS**。

- [ ] **Step 5: commit**
  ```
  git add internal/memory/service*.go
  git commit -m "feat(memory): Service layer with validation + cross-tenant safety"
  ```

---

## Task 4: REST Handler

**Files:**
- Create: `internal/memory/handler.go`
- Create: `internal/memory/handler_test.go`

- [ ] **Step 1: 写 `handler_test.go`** —— `mockHandlerSvc` (函数字段) + `auth.NewJWT` 签发 token：
  - Create OK / Create_Validation（ErrEmptyContent → 400 "validation: content"；ErrInvalidType → 400 "validation: type"） / Create Unauthorized 401
  - List OK with filters（验证 query string → ListFilter 映射） / 验证响应 envelope `{"memories":[...]}`
  - Get NotFound 404 / Get BadID 400
  - Update_PartialFields（验证 `Content` 设置、`TagsSet=true && Tags=[]` 清空） / Update_TagsNotSet（验证 TagsSet=false）
  - Delete OK 204 / Delete NotFound 404

- [ ] **Step 2: 跑测试确认失败**。

- [ ] **Step 3: 写 `handler.go`**：
  - 本地 `HandlerService` interface（context.Context-based 签名）
  - `Handler.Register(rg)` 挂 5 个路由到 `rg.Group("/memories")`
  - `claims(c)` + `parseID(c)` helper
  - `mapErr` 统一映射：NotFound 404 / Empty / InvalidType / EmptySearch → 400 / else 500
  - Update 用 `map[string]json.RawMessage` 解 body，配合 `parseUpdate` 设置 `TagsSet`
  - 响应 envelope `{"memories":[...]}`

- [ ] **Step 4: 跑测试全 PASS。`go vet ./internal/memory/...` 干净。**

- [ ] **Step 5: commit**
  ```
  git add internal/memory/handler*.go
  git commit -m "feat(memory): REST CRUD handler for /memories endpoints"
  ```

---

## Task 5: 内部 MCP 工具

**Files:**
- Create: `internal/toolbus/tools/memory.go`
- Create: `internal/toolbus/tools/memory_test.go`

- [ ] **Step 1: 写 `memory_test.go`** —— `mockMemSvc` (函数字段) 模拟 Service：
  - `memory.save` OK（验证 default Source=agent）
  - `memory.save` ValidationWraps（`ErrInvalidType` → `errors.Is(err, toolbus.ErrInvalidArguments)`）
  - `memory.search` OK（验证 query 透传 + 输出含 content）
  - `memory.search` EmptyParams（`ErrEmptySearch` → wrap as InvalidArguments）
  - `memory.list` OK（验证 type / limit 透传）
  - `memory.delete` OK（输出 `{"ok":true}`） / Delete NotFound 不 wrap

- [ ] **Step 2: 跑测试确认失败**。

- [ ] **Step 3: 写 `memory.go`**：
  - 本地 `MemoryService` interface（Create / Search / List / Delete）
  - `wrapMemoryErr` helper：Empty / InvalidType / EmptySearch → `fmt.Errorf("%w: %v", toolbus.ErrInvalidArguments, err)`；其余原样返
  - 4 个 Tool 类型分别实现 `Name() / Description() / Schema() / Invoke()`
  - `memory.save` 默认 source = `"agent"`（agent 调用为主）
  - 输出格式：save → `{"id":...}`；search/list → `{"items":[...]}`；delete → `{"ok":true}`
  - JSONSchema 标 `additionalProperties: false`

- [ ] **Step 4: 跑测试全 PASS**（`go test ./internal/toolbus/tools/... -count=1 -run TestMemory`）。

- [ ] **Step 5: commit**
  ```
  git add internal/toolbus/tools/memory*.go
  git commit -m "feat(memory): internal MCP tools memory.{save,search,list,delete}"
  ```

---

## Task 6: 装配进 main.go

**Files:** Modify `cmd/server/main.go`

- [ ] **Step 1: import `internal/memory`**。

- [ ] **Step 2: 装配 memory service + handler + 4 tools**（在 toolbus 注册块内）：
  ```go
  memoryService := memory.NewService(memory.NewRepo(pool))
  memoryHandler := memory.NewHandler(memoryService)
  _ = toolRegistry.Register(tools.NewMemorySave(memoryService))
  _ = toolRegistry.Register(tools.NewMemorySearch(memoryService))
  _ = toolRegistry.Register(tools.NewMemoryList(memoryService))
  _ = toolRegistry.Register(tools.NewMemoryDelete(memoryService))
  ```
  在 protected group 内挂 `memoryHandler.Register(protected)`。

- [ ] **Step 3: `go build ./... && go vet ./...` 全清。**

- [ ] **Step 4: commit**
  ```
  git add cmd/server/main.go
  git commit -m "feat(memory): wire Service + REST handler + 4 MCP tools into main"
  ```

---

## Task 7: E2E 扩到 25 步

**Files:** Modify `deploy/compose/test-e2e.sh`

- [ ] **Step 1: 把所有 `[N/21]` 改 `[N/25]`**（`sed -i 's|\[\([0-9]\+\)/21\]|[\1/25]|g'`）。

- [ ] **Step 2: 更新 step 13 tool 列表**：现在 12 个工具（添加 4 个 memory.*）：
  ```
  fs.glob,fs.list,fs.read,fs.write,grep,llm.chat,llm.embed,memory.delete,memory.list,memory.save,memory.search,shell.exec,
  ```

- [ ] **Step 3: 在 [21/25] WS round-trip 之后追加：**
  - [22/25] POST /memories x2 (preference + knowledge)
  - [23/25] GET /memories?type=preference&tag=go 过滤，断言 length=1
  - [24/25] `memory.save` via tool → `memory.search` via tool round-trip 找回
  - [25/25] DELETE /memories/{id}，再 GET 返 404

- [ ] **Step 4: `bash -n deploy/compose/test-e2e.sh` 语法清。E2E 真跑由人或 CI 执行。**

- [ ] **Step 5: commit**
  ```
  git add deploy/compose/test-e2e.sh
  git commit -m "test(e2e): extend to 25 steps with /memories CRUD + MCP round-trip"
  ```

---

## Task 8: README

**Files:** Modify `README.md`

- [ ] **Step 1: 切片 7 复选框打 `[x]`。**
- [ ] **Step 2: 端点表追加 5 行：POST/GET/GET/PUT/DELETE /memories。**
- [ ] **Step 3: 把 "/tools 列出 8 个 internal tools" 改成 "12 个"，并新增 "内部 MCP 工具" 一节列全 12 个。**
- [ ] **Step 4: commit**
  ```
  git add README.md
  git commit -m "docs: mark slice 7 complete and document /memories endpoints + memory tools"
  ```

---

## Task 9: 写正式 slice plan

**File:** `docs/superpowers/plans/2026-05-20-slice-07-memory.md`

- [ ] **Step 1: 写本文件（TDD 风格 + `- [ ]` 步骤 + 每个 Task 末尾 commit），可被 agentic worker 直接执行。**
- [ ] **Step 2: commit**
  ```
  git add docs/superpowers/plans/2026-05-20-slice-07-memory.md
  git commit -m "docs(memory): formal slice 7 implementation plan"
  ```

---

## 验收 Checklist

- [ ] `docs/superpowers/specs/2026-05-20-slice-07-memory-design.md` 已存在
- [ ] migration 0009 up + down 在 dockertest 跑通（通过 session 包测试间接覆盖）
- [ ] `go test ./internal/memory/... -count=1` 全 PASS（含 dockertest）
- [ ] `go test ./internal/toolbus/tools/... -run TestMemory -count=1` 全 PASS
- [ ] `go vet ./...` 干净，`go build ./...` 干净
- [ ] 跨租户访问 memory 返 404（service_test + handler_test 覆盖）
- [ ] Search 空参数返 `ErrEmptySearch` → 400（service_test + tool test 覆盖）
- [ ] Search 命中触发 `last_used_at` 推进（repo_test 覆盖）
- [ ] E2E 25/25 在 compose 环境跑通
- [ ] README 切片 7 勾选 + 5 个 /memories 端点 + 4 个 memory.* 工具入表
- [ ] git tree clean，commit 按 Conventional Commits 切分

## 关键不变量

1. **跨租户 404，不泄露存在性** —— 所有 repo SELECT/UPDATE/DELETE 都 `WHERE tenant_id=$X AND owner_user_id=$Y`，错过返 `ErrMemoryNotFound`。
2. **type 白名单** —— DB CHECK + Service `isValidType` 双重保护。
3. **Search 必带过滤** —— query/type/tags 至少一个非空，避免全表扫退化为 dump。
4. **Search 副作用** —— 命中后 `last_used_at` 推进；scoped 在 (tenant, owner) 之内。
5. **UpdateRequest.TagsSet** —— Handler 解析时区分"未传 tags 字段（保留）"vs"传了 []（清空）"。Service / Repo 都按这个语义读 `TagsSet`。
6. **MCP tool 错误映射** —— validation 错（Empty/InvalidType/EmptySearch）必须 wrap 成 `toolbus.ErrInvalidArguments`；NotFound 原样向上抛。
