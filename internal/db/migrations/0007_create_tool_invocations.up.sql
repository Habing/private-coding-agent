CREATE TABLE tool_invocations (
    id              BIGSERIAL PRIMARY KEY,
    occurred_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    tenant_id       UUID NOT NULL,
    user_id         UUID NOT NULL,
    tool_name       TEXT NOT NULL,
    status          TEXT NOT NULL,
    error_class     TEXT NOT NULL DEFAULT '',
    duration_ms     INT NOT NULL,
    input_sha256    TEXT NOT NULL DEFAULT '',
    output_sha256   TEXT NOT NULL DEFAULT ''
);

CREATE INDEX tool_invocations_tenant_time_idx
    ON tool_invocations(tenant_id, occurred_at DESC);
