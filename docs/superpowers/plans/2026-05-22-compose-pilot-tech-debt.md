# Compose Pilot Tech Debt — Implementation Plan

> **Goal:** 单实例 compose 运维可靠性：备份、workflow 保留、Reflection 持久队列。  
> **Track doc:** [`docs/P2-COMPOSE-PILOT.md`](../../P2-COMPOSE-PILOT.md)  
> **Depends on:** Full P1 Slice 22 完成

---

## 执行规则

**每完成一个 Task，必须跑完该 Task 的「改完必测」+ 轨道回归（`go test ./...` + compose E2E 67/67），再勾选 `[x]`。**

```bash
go test ./... -count=1
go vet ./...
cd deploy/compose && ./test-e2e.sh
```

---

## Task 1 — #11 备份脚本 + Redis AOF

- [x] `deploy/compose/backup/backup.sh`（pg_dump `-Fc`）
- [x] `deploy/compose/backup/restore.sh`（交互确认 `RESTORE`）
- [x] `deploy/compose/backup/README.md`
- [x] `docker-compose.yml` Redis：`--appendonly yes --appendfsync everysec`
- [x] `docs/DEPLOY.md` §9 更新

**改完必测：**

```bash
bash -n deploy/compose/backup/backup.sh deploy/compose/backup/restore.sh
# stack 运行中：
cd deploy/compose/backup && ./backup.sh
docker compose exec redis redis-cli CONFIG GET appendonly   # → yes
go test ./... -count=1
cd deploy/compose && ./test-e2e.sh
```

---

## Task 2 — #14 workflow_runs retention

- [x] `internal/workflow/retention.go` — `StartRunsRetention` + `DeleteRunsOlderThan`
- [x] `internal/config/config.go` — `WorkflowConfig` + `applyPilotDefaults`
- [x] `config/config.example.yaml` — `workflow.runs_retention_days` / `retention_interval`
- [x] `cmd/server/main.go` — 启动 retention goroutine
- [x] `internal/workflow/repo_test.go` — `TestRepo_DeleteRunsOlderThan`

**改完必测：**

```bash
go test ./internal/workflow/... -run DeleteRunsOlderThan -count=1
go test ./... -count=1
cd deploy/compose && ./test-e2e.sh
```

---

## Task 3 — #15 Reflection 持久化队列

- [x] `internal/db/migrations/0023_reflection_jobs.{up,down}.sql`
- [x] `internal/reflection/job_repo.go`
- [x] `internal/reflection/worker.go` — store + poll + retry + proposal TTL
- [x] `internal/reflection/types.go` — `ReflectionJob.JobID`
- [x] `config/config.example.yaml` — reflection 队列配置
- [x] `cmd/server/main.go` — `NewJobRepo` + `WorkerOptions`
- [x] `internal/reflection/job_repo_test.go`

**改完必测：**

```bash
go test ./internal/reflection/... -count=1
go test ./... -count=1
cd deploy/compose && ./test-e2e.sh
```

---

## Task 4 — #12 全量 re-embed admin ✅

- [x] Admin API：`POST /admin/memories/re-embed` 按 tenant 重算 embedding
- [x] 进度与 audit（`memory.reembed.start/complete`）
- [x] 文档：换模型 SOP（P2-COMPOSE-PILOT + DEPLOY §8）

**改完必测（实现时）：**

```bash
go test ./internal/memory/... -count=1
go test ./... -count=1
cd deploy/compose && ./test-e2e.sh   # + 新 E2E 步若加
```

**非目标：** 在线双写双索引；自动检测模型变更。

---

## Task 5 — #13 Snapshot restore ✅

- [x] `POST /sandbox/snapshots/restore/:id` → 新 running sandbox
- [x] K8sDriver 仍 `ErrSnapshotDisabled`（compose/docker only）
- [x] E2E 步 68：snapshot → restore → exec 验证 marker.txt

**改完必测（实现时）：**

```bash
go test ./internal/sandbox/... -count=1
go test ./... -count=1
cd deploy/compose && ./test-e2e.sh
```

---

## 文档同步清单（每项 Task 完成后）

- [x] `docs/P2-COMPOSE-PILOT.md` 状态表
- [x] `docs/SLICE-VERIFICATION.md` Compose Pilot 段
- [x] `docs/WORKFLOW.md` retention 说明
- [x] `HANDOFF.md` §3.3 / §5.4
- [x] Task 4/5 完成时再更新 README 勾选

**非目标：** K8s 多副本 WS sticky、跨进程 Tool Bus、Horizontal Reflection worker。
