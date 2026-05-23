---
name: workflow-template-authoring
description: Built-in workflow templates, slot filling, and workflow.propose for NL authoring (Slice 19b Path C).
---

# Workflow Template Authoring (Path C)

Use this SKILL when the user wants a **repeatable automation** (weekly report,
notifications, webhooks, tool chains) — prefer templates over hand-written DSL.

## When to use `workflow.propose` vs `workflow.create`

| Situation | Tool |
|-----------|------|
| Matches a built-in template (schedule, notify, summarize, chain) | `workflow.propose` with `template_id` + `slots` |
| User gave natural language; template likely fits | `workflow.propose` with `user_message` + `slug` + `name` |
| Complex custom DAG, no template fits | `workflow.create` / `workflow.update` (Path B) + read workflow-dsl-authoring SKILL |

**Never call `workflow.publish`** from this profile — admin confirms in UI or REST.

## Notify / forward tools (Slice 25b)

For `notify_tool` / `forward_tool` slots, prefer **installed connectors** over `llm.chat`:

1. Check `GET /tools` for `mcp.<slug>.<tool>` (Slack/GitHub MCP) or `http.fetch`.
2. Admin catalog: `GET /admin/connectors/catalog` lists recipes + install status.
3. Example notify: `"notify_tool": "mcp.slack.post_message"` with args per MCP schema.
4. Fallback when no connector installed: `"notify_tool": "llm.chat"` as in examples below.

## Built-in templates (GET /agent/workflow/templates)

| template_id | Use when |
|-------------|----------|
| `cron-notify` | 定时 / cron / 每周 / 工作日 + 通知 |
| `webhook-forward` | Webhook / 回调 / 转发 |
| `http-fetch-notify` | HTTP 拉取 + 通知 |
| `llm-summarize-notify` | LLM 摘要 + 通知 |
| `tool-chain` | 顺序调用 2–3 个 ToolBus 工具 |

## `workflow.propose` examples

Template + slots:

```json
{
  "slug": "weekly-summary",
  "name": "每周摘要",
  "template_id": "llm-summarize-notify",
  "slots": {
    "prompt": "Summarize open tasks",
    "model": "default-mock:text",
    "notify_tool": "llm.chat",
    "notify_args": {
      "model": "default-mock:text",
      "messages": [{ "role": "user", "content": "weekly digest" }]
    }
  }
}
```

Natural language (server classifies template):

```json
{
  "slug": "monday-report",
  "name": "周一报告",
  "user_message": "每周一 9 点用 LLM 汇总待办并通知团队"
}
```

Response includes `proposal_id`, `dry_run_ok`, `summary`. Tell the user to confirm
in the chat card (admin) or submit for approval (member).

## After propose

1. If `dry_run_ok` is false, fix slots/DSL and call `workflow.propose` again.
2. If ok, ask the user to click **确认发布** (admin) or **提交审批** (member).
3. Do not re-create the same slug unless the user asks to replace the draft.
