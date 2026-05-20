CREATE TABLE memories (
    id              UUID PRIMARY KEY,
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    owner_user_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type            TEXT NOT NULL CHECK (type IN ('profile','preference','knowledge','lesson')),
    content         TEXT NOT NULL,
    tags            TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    source          TEXT NOT NULL DEFAULT 'user',
    source_msg_id   UUID,
    last_used_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX memories_tenant_owner_idx
    ON memories(tenant_id, owner_user_id, created_at DESC);

CREATE INDEX memories_tags_idx ON memories USING GIN (tags);
