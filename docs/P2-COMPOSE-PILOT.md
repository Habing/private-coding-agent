# Compose 试点技术债 — P2 运维收口

> **状态**：✅ 完成（2026-05-23）  
> **范围**：**单实例 `docker-compose`** 部署形态；不阻塞 K8s 多副本 / Slice 23  
> **实施计划**：[`docs/superpowers/plans/2026-05-22-compose-pilot-tech-debt.md`](superpowers/plans/2026-05-22-compose-pilot-tech-debt.md)  
> **部署说明**：[`docs/DEPLOY.md`](DEPLOY.md) §9

---

## 背景

Full P1（Slice 22）完成后，compose 单实例试点仍有几项**运维可靠性**缺口：无备份脚本、Reflection 进程内队列重启丢任务、`workflow_runs` 无限增长等。本轨道在 **不扩 P1 切片编号** 的前提下收口，优先级高于 Slice 23（N8N 可选）。

| 编号 | 项 | 状态 |
|------|-----|------|
| **#11** | Postgres 备份 / Redis AOF | ✅ 已交付 |
| **#14** | `workflow_runs` 保留策略 | ✅ 已交付 |
| **#15** | Reflection 持久化队列 + 重试 | ✅ 已交付 |
| **#12** | 换 embedding 模型后全量 re-embed admin | ✅ 已交付 |
| **#13** | 从 snapshot 恢复沙箱 | ✅ 已交付（Docker only） |

---

## 执行纪律：每项改完必测

**禁止**在未跑回归的情况下勾选完成或合并 PR。顺序：

```text
实现单项 → 跑「该项最低测试」→ 跑「轨道回归」→ 更新本文 + plan 勾选 → 下一项
```

### 单项最低测试（按编号）

| 编号 | 改完必跑 |
|------|----------|
| **#11** | `bash -n deploy/compose/backup/*.sh`；compose 起 stack 后 `./deploy/compose/backup/backup.sh` 产出 `.dump`；`docker compose exec redis redis-cli CONFIG GET appendonly` → `yes` |
| **#14** | `go test ./internal/workflow/... -run DeleteRunsOlderThan -count=1`；server 启动日志含 `workflow retention` |
| **#15** | `go test ./internal/reflection/... -count=1`；迁移 `0023_reflection_jobs` 在干净库 `db.Migrate` 成功 |
| **#12** | `go test ./internal/memory/... -count=1`；E2E 步 69 `POST /admin/memories/re-embed` |
| **#13** | `go test ./internal/sandbox/... -count=1`；E2E 步 68 snapshot restore |

### 轨道回归（任意一项改完后）

```bash
cd F:/project/private-coding-agent
go test ./... -count=1
go vet ./...
go build -o bin/server ./cmd/server
cd deploy/compose && ./test-e2e.sh    # 期望 69/69
```

WebUI 若动到前端，额外：`cd internal/webui && npm test -- --run`。

---

## 已交付摘要

### #11 备份 / DR

- `deploy/compose/backup/backup.sh` — `pg_dump -Fc` + 可选 MinIO `mc mirror`
- `deploy/compose/backup/restore.sh` — 停 server、输入 `RESTORE` 确认
- `deploy/compose/docker-compose.yml` — Redis AOF
- `docs/DEPLOY.md` §9 指向脚本

### #14 workflow_runs 保留

- 配置 `workflow.runs_retention_days`（默认 **90**）、`workflow.retention_interval`（默认 **24h**）
- `internal/workflow/retention.go` — 启动 purge + 后台 ticker
- `Repo.DeleteRunsOlderThan`

### #15 Reflection 持久化队列

- 迁移 `0023_reflection_jobs`
- `internal/reflection/job_repo.go` + Worker DB poll / 指数退避 / pending proposal TTL
- 配置 `reflection.max_attempts`、`retry_base_interval`、`poll_interval`、`proposal_pending_ttl_days`

### #12 全量 re-embed

- `POST /admin/memories/re-embed`（admin-only，按 tenant 批量重算）
- `Service.ReEmbedTenant` + `Repo.ListByTenant` / `UpdateEmbedding`
- Audit：`memory.reembed.start` / `memory.reembed.complete`

**换模型 SOP：**

1. 更新 `memory.embedding_model`（或 `PCA_MEMORY_EMBEDDING_MODEL`）
2. 重启 server（新 embedder 生效）
3. `curl -X POST /admin/memories/re-embed -H "Authorization: Bearer $ADMIN_JWT"`
4. 抽查 `memory.search` mode=vector 召回

### #13 Snapshot restore（Docker）

- `POST /sandbox/snapshots/restore/:id` → 新 running sandbox
- MinIO tar → `docker load` → 容器启动（无 `/workspace` tmpfs 覆盖）
- K8sDriver 返回 `503 snapshot_disabled`
- Audit：`sandbox.snapshot.restore`

---

## 变更日志

| 日期 | 项 | 说明 |
|------|-----|------|
| 2026-05-23 | #11/#14/#15 | 首版交付 + 本文档 |
| 2026-05-23 | #12/#13 | re-embed admin + snapshot restore；E2E 69/69 |

| 文档 | 关系 |
|------|------|
| [`P1-ROADMAP.md`](P1-ROADMAP.md) | Full P1 已完成；本轨道为 **P1 后 compose 运维** |
| [`HANDOFF.md`](../HANDOFF.md) §3.3 | 技术债映射已更新 |
| [`WORKFLOW.md`](WORKFLOW.md) | retention 行为说明 |
| [`SLICE-VERIFICATION.md`](SLICE-VERIFICATION.md) | Compose Pilot 验收表 |

---

## 与主路线图关系
