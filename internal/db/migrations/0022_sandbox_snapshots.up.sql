-- Slice 22b (Snapshot → MinIO): sandbox_snapshots — per-tenant catalog of
-- container snapshots persisted to S3-compatible object storage.
--
-- Snapshot lifetime is decoupled from session lifetime: ON DELETE SET NULL on
-- session_id so destroying a sandbox does NOT remove the snapshot row — the
-- object stays in MinIO and the metadata stays here. object_key is the full
-- S3 path including tenant_id/session_id segments (set at snapshot creation
-- time so a future bucket policy can scope tenant access by key prefix).
--
-- size_bytes is the uploaded tar size returned by minio-go UploadInfo.Size.
-- image_ref is the docker image tag (pca-snapshot-<sessionID>:<unix_ts>) that
-- the driver committed before exporting; cleared once ImageRemove succeeds.

CREATE TABLE IF NOT EXISTS sandbox_snapshots (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    session_id  UUID REFERENCES sandbox_sessions(id) ON DELETE SET NULL,
    object_key  TEXT NOT NULL,
    size_bytes  BIGINT NOT NULL,
    image_ref   TEXT NOT NULL DEFAULT '',
    metadata    JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS sandbox_snapshots_tenant_created_idx
    ON sandbox_snapshots (tenant_id, created_at DESC);

CREATE INDEX IF NOT EXISTS sandbox_snapshots_session_idx
    ON sandbox_snapshots (session_id) WHERE session_id IS NOT NULL;
