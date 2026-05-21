-- Alibaba Cloud Model Studio (DashScope) OpenAI-compatible endpoint.
-- Chat model id: qwen3.6-plus (see Aliyun docs for region-specific snapshots).
-- API key: set env DASHSCOPE_API_KEY on the server process (compose: .env).
INSERT INTO providers (name, type, base_url, api_key_env)
VALUES (
    'dashscope',
    'openai',
    'https://dashscope.aliyuncs.com/compatible-mode',
    'DASHSCOPE_API_KEY'
)
ON CONFLICT (name) DO UPDATE SET
    type = EXCLUDED.type,
    base_url = EXCLUDED.base_url,
    api_key_env = EXCLUDED.api_key_env,
    enabled = TRUE,
    updated_at = now();
