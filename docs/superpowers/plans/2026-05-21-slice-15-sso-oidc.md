# Slice 15 — SSO (OIDC) Implementation Plan

> **Goal:** OIDC 登录流、JIT 用户、配置开关、E2E **46**。

**Design:** MVP-P1 spec § Slice 15

**Depends on:** Slice 13（JWT jti / auth 模块）

---

## Task 1 — Config

- [ ] `auth.oidc_enabled`、`issuer`、`client_id`、`client_secret_env`、`redirect_url`
- [ ] `auth.local_enabled`（默认 true）

## Task 2 — OIDC client

- [ ] `internal/auth/oidc.go`：discovery、PKCE、token exchange
- [ ] `GET /auth/oidc/login`（state/nonce cookie）
- [ ] `GET /auth/oidc/callback` → issue PCA JWT

## Task 3 — User mapping

- [ ] `sub`+`iss` 唯一；不存在则 `Register` JIT（role=member）
- [ ] Audit `auth.oidc.login.success` / `failure`

## Task 4 — E2E 46

- [ ] compose 内 `mock-oidc` 或测试 handler 返回固定 id_token
- [ ] 完整 code flow → `/me` 200

## Task 5 — Docs

- [ ] `deploy/compose/OIDC.md`：Keycloak / Azure AD 示例

**非目标：** LDAP（15b）、多租户自助注册 UI
