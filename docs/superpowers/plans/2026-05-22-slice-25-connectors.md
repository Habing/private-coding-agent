# Slice 25 — Connectors Implementation Plan

> **Goal:** Server 侧连接器，Agent 不依赖沙箱出网；首交付 `http.fetch`（25a）。

**Design:** [`specs/2026-05-22-slice-25-connectors-design.md`](../specs/2026-05-22-slice-25-connectors-design.md)

**Depends on:** ToolBus ✅、MCP manager ✅（21b，后续 Slack 等）

---

## 25a — `http.fetch` ✅

- [x] `internal/toolbus/tools/http_fetch.go` + tests
- [x] `config.connectors.http_fetch` + `config.example.yaml`
- [x] Register in `cmd/server/main.go` when enabled
- [x] `coding` profile allowlist + system prompt hint
- [x] `mutating_test` — non-mutating
- [x] Compose dev defaults + E2E step 13
- [x] Design spec (this plan)

## 25b — Catalog + workflow notify ✅

- [x] `GET /admin/connectors/catalog` — static recipes + MCP/http.fetch status
- [x] Web UI `/admin/connectors` — recipe cards + MCP install flow
- [x] Template market `tool_picker` + `ToolBusToolSelect` for notify/forward slots
- [x] `SlotSpec.suggested_tools` on notify templates
- [x] `docs/CONNECTORS.md` — Slack / GitHub / mock recipes
- [x] E2E step 63 — catalog shows dev-mock after MCP register

---

## Operator quick start

```yaml
connectors:
  http_fetch:
    enabled: true
    allow_hosts:
      - "*.baidu.com"
      - "api.github.com"
    block_private_ips: true
```

Agent prompt example: 「用 http.fetch 拉取 https://news.baidu.com/ …」

**Do not** enable sandbox `bridge` network in production for web access; use server-side tools.
