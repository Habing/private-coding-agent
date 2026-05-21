# 接入阿里云百炼 Qwen（OpenAI 兼容模式）

## 1. 获取 API Key

1. 登录 [阿里云百炼控制台](https://bailian.console.aliyun.com/)
2. 创建 **API-KEY**（中国内地账号使用北京地域 endpoint，见下）

## 2. 配置环境变量

在 `deploy/compose/.env` 中设置（不要提交到 Git）：

```env
DASHSCOPE_API_KEY=sk-xxxxxxxx
PCA_MEMORY_EMBEDDING_MODEL=dashscope:text-embedding-v4
```

`deploy/compose/.env.example` 已包含占位项。`docker-compose.yml` 在未设置时也会默认 `dashscope:text-embedding-v4`。

## 3. 启动 / 重建服务

迁移 `0012` 会在 server 启动时注册 provider `dashscope`：

| 字段 | 值 |
|------|-----|
| base_url | `https://dashscope.aliyuncs.com/compatible-mode`（Gateway 请求 `/v1/chat/completions`、`/v1/embeddings`） |
| api_key_env | `DASHSCOPE_API_KEY` |

```powershell
cd deploy/compose
docker compose up -d --build postgres redis mock-provider server
```

## 4. 使用模型

请求里使用 **`provider:model`** 格式：

```text
dashscope:qwen3.6-plus
```

Web UI 新建会话已默认该模型（需重建 server 镜像后生效）。

也可用快照版：`dashscope:qwen3.6-plus-2026-04-02`

## 5. 验证

```powershell
# 登录后
curl.exe -fsS -X POST http://localhost:8080/v1/chat/completions `
  -H "Authorization: Bearer <TOKEN>" `
  -H "Content-Type: application/json" `
  -d "{\"model\":\"dashscope:qwen3.6-plus\",\"messages\":[{\"role\":\"user\",\"content\":\"你好，用一句话介绍你自己\"}]}"
```

## 6. 记忆向量（text-embedding-v4）

| 配置项 | 值 |
|--------|-----|
| `PCA_MEMORY_EMBEDDING_MODEL` | `dashscope:text-embedding-v4` |
| 输出维度 | **1536**（Gateway 自动带 `dimensions=1536`，与 DB `vector(1536)` 一致） |

验证 embedding：

```powershell
curl.exe -fsS -X POST http://localhost:8080/v1/embeddings `
  -H "Authorization: Bearer <TOKEN>" `
  -H "Content-Type: application/json" `
  -d "{\"model\":\"dashscope:text-embedding-v4\",\"input\":[\"测试\"],\"dimensions\":1536}"
```

返回向量的 `length` 应为 **1536**。

**注意**：从 mock 向量切到百炼后，旧记忆的 embedding 与新区不兼容，语义检索会变差。可清空 `memories` 表或只保留新写入的记忆。

## 7. 说明

- **E2E / 离线测试**：在 `.env` 设 `PCA_MEMORY_EMBEDDING_MODEL=default-mock:text`，对话仍用 `default-mock:gpt-4o`。
- 国际站账号请改用 `dashscope-intl.aliyuncs.com` endpoint，并修改 DB 中 `providers.base_url` 或新增一条 provider。
