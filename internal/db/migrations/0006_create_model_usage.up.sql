CREATE TABLE model_usage (
    id              BIGSERIAL PRIMARY KEY,
    occurred_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    tenant_id       UUID NOT NULL,
    user_id         UUID NOT NULL,
    provider_id     UUID NOT NULL REFERENCES providers(id),
    provider_type   TEXT NOT NULL,
    model           TEXT NOT NULL,
    action          TEXT NOT NULL,
    stream          BOOLEAN NOT NULL,
    status          TEXT NOT NULL,
    error_class     TEXT NOT NULL DEFAULT '',
    input_tokens    INT NOT NULL DEFAULT 0,
    output_tokens   INT NOT NULL DEFAULT 0,
    duration_ms     INT NOT NULL
);

CREATE INDEX model_usage_tenant_time_idx
    ON model_usage(tenant_id, occurred_at DESC);
