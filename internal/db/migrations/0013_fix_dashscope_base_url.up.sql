-- Fix double /v1/v1/ in upstream paths: base_url must not include the /v1 suffix
-- because OpenAIProvider appends /v1/chat/completions and /v1/embeddings.
UPDATE providers
SET base_url = 'https://dashscope.aliyuncs.com/compatible-mode',
    updated_at = now()
WHERE name = 'dashscope'
  AND base_url = 'https://dashscope.aliyuncs.com/compatible-mode/v1';
