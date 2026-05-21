# P1 MVP — 最小企业版设计 Spec

> Status: Approved — 执行中（2026-05-21）  
> Related: 主 spec §11 P1、[`docs/P1-ROADMAP.md`](../../P1-ROADMAP.md)、Slice 6 ADR-41、Slice 12 ADR-65/67  
> Full P1: [`2026-05-21-p1-full-enterprise-design.md`](2026-05-21-p1-full-enterprise-design.md)

## 1. 目标

在 **P0 切片 1～12 已交付** 基础上，达到 **企业试点** 可上线标准：

- 企业身份：**OIDC SSO**（本地登录可配置保留）
- 多租户运营：**provider 租户隔离**、**配额与 rate limit**
- 产品路径：**创建会话即有沙箱**，Web 聊天可直接触发 `fs.*` / `shell.exec`
- 可运维：**Memory 自动注入 + 管理 UI**、**Skills 12b** 租户配置
- 安全基线：JWT 吊销、HTTP 超时、审计扩展；沙箱风险 **文档化**（seccomp 深化在 Slice 22）

**不在 MVP-P1**：Workflow Engine、`agent.delegate`、Reflection Agent、K8sDriver、N8N。

---

## 2. 前置条件（Gate）

见 [`docs/P1-ROADMAP.md`](../../P1-ROADMAP.md) Gate G1–G4。  
**硬性**：`test-e2e.sh` 步骤 **1～42** 全 PASS；`main` 与 `origin/main` 同步（无大块未提交 P0 功能）。

---

## 3. 切片分解

### Slice 13 — Enterprise Foundation

| 维度 | 决策 |
|------|------|
| `providers.tenant_id` | NULL = 平台全局；非 NULL = 仅该租户可见 |
| Registry.Resolve | 先 tenant 专属，再全局 fallback（可配置禁止 fallback） |
| Quota | Redis 计数：`llm.tokens/day`、`sandbox.active`、`tool.invoke/min` |
| Rate limit | Gin 中间件或统一 `internal/quota`：按 tenant+user |
| Logout | `POST /auth/logout` + `jti` 黑名单（Redis TTL = JWT 剩余寿命） |
| HTTP | `ReadHeaderTimeout`、`IdleTimeout`、`WriteTimeout` 可配置 |
| Audit | `quota.exceeded`、`auth.logout`、`provider.denied` |

**迁移**：`0014_providers_tenant.up.sql`（示例编号，实现时顺延）。

**E2E**：43 quota 超限 429；44 logout 后旧 JWT 401。

---

### Slice 14 — Session ↔ Sandbox 强绑定

| 维度 | 决策 |
|------|------|
| 字段 | `sessions.sandbox_id UUID REFERENCES sandbox_sessions(id)` |
| 创建时机 | `POST /sessions` 成功后同步 `Sandbox.Create`；失败则 503 或回滚 session（实现选一种并写死） |
| 销毁 | `DELETE /sessions/:id`（archive）时 `Sandbox.Destroy`；reconciler 兜底孤儿 |
| Agent | `ContextComposer` 或 system 追加：`Current sandbox_id: <uuid>` |
| 弱绑定兼容 | 工具仍接受显式 `sandbox_id`；与 session 不一致时以 tool input 为准并 audit warn |

**E2E**：45 建 session → GET session 含 sandbox_id → WS「列出 /workspace 文件」→ `fs.list` 成功。

**ADR-41 演进**：由「仅弱绑定」升级为「默认强绑定 + 可覆盖」。

---

### Slice 15 — SSO (OIDC)

| 维度 | 决策 |
|------|------|
| 协议 | OIDC Authorization Code + PKCE |
| 端点 | `GET /auth/oidc/login`、`GET /auth/oidc/callback` |
| 用户映射 | `sub` + `iss` → 查找或 JIT 创建 `users`（`tenant_id` 由配置或 claim 映射） |
| 配置 | `auth.oidc_issuer`、`client_id`、`client_secret_env`、`redirect_url` |
| 本地登录 | `auth.local_enabled` 默认 true（开发）；生产 false |

**E2E**：46 mock OIDC（测试 double 或 wiremock 容器）→ JWT → `/me`。

**LDAP**：推迟 **15b**（Full P1 前补丁），不在 MVP 阻塞。

---

### Slice 16 — Enterprise Web

| 维度 | 决策 |
|------|------|
| 沙箱文件浏览 | Chat 页侧栏：树形目录，`GET /sandbox/sessions/{id}/files`；只读预览 |
| Memory Loader | `internal/memory/loader.go`：首条 user 消息前 top-K（vector 默认，可 keyword） |
| 注入点 | `agent.ContextComposer` 在 Skills 之后、历史之前插入 `## Relevant memories` |
| Memory UI | `/memories` 路由：列表、删除、编辑；复用现有 REST |
| Settings（最小） | 展示 session.model、profile（只读）；改 model 用 PATCH session（可 16 或 16b） |

**E2E**：47 预置 memory → 新 session WS 首问 → 回复/audit 体现注入；48 files API 经 UI 代理返回列表。

---

### Slice 17 — Skills 12b (Tenant Skills API)

| 维度 | 决策 |
|------|------|
| 表 | 见 Slice 12 design §12b：`skills`、`tenant_profile_skills` |
| API | Admin：`POST/GET/PUT/DELETE /admin/skills`；绑定 profile |
| 注入 | Resolver：filesystem platform + DB tenant skills（disabled 跳过） |
| Web | Admin 页（`AdminGuard`）：启用/禁用、编辑 body |

**E2E**：49 写入 tenant skill → session run → `skill.inject` 含 key。

---

## 4. MVP-P1 完成检查表

- [ ] Slice 13–17 全部 merge
- [ ] E2E **55/55** PASS
- [ ] README 勾选切片 13–17
- [ ] `HANDOFF.md` 标 MVP-P1 完成日期
- [ ] 部署文档：SSO 配置、quota 环境变量、生产 `auth.local_enabled=false`
- [ ] 安全：`docs/SECURITY-SANDBOX.md` 或 README 小节（docker.sock、资源限制）

---

## 5. 开放问题（MVP 内决策）

| # | 问题 | 默认 |
|---|------|------|
| 1 | session 创建沙箱失败是否仍返回 session？ | **否**，整体 503 |
| 2 | OIDC JIT 用户默认 role？ | `member`；首个用户 `admin` 仅 bootstrap |
| 3 | Quota 超限 HTTP 码？ | **429** + `quota_exceeded` |
| 4 | 文件浏览最大文件预览？ | 256KB，更大仅下载提示 |

---

## 6. 文档索引

| 切片 | Plan |
|------|------|
| 13 | `plans/2026-05-21-slice-13-enterprise-foundation.md` |
| 14 | `plans/2026-05-21-slice-14-session-sandbox-binding.md` |
| 15 | `plans/2026-05-21-slice-15-sso-oidc.md` |
| 16 | `plans/2026-05-21-slice-16-enterprise-web.md` |
| 17 | `plans/2026-05-21-slice-17-skills-12b.md` |
