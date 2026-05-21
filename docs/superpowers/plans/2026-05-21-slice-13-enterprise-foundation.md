# Slice 13 — Enterprise Foundation Implementation Plan

> **Goal:** 租户级 provider 隔离、配额/rate limit、JWT logout、HTTP 超时、相关审计与 E2E **43–44**。

**Design:** MVP-P1 spec § Slice 13 — [`../specs/2026-05-21-p1-mvp-enterprise-design.md`](../specs/2026-05-21-p1-mvp-enterprise-design.md)

**Architecture:** 新建 `internal/quota`（或 `internal/tenant/quota`）封装 Redis 滑动窗口/日计数；`modelgw.Registry` 与 `providers` 迁移加 `tenant_id`；`auth` 增加 `jti` 黑名单；`httpx` 配置超时。

---

## Task 1 — Migration `providers.tenant_id`

- [ ] `0014_providers_tenant.up.sql`：`tenant_id UUID NULL REFERENCES tenants(id)`
- [ ] 索引 `(tenant_id, enabled)`；现有行 `tenant_id NULL` = 全局
- [ ] `Registry.Resolve(tenantID, modelRef)` 过滤逻辑 + 单测

## Task 2 — Quota service

- [ ] `quota.Check(ctx, tenantID, userID, kind)` → ok / `ErrQuotaExceeded`
- [ ] 配置：`quota.llm_tokens_per_day`、`quota.sandbox_max_active`、`quota.tool_invoke_per_minute`
- [ ] 接入点：`modelgw` record 前、`sandbox.Create` 前、`toolbus.Invoke` 前

## Task 3 — HTTP rate limit middleware

- [ ] 按 tenant+user 限流 protected 路由（可与 quota 共用 Redis key 前缀）
- [ ] 429 + `rate_limit_error`

## Task 4 — JWT logout

- [ ] Claims 增加 `jti`；签发时写入
- [ ] `POST /auth/logout`；Redis `SET revoked:<jti> 1 EX <ttl>`
- [ ] `auth.Middleware` 检查黑名单
- [ ] Audit `auth.logout`

## Task 5 — HTTP server timeouts

- [ ] `config.Server.ReadHeaderTimeout` 等
- [ ] `httpx.NewServer` 应用

## Task 6 — E2E + docs

- [x] `test-e2e.sh` 步骤 43–44
- [x] `config.example.yaml` + README quota 小节
- [x] 更新 `SLICE-VERIFICATION.md` 切片 13

**非目标：** OIDC（15）、session-sandbox（14）、audit hash chain（22）
