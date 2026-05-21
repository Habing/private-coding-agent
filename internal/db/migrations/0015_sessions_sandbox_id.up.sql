-- Slice 14: bind each session to one sandbox. NULL only for rows created
-- before this migration; new sessions always set sandbox_id at create time.
ALTER TABLE sessions
    ADD COLUMN sandbox_id UUID NULL REFERENCES sandbox_sessions(id) ON DELETE SET NULL;

CREATE INDEX sessions_sandbox_id_idx
    ON sessions(sandbox_id)
    WHERE sandbox_id IS NOT NULL;
