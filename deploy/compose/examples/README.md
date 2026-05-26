# Compose examples

## e2e-mock-chain (P0 validation)

| File | Purpose |
|------|---------|
| [`e2e-mock-chain.yaml`](e2e-mock-chain.yaml) | `fetch_status` → 分支 → `record_event` + `echo` |

Prereq: compose `mock-mcp` + PCA `slug=e2e-mock` → Refresh（≥3 tools）。

```bash
docker compose up -d mock-mcp
# Admin 注册 http://mock-mcp:8083 → 导入 YAML → publish → invoke
# inputs: {"scenario":"degraded"} 或 "ok"
```

See [`docs/NL-WORKFLOW-MCP-VALIDATION.md`](../../../docs/NL-WORKFLOW-MCP-VALIDATION.md).

---

## data-prep MCP (P1 pilot)

| File | Purpose |
|------|---------|
| [`data-prep-pipeline.yaml`](data-prep-pipeline.yaml) | Workflow DSL: load → AI denoise → summarize → write |
| [`examples/mcp-services/data-prep/fixtures/batch.sample.json`](../../../examples/mcp-services/data-prep/fixtures/batch.sample.json) | Sample dirty batch (auto-seeded on first start) |

```bash
cd deploy/compose
docker compose up -d --build mcp-data-prep
curl -fsS http://localhost:8085/healthz
# Optional: copy more files into the named volume inbox
# docker cp my.json $(docker compose ps -q mcp-data-prep):/data/inbox/
```

PCA registration (`slug=data-prep`, `url=http://mcp-data-prep:8085/`, `auth_type=none`) → Refresh tools.

Default **`DATA_PREP_MOCK_LLM=true`** (no API key). Set `LLM_API_KEY` in compose `.env` for real DashScope.

See [`docs/domains/data-prep.md`](../../../docs/domains/data-prep.md) and [`examples/mcp-services/data-prep/README.md`](../../../examples/mcp-services/data-prep/README.md).

---

## Slack MCP (production sidecar)

| File | Purpose |
|------|---------|
| [`slack-mcp.production.yaml`](slack-mcp.production.yaml) | `korotovsky/slack-mcp-server` sidecar (internal `:13080`) |
| [`slack-mcp.env.example`](slack-mcp.env.example) | `SLACK_BOT_TOKEN` + `SLACK_MCP_API_KEY` |

### Quick start

```bash
cd deploy/compose
# Merge slack secrets into your .env (see slack-mcp.env.example)
docker compose \
  -f docker-compose.yml \
  -f examples/slack-mcp.production.yaml \
  up -d slack-mcp server
```

### PCA registration

| Field | Value |
|-------|-------|
| slug | `slack` |
| url | `http://slack-mcp:13080/sse` |
| auth_type | `bearer` |
| auth_token | same as `SLACK_MCP_API_KEY` |

Use **Admin → MCP 服务 → 测试连接**。若失败，见下方「传输兼容」。

启用发消息工具后，刷新工具缓存。工作流 `notify_tool` 通常为 `mcp.slack.conversations_add_message`（以 `tools/list` 为准）。

### Example `cron-notify` slots

```json
{
  "notify_tool": "mcp.slack.conversations_add_message",
  "notify_args": {
    "channel_id": "#alerts",
    "payload": "Weekly report ready",
    "content_type": "text/plain"
  }
}
```

### 传输兼容（重要）

PCA Slice **21b** MCP 客户端使用 **JSON-RPC HTTP POST**（与 compose 内 `mock-mcp` 相同）。

`korotovsky/slack-mcp-server` 默认 `--transport sse`，与 POST 客户端**可能不兼容**。生产可选：

| 方案 | 说明 |
|------|------|
| **A. Slack 托管 MCP** | 注册 `https://mcp.slack.com/mcp` + OAuth（工作区管理员批准）。无需 sidecar。 |
| **B. 本 sidecar** | 内网部署 + Bearer；**必须**通过 MCP「测试连接」验证。 |
| **C. 自研 HTTP MCP** | 参考 `internal/mcp/mockserver` 包装 Slack Web API（POST `/`）。 |

Sidecar **不映射 host 端口**（仅 `expose`），仅 PCA server 容器可访问。

### Slack App 权限（Bot `xoxb-`）

- `chat:write` — 发消息
- `channels:read` — 解析 `#channel` 名（建议开启 user/channel cache）

创建 App：https://api.slack.com/apps → OAuth & Permissions → Install to Workspace → 复制 Bot Token。

---

See [`docs/CONNECTORS.md`](../../../docs/CONNECTORS.md).
