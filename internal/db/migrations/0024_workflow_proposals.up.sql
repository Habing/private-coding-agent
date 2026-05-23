-- Slice 19b: NL workflow authoring proposals — draft DSL + dry-run snapshot +
-- in-chat confirm / admin approval before publish.

CREATE TABLE IF NOT EXISTS workflow_proposals (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    session_id          UUID REFERENCES sessions(id) ON DELETE SET NULL,
    created_by          UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    slug                TEXT NOT NULL,
    name                TEXT NOT NULL,
    description         TEXT NOT NULL DEFAULT '',
    dsl_yaml            TEXT NOT NULL,
    source              TEXT NOT NULL DEFAULT 'freeform',
    template_id         TEXT,
    slots_json          JSONB NOT NULL DEFAULT '{}'::jsonb,
    dry_run_ok          BOOLEAN NOT NULL DEFAULT false,
    dry_run_output_json JSONB,
    dry_run_error       TEXT,
    status              TEXT NOT NULL DEFAULT 'draft'
                        CHECK (status IN (
                            'draft',
                            'pending_approval',
                            'confirmed',
                            'published',
                            'rejected'
                        )),
    published_at        TIMESTAMPTZ,
    decided_by          UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS workflow_proposals_tenant_status_created_idx
    ON workflow_proposals (tenant_id, status, created_at DESC);

CREATE INDEX IF NOT EXISTS workflow_proposals_tenant_slug_idx
    ON workflow_proposals (tenant_id, slug, created_at DESC);

CREATE INDEX IF NOT EXISTS workflow_proposals_session_idx
    ON workflow_proposals (session_id)
    WHERE session_id IS NOT NULL;
