-- Runtime-editable http.fetch host allowlist (admin WebUI, Slice 25c).
CREATE TABLE IF NOT EXISTS http_fetch_settings (
    id           TEXT PRIMARY KEY,
    allow_hosts  TEXT[] NOT NULL DEFAULT '{}',
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO http_fetch_settings (id, allow_hosts)
VALUES ('global', '{}')
ON CONFLICT (id) DO NOTHING;
