CREATE TABLE sandbox_sessions (
    id            UUID PRIMARY KEY,
    tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    owner_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    project_id    UUID,
    container_id  TEXT,
    image         TEXT NOT NULL,
    status        TEXT NOT NULL,
    network_mode  TEXT NOT NULL,
    cpus          REAL NOT NULL DEFAULT 1.0,
    memory_mb     BIGINT NOT NULL DEFAULT 512,
    pids_limit    BIGINT NOT NULL DEFAULT 256,
    labels        JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    destroyed_at  TIMESTAMPTZ
);

CREATE INDEX sandbox_sessions_tenant_status_idx
    ON sandbox_sessions(tenant_id, status);
