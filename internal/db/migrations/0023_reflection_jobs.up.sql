CREATE TABLE IF NOT EXISTS reflection_jobs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    session_id      UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    status          TEXT NOT NULL DEFAULT 'pending'
                    CHECK (status IN ('pending', 'processing', 'completed', 'failed')),
    attempts        INT NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    max_attempts    INT NOT NULL DEFAULT 3 CHECK (max_attempts > 0),
    next_run_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_error      TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (session_id)
);

CREATE INDEX IF NOT EXISTS reflection_jobs_poll_idx
    ON reflection_jobs (next_run_at ASC)
    WHERE status IN ('pending', 'processing');
