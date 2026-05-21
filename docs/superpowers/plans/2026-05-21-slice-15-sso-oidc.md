# Slice 15 — SSO (OIDC) Implementation Plan

> **Goal:** OIDC 登录流、JIT 用户、配置开关、E2E **46**。

**Design:** MVP-P1 spec § Slice 15

**Depends on:** Slice 13（JWT jti / auth 模块）

---

## Task 1 — Config

- [x] `auth.oidc.enabled`、`issuer`、`client_id`、`client_secret_env`、`redirect_url`
- [x] `auth.local_enabled`（默认 true，见 config.example.yaml）

## Task 2 — OIDC client

- [x] `internal/auth/oidc_client.go`：discovery、PKCE、token exchange
- [x] `GET /auth/oidc/login`（state/nonce cookie）
- [x] `GET /auth/oidc/callback` → issue PCA JWT

## Task 3 — User mapping

- [x] `sub`+`iss` 唯一；不存在则 JIT（role=member）
- [x] Audit `auth.oidc.login.success` / `failure`

## Task 4 — E2E 46

- [x] compose `mock-oidc` + RS256 id_token
- [x] 完整 code flow → `/me` 200

## Task 5 — Docs

- [x] `deploy/compose/OIDC.md`：Keycloak / Azure AD 示例

**非目标：** LDAP（15b）、多租户自助注册 UI
