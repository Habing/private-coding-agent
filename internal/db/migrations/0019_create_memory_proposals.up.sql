-- Slice 20 (Reflection Agent): memory proposals — extracted by the Reflector
-- after a session is archived, queued for admin review.
--
-- status transitions:
--   pending → approved | rejected   (admin POST decision)
--   pending → auto_approved         (confidence ≥ auto_approve_threshold; set
--                                    inline at insert time)
-- memory_id is filled when status becomes approved or auto_approved and points
-- at the memories row that resulted from memory.Service.Create (either freshly
-- inserted or returned by the 0.92 dedup hit). Intentionally NOT a FK so a
-- later memory delete still leaves the proposal audit trail intact.

CREATE TABLE IF NOT EXISTS memory_proposals (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    owner_user_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    session_id      UUID REFERENCES sessions(id) ON DELETE SET NULL,
    type            TEXT NOT NULL CHECK (type IN ('profile','preference','knowledge','lesson')),
    content         TEXT NOT NULL,
    tags            TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    confidence      REAL NOT NULL CHECK (confidence >= 0 AND confidence <= 1),
    status          TEXT NOT NULL DEFAULT 'pending'
                    CHECK (status IN ('pending','approved','auto_approved','rejected')),
    memory_id       UUID,
    decided_at      TIMESTAMPTZ,
    decided_by      UUID,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS memory_proposals_tenant_status_created_idx
    ON memory_proposals (tenant_id, status, created_at DESC);

CREATE INDEX IF NOT EXISTS memory_proposals_owner_status_idx
    ON memory_proposals (owner_user_id, status);
