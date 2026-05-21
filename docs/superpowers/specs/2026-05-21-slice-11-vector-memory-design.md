# Slice 11 — Vector Memory Design Spec

> Status: Draft → Approved (2026-05-21)
> Author: hanbing
> Related: Slice 7（memory 骨架）、Slice 3（model gateway / embeddings）、ADR-53（pgvector 推后）

## 1. 概述

Slice 7 把记忆系统跑通了，但 ADR-53 显式把 pgvector 推到后续。本切片闭环 spec §7 中关于"语义检索 + 去重"的最小可用集：

- DB 增加 `embedding vector(1536)` 列 + ivfflat partial cosine 索引
- `memory.Create` 同步走 Gateway 算 embedding；命中 0.92 cosine 阈值则不写新行，touch 既有行并返回原 id
- `memory.Search` 默认走 cosine 排序，`mode=keyword` 显式退回 ILIKE
- mock provider 的 `/v1/embeddings` 返回 deterministic 1536-d 单位向量（同 input 同向量）
- 不引入 confidence / 衰减 / Reflection / 自动注入；保留给后续切片

**与 spec §7 / Slice 7 差距推进**：
| 项 | Slice 7 | Slice 11 |
|---|---|---|
| 向量索引 | 无 | `embedding vector(1536)` + ivfflat partial cosine |
| 检索 | ILIKE + tags + type | default vector + explicit keyword + auto fallback |
| 去重 | 无 | Create 前 cosine top-1，>= 0.92 → touch 既有 |
| Embedding 通道 | 闲置 | Create / Search 同步调 Gateway.Embeddings |
| Confidence / 衰减 | 推后 | 仍推后到 Reflection 切片 |

## 2. 前置条件

依赖 Slice 7（memory 骨架）+ Slice 3（modelgw embeddings 通道）+ Slice 10（observability metrics 收集 model.embed 子 span / `pca_model_calls_total{kind="embed"}`）已完工。

## 3. 关键决策（ADR）

### ADR-58 — Embedding 维度硬编码 1536
迁移文件、`memory.EmbeddingDim` 常量、mock provider 三处必须保持一致。理由：
- 1536 是 OpenAI ada-002 / text-embedding-3-small 的本征维度，工业默认值
- 切到不同维度（e.g. bge-base 768）需要新 migration + 数据重建；不通过运行时配置切换
- 不引入"运行时维度校验外可配"的复杂度

### ADR-59 — ivfflat lists=100 + partial WHERE embedding IS NOT NULL
理由：
- ivfflat 在 100 lists 下适配 ~10K 行；当前流量远低于此但保留头部余量
- 老 slice-7 行没有 embedding；partial 索引让 vector 查询自然跳过它们
- 真实需求出现时可加 `SET ivfflat.probes` 或重建 lists

非目标：HNSW（pgvector 0.5+ 才有，社区基准在 < 10K 行时与 ivfflat 差距不显著）

### ADR-60 — Search default vector，`mode=keyword` 显式退回
理由：
- 大部分检索目的是"找语义相近的记忆"，关键字模式偶尔够用但不是 default
- `mode=keyword` 暴露给前端 / 测试做 A/B 对照
- query 为空时 vector 没有意义，自动退回 keyword（保留 type/tag-only 过滤场景）

### ADR-61 — Create 0.92 dedup hit → touch + 返回原 id（200，不是 201）
理由：
- 真实用户和 Agent 都会反复触发 `memory.save`，dedup 必须自动
- 阈值 0.92 是 OpenAI ada-002 文档与社区共识的"语义近似"水位
- 命中返回 200 + 原 memory，新建返回 201；调用方靠状态码区分（`memory.save` 工具暴露 `created` bool 字段）
- 不引入 `merged: true` JSON 字段，OpenAPI body schema 不变

### ADR-62 — Embedder 同步、失败拒绝；不静默落库无 embedding 行
理由：
- 异步落库会出现"看似入库但 vector search 不可见"的隐形分裂
- 同步成本（mock 1ms / prod 100-300ms）记忆是低频写，可接受
- 失败要么是 embedder 配置错（生产事故，必须 5xx 上报）要么瞬时网络（业务可重试）
- 兜底：`memory.embed_on_write=false` 整片功能降级；Create 不算 embedding，Search 永远 keyword

### ADR-63 — Mock embedding 用 sha256+L2-normalize 出 deterministic 1536-d
理由：
- 测试需要"同 input 同向量"（dedup 测试可重复）
- 测试需要"不同 input 不同向量"（vector ranking 测试有意义）
- L2-normalize 后 cosine = 点积，便于人工算预期分
- 不引入真模型依赖

## 4. Schema 变更

`internal/db/migrations/0010_memories_embedding.up.sql`：

```sql
CREATE EXTENSION IF NOT EXISTS vector;
ALTER TABLE memories ADD COLUMN embedding vector(1536);
CREATE INDEX memories_embedding_idx
  ON memories USING ivfflat (embedding vector_cosine_ops)
  WITH (lists = 100)
  WHERE embedding IS NOT NULL;
```

down.sql：drop index、drop column。extension 不 drop（其他切片可能依赖）。

## 5. 接口

### Embedder 抽象

```go
// internal/memory/embedder.go
const EmbeddingDim = 1536

type Embedder interface {
    Embed(ctx context.Context, inputs []string) ([][]float32, error)
    Dim() int
}

type GatewayEmbedder struct{ /* gw, model */ }
// Embed 从 auth.FromCtx(ctx) 解 tenant/user → Gateway.Embeddings
```

### Repo

新增：
- `Insert(ctx, m, embedding []float32) (*Memory, error)` — 第三参 nil 时写 NULL
- `Update(ctx, t, u, id, req, embedding []float32) (*Memory, error)` — nil 表示不变
- `SearchVector(ctx, t, u, qVec, req) ([]SearchResult, error)` — `<=>` cosine 排序，触发 touch
- `FindSimilar(ctx, t, u, qVec, threshold) (*Memory, score, error)` — top-1 + 阈值过滤
- `TouchLastUsed(ctx, t, u, id)` helper
- 原 `Search` 重命名为 `SearchKeyword`，返回类型 `[]SearchResult`（保留 keyword path 的统一返回类型，score = 0 + `omitempty`）

### Service

```go
NewService(repo *Repo, embedder Embedder, cfg MemoryConfig) *Service

// Create 路径
// 1. validate
// 2. if vectorEnabled: embed + FindSimilar(threshold) → touch & 返回 existing
// 3. miss: Insert with embedding
// 返回 *CreateResult{Memory, Created bool}

// Update 路径：Content 变化 + vectorEnabled → 重算 embedding；不 dedup

// Search 路径
// req.Mode in {"", "vector", "keyword"}
// "" + query="" → keyword (filter-only)
// "" + query≠"" + vectorEnabled → vector
// "vector" + (query="" || !vectorEnabled) → error
// "keyword" → keyword
```

### REST

`/memories` POST 返回值 schema 不变；状态码 200（dedup）vs 201（新建）。

`/memories/search`（沿用 Slice 7）request body 新增可选 `mode` 字段。

### MCP 工具

- `memory.save` 响应 `{"id", "created"}` — `created` 是 bool（false 表示 dedup hit）
- `memory.search` request schema 新增 `mode: ["vector", "keyword"]`，响应增 `score: float`（仅 vector path 非零）

## 6. 数据流

```
POST /memories
  → Handler → Service.Create
                ├── validate
                ├── if vectorEnabled:
                │     embedder.Embed([content])
                │     Repo.FindSimilar(qVec, 0.92)
                │       hit → Repo.TouchLastUsed → return existing (200)
                │       miss → fall through
                └── Repo.Insert(m, vec)  (201)

POST /memories/search  /  memory.search tool
  → Service.Search resolve mode
      ├── vector: embedder.Embed([query]) + Repo.SearchVector
      │     SELECT ..., 1 - (embedding <=> $vec) AS score
      │     FROM memories
      │     WHERE tenant_id=$ AND owner_user_id=$ AND embedding IS NOT NULL
      │           [AND type=$] [AND tags && $]
      │     ORDER BY embedding <=> $vec ASC
      │     LIMIT $
      │     // touch last_used_at on returned ids
      └── keyword: Repo.SearchKeyword (legacy ILIKE)
```

## 7. 不变量

1. **同步 embed** — Create 与 vector Search 必须等 embedder 返回；失败拒绝（5xx）
2. **partial 索引保护** — Repo.SearchVector 的 WHERE 显式加 `embedding IS NOT NULL`（双保险）
3. **租户 + 所有人隔离** — 所有新 query 仍 `WHERE tenant_id=$ AND owner_user_id=$`
4. **DimensionMismatch fail-loud** — 切片长度 != 1536 直接 5xx，不静默 Insert
5. **dedup 不破坏 caller 语义** — 命中返回 200 + 原 memory（不是 201）
6. **mock embedding deterministic** — 同 input 同向量；否则 dedup 测试 flaky
7. **score 仅 vector path** — keyword 返回 score=0 + JSON `omitempty`
8. **配置兜底** — `embed_on_write=false` 整片功能降级（运维 kill switch）
9. **不动 audit / metrics 表面** — 沿用 `pca_model_calls_total{kind="embed"}` 与 `tool.invoke.error`

## 8. 配置

```yaml
memory:
  embedding_model: "default-mock:text"  # provider:model
  dedup_threshold: 0.92                 # 0 disables dedup
  embed_on_write: true                  # ops kill switch
```

env: `PCA_MEMORY_EMBEDDING_MODEL` / `PCA_MEMORY_DEDUP_THRESHOLD` / `PCA_MEMORY_EMBED_ON_WRITE`

## 9. 测试

### 单测
- `embedder_test.go`：fakeEmbedder + GatewayEmbedder 维度校验
- `repo_test.go`（dockertest, pgvector/pgvector:pg16）：FindSimilar / SearchVector / TouchLastUsed 覆盖
- `service_test.go`：mode 路由、dedup hit/miss、Update 重算

### E2E（39 步）
- [36/39] vector ranking：两条相似 memory，vector mode 检索 score 排序正确
- [37/39] keyword mode 走 ILIKE 命中
- [38/39] Create 同内容第二次 → 返回与第一次相同 id（dedup）
- [39/39] Create 显著不同内容 → 新 id（无误并）

## 10. 风险与折衷

1. **pgvector 镜像 vs alpine** — +130MB，可接受
2. **同步 embed 拖慢 Create P95** — mock 1ms，prod 100-300ms；记忆低频写
3. **ivfflat 在 <100 行场景召回偏低** — 测试用精确比对 + 阈值，不依赖召回率；prod 真实流量自然填充。如果 e2e flaky，`SET ivfflat.probes = 100` 兜底
4. **跨 provider embedding 不可比** — 换 model = 清表；文档强调，不做迁移工具
5. **历史无 embedding 行被 vector search 跳过** — partial 索引 by design；admin re-embed 留给 Reflection 切片
6. **Update 不去重** — 显式覆盖语义；与 Create 的语义差需要 README 说明

## 11. 推迟（明确出栈）

- Confidence / 衰减 / 重排 → Reflection 切片
- Project / Tenant scope memory → 另起切片
- 会话起始自动注入（session WS start hook）→ 另起切片
- Reflection Agent 异步 worker → 另起切片
- Hybrid（vector + keyword RRF）→ 本切片 mode 互斥
- 历史 backfill / re-embed admin endpoint → 留到 Reflection 切片
- 维度可配 / chunking 长文本 → 不在本切片范围
