UPDATE providers
SET base_url = 'https://dashscope.aliyuncs.com/compatible-mode/v1',
    updated_at = now()
WHERE name = 'dashscope';
