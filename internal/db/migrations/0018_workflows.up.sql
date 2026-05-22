-- Slice 19 (Workflow Engine): YAML-DSL workflow definitions + per-invoke run
-- log. workflows holds a single row per (tenant_id, slug); editing bumps
-- `version` and forces `published=false` (admin must explicitly re-publish so
-- the DB ↔ ToolBus pairing stays in sync). workflow_runs is append-only and
-- tracks every execution including Dry-Run.

CREATE TABLE IF NOT EXISTS workflows (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    slug         TEXT NOT NULL,
    name         TEXT NOT NULL,
    description  TEXT NOT NULL DEFAULT '',
    dsl_yaml     TEXT NOT NULL,
    version      INT  NOT NULL DEFAULT 1,
    published    BOOLEAN NOT NULL DEFAULT false,
    published_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, slug)
);

CREATE INDEX IF NOT EXISTS workflows_tenant_published_idx
    ON workflows (tenant_id, published);

CREATE TABLE IF NOT EXISTS workflow_runs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL,
    user_id         UUID NOT NULL,
    workflow_id     UUID NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    version_at_run  INT  NOT NULL,
    dry_run         BOOLEAN NOT NULL DEFAULT false,
    status          TEXT NOT NULL,
    inputs_json     JSONB NOT NULL DEFAULT '{}'::jsonb,
    outputs_json    JSONB,
    error_text      TEXT,
    duration_ms     INT,
    started_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at     TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS workflow_runs_workflow_idx
    ON workflow_runs (workflow_id, started_at DESC);

CREATE INDEX IF NOT EXISTS workflow_runs_tenant_user_idx
    ON workflow_runs (tenant_id, user_id, started_at DESC);
