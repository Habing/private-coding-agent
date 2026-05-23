# 生产化演练 Runbook — Compose 试点

> **状态**：演练清单（2026-05-23）  
> **前提**：Full P1 核心完成；Slice 23（N8N）**跳过**  
> **环境**：单实例 `deploy/compose`（kind/Helm 见 [`DEPLOY-K8S.md`](DEPLOY-K8S.md) §10）

在真实试点或发版前，按本清单跑一轮 **DR + embedding 运维 SOP**，并记录结果。

---

## 0. 前置

```bash
cd deploy/compose
cp -n .env.example .env
docker build -t pca/sandbox:base ../../sandbox/image   # 首次
docker compose up -d --build
curl -fsS http://localhost:8080/healthz

# 插入 demo 用户（与 E2E 相同）
HASH='$2a$10$WJBaC0mXl/yIgPXKW8WbPujOAidLdmaDPlduPdV8i11ZHaFvcgUrC'
docker compose exec -T postgres psql -U app -d app -v ON_ERROR_STOP=1 <<SQL
INSERT INTO users (tenant_id, email, password_hash, name, role)
VALUES ((SELECT id FROM tenants WHERE slug='default'),
        'demo@example.com', '$HASH', 'Demo', 'admin')
ON CONFLICT (tenant_id, email) DO NOTHING;
SQL

docker compose exec redis redis-cli CONFIG GET appendonly   # → yes
```

**一键脚本**（含 bootstrap + 备份/恢复/re-embed）：`deploy/compose/pilot-run.sh`

| 检查项 | 期望 |
|--------|------|
| `/healthz` | 200 |
| Redis AOF | `appendonly yes` |
| MinIO（快照） | `curl -fsS http://localhost:9000/minio/health/live`（若启用 snapshot） |

---

## 1. 备份演练（#11）

```bash
cd deploy/compose/backup
bash -n backup.sh restore.sh
./backup.sh
ls -lh ../backups/pca-pg-*.dump | tail -1
pg_restore --list ../backups/pca-pg-*.dump | head -5   # 需本机 pg_restore，可选
```

**通过标准：**

- [ ] 产出 `deploy/compose/backups/pca-pg-<UTC>.dump`，大小 > 10 KB
- [ ] 脚本 exit 0
- [ ] （可选）MinIO mirror 目录或日志无 fatal error

---

## 2. 恢复演练（#11，破坏性）

> **警告**：会 **清空并重建** 当前 Postgres 库。仅在试点/预发环境执行。

```bash
# 2a. 写入“恢复前”标记数据
TOK=$(curl -fsS -X POST http://localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"tenant":"default","email":"demo@example.com","password":"demo123"}' | jq -r .token)

curl -fsS -X POST http://localhost:8080/memories \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"type":"knowledge","content":"pilot-dr-marker-pre","tags":["pilot-dr"]}'

# 2b. 备份
cd deploy/compose/backup && ./backup.sh
DUMP=$(ls -t ../backups/pca-pg-*.dump | head -1)

# 2c. 写入“恢复后不应存在”的数据
curl -fsS -X POST http://localhost:8080/memories \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"type":"knowledge","content":"pilot-dr-marker-post","tags":["pilot-dr"]}'

# 2d. 恢复（输入 RESTORE 确认）
echo RESTORE | ./restore.sh "$DUMP"
sleep 15
curl -fsS http://localhost:8080/healthz

# 2e. 重新登录（JWT 可能因用户行变化失效）
TOK=$(curl -fsS -X POST http://localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"tenant":"default","email":"demo@example.com","password":"demo123"}' | jq -r .token)

PRE=$(curl -fsS "http://localhost:8080/memories?tag=pilot-dr" \
  -H "Authorization: Bearer $TOK" | jq -r '[.memories[]? | select(.content=="pilot-dr-marker-pre")] | length')
POST=$(curl -fsS "http://localhost:8080/memories?tag=pilot-dr" \
  -H "Authorization: Bearer $TOK" | jq -r '[.memories[]? | select(.content=="pilot-dr-marker-post")] | length')
echo "pre=$PRE post=$POST"
```

**通过标准：**

- [ ] `pre=1`（备份内数据恢复）
- [ ] `post=0`（备份后写入的数据被 rollback）
- [ ] `/healthz` 恢复后 200

---

## 3. Re-embed SOP（#12）

换 embedding 模型或维度变更后执行：

```bash
# 1. 更新 config / env
#    memory.embedding_model 或 PCA_MEMORY_EMBEDDING_MODEL
# 2. 重启 server
docker compose restart server
sleep 10

# 3. 全量 re-embed（admin JWT）
TOK=$(curl -fsS -X POST http://localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"tenant":"default","email":"demo@example.com","password":"demo123"}' | jq -r .token)

curl -fsS -X POST http://localhost:8080/admin/memories/re-embed \
  -H "Authorization: Bearer $TOK" | jq .

# 4. 审计
curl -fsS "http://localhost:8080/audit?action=memory.reembed.complete&limit=5" \
  -H "Authorization: Bearer $TOK" | jq '.entries[0].action'

# 5. 抽查 vector 召回（需 mock-provider embeddings 或真实 provider）
curl -fsS -X POST http://localhost:8080/tools/invoke \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"tool":"memory.search","args":{"query":"pilot","mode":"vector","limit":3}}' | jq .
```

**通过标准：**

- [ ] `re-embed` 返回 `updated >= 1`
- [ ] audit 含 `memory.reembed.start` / `memory.reembed.complete`
- [ ] `memory.search` mode=vector 有命中（数据集非空时）

---

## 4. 回归（可选但推荐）

```bash
cd deploy/compose && ./test-e2e.sh   # 69/69
```

---

## 5. 演练记录模板

| 日期 | 环境 | 备份 | 恢复 | re-embed | E2E | 操作人 | 备注 |
|------|------|------|------|----------|-----|--------|------|
| 2026-05-23 | compose | ✅ | ✅ pre=1 post=0 | ✅ updated=8 | — | agent | `pilot-run.sh` |

---

## 相关文档

| 文档 | 内容 |
|------|------|
| [`DEPLOY.md`](DEPLOY.md) §9 | 备份脚本入口 |
| [`P2-COMPOSE-PILOT.md`](P2-COMPOSE-PILOT.md) | #11–#15 交付摘要 |
| [`DEPLOY-K8S.md`](DEPLOY-K8S.md) | kind nightly 6 步（K8s 路径独立验证） |
