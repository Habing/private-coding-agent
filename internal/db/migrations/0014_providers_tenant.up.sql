-- Slice 13: tenant-scoped providers. tenant_id NULL = platform global row.
-- Uniqueness becomes (tenant_id, name) so a tenant may shadow a global name.
-- PG 15+ NULLS NOT DISTINCT makes two global rows with the same name still
-- conflict (NULL == NULL for index purposes).
ALTER TABLE providers DROP CONSTRAINT providers_name_key;

ALTER TABLE providers
    ADD COLUMN tenant_id UUID NULL REFERENCES tenants(id) ON DELETE CASCADE;

CREATE UNIQUE INDEX providers_tenant_name_uniq
    ON providers (tenant_id, name) NULLS NOT DISTINCT;

CREATE INDEX providers_tenant_enabled_idx
    ON providers (tenant_id, enabled) WHERE enabled = TRUE;
