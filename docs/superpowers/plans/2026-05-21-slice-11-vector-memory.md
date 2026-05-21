# Slice 11 — Vector Memory (pgvector + cosine search + 0.92 dedup)

## Context

切片 7 把记忆系统跑通了，但 ADR-53 显式把 pgvector 推到后续：当前 `memory.search` 只能 ILIKE 关键字 + tag 过滤 + type 过滤。三个真实痛点：

1. **零语义召回** —— "user prefers Go" 和 "loves golang" 是同一回事，关键字检索一个都搜不出另一个。
2. **重复条目无法发现** —— Create 不去重，agent 反复触发 `memory.save` 时 DB 里出现近似副本。
3. **embedding 信道闲置** —— `/v1/embeddings` 与 `llm.embed` 工具已就绪，但唯一调用方是用户自己写代码；记忆系统是它最自然的客户。

本切片闭环：往 `memories` 表加 `embedding vector(1536)` 列 + ivfflat 索引；`memory.Create` 同步走 Gateway 算 embedding 并落库；`memory.Search` 默认走 cosine（`<=>`）并按相似度排序；Create 前先做 0.92 cosine 相似度查询，命中则不写新行、推进既有行的 `last_used_at` 并返回旧 id。

**用户已确认本切片边界**：
1. Embedding 维度：**固定 1536**（迁移文件硬编码），mock provider 同步改为返回 1536 维向量
2. Search 策略：**默认 vector，`mode=keyword` 显式退回 ILIKE**；query 为空时自动退回 keyword
3. Create 去重：**包含**，阈值可配（`memory.dedup_threshold`，默认 0.92），命中走 touch 而非 insert
4. **不进 confidence 列**，不做衰减；这些 hook 留给 Reflection 切片

## Goal

- migration 0010：`CREATE EXTENSION IF NOT EXISTS vector;` + `ALTER TABLE memories ADD COLUMN embedding vector(1536);` + `CREATE INDEX ... USING ivfflat (embedding vector_cosine_ops) WITH (lists=100) WHERE embedding IS NOT NULL;`
- `internal/memory/embedder.go` 抽出 `Embedder` 接口（`Embed(ctx, []string) ([][]float32, error)` + `Dim() int`），生产实现包 `modelgw.Gateway`；测试用 stub。包级常量 `EmbeddingDim = 1536`
- `Service` 构造改为 `NewService(repo, embedder, cfg)`，`cfg` 含 `EmbeddingModel string`、`DedupThreshold float64`、`EmbedOnWrite bool`（feature flag 兜底）
- `Service.Create` 路径：算 embedding → cosine top-1 比对 → 命中阈值则 touch + 返回；未命中则 `Repo.Insert(... embedding)`
- `Service.Search` 新增 `Mode` 字段（`""` / `"vector"` / `"keyword"`），默认 vector；query 空 → 自动退回 keyword
- `Repo` 新增 `SearchVector(ctx, tenantID, ownerUserID, qVec, filter, limit)` 用 `ORDER BY embedding <=> $vec` + `LIMIT`，外加 type/tag 过滤；命中后同样 touch last_used_at
- `Repo` 新增 `FindSimilar(ctx, tenantID, ownerUserID, qVec, threshold) (*Memory, error)` 返回 top-1 且 `1 - distance >= threshold`，否则 `ErrMemoryNotFound`
- mock provider 的 `/v1/embeddings` 改为：根据 input 文本做 deterministic 1536-d 向量（**不能是常量** — 否则相似度永远 1.0，无法测排序）
- dockertest pool 镜像 `postgres:16` → `pgvector/pgvector:pg16`（10 个 \_test.go）
- compose `postgres:16-alpine` → `pgvector/pgvector:pg16`（数据卷兼容，因为 pgvector 镜像就是 from postgres:16）
- e2e 35 → 39 步：vector search 命中、keyword mode 退回、Create 触发去重命中既有 id、新建 + search 后返回的 score 字段大于阈值
- 新增"记忆系统"小节到 README 说明 vector / keyword / dedup 行为
- 归档 plan 到 `docs/superpowers/plans/2026-05-21-slice-11-vector-memory.md`

## 非目标（明确出栈）

- **Confidence / 衰减 / 重排** —— 推到 Reflection 切片
- **Project / Tenant scope memory** —— 仍只 User scope；另起切片
- **会话起始自动注入** —— 触及 session WS handler，另起切片
- **Reflection Agent** —— 异步 worker，另起切片
- **Hybrid (vector + keyword RRF)** —— mode=vector 与 mode=keyword 互斥；不引入融合排序
- **Backfill 历史无 embedding 行** —— slice 7 demo 数据丢失检索可见性，靠 ivfflat WHERE 过滤掉无 embedding 行；不实现一次性回填
- **维度可配** —— 1536 硬编码；改维度走新 migration
- **生产 embedding model 选型 / 计费** —— 不在本切片
- **chunking / 长文本切片 embedding** —— 假设 memory.content < embedding token 上限（OpenAI ada-002 8K token；记忆条目通常一行字）

## Architecture

```
POST /memories  /  memory.save (tool)
  Handler -> Service.Create
              1. validate (existing)
              2. embedder.Embed([content])            <- new
              3. Repo.FindSimilar(qVec, threshold)    <- new
                 hit  ->  Repo.TouchLastUsed(id)     <- new (light helper)
                          return existing memory
                 miss ->  Repo.Insert(m{...,embedding=qVec})  <- column added

POST /memories/search via Service.Search
                       /  GET /memories?q=... (ListFilter, unchanged)
                       /  memory.search (tool)
  mode resolution:
    explicit "vector"  or  (mode="" && query!="" && embedder available) -> vector
    explicit "keyword" or  (mode="" && query=="")                       -> keyword
  vector path:
    embedder.Embed([query])
    Repo.SearchVector(qVec, filter, limit)
       SELECT ..., 1 - (embedding <=> $vec) AS score
       FROM memories
       WHERE tenant_id=$1 AND owner_user_id=$2 AND embedding IS NOT NULL
             [AND type=$3] [AND tags && $4]
       ORDER BY embedding <=> $vec  ASC
       LIMIT $N
    touch last_used_at on the returned ids
    return []SearchResult{Memory, Score float64}
  keyword path:
    same as today, score = 0 (omitted from JSON)

POST /v1/embeddings (mock provider)
  before: constant [0.1, 0.2, 0.3]
  after:  deterministic 1536-d vector from sha256(input)
          first 1536 bytes mapped to floats in [-1,1], then L2-normalized
          -> different inputs -> different vectors -> meaningful cosine
```

## 关键不变量

1. **embedding 同步** — Create 与 Search 必须等 embedder 返回；超时直接给上游报错（不静默落库无 embedding）。允许的兜底：embedder 失败 → Create **拒绝**（5xx）。理由：避免"看似入库但不参与 vector search"的隐形分裂。
2. **ivfflat partial index 过滤掉无 embedding 行** — `WHERE embedding IS NOT NULL`；Repo.SearchVector 的 WHERE 显式加 `embedding IS NOT NULL` 双保险。
3. **租户 + 所有人隔离不变** — 所有新 query 仍包含 `tenant_id=$ AND owner_user_id=$`；vector 检索不存在跨租户泄漏窗口。
4. **DimensionMismatch fail-loud** — Service 启动期断言 `embedder.Dim() == EmbeddingDim`；运行期若返回切片长度 != 1536，Insert 不写、直接 5xx。
5. **dedup 不破坏 caller 语义** — Create 命中阈值时返回 200 + 原 memory（不是 201），调用方靠 id 一致性即可识别；不引入 `merged: true` 字段（保持 OpenAPI 不变，文档说明）。
6. **mock embedding deterministic** — 同样 input 多次调用必须返回完全相同的向量；否则去重测试会 flaky。
7. **score 仅在 vector path 出现** — keyword 路径返回的 SearchResult 不带 score（或 score=0 + `omitempty`）。前端不读 score；这是后端契约。
8. **配置覆盖路径** — `embedding_model` / `dedup_threshold` / `embed_on_write` 都在 `MemoryConfig`，env: `PCA_MEMORY_*`。`embed_on_write=false` 时整片功能降级（Create 不算 embedding、Search 永远走 keyword），用于运维兜底而非测试默认。
9. **不动 audit / metrics 接口** — 不新增专用指标（已有的 `pca_model_calls_total{kind="embed"}` 足够），不新增 audit action（沿用 `tool.invoke.error`）。

## Tech Stack

复用：
- `internal/modelgw/gateway.go` `Gateway.Embeddings`
- `internal/memory/repo.go` `Repo.Insert` / `Repo.Search` 模板
- `internal/db/migrate.go` golang-migrate runner
- `github.com/google/uuid`
- dockertest 模板（10 处 repo_test.go）

新增依赖：
- `github.com/pgvector/pgvector-go`（pgvector 的 pgx 编/解码器；版本 v0.2.x）
  - 提供 `pgvector.Vector` 类型 + 通过 pgx 自动注册 codec
  - 用法：`pgvector.NewVector(vec []float32)`、`vec.Slice() []float32`

注意：pgx v5 codec 注册需在 pool 创建后通过 `AfterConnect` hook 调一次（详见 pgvector-go README）；落点放在 `internal/db/pool.go`。

## 工作分解

### Task 0 — design spec

**Files:** Create `docs/superpowers/specs/2026-05-21-slice-11-vector-memory-design.md`

按现有 spec 模板：概述 / ADR / schema / 接口 / 数据流 / 测试 / 风险。重点 ADR：
- ADR-58 维度固定 1536
- ADR-59 ivfflat lists=100 cosine_ops + partial index
- ADR-60 default vector mode、`mode=keyword` 显式退回
- ADR-61 Create 0.92 dedup → touch 既有行返回原 id
- ADR-62 embedder 同步、失败直接报错不静默落库
- ADR-63 mock embedding deterministic 1536-d（hash → [-1,1] → L2 normalize）

### Task 1 — DB migration + pgvector pool codec

**Files:**
- Create `internal/db/migrations/0010_memories_embedding.up.sql`
- Create `internal/db/migrations/0010_memories_embedding.down.sql`
- Modify `internal/db/pool.go`（pgx codec 注册）

up.sql：
```sql
CREATE EXTENSION IF NOT EXISTS vector;
ALTER TABLE memories ADD COLUMN embedding vector(1536);
CREATE INDEX memories_embedding_idx
  ON memories USING ivfflat (embedding vector_cosine_ops)
  WITH (lists=100)
  WHERE embedding IS NOT NULL;
```

down.sql：drop index、drop column。extension 不 drop（其他切片可能依赖）。

pool.go：`pgxpool.NewWithConfig` 后用 `AfterConnect` hook 调 `pgvector.RegisterTypes(ctx, conn)`。

### Task 2 — Embedder 接口 + 生产实现

**Files:** Create `internal/memory/embedder.go` + `embedder_test.go`

接口：
```go
const EmbeddingDim = 1536
type Embedder interface {
    Embed(ctx context.Context, inputs []string) ([][]float32, error)
    Dim() int
}
```

生产实现 `GatewayEmbedder` 包 `*modelgw.Gateway` + `model string`。Gateway.Embeddings 需要 tenant/user，从 ctx 拿 `auth.Claims`（已有 `auth.FromCtx`）；Create/Search 路径上一定有用户上下文；测试场景 ctx 可注入。

### Task 3 — Repo 扩展

**Files:** Modify `internal/memory/repo.go` + `repo_test.go`

新增方法：
- `Insert` 改签名：把 embedding 作为字段写入；调用方传 `pgvector.NewVector(vec)`，nil 时插入 NULL
- `FindSimilar(ctx, tenant, owner, qVec, threshold) (*Memory, error)`
  - `SELECT ..., 1 - (embedding <=> $vec) AS score WHERE ... ORDER BY embedding <=> $vec ASC LIMIT 1`
  - score < threshold → `ErrMemoryNotFound`
- `SearchVector(ctx, tenant, owner, qVec, filter SearchRequest, limit int) ([]SearchResult, error)`
  - 走 cosine 排序 + filter + touch last_used_at
- `TouchLastUsed(ctx, tenant, owner, id)` 单独小 helper

现有 `Repo.Search` 改名为 `SearchKeyword` 让 Service 路由更清楚。

新增类型：
```go
type SearchResult struct {
    Memory
    Score float64 `json:"score,omitempty"` // cosine sim, only set on vector path
}
```

### Task 4 — Service 改造

**Files:** Modify `internal/memory/service.go` + `service_test.go`

- `NewService(repo, embedder, cfg MemoryConfig)`
- `Create` 路径：
  1. validate（已有）
  2. `vec, err := embedder.Embed(ctx, []string{content})`
  3. `existing, err := repo.FindSimilar(ctx, t, u, vec, cfg.DedupThreshold)`
     - 命中 → `repo.TouchLastUsed`，return existing
     - miss → `repo.Insert(m with embedding=vec)`
- `Search` 路径：
  - 新 `SearchRequest.Mode string`
  - 解析模式（vector default、keyword 显式、query="" 自动 keyword）
  - vector → `embedder.Embed([query])` → `repo.SearchVector`
  - keyword → `repo.SearchKeyword`（原 Search）
- `Update` 改 content 时同步重算 embedding（不去重 — Update 是显式覆盖，不该被静默并掉）

types.go：
- `SearchRequest` 加 `Mode string \`json:"mode,omitempty"\``
- `Memory` 不暴露 embedding 字段（json:"-"）
- 新 `MemoryConfig` 移到 types.go

### Task 5 — Handler / tool 透传

**Files:** Modify `internal/memory/handler.go` + `handler_test.go`、`internal/toolbus/tools/memory.go` + `_test.go`

- handler：Create 响应改为命中去重时返 200，新建时返 201（区分语义；前端可不读，文档说明）
- memory.search tool：input schema 加 `mode` 可选字段

### Task 6 — mock provider deterministic embed

**Files:** Modify `internal/modelgw/mockserver/main.go`

```go
func embed(w http.ResponseWriter, r *http.Request) {
    var req struct { Model string; Input []string }
    _ = json.NewDecoder(r.Body).Decode(&req)
    data := make([]map[string]any, len(req.Input))
    for i, txt := range req.Input {
        data[i] = map[string]any{
            "index": i, "object": "embedding",
            "embedding": deterministicVec(txt, 1536),
        }
    }
    // ...
}

// deterministicVec uses sha256 to seed a PRNG that emits 1536 floats in [-1,1],
// then L2-normalizes the slice. Same input -> identical vector.
func deterministicVec(s string, dim int) []float32 { ... }
```

### Task 7 — dockertest + compose 镜像迁移

**Files:**
- 10 个 `*_test.go` 用 sed: `Repository: "postgres", Tag: "16"` → `Repository: "pgvector/pgvector", Tag: "pg16"`
- `deploy/compose/docker-compose.yml`: `image: postgres:16-alpine` → `image: pgvector/pgvector:pg16`
- pgvector/pgvector:pg16 是 from postgres:16（非 alpine），体积 ~360MB（vs alpine 230MB）；数据卷格式兼容

### Task 8 — config + main wiring

**Files:**
- `internal/config/config.go`：新增 `MemoryConfig{EmbeddingModel string; DedupThreshold float64; EmbedOnWrite bool}`
- `config/config.example.yaml`：加 `memory:` 段
- `cmd/server/main.go`：
  - 在 modelGateway 之后构造 `memory.Embedder`
  - `memory.NewService(repo, embedder, cfg.Memory)`

默认值：
- `EmbeddingModel`: `default-mock:text`（dev / compose）；prod 必改
- `DedupThreshold`: 0.92
- `EmbedOnWrite`: true

### Task 9 — E2E 35 → 39 步

**Files:** Modify `deploy/compose/test-e2e.sh`

- 全文 sed `/35]` → `/39]`
- 追加：
  - [36/39] 创建两条相似 memory（"loves go" + "user prefers Go"），search query="golang" mode=vector 期望返回两条且第一条 score >= 第二条
  - [37/39] search query="postgres" mode=keyword 走 ILIKE，期望命中 "uses postgres 16"
  - [38/39] 再 Create "loves go" 第二次 → 响应 id 与第一次相同（去重命中）
  - [39/39] Create 内容明显不同 → 返回新 id（确认未误并）

### Task 10 — README + plan archive

**Files:** Modify `README.md`、Create `docs/superpowers/plans/2026-05-21-slice-11-vector-memory.md`

- README "切片进度" 加切片 11
- 新增"记忆系统" 小节：列出三种检索路径、dedup 行为、维度硬编码与生产 model 选型注意
- plan 归档（拷贝本文件）

## 关键文件清单

**新增（5 个）：**
- `internal/db/migrations/0010_memories_embedding.up.sql` + `down.sql`
- `internal/memory/embedder.go` + `_test.go`
- `docs/superpowers/specs/2026-05-21-slice-11-vector-memory-design.md`
- `docs/superpowers/plans/2026-05-21-slice-11-vector-memory.md`

**修改（约 17 个）：**
- `internal/db/pool.go`（pgvector codec 注册）
- `internal/memory/{types,repo,service,handler}.go` + 各 `_test.go`
- `internal/toolbus/tools/memory.go` + `_test.go`
- `internal/modelgw/mockserver/main.go`
- `internal/config/config.go` + `config/config.example.yaml`
- `cmd/server/main.go`
- `deploy/compose/docker-compose.yml`
- `deploy/compose/test-e2e.sh`
- `README.md`
- 10 个 `*_test.go`（dockertest pool image swap，机械替换）

## 验证

```bash
go vet ./...
go test ./internal/memory/... ./internal/modelgw/... -count=1
go test ./... -count=1

cd internal/webui && npm test -- --run && npm run lint && npm run build

cd deploy/compose && ./test-e2e.sh   # 期望 39/39 PASS

# 手动 smoke (compose up 之后):
# 1) 用 default-mock:text 创建几条相似内容,确认 dedup 与 vector ranking
# 2) curl /metrics | grep model_calls_total.*embed   # embed 调用数应随 memory 创建增长
# 3) 在 Jaeger UI 查 model.embed 子 span 是否出现在 memory.Create 链路上
```

## Acceptance

- [ ] migration 0010 up/down 双向跑通；`\dx vector` 显示已安装
- [ ] `internal/memory/embedder.go` Embedder 接口测试过
- [ ] `Repo.FindSimilar` / `SearchVector` 单测过（dockertest 用 pgvector 镜像，注入确定性向量）
- [ ] `Service.Create` dedup 命中 → 返回原 id + touch；未命中 → 新 id；Update content 重算 embedding
- [ ] `Service.Search` mode=vector 默认；mode=keyword 显式退回；query 空时自动退回 keyword
- [ ] mock provider 返回 1536-d；同输入两次调用向量完全一致
- [ ] 10 个 dockertest 文件全部跑过 pgvector/pgvector:pg16 镜像
- [ ] compose up 后 postgres 容器是 pgvector 版本，server 启动 codec 注册无报错
- [ ] E2E 39 步全过
- [ ] `git tree clean`，commit 按 Conventional Commits 切分（建议 4 个：migration+codec、embedder+repo、service+handler+mock、compose+e2e+docs）

## 风险与折衷

1. **pgvector 镜像 vs alpine** — 体积 +130MB，dev pull 一次而已；prod 容器复用 layer，影响可忽略。
2. **同步 embed 拖慢 Create P95** — mock provider 1ms 级；prod 远程 embed 100-300ms，Create 从亚毫秒变 ~300ms。可接受（记忆是低频写操作）；若变痛点，加 `embed_on_write=false` 兜底或改异步。
3. **ivfflat 在 100 行级数据下召回低** — pgvector 建议 lists ≈ rows^0.5；100 lists 适配 ~10K 行；测试只有几条记录时 ivfflat 可能跳过部分行。**缓解**：测试场景 query 精确比对 + 阈值不依赖召回率；prod 真实流量自然填充。如果 e2e 不稳，临时方案是测试中 `SET ivfflat.probes = 100`（探针数=lists 等于全扫）。
4. **同输入向量稳定性** — 依赖 mock 的 sha256+normalize 是 deterministic 的；切换真模型后用户必须知道"换 embedding model 后老向量必须重新生成"。文档强调。
5. **Update content 不去重** — 用户显式改内容时不应该被静默并到别的记忆上；这是 Create 与 Update 的语义差。`Update` 只算 embedding + 写回，不查 FindSimilar。
6. **历史无 embedding 行被 vector search 跳过** — slice 7 demo 数据丢失检索可见性；接受的 trade-off（详见非目标）。如要补：admin endpoint `POST /memories/_reembed` 扫一遍，留到 Reflection 切片一起做。
7. **embedding 列大小** — 1536 * 4 bytes = 6 KB/行，10K 条 = 60 MB；不构成问题。
8. **跨 provider embedding 不可比** — 切 OpenAI ada-002 → bge-base 后向量空间不同，老向量必须丢弃。本切片不处理；运维必须知道"换 embedding model = 清表"。
