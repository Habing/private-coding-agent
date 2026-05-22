-- Slice 21b (External MCP Manager): mcp_servers — tenant-scoped registry of
-- external MCP servers reachable over HTTP JSON-RPC 2.0. Admin REST CRUD
-- writes here; Manager republishes from this table on boot.
--
-- slug is the short namespace used when binding tools into the ToolBus as
-- mcp.<slug>.<tool>; it must be unique per tenant. tools_cache snapshots the
-- last successful tools/list response so boot does not need to hit each
-- server before serving requests; admin clicks "Refresh tools" to re-pull.

CREATE TABLE IF NOT EXISTS mcp_servers (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    slug            VARCHAR(64) NOT NULL,
    name            VARCHAR(128) NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    url             TEXT NOT NULL,
    transport       VARCHAR(16) NOT NULL DEFAULT 'http'
                    CHECK (transport IN ('http')),
    auth_type       VARCHAR(16) NOT NULL DEFAULT 'none'
                    CHECK (auth_type IN ('none','bearer')),
    auth_token      TEXT NOT NULL DEFAULT '',
    headers         JSONB NOT NULL DEFAULT '{}'::jsonb,
    enabled         BOOLEAN NOT NULL DEFAULT true,
    last_seen_at    TIMESTAMPTZ,
    last_error      TEXT NOT NULL DEFAULT '',
    tools_cache     JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, slug)
);

CREATE INDEX IF NOT EXISTS mcp_servers_tenant_enabled_idx
    ON mcp_servers (tenant_id, enabled) WHERE enabled = true;
