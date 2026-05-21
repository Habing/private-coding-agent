DROP INDEX IF EXISTS providers_tenant_enabled_idx;
DROP INDEX IF EXISTS providers_tenant_name_uniq;
ALTER TABLE providers DROP COLUMN IF EXISTS tenant_id;
ALTER TABLE providers ADD CONSTRAINT providers_name_key UNIQUE (name);
