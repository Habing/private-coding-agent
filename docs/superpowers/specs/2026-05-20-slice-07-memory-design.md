# Slice 7 — Memory (basic) Design Spec

> Status: Draft → Approved (2026-05-20)
> Author: hanbing
> Related: spec §7（记忆子系统），slice 6（session 持久化）

## 1. 概述

切片 7 实现 spec §7 中描绘的"记忆子系统"的最小骨架：让用户和 Agent 能持久地写入、读出、检索结构化记忆条目。

**这一刀只做"被动存取层"**：

- 一张表 `memories`，行级 multi-tenant 隔离
- REST CRUD（`/memories`）供前端/手工管理
- 内部 MCP 工具 `memory.save / memory.search / memory.list / memory.delete`，使 Agent 在 ReAct 循环中可以显式读写
- 搜索：关键字 ILIKE + 标签 `&&` 数组重叠 + 类型过滤；不引入 pgvector，不算 embedding
- Scope：仅 User scope（按 `tenant_id + owner_user_id` 隔离）；Project / Tenant scope 推后
- 不做 session-start 自动注入、不做 Reflection Agent

**与 spec §7 的差距（明示推迟）**：
| spec §7 项 | 本切片状态 |
|---|---|
| 三层 Working/User/Project/Tenant | 仅 User；Working 由 Redis + session 自然承担；Project/Tenant 推后到 slice 9 |
| 四种类型 profile/preference/knowledge/lesson | 表里有 `type` 列保留四种值，但本切片不强制业务区分 |
| pgvector 向量相似度、0.92 去重、confidence 衰减 | 全部推后 |
| 自动注入会话上下文 | 不做；Agent 通过工具显式查 |
| Reflection Agent 异步提议 | 不做 |
| UI 隐私控制（导出/一键清空） | 不在本切片，但 DELETE /memories 已经具备"一键清空 by tag/type"的能力 |

## 2. 前置条件

依赖 Slice 1.5 / 2 / 3 / 4 / 5 / 6 已完工（HEAD = `0360877` "fix(e2e): route websocat via compose network"）。

Slice 4 (Tool Bus) 是核心复用点：内部 MCP 工具的 `Tool` 接口、`tools.Registry`、`Bus.Invoke` 在本切片完全沿用。

## 3. 核心需求

### 3.1 数据持久化
- 一条记忆行包含：`id, tenant_id, owner_user_id, type, content, tags, source, source_msg_id, last_used_at, created_at, updated_at`
- `type` 枚举：`profile | preference | knowledge | lesson`，DB 层用 CHECK 约束
- `tags TEXT[]`：自由打标，GIN 索引支持快速 `&&` 重叠查询
- `content TEXT`：自由文本（短句最佳，本切片不限长）
- `source TEXT`：手填 / chat / agent / reflection（保留位，本切片只 user-set "user" 或 "agent"）
- `source_msg_id UUID NULL`：可选关联到 `messages.id`，无 FK（messages 可能跨会话归档）
- `last_used_at TIMESTAMPTZ`：每次 Search 命中后 touch；用于将来 LRU 归档

### 3.2 隔离与安全
- 所有读路径都带 `WHERE tenant_id=$1 AND owner_user_id=$2`
- 跨租户 / 跨 owner 访问返 `ErrMemoryNotFound`（404），不区分"不存在"与"无权"
- Memory ID 在 update / delete 路径里也按 `(tenant, owner, id)` 三元组过滤

### 3.3 REST 表面
| Method | Path | 说明 |
|---|---|---|
| POST | /memories | 创建一条 |
| GET | /memories | 列出当前用户的全部，可按 `?type=&tag=&q=` 过滤 |
| GET | /memories/{id} | 单条详情 |
| PUT | /memories/{id} | 更新 content / tags / type |
| DELETE | /memories/{id} | 删除 |

### 3.4 内部 MCP 工具
| Name | Input | Output |
|---|---|---|
| `memory.save` | `{type, content, tags?, source?, source_msg_id?}` | `{id}` |
| `memory.search` | `{query?, type?, tags?, limit?}` | `{items: [{id,type,content,tags,confidence,last_used_at}]}` |
| `memory.list` | `{type?, tags?, limit?, offset?}` | `{items: [...], total}` |
| `memory.delete` | `{id}` | `{ok: true}` |

工具调用上下文的 `tenant_id + user_id` 来自 `toolbus.Invoke` 的 ctx claims（已有机制）。

### 3.5 失败模式
- 输入校验失败 → `validation:` 前缀 error，handler 映射 400
- 跨租户 / 找不到 → `ErrMemoryNotFound` → 404
- 数据库错误 → 500 "internal"

## 4. 整体架构

```
HTTP REST                Tool Bus
  /memories                memory.{save,search,list,delete}
        \                          /
         v                        v
        memory.Service (CRUD + Search)
                 |
                 v
            memory.Repo (pgxpool)
                 |
                 v
            PostgreSQL.memories
```

**依赖方向**：
- `internal/memory` 仅 import `auth`、`pgx`、`uuid`；**不 import `session` / `agent`**
- `internal/toolbus/tools/memory_*.go` 是 Tool Bus 的适配层，import `internal/memory`
- main.go 装配：注入 `*memory.Service` 给 handler 和 4 个 tool

复用：
- repo 模板 = `internal/session/repo.go`
- handler 模板 = `internal/session/handler.go`
- 内部 MCP 工具模板 = `internal/toolbus/tools/llm_*.go`（slice 4）

## 5. 接口与数据模型

### 5.1 数据库 (migration 0009)

```sql
CREATE TABLE memories (
  id UUID PRIMARY KEY,
  tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  owner_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  type TEXT NOT NULL CHECK (type IN ('profile','preference','knowledge','lesson')),
  content TEXT NOT NULL,
  tags TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
  source TEXT NOT NULL DEFAULT 'user',
  source_msg_id UUID,
  last_used_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX memories_tenant_owner_idx ON memories(tenant_id, owner_user_id, created_at DESC);
CREATE INDEX memories_tags_idx ON memories USING GIN (tags);
-- content trigram 索引仅在 pg_trgm 可用时启用（postgres:16-alpine 默认不带）。
-- 切片 7 用 ILIKE %q% 即可，规模上来后再加 pg_trgm。
```

### 5.2 Go 类型 (internal/memory/types.go)

```go
type Memory struct {
    ID           uuid.UUID  `json:"id"`
    TenantID     uuid.UUID  `json:"tenant_id"`
    OwnerUserID  uuid.UUID  `json:"owner_user_id"`
    Type         string     `json:"type"`
    Content      string     `json:"content"`
    Tags         []string   `json:"tags"`
    Source       string     `json:"source"`
    SourceMsgID  *uuid.UUID `json:"source_msg_id,omitempty"`
    LastUsedAt   time.Time  `json:"last_used_at"`
    CreatedAt    time.Time  `json:"created_at"`
    UpdatedAt    time.Time  `json:"updated_at"`
}

type CreateRequest struct {
    Type        string     `json:"type"`
    Content     string     `json:"content"`
    Tags        []string   `json:"tags,omitempty"`
    Source      string     `json:"source,omitempty"`
    SourceMsgID *uuid.UUID `json:"source_msg_id,omitempty"`
}

type UpdateRequest struct {
    Type    *string  `json:"type,omitempty"`
    Content *string  `json:"content,omitempty"`
    Tags    []string `json:"tags,omitempty"`  // nil = 不动；空数组 = 清空
}

type SearchRequest struct {
    Query string   `json:"query,omitempty"`  // ILIKE %query%
    Type  string   `json:"type,omitempty"`   // 等值
    Tags  []string `json:"tags,omitempty"`   // && 重叠（任一命中）
    Limit int      `json:"limit,omitempty"`  // 默认 20，上限 100
}

const (
    TypeProfile    = "profile"
    TypePreference = "preference"
    TypeKnowledge  = "knowledge"
    TypeLesson     = "lesson"
)
```

### 5.3 错误哨兵

```go
var (
    ErrMemoryNotFound = errors.New("memory not found")
    ErrEmptyContent   = errors.New("content required")
    ErrInvalidType    = errors.New("invalid memory type")
)
```

### 5.4 Service 接口

```go
type Service struct { repo *Repo }

func (s *Service) Create(ctx, tid, uid, req CreateRequest) (*Memory, error)
func (s *Service) Get(ctx, tid, uid, id) (*Memory, error)
func (s *Service) List(ctx, tid, uid, filter ListFilter) ([]Memory, error)
func (s *Service) Update(ctx, tid, uid, id, req UpdateRequest) (*Memory, error)
func (s *Service) Delete(ctx, tid, uid, id) error
func (s *Service) Search(ctx, tid, uid, req SearchRequest) ([]Memory, error)
```

Search 命中后由 Repo 在同一 SQL 里 `UPDATE memories SET last_used_at=now() WHERE id = ANY($ids) AND tenant_id=$x AND owner_user_id=$y`，无单独事务。

### 5.5 内部 MCP 工具

每个工具实现 `toolbus.Tool` 接口 (`Name() / Schema() / Invoke(ctx, args)`)。`Invoke` 内部从 ctx 取 claims 拿 tenant + user。

| Tool name | input.JSONSchema | 行为 |
|---|---|---|
| `memory.save` | type required, content required, tags?, source?, source_msg_id? | 调 `Service.Create` |
| `memory.search` | query? OR type? OR tags?（至少一个），limit (≤50) | 调 `Service.Search` |
| `memory.list` | type?, tags?, limit (≤50), offset | 调 `Service.List` |
| `memory.delete` | id required | 调 `Service.Delete`，成功 `{ok:true}` |

## 6. 数据流

### 6.1 REST 创建
```
client → POST /memories {type,content,tags}
       → handler.create → claims (tid,uid)
       → svc.Create(ctx, tid, uid, req)
       → repo.Insert → SELECT round-trip
       → 201 {memory}
```

### 6.2 Agent 调 memory.search
```
agent.Engine yield → assistant_message (tool_calls=[memory.search])
                  → engine 路由到 toolbus.Bus.Invoke
                  → memorySearchTool.Invoke(ctx, args)
                       (ctx 携带 auth claims)
                  → svc.Search(ctx, tid, uid, req)
                  → repo.Search (ILIKE + tag && + type=)
                  → repo.touchLastUsed(ids...)
                  → return {items: [...]}
                  → engine yield → tool_result event
                  → 落到 messages 表（slice 6 已自动处理）
```

### 6.3 跨租户访问
```
userA 持 token, GET /memories/{id-belongs-to-userB}
  → handler.get → claims (tidA, uidA)
  → svc.Get(ctx, tidA, uidA, id)
  → repo.Get → SELECT ... WHERE id=$1 AND tenant_id=$2 AND owner_user_id=$3
  → pgx.ErrNoRows
  → ErrMemoryNotFound
  → 404 not_found
```

## 7. 测试策略

### 7.1 单元（不依赖 DB）
- `errors_test.go`：哨兵非空
- handler_test.go：mock service，覆盖 200/201/400/404/401 五种状态码 × 每个端点

### 7.2 集成（dockertest postgres:16）
- `repo_test.go`：
  - Create / Get / List / Update / Delete happy path
  - GetNotFound 跨 tenant / 跨 owner
  - List filter by type / tag
  - Search ILIKE / Search tags && / Search type / Search 组合 / Search empty params 报错
  - Search 命中后 last_used_at 更新
  - Update partial（仅改 content；type 不变）
  - Delete returns ErrMemoryNotFound when 0 rows
- `service_test.go`：用真 repo + dockertest，覆盖 Service 层的 validation 与跨租户隔离

### 7.3 Tool Bus 集成
- `internal/toolbus/tools/memory_*_test.go`：用真 *memory.Service + dockertest
- 验证 `memory.save` → `memory.search` round-trip：保存后通过 query 找回
- 验证跨租户：toolA 的 ctx 拿不到 toolB 写的记忆

### 7.4 E2E（compose）
- [22/25] POST /memories 写入两条不同 type
- [23/25] GET /memories?type=preference&tag=go 过滤
- [24/25] memory.save via tool → memory.search via tool 找回
- [25/25] DELETE /memories/{id}，再 GET 返 404

## 8. Task 拆解（10 Task）

见 `docs/superpowers/plans/2026-05-20-slice-07-memory.md`（待 Task 9 产出）。

## 9. 验收清单

- [ ] migration 0009 up/down 在 dockertest 跑通
- [ ] `internal/memory/*` 单测 + 集成测全 PASS
- [ ] `internal/toolbus/tools/memory_*_test.go` round-trip 测过
- [ ] 跨租户 404 专项测过
- [ ] `go test ./...` 全绿；`go vet` / `go build` 干净
- [ ] E2E 25/25 在 compose 跑过
- [ ] README 切片 7 勾选，端点 + 工具表更新
- [ ] git tree clean，commit 按 Conventional Commits

## 10. 风险与开放问题

| 风险 | 影响 | 缓解 |
|---|---|---|
| `tags TEXT[]` 在 pgx v5 的扫描兼容性 | 中（pgx v5 默认支持 `pgtype.FlatArray`） | repo 测试覆盖；如出问题降级到 JSONB |
| ILIKE %q% 全表扫 | 低，本切片单 user 数据量小 | 推后到 pg_trgm + GIN，slice 9 |
| `type` 取值漂移 | 低 | DB CHECK + service-layer 白名单 |
| Memory ID 可被枚举 → 跨租户尝试 | 低 | scoped SELECT 已经 404；不暴露存在性 |
| 工具 `memory.search` 无 query 无 tag 无 type 返全表 | 中（结果太大） | 至少要求一个过滤条件；否则 400 |

**开放问题（推后到后续切片）**：
- Project memory：要先有 `projects` 表（slice 8/9）
- Tenant memory：写权限需要 admin 角色，需要 RBAC 加强
- pgvector：换镜像 + 写入 embeddings 调用 → 工作量评估在 slice 9
- 自动注入：会冲击 session.Service 的 SendMessage 路径，需要 token 预算管理

## 11. ADR 摘要

- **ADR-51**：单表 `memories`，type 列承载 4 种类别，不分表。理由：本切片量小；type 仅作过滤维度，未来加 polymorphism 也容易加 child table。
- **ADR-52**：仅做 User scope。Project/Tenant scope 推到 slice 9，避免 RBAC 提前膨胀。
- **ADR-53**：搜索用 ILIKE + tag `&&`，不上 pgvector。理由：保持 "basic" 边界；切换到 pgvector 时 repo.Search 是单点替换。
- **ADR-54**：内部 MCP 工具 + REST 双暴露。Agent 走工具是核心路径，REST 是 UI / 手工管理。
- **ADR-55**：不自动注入 / 不做 Reflection。理由：避免与 session.Service 耦合；显式调用边界清晰。
- **ADR-56**：每次 search 命中后 touch `last_used_at`。理由：将来 LRU 归档需要这个时间戳，越早积累越好。
- **ADR-57**：搜索至少需要一个过滤条件（query / type / tags 之一非空），否则 400。理由：避免无意义全表扫描。

## 12. 与 Spec 主文档对齐

| Spec §7 节 | 本切片落实 |
|---|---|
| 7.1 三层记忆 | 仅 User scope；其他推后 |
| 7.2 四种条目类型 | 表 schema 落实，业务区分由用户/agent 自行使用 |
| 7.3 学习信号 | 不在本切片（无 Reflection） |
| 7.4 读写策略 | 仅"运行时按需"通过 memory.search；不做会话起始注入 |
| 7.5 质量保证 | 不在本切片（无相似度合并、无 confidence 衰减） |
| 7.6 隐私与控制 | DELETE /memories/{id} + 列表过滤具备基本能力；UI 在 slice 8 |
| 7.7 Memory 作为内部 MCP | 完整落实 4 个工具 |
