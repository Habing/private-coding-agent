DROP INDEX IF EXISTS memories_embedding_idx;

ALTER TABLE memories DROP COLUMN IF EXISTS embedding;
