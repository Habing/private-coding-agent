# mcp-data-prep

HTTP MCP server for the **`data-prep`** domain: read files from inbox, **AI denoise** via LLM (or mock), write to outbox.

Design: [`docs/domains/data-prep.md`](../../../docs/domains/data-prep.md)

## Tools

| Tool | MCP name |
|------|----------|
| Read inbox file | `load_file` |
| AI clean records | `ai_denoise_records` |
| Load + AI (one step) | `ai_denoise_file` |
| Before/after stats | `summarize_run` |
| Write outbox | `write_file` |

Bus names: `mcp.data-prep.<tool>` after PCA registration (`slug=data-prep`).

## Run locally

```bash
cd examples/mcp-services/data-prep
go test ./...
DATA_PREP_INBOX=../../../deploy/compose/data-prep/inbox \
DATA_PREP_OUTBOX=../../../deploy/compose/data-prep/outbox \
DATA_PREP_MOCK_LLM=true \
go run .
```

## Compose

```bash
cd deploy/compose
docker compose up -d --build mcp-data-prep
curl -fsS http://localhost:8085/healthz
```

Register in PCA (admin JWT):

```json
{
  "slug": "data-prep",
  "name": "Data prep (AI)",
  "url": "http://mcp-data-prep:8085/",
  "auth_type": "none"
}
```

Refresh tools, then invoke `mcp.data-prep.ai_denoise_file` or publish workflow `data-prep-pipeline` (see `docs/domains/data-prep.md`).

## LLM

| Env | Default |
|-----|---------|
| `LLM_BASE_URL` | DashScope compatible endpoint |
| `LLM_API_KEY` | — (empty → **mock** mode) |
| `LLM_MODEL` | `qwen-plus` |
| `DATA_PREP_MOCK_LLM` | `true` forces mock |

Mock mode: drop `noise` rows, dedupe by `id`, no external API.

## Sample pipeline (curl)

After compose + PCA registration, from host with token:

```bash
curl -s -X POST http://localhost:8080/tools/invoke \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{
    "tool": "mcp.data-prep.ai_denoise_file",
    "input": {
      "path": "batch.sample.json",
      "format": "json",
      "instructions": "Remove duplicates and noise rows; keep id name amount note"
    }
  }'
```
