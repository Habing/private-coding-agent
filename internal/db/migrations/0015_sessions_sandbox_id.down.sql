DROP INDEX IF EXISTS sessions_sandbox_id_idx;
ALTER TABLE sessions DROP COLUMN IF EXISTS sandbox_id;
