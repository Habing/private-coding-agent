-- Slice 22a (Audit hash chain): make audit_log tamper-evident by chaining
-- each row's SHA-256 over the previous row's hash. prev_hash + entry_hash are
-- raw 32-byte values; the application computes and writes both under a single
-- pg_advisory_xact_lock so concurrent Append calls can never fork the chain.
--
-- Existing rows ("pre-chain") get the all-zero default and are reported as a
-- chain prefix the verifier explicitly skips. The first new Append after the
-- migration starts the chain from the genesis (32 zero bytes).

ALTER TABLE audit_log
    ADD COLUMN IF NOT EXISTS prev_hash  BYTEA NOT NULL
        DEFAULT '\x0000000000000000000000000000000000000000000000000000000000000000'::bytea,
    ADD COLUMN IF NOT EXISTS entry_hash BYTEA NOT NULL
        DEFAULT '\x0000000000000000000000000000000000000000000000000000000000000000'::bytea;
