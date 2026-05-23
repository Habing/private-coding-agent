# 部署 Runbook

> 适用范围：MVP-P1（切片 1–17）+ Full-P1 切片 22d2 的 Helm 形态。审计哈希链等 Full-P1 剩余进度见 [`p1-full-enterprise-design.md`](superpowers/specs/2026-05-21-p1-full-enterprise-design.md)。

本文聚焦"从 dev compose 到生产试点"的差异：必须切换的开关、不能放弃的硬指标、上线前 checklist。K8s/Helm 部署单独见 [`DEPLOY-K8S.md`](DEPLOY-K8S.md)。

## 1. 部署形态

| 形态 | 入口 | 适合 |
|------|------|------|
| `deploy/compose/docker-compose.yml` | `docker compose up -d --build` | 单机试点（≤ 50 并发用户）；用 docker.sock 妥协 |
| 二进制 + 自带 Postgres/Redis/Docker | `bin/server -config /etc/pca/config.yaml` | 已有 Postgres/Redis 集群的内网环境 |
| K8s / Helm（22d2 ✅） | `helm install pca ./deploy/helm/pca` | 生产试点 / 多用户；K8sDriver 走 SA + RBAC，无 docker.sock |

二进制路径由 [`Dockerfile`](../Dockerfile) 构出，使用 distroless **以 root 运行**——见 [`SECURITY-SANDBOX.md`](SECURITY-SANDBOX.md)。

## 2. 配置加载

`internal/config.Load` 用 viper 按以下顺序合并：

1. `--config` 指向的 YAML（必须存在；参考 [`config/config.example.yaml`](../config/config.example.yaml)）
2. 环境变量 `PCA_*`，下划线代表层级（`PCA_AUTH_JWT_SECRET` → `auth.jwt_secret`）
3. 部分子段在代码里有 default（切片 13/15/Skills），见 `applySlice*Defaults`

约定：**生产环境只往 env 里放敏感值（JWT secret、OIDC client secret、DSN 密码）**；其余走 YAML 走 Git 可追溯。

## 3. 生产必须切换的开关

### 3.1 认证

```yaml
auth:
  jwt_secret: ${PCA_AUTH_JWT_SECRET}   # 必须 >= 32 字符；不能是 "change-me-in-production"（启动会拒绝）
  jwt_ttl: "24h"
  local_enabled: false                  # 生产关闭邮箱/密码登录，强制走 OIDC
  oidc:
    enabled: true
    issuer: "https://idp.example.com"
    client_id: "pca-prod"
    client_secret_env: "OIDC_CLIENT_SECRET"   # 实际密文走 env
    redirect_url: "https://agent.example.com/auth/oidc/callback"
    tenant_slug: "default"
```

启动期 `auth.ValidateJWTConfig` 会拒绝弱密钥；`auth.local_enabled=false && oidc.enabled=false` 也会被启动期校验拒绝（避免锁死）。

OIDC 字段对接 Keycloak / Azure AD 的细节见 [`deploy/compose/OIDC.md`](../deploy/compose/OIDC.md)。

### 3.2 Quota & RateLimit

```yaml
quota:
  llm_tokens_per_day:     200000   # 每 (tenant, user) 每天，跨 chat+embeddings；0 = 关
  sandbox_max_active:     5        # 每 tenant 同时存活沙箱；0 = 关
  tool_invoke_per_minute: 120      # HTTP /tools/invoke; 0 = 关
rate_limit:
  per_minute:             600      # /protected 组每 (tenant, user) 每分钟；0 = 关
```

任意字段为 0 即关闭对应 check（路径不变，counter 不写 Redis）。生产**至少**保留 `llm_tokens_per_day` 与 `sandbox_max_active`——前者锁住模型账单，后者锁住 docker 资源。环境变量覆写：`PCA_QUOTA_LLM_TOKENS_PER_DAY` 等。

429 与 `quota_exceeded` 出现时审计写 `quota.reject`，可在 Prometheus 看 `pca_quota_rejects_total`。

### 3.3 服务器超时

```yaml
server:
  port: 8080
  mode: release                      # debug 会打开 gin 调试输出
  ws_allowed_origins:                # 不要保持 ["*"]；填实际前端 origin
    - "https://agent.example.com"
  read_timeout: "30s"
  # write_timeout 保持空——设了会切断 SSE / WebSocket。
  idle_timeout: "120s"
```

`ReadHeaderTimeout=10s` 硬编码在 `cmd/server/main.go` 防 slow-loris。

### 3.4 Memory（向量记忆）

```yaml
memory:
  embedding_model: "dashscope:text-embedding-v4"   # 或自托管 1536-d 模型
  dedup_threshold: 0.92                            # cosine 阈值；0 关 dedup
  embed_on_write: true                             # 运维 kill switch；false 退回 keyword
  inject_top_k: 5
  inject_max_chars: 4000
```

切换 embedding model 后**老向量必须重算**（不同模型空间不可比）。使用 admin 接口：

```bash
curl -X POST http://localhost:8080/admin/memories/re-embed \
  -H "Authorization: Bearer $ADMIN_JWT"
```

返回 `{total, updated, failed, embedding_model}`；审计事件 `memory.reembed.*`。详见 [`P2-COMPOSE-PILOT.md`](P2-COMPOSE-PILOT.md) #12。

### 3.5 Skills

```yaml
skills:
  enabled: true
  dirs: ["/etc/pca/skills"]          # 平台级 SKILL.md 目录，挂只读盘
  default_skill_ids: []
  max_injected_chars: 24000
  max_skills_per_run: 5
```

租户级 Skill 通过 `/admin/skills` API 入库（见 [README.md "租户 Skill"](../README.md)），不需要预填 YAML。

### 3.6 可观测性

```yaml
telemetry:
  service_name: "private-coding-agent"
  otlp_endpoint: "otel-collector:4317"             # 不填 = 不导出 trace
observability:
  log_format: "json"                                # 生产保持 json
  log_level:  "info"
  metrics_token: "${PCA_OBSERVABILITY_METRICS_TOKEN}"   # Prom scraper 静态 token；与 prometheus.yml 对齐
```

`/metrics` 双通道：scrape job 用静态 token；admin JWT 用于 ad-hoc 调试。

## 4. 完整 env 变量速查

按 viper 规则，下表都是 `PCA_` 前缀，`.` → `_`：

| 变量 | 默认 | 备注 |
|------|------|------|
| `PCA_SERVER_PORT` | `8080` | |
| `PCA_SERVER_MODE` | `debug` | 生产改 `release` |
| `PCA_SERVER_WS_ALLOWED_ORIGINS` | `["*"]` | YAML 列表，env 用 JSON 数组字符串 |
| `PCA_DB_DSN` | — | 必填 |
| `PCA_REDIS_ADDR` | — | 必填 |
| `PCA_AUTH_JWT_SECRET` | — | **>= 32 字符；非 placeholder** |
| `PCA_AUTH_JWT_TTL` | `24h` | |
| `PCA_AUTH_LOCAL_ENABLED` | `true` | 生产 `false` |
| `PCA_AUTH_OIDC_ENABLED` | `false` | 生产 `true` |
| `PCA_AUTH_OIDC_ISSUER` / `_CLIENT_ID` / `_REDIRECT_URL` / `_TENANT_SLUG` | — | OIDC 必填 |
| `PCA_AUTH_OIDC_CLIENT_SECRET_ENV` | `OIDC_CLIENT_SECRET` | 指向密文所在 env 名 |
| `OIDC_CLIENT_SECRET` | — | OIDC 密文实际值 |
| `PCA_TELEMETRY_OTLP_ENDPOINT` | 空 | 空 = 关 |
| `PCA_OBSERVABILITY_METRICS_TOKEN` | 空 | 空 = `/metrics` 只接 admin JWT |
| `PCA_OBSERVABILITY_LOG_FORMAT` / `_LOG_LEVEL` | `json` / `info` | |
| `PCA_QUOTA_LLM_TOKENS_PER_DAY` | `200000` | 0 关闭 |
| `PCA_QUOTA_SANDBOX_MAX_ACTIVE` | `5` | 0 关闭；compose 默认 1 用于 E2E |
| `PCA_QUOTA_TOOL_INVOKE_PER_MINUTE` | `120` | 0 关闭 |
| `PCA_RATE_LIMIT_PER_MINUTE` | `600` | 0 关闭 |
| `PCA_MEMORY_EMBEDDING_MODEL` | `default-mock:text` | 生产改远程模型名 |
| `PCA_MEMORY_DEDUP_THRESHOLD` | `0.92` | |
| `PCA_MEMORY_EMBED_ON_WRITE` | `true` | 故障期可临时 `false` |
| `PCA_MEMORY_INJECT_TOP_K` / `_MAX_CHARS` | `5` / `4000` | |
| `PCA_SKILLS_ENABLED` | `true` | |
| `PCA_SKILLS_DIRS` | `["skills"]` | |
| `PCA_PROVIDERS_DISALLOW_GLOBAL_FALLBACK` | `false` | 多租户硬模式建议 `true` |
| `DOCKER_HOST` | `unix:///var/run/docker.sock` | 沙箱挂载点 |
| `DASHSCOPE_API_KEY` | — | DashScope 模型必填 |

## 5. 数据库与 Redis

- Postgres：要求 16+ 且**装好 `pgvector` 扩展**（compose 用 `pgvector/pgvector:pg16` 镜像）。迁移在 server 启动期自动跑（`internal/db.Migrate`，golang-migrate），无需手工 `psql`。
- pgvector ivfflat：`internal/db.Connect` 的 `AfterConnect` 把 `ivfflat.probes` 设成 100（默认 1 在小数据集会漏召）。生产数据量上来后 lists 调大时，probes 同步上调。
- Redis：用于 JWT 撤销名单、quota counter、rate-limit counter。建议独立实例或 AOF on——丢数据=丢撤销名单。

## 6. 启动期自检

server 启动会一次性校验：

1. `auth.ValidateJWTConfig` — 弱密钥/placeholder/长度 < 32 直接退出
2. `auth.local_enabled || auth.oidc.enabled` — 都关会退出
3. `auth.OIDC.Enabled` 时 `OIDCConfig.Valid()` — 字段缺一退出
4. `db.Migrate` — 迁移失败退出
5. `sandbox.RunReconciler` — 启动时核对活跃沙箱与 docker 实际容器；container 不存在的行被标记 `destroyed`

日志全部走 slog；启动期错误是 `level=ERROR` + 进程 exit code != 0。

## 7. 上线前 Checklist

- [ ] `PCA_AUTH_JWT_SECRET` 长度 >= 32，且不是 dev 默认值
- [ ] `auth.local_enabled=false` & `auth.oidc.enabled=true`
- [ ] `OIDC_CLIENT_SECRET` 在 env / Vault 里，不在 YAML
- [ ] OIDC callback URL 走 HTTPS
- [ ] `server.ws_allowed_origins` 不再是 `["*"]`
- [ ] `server.mode: release`
- [ ] `quota.*` 与 `rate_limit.per_minute` 留至少两个开启
- [ ] `memory.embedding_model` 指向生产模型（不再是 `default-mock:text`）
- [ ] `observability.log_format: json`，日志接 Loki / ELK
- [ ] `telemetry.otlp_endpoint` 指向生产 collector（或显式留空表示放弃 trace）
- [ ] `metrics_token` 已与 Prometheus scrape job 对齐
- [ ] DB Postgres 装好 `pgvector` 且数据卷已备份
- [ ] Redis 启用持久化（AOF 或定期 RDB 落盘）
- [ ] Docker daemon 已读 [`SECURITY-SANDBOX.md`](SECURITY-SANDBOX.md)（**这是 MVP 阶段最大的攻击面**）
- [ ] 第一个 admin 用户用 OIDC `sub` 提前 seed 进 `users` 表（首次自助注册仍可行，但 admin 角色不会自动给 — 用 SQL 改）

## 8. 升级流程

每个切片的迁移已加进 `internal/db/migrations/`；升级二进制时迁移自动跑。回滚要小心：

- 切片 11 之前的二进制起不来——pgvector 字段不存在但代码会引用
- 切片 17 之前的二进制起不来——`skills` 表不存在但代码会引用

约束：**只支持往前升级**，不要试图把 server 二进制回退到老版本而保留新迁移。生产回滚走"回滚迁移 + 回滚二进制"两步，迁移 down.sql 已就绪。

## 9. 备份 / DR

Compose 试点已提供脚本（`deploy/compose/backup/`）：

```bash
cd deploy/compose/backup
./backup.sh                    # pg_dump 全库 + 可选 MinIO mirror
./restore.sh path/to/dump.dump # 需先停 server，输入 RESTORE 确认
```

要点：

- **Postgres**：`pg_dump -Fc` 自定义格式；核心表 audit_log / messages / memories / workflow_runs
- **Redis**：compose 已启用 AOF（`appendonly yes`，`appendfsync everysec`）；撤销名单丢失窗口极小
- **MinIO / 快照**：`backup.sh` 可选 `mc mirror`；沙箱容器默认 tmpfs，无持久工作区

生产环境建议 cron daily 跑 `backup.sh`，保留 7–30 天 off-site 副本。

**生产化演练清单**（备份/restore、re-embed SOP）：[`docs/PILOT-RUNBOOK.md`](PILOT-RUNBOOK.md)

## 10. 验证

```bash
# 本地 compose 烟囱测试
cd deploy/compose && cp .env.example .env && ./test-e2e.sh    # 78/78 PASS

# 生产烟囱
curl -fsS https://agent.example.com/healthz
curl -fsS -H "Authorization: Bearer $ADMIN_JWT" https://agent.example.com/metrics | head -5
```

如果 OIDC 集成报错，先看 `/auth/oidc/discovery` 是否能从 server 容器内 curl 到 issuer。
