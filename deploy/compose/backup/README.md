# Compose 试点备份 / 恢复

面向 **单实例 docker-compose** 的最小 DR 脚本（技术债 #11）。  
计划与验收：[`docs/P2-COMPOSE-PILOT.md`](../../../docs/P2-COMPOSE-PILOT.md)

## 备份

```bash
cd deploy/compose/backup
chmod +x backup.sh restore.sh
./backup.sh
```

输出目录默认 `deploy/compose/backups/`：

- `pca-pg-<UTC时间>.dump` — Postgres 自定义格式（`pg_dump -Fc`）
- 若本机有 `mc` 且 MinIO 在 `:9000` 可达，会额外 mirror 快照 bucket

建议 cron（Linux）：

```cron
0 3 * * * cd /path/to/private-coding-agent/deploy/compose/backup && ./backup.sh
```

## 恢复

```bash
./restore.sh ../backups/pca-pg-20260523T030000Z.dump
```

会 **stop server** → `pg_restore --clean` → 重启 postgres/redis/server。

## 必保数据

| 组件 | 备份方式 |
|------|----------|
| Postgres | `backup.sh`（sessions/messages/memories/workflows/audit/mcp/skills 等） |
| Redis | compose 已开 AOF（`appendonly yes`）；丢数据窗口 ≤1s |
| MinIO 快照 | 可选 `mc mirror`；丢 MinIO = 快照对象不可用 |
| 沙箱容器 | tmpfs workspace，**无需备份** |

## RPO / RTO（试点目标）

- **RPO**：24h（日备）+ Redis AOF
- **RTO**：人工 restore + 重启，约 15–30 分钟

## Windows 注意

脚本须 **LF** 换行。若 `bash -n backup.sh` 报 `unexpected end of file`，在 Git Bash 执行：

```bash
sed -i 's/\r$//' backup.sh restore.sh
```

改完脚本后跑：`bash -n *.sh` → `go test ./...` → `cd deploy/compose && ./test-e2e.sh`（见 P2-COMPOSE-PILOT 验收表）。
