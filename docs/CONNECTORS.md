# Connectors（Slice 25）

Server 侧连接器让 Agent 访问外部系统，**沙箱无需出网**。

| 类型 | 机制 | 配置 |
|------|------|------|
| MCP | Admin 注册 MCP 服务 → ToolBus `mcp.<slug>.<tool>` | Web UI **连接器** / **MCP 服务** |
| HTTP | `http.fetch` 工具 | `config.connectors.http_fetch` |

Web UI：**连接器**（`/admin/connectors`）展示内置 recipe 与安装状态；工作流模板市场的「通知工具」槽位可从已注册工具中选择。

---

## Slack

1. **Compose 生产示例**：[`deploy/compose/examples/slack-mcp.production.yaml`](../deploy/compose/examples/slack-mcp.production.yaml) + [README](../deploy/compose/examples/README.md)（sidecar + PCA 注册表）。
2. 部署兼容 [MCP](https://modelcontextprotocol.io/) 的 Slack 桥接服务（需 **JSON-RPC HTTP POST**，与 PCA `mock-mcp` 相同传输；SSE-only 服务需额外网关）。
3. 在 **连接器 → Slack → 安装 MCP** 或 **MCP 服务** 中创建：
   - **slug:** `slack`（与目录 recipe 一致，便于模板推荐）
   - **url:** sidecar 示例 `http://slack-mcp:8084/`；或 Slack 托管 `https://mcp.slack.com/mcp`（按工作区策略）
   - **auth:** Bearer（`SLACK_MCP_API_KEY` 或 Slack OAuth token，按 MCP 服务文档）
4. 刷新工具列表后，工作流模板 `notify_tool` 可选 `mcp.slack.post_message` 等（以 MCP 实际 `tools/list` 为准）。
5. `notify_args` 按 MCP 工具的 `inputSchema` 填写，例如频道与正文。

---

## GitHub

1. 部署 GitHub MCP HTTP 服务。
2. 注册 **slug:** `github`，URL 与 token 按服务文档配置。
3. 模板转发/通知槽位可选用 `mcp.github.create_issue` 等。

---

## HTTP 拉取（http.fetch）

无需 MCP。在 `config.yaml`：

```yaml
connectors:
  http_fetch:
    enabled: true
    allow_hosts:
      - "*.example.com"
    block_private_ips: true
```

Agent 或工作流步骤使用 `http.fetch`，参数见 ToolBus schema。

---

## Dev Mock（Compose E2E）

Compose 内置 `mock-mcp`（`:8083`，工具 `echo`）。

- **slug:** `e2e-mock`
- **url:** `http://mock-mcp:8083`（server 容器内）
- Bus 工具名：`mcp.e2e-mock.echo`

E2E step 63 注册该服务并验证 connector catalog 中 **dev-mock** 为已安装。

示例 `cron-notify` 槽位：

```json
{
  "notify_tool": "mcp.e2e-mock.echo",
  "notify_args": { "text": "weekly report ready" }
}
```

---

## 安全

- 沙箱默认 `internal` 网络，禁止 `curl` 出网 — 见 [`SECURITY-SANDBOX.md`](SECURITY-SANDBOX.md) §4。
- `http.fetch` 使用 host 白名单 + 可选 SSRF（`block_private_ips`）。
- MCP token 存 DB，API 响应中 redact 为 `***`。
