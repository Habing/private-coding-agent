CREATE TABLE sessions (
    id              UUID PRIMARY KEY,
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    owner_user_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title           TEXT NOT NULL DEFAULT '',
    model           TEXT NOT NULL,
    profile         TEXT NOT NULL DEFAULT 'coding',
    status          TEXT NOT NULL DEFAULT 'active',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX sessions_tenant_owner_idx
    ON sessions(tenant_id, owner_user_id, created_at DESC);

CREATE TABLE messages (
    id              UUID PRIMARY KEY,
    session_id      UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    tenant_id       UUID NOT NULL,
    seq             BIGINT NOT NULL,
    role            TEXT NOT NULL,
    content         TEXT NOT NULL DEFAULT '',
    tool_call_id    TEXT NOT NULL DEFAULT '',
    tool_calls      JSONB,
    metadata        JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (session_id, seq)
);

CREATE INDEX messages_session_seq_idx ON messages(session_id, seq);
