-- Slice 24: cron + webhook triggers for published workflows.
-- Rows sync from DSL triggers: on publish; unpublish sets enabled=false.

CREATE TABLE IF NOT EXISTS workflow_triggers (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    workflow_id     UUID NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    trigger_id      TEXT NOT NULL,
    kind            TEXT NOT NULL,
    cron_expr       TEXT,
    timezone        TEXT NOT NULL DEFAULT 'UTC',
    webhook_token   TEXT,
    default_inputs  JSONB NOT NULL DEFAULT '{}'::jsonb,
    enabled         BOOLEAN NOT NULL DEFAULT true,
    next_run_at     TIMESTAMPTZ,
    last_run_at     TIMESTAMPTZ,
    last_status     TEXT,
    last_error      TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workflow_id, trigger_id),
    UNIQUE (webhook_token)
);

CREATE INDEX IF NOT EXISTS workflow_triggers_due_idx
    ON workflow_triggers (enabled, kind, next_run_at)
    WHERE kind = 'cron' AND enabled = true;

CREATE INDEX IF NOT EXISTS workflow_triggers_tenant_workflow_idx
    ON workflow_triggers (tenant_id, workflow_id);
