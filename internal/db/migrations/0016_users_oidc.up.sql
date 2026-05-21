-- Slice 15: OIDC identity mapping (sub + iss per tenant).
ALTER TABLE users
    ADD COLUMN oidc_iss TEXT,
    ADD COLUMN oidc_sub TEXT;

CREATE UNIQUE INDEX users_tenant_oidc_idx
    ON users (tenant_id, oidc_iss, oidc_sub)
    WHERE oidc_iss IS NOT NULL AND oidc_sub IS NOT NULL;
