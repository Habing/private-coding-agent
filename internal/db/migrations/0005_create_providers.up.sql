CREATE TABLE providers (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name         TEXT NOT NULL UNIQUE,
    type         TEXT NOT NULL,
    base_url     TEXT NOT NULL,
    api_key_env  TEXT NOT NULL DEFAULT '',
    enabled      BOOLEAN NOT NULL DEFAULT TRUE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX providers_enabled_idx ON providers(enabled) WHERE enabled = TRUE;

INSERT INTO providers (name, type, base_url, api_key_env)
VALUES ('default-mock', 'openai', 'http://mock-provider:8081', '');
