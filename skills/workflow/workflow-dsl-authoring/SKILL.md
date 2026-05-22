---
name: workflow-dsl-authoring
description: Workflow Engine DSL grammar, available tools, and admin-tool usage rules. Read this before calling workflow.create or workflow.update.
---

# Workflow DSL Authoring

This SKILL documents the YAML DSL accepted by `workflow.create` and
`workflow.update` in this platform. If you write DSL without consulting this
spec, you WILL get field names wrong — they do not match common workflow
engines (it is not GitHub Actions, not Argo, not n8n).

## Hard rules — these are the top mistakes to avoid

1. **Tool argument key is `args:`, NOT `with:`.**
2. **Tool name is `shell.exec` (with the dot), NOT `shell`.** Same for
   `fs.read` / `fs.write` / `fs.list` / `fs.glob` / `llm.chat` / `llm.embed`
   / `memory.save` / `memory.search` / `memory.list` / `memory.delete`.
3. **Step output is read as `${steps.<id>.output.<field>}`, NOT
   `${steps.<id>.<field>}`.** There is a literal `.output.` between the step
   id and the data path.
4. **The DSL's top-level `id:` field MUST equal the slug** you pass to
   `workflow.create`. They must be byte-equal kebab-case.
5. **shell.exec / fs.* / grep all require a `sandbox_id` UUID** in `args`.
   Workflows have no implicit sandbox. The caller must pass `sandbox_id` as
   an `inputs:` value and you forward it via `${inputs.sandbox_id}`.
6. **Publish path is `POST /admin/workflows/<slug>/publish`** — no `/api`
   prefix. You cannot publish yourself; you can only `workflow.create` /
   `workflow.update`. Tell the user to publish via admin REST.

## Skeleton

```yaml
id: my-slug             # MUST equal the slug arg to workflow.create
name: "Human name"
description: "What this workflow does"

inputs:
  some_input:
    type: string        # string | int | number | bool | object | array
    default: "x"        # optional; if omitted the input is required

steps:
  - id: step1
    use: llm.chat       # tool node — `use:` triggers tool dispatch
    args:               # NOT `with:`
      model: "default-mock:text"
      messages:
        - { role: user, content: "Say hi" }
    timeout: 30s        # optional, default 60s
    on_error: fail      # fail | continue

  - id: pick
    assign:             # assign node — set variables from expressions
      reply: ${steps.step1.output.choices[0].message.content}

outputs:                # final result of the workflow
  answer: ${vars.reply}
```

## The 6 node kinds

A node's kind is inferred from which top-level field is set. Setting more
than one of these on the same step is a validate error.

| Trigger field | Kind | What it does |
|---|---|---|
| `use:` | tool | Calls a Tool Bus tool with the value of `args:` as input |
| `assign:` | assign | Map of `{var: ${expr}}` — writes evaluated values into `vars` |
| `if:` | if | `if: <bool expr>` with `then: [steps]` and optional `else: [steps]` |
| `foreach:` | foreach | `foreach: <list expr>`, `as: <name>`, `steps: [...]`; each iter sets `vars[<name>]=item` |
| `parallel:` | parallel | `parallel: [[branch1...], [branch2...]]` — branches run concurrently, first error cancels siblings |
| `wait:` | wait | `wait: 100ms` — ctx-aware sleep |

## Expression language

- `${inputs.<key>}` — caller-supplied input
- `${vars.<key>}` — value set by an `assign` node
- `${steps.<id>.output}` — full output of a previous tool step (any type)
- `${steps.<id>.output.<path>.<deep>}` — drill into JSON output
- `${steps.<id>.error}` — error message if that step failed with `on_error: continue`
- Operators: `==  !=  <  >  <=  >=  &&  ||  !` (no parens, no arithmetic, no string functions)
- A single `${expr}` preserves the underlying type (number stays number, list stays list); strings with mixed literal+expr concatenate via `fmt.Sprint`.

## Available tools (full list — these are valid `use:` values)

| Name | Needs sandbox_id | Output key fields |
|---|---|---|
| `shell.exec` | yes | `exit_code`, `stdout`, `stderr`, `timed_out`, `duration_ms` |
| `fs.read` | yes | `content`, `size` |
| `fs.write` | yes | `bytes_written` |
| `fs.list` | yes | `entries` (list of `{name,type,size}`) |
| `fs.glob` | yes | `matches` (list of paths) |
| `grep` | yes | `matches` (list of strings) |
| `llm.chat` | no | OpenAI-shape `{choices:[{message:{content,role}}], usage:{...}}` |
| `llm.embed` | no | `{data:[{embedding:[...]}], usage:{...}}` |
| `memory.save` | no | `{id, created}` |
| `memory.search` | no | `{items:[{id,content,score,...}]}` |
| `memory.list` | no | `{items:[...]}` |
| `memory.delete` | no | `{deleted}` |
| `agent.delegate` | inherits | `{result, sub_steps, status, sub_tool_calls}` |
| `workflow.<other-slug>` | depends | Whatever that workflow's `outputs:` is |

`workflow.create`, `workflow.update`, `workflow.list`, `workflow.get` are
admin tools for editing workflows themselves — do NOT use them as `use:`
inside a DSL. They are only callable by you, the Agent, directly.

## Two good examples — copy these patterns

**Example A — pure memory + llm.chat (no sandbox needed):**

```yaml
id: summarize-memory
name: "Summarize relevant memories"
inputs:
  query: { type: string }
steps:
  - id: search
    use: memory.search
    args:
      query: ${inputs.query}
      top_k: 5
  - id: ask
    use: llm.chat
    args:
      model: "default-mock:text"
      messages:
        - { role: system, content: "Summarize the following memory hits." }
        - { role: user, content: "Hits: ${steps.search.output.items}" }
outputs:
  summary: ${steps.ask.output.choices[0].message.content}
```

**Example B — shell.exec (needs sandbox_id input):**

```yaml
id: lint-check
name: "Run make lint"
description: "Runs `make lint` in a caller-provided sandbox and surfaces the exit code."
inputs:
  sandbox_id: { type: string }     # caller must pass a real sandbox UUID
steps:
  - id: lint
    use: shell.exec
    args:
      sandbox_id: ${inputs.sandbox_id}
      cmd: ["make", "lint"]        # cmd is a string array, not a single command string
      timeout_sec: 60
outputs:
  exit_code: ${steps.lint.output.exit_code}
  stderr: ${steps.lint.output.stderr}
```

## When the user asks "create a workflow"

1. Call `workflow.list` first to check if the slug is taken.
2. If they want to modify an existing workflow, call `workflow.get` to read
   the current DSL before composing the update.
3. Write the DSL following the spec above. Re-check: `args:` not `with:`,
   dotted tool names, `${steps.X.output.Y}` paths.
4. Call `workflow.create` (or `workflow.update`).
5. The response is `{ok:true, slug, version, published:false}`. The
   workflow is **not** live yet.
6. Tell the user the publish command, exactly:
   `POST /admin/workflows/<slug>/publish` with an admin Bearer token. No
   `/api` prefix, no other paths.

Do not invent paths. Do not claim you can publish — you cannot.
