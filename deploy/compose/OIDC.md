# OIDC SSO（切片 15）

PCA 支持 **OIDC Authorization Code + PKCE**。本地开发可用 compose 内置 `mock-oidc`；生产可对接 Keycloak、Azure AD 等。

## Compose / E2E（mock-oidc）

`deploy/compose/docker-compose.yml` 已启用：

| 变量 | 默认 |
|------|------|
| `PCA_AUTH_OIDC_ENABLED` | `true` |
| `PCA_AUTH_OIDC_ISSUER` | `http://mock-oidc:8082`（server 容器内） |
| `PCA_AUTH_OIDC_CLIENT_ID` | `pca-e2e` |
| `OIDC_CLIENT_SECRET` | `e2e-oidc-secret-change-me` |
| `PCA_AUTH_OIDC_REDIRECT_URL` | `http://localhost:8080/auth/oidc/callback` |

浏览器或 E2E：

1. `GET http://localhost:8080/auth/oidc/login?tenant=default`
2. 跟随重定向完成 mock 授权
3. `GET /auth/oidc/callback` 返回 `{"token":"..."}`

验证：`GET /me` + `Authorization: Bearer <token>`。

## Keycloak（示例）

1. Realm → Clients → Create：`client_id=pca`，Access Type `confidential`，Valid Redirect URIs `https://pca.example.com/auth/oidc/callback`。
2. 开启 **Standard flow**，复制 Client secret 到环境变量 `OIDC_CLIENT_SECRET`。
3. `config.yaml` / 环境变量：

```yaml
auth:
  local_enabled: false   # 生产仅 SSO 时
  oidc:
    enabled: true
    issuer: "https://keycloak.example.com/realms/myrealm"
    client_id: "pca"
    client_secret_env: "OIDC_CLIENT_SECRET"
    redirect_url: "https://pca.example.com/auth/oidc/callback"
    tenant_slug: "default"
```

## Azure AD（示例）

1. App registrations → 新建应用 → Redirect URI：`Web` → `https://pca.example.com/auth/oidc/callback`。
2. Certificates & secrets → Client secret → 写入 `OIDC_CLIENT_SECRET`。
3. Issuer（v2.0）：

```text
https://login.microsoftonline.com/<tenant-id>/v2.0
```

4. `client_id` = Application (client) ID；其余配置同 Keycloak 段。

## 用户映射

- IdP `sub` + `iss` 在租户内唯一；首次登录 **JIT** 创建 `users`（`role=member`）。
- 审计：`auth.oidc.login.success` / `auth.oidc.login.failure`。
- 本地密码登录由 `auth.local_enabled` 控制（默认 `true`，生产可关）。
