-- Slice 17 (Skills 12b): tenant-scoped Skill storage + per-profile binding.
-- skill_key is the same logical identifier namespace as the filesystem
-- registry (`name` in SKILL.md frontmatter). For a given tenant, the DB row
-- shadows the filesystem skill with the same key.

CREATE TABLE IF NOT EXISTS skills (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    skill_key    TEXT NOT NULL,
    description  TEXT NOT NULL DEFAULT '',
    body         TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    enabled      BOOLEAN NOT NULL DEFAULT true,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, skill_key)
);

CREATE INDEX IF NOT EXISTS skills_tenant_enabled_idx
    ON skills (tenant_id) WHERE enabled = true;

-- tenant_profile_skills binds a (tenant, profile) pair to an ordered list of
-- skill_keys, overriding the in-code Profile.SkillIDs. A row exists only when
-- a tenant has customised that profile; absence means fall through to code.
CREATE TABLE IF NOT EXISTS tenant_profile_skills (
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    profile_name TEXT NOT NULL,
    skill_key    TEXT NOT NULL,
    sort_order   INT  NOT NULL DEFAULT 0,
    PRIMARY KEY (tenant_id, profile_name, skill_key)
);

CREATE INDEX IF NOT EXISTS tenant_profile_skills_lookup_idx
    ON tenant_profile_skills (tenant_id, profile_name, sort_order);
