# Slice 19b — NL Workflow Authoring (B+C) Implementation Plan

> **Goal:** 全程自然语言建工作流——**模板填槽（C）优先 + 自由生成（B）兜底**；Dry-Run 预览；对话内确认发布。E2E **70–75** 步。

**Design:** [`specs/2026-05-23-slice-19b-nl-workflow-authoring-design.md`](../specs/2026-05-23-slice-19b-nl-workflow-authoring-design.md)

**Depends on:** Slice 19a ✅、Slice 18 ✅、Slice 21 ✅

**Estimated effort:** 2.5–3.5 人周

**纪律：** 每完成一个 Task → `go test` 相关包 → 同步 `WORKFLOW.md` / `SLICE-VERIFICATION.md` / 本 plan 勾选 → **单独 commit**（不与其他 Task 混提交）。

---

## Context

19a 交付 Workflow Engine；用户仍须手写 YAML 或让 Agent 起草后 **手动 REST 发布**。主 spec P1「NL → Workflow 全流程」缺 **确认 UI + publish 工具 + 模板层**。

本切片闭环 B+C：

1. **C：** `internal/workflow/template` 模板 registry + slot 渲染 + NL 填槽
2. **B：** 现有 `workflow-authoring` + `workflow.create` 作为 fallback
3. **统一：** `workflow.proposals` 表 + Dry-Run + 对话卡片确认 + `workflow.publish` 工具
4. **路由：** orchestrator 规则识别「建自动化」意图

---

## Goal（交付清单）

- [x] `internal/db/migrations/0024_workflow_proposals.{up,down}.sql` *(Task 1)*
- [x] `internal/workflow/template/` — catalog、render、slot validate、NL extractor *(Task 2)*
- [x] `internal/workflow/proposal_*.go` — Repo + Service（create / dry_run / confirm / approve）*(Task 1)*
- [ ] `internal/workflow/tools_publish.go` — `workflow.publish`、`workflow.propose` 工具
- [ ] `internal/agent/handler.go` — `GET /agent/workflow/templates`、`POST/GET /agent/workflow/proposals[/:id]`、`POST .../confirm`
- [ ] `internal/workflow/handler.go` — `POST /admin/workflow/proposals/:id/approve`
- [ ] Orchestrator 规则 `nl-workflow-author`（config example + E2E marker）
- [ ] Web UI：`WorkflowProposalCard` + 聊天流嵌入；`profileLabels` / 中文文案
- [ ] Skill 更新：`skills/workflow/workflow-dsl-authoring/SKILL.md` + 新 `skills/workflow/workflow-template-authoring/SKILL.md`
- [ ] mock-provider markers：`E2E_WF_TEMPLATE_V1`、`E2E_WF_PROPOSAL_V1`
- [ ] E2E 步骤 **70–75**；`docs/WORKFLOW.md` §8；`SLICE-VERIFICATION.md` 新行

---

## 非目标（明确出栈）

- cron/webhook `triggers:` DSL 段（Slice 24）
- MCP Slack/GitHub 连接器（Slice 25；模板 v1 用 `llm.chat` + mock 或已有 tool 名占位）
- 流程图可视化编辑器
- 模板跨租户市场
- member 绕过审批直接 publish
- 自动 silent publish（无 confirm）

---

## Architecture

```
用户 (coding profile 会话)
  │
  ├─ orchestrator: nl-workflow-author 规则 → hint 注入
  │
  ├─ Path C: agent 调 workflow.propose { template_id, slots }
  │     → template.Render(slots) → dsl_yaml
  │     → proposal.Service.Create → Service.Invoke(dry_run=true)
  │     → 返回 proposal_id + 中文摘要
  │
  ├─ Path B: agent.delegate(workflow-authoring) → workflow.create
  │     → 父 agent 调 workflow.propose { dsl_yaml, slug, name }
  │
  ▼
Web 聊天 WorkflowProposalCard
  ├─ admin: POST /agent/workflow/proposals/:id/confirm
  │     → workflow.publish(proposal) → Bus.Register
  └─ member: confirm → status=pending_approval
        → admin POST /admin/workflow/proposals/:id/approve → publish
```

---

## Task 1 — Migration + Proposal model

**Status:** ✅ 2026-05-23 — `go test ./internal/workflow/...` PASS

**Files:**

- `internal/db/migrations/0024_workflow_proposals.up.sql`
- `internal/db/migrations/0024_workflow_proposals.down.sql`
- `internal/workflow/proposal_types.go`
- `internal/workflow/proposal_repo.go`
- `internal/workflow/proposal_service.go`
- `internal/workflow/proposal_test.go`

**Schema:** 见 design §4。

**Tests:**

- Create proposal → dry_run_ok=true/false 分支
- Confirm admin → workflows 行 + published=true + bus 含 tool
- Member confirm → pending；approve → published
- Expired / rejected 不可 publish

---

## Task 2 — Template catalog (Path C)

**Status:** ✅ 2026-05-23 — 5 模板 render 单测 + `Validate` PASS

**Files:**

- `internal/workflow/template/catalog.go` — 注册 5 个内置模板
- `internal/workflow/template/render.go` — slots → DSL YAML
- `internal/workflow/template/slots.go` — JSON schema 校验
- `internal/workflow/template/extract.go` — LLM 结构化填槽（`llm.chat` + JSON mode / mock）
- `internal/workflow/template/catalog_test.go`
- `templates/workflow/*.yaml.tmpl` — 模板文件（embed）

**内置模板 v1：**

| ID | 渲染结果要点 |
|----|-------------|
| `cron-notify` | assign 占位 schedule 文案 + `use: ${notify_tool}` |
| `webhook-forward` | comment 触发器占位 + forward tool 链 |
| `http-fetch-notify` | `llm.chat` 或 HTTP 通过 `shell.exec curl`（文档标注 mutating 慎用）→ notify |
| `llm-summarize-notify` | `llm.chat` → assign → notify tool |
| `tool-chain` | 顺序 2–3 个 `use:` 节点 |

**Extract 流程：**

```go
// extract.go — 输入 user NL + template_id，输出 map[string]any slots
// 失败返回 err → 上层 fallback Path B
```

- [x] 每个模板 render 单测（合法 DSL 过 validate）
- [x] extract 规则填槽单测（ClassifyAndExtract）

---

## Task 3 — workflow.propose + workflow.publish tools

**Files:**

- `internal/workflow/tools_publish.go`
- `internal/workflow/tools_publish_test.go`
- 修改 `internal/workflow/tools_admin.go` — `NewAdminTools` 追加 publish/propose

**`workflow.propose` input:**

```json
{
  "slug": "weekly-pr-summary",
  "name": "每周 PR 汇总",
  "description": "可选",
  "template_id": "llm-summarize-notify",
  "slots": { "prompt": "...", "notify_tool": "llm.chat", "notify_args": {} },
  "dsl_yaml": "可选，Path B 直接传"
}
```

**`workflow.publish` input:**

```json
{ "proposal_id": "uuid" }
```

或 `{ "slug": "..." }`（已 dry_run 的 draft workflow，admin 快捷发布）。

- [ ] admin gate 与 create 一致
- [ ] publish 前断言 proposal.dry_run_ok
- [ ] 审计 `workflow.proposal.confirm` + 现有 publish audit

---

## Task 4 — HTTP handlers

**Files:**

- `internal/agent/workflow_handler.go` — templates + proposals CRUD/confirm
- `internal/workflow/proposal_handler.go` — admin approve
- `cmd/server/main.go` — 路由挂载

| 路由 | Auth |
|------|------|
| `GET /agent/workflow/templates` | Bearer |
| `POST /agent/workflow/proposals` | Bearer |
| `GET /agent/workflow/proposals/:id` | Bearer（同 tenant） |
| `POST /agent/workflow/proposals/:id/confirm` | Bearer |
| `POST /admin/workflow/proposals/:id/approve` | admin |

**POST /agent/workflow/proposals body 两种模式：**

1. `{ "user_message": "每周一..." }` — 服务端 classify template + extract + render
2. `{ "template_id", "slots", "slug", "name" }` — Agent 显式填槽

- [ ] handler 单测：401/403/404/409
- [ ] classify：关键词 + template 描述 embedding（v1 可用 rules，embedding 推 P2）

---

## Task 5 — Profile + Skill + Orchestrator

**Files:**

- `internal/agent/profile.go` — `workflow-authoring` ToolAllowlist 加 `workflow.propose`（不含 publish）
- `coding` profile 加 `workflow.propose` + 已有 delegate
- `skills/workflow/workflow-template-authoring/SKILL.md` — 模板 catalog、填槽规则、何时 delegate B
- `config.example.yaml` — orchestrator 规则 `nl-workflow-author`
- `internal/modelgw/mockserver/main.go` — E2E markers

**workflow-authoring system prompt 追加：**

> 优先查阅 template catalog；匹配则 workflow.propose(template_id=...)；否则 workflow.create。不要尝试 publish。

- [ ] `GET /agent/profiles` 不变（4 项）
- [ ] orchestrator E2E marker 步骤

---

## Task 6 — Web UI（对话内确认）

**Files:**

- `internal/webui/src/components/WorkflowProposalCard.tsx`
- `internal/webui/src/pages/Home.tsx` — 解析 SSE/WS 事件 `workflow_proposal`（或轮询 proposal_id）
- `internal/webui/src/lib/workflowProposalLabels.ts`
- `internal/webui/src/components/WorkflowProposalCard.test.tsx`

**卡片内容：**

- 标题：`工作流草案：{name}`
- 来源 badge：`模板 · cron-notify` / `自由生成`
- 步骤摘要（从 DSL 解析 id+use，或后端 `summary` 字段）
- Dry-Run：`✓ 模拟通过` / `✗ 失败：{error}`
- 按钮：确认发布 | 提交审批 | 在工作流页编辑

**事件协议（agent.run SSE 扩展）：**

```json
{ "type": "workflow_proposal", "proposal_id": "...", "summary": "..." }
```

- [ ] admin 确认 → invalidate queries `workflows` + toast
- [ ] vitest 覆盖按钮可见性（admin vs member）

---

## Task 7 — E2E（70–75）

**File:** `deploy/compose/test-e2e.sh`

| 步 | 场景 |
|----|------|
| 70 | `GET /agent/workflow/templates` ≥5 项 |
| 71 | `POST /agent/workflow/proposals` template=`llm-summarize-notify` + slots → dry_run_ok |
| 72 | admin `confirm` → `GET /tools` 含 `workflow.e2e-nl-template` |
| 73 | member 创建 proposal → confirm → status pending；admin approve → published |
| 74 | `agent.run` + `E2E_WF_PROPOSAL_V1` → tool_call `workflow.propose` → SSE `workflow_proposal` |
| 75 | Path B：`E2E_WF_FREEFORM_V1` → delegate/workflow.create → propose → confirm |

更新所有 `[n/69]` → `[n/75]` 计数。

---

## Task 8 — 文档

- [ ] `docs/WORKFLOW.md` — 新增 §8「自然语言建流（B+C）」
- [ ] `docs/SLICE-VERIFICATION.md` — Slice 19b 行
- [ ] `HANDOFF.md` — 19b 状态
- [ ] `README.md` — 切片进度（可选勾选 19b）

---

## Verification

```bash
go test ./internal/workflow/... -count=1
go test ./internal/agent/... -count=1
cd internal/webui && npm test -- --run
go build ./...
./deploy/compose/test-e2e.sh   # 75/75
```

---

## Acceptance checklist

- [ ] 用户仅对话即可生成 proposal（C 或 B）
- [ ] 发布前必有 Dry-Run 成功记录
- [ ] admin 对话内 confirm 后 `workflow.<slug>` 立即可 invoke
- [ ] member 不可直接 publish；审批链 audit 完整
- [ ] 模板渲染 DSL 100% 过 validate（单测覆盖）
- [ ] orchestrator 规则 + E2E marker 证明 hint 注入
- [ ] 中文 UI 卡片 + 工具箱/workflow 页描述一致

---

## 与后续切片接口

| 切片 | 接口 |
|------|------|
| **Slice 24 Triggers** | proposal 卡片展示 cron/webhook；模板加 `triggers:` 段 |
| **Slice 25 Connectors** | 模板 slot `notify_tool` 默认 `mcp.slack.*` |
| **Slice 19c（可选）** | 模板市场、版本 diff |

---

## 实施顺序建议

1. Task 1 Migration + proposal service  
2. Task 2 Template catalog（C 核心）  
3. Task 3 publish/propose tools  
4. Task 4 HTTP handlers  
5. Task 7 E2E 70–73（后端闭环）  
6. Task 5 Skill + orchestrator  
7. Task 6 Web UI 卡片  
8. Task 7 E2E 74–75 + Task 8 文档  

---

## 人工冒烟脚本（compose 试点）

```bash
TOK=$(curl -s -X POST http://localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"tenant":"default","email":"demo@example.com","password":"demo123"}' \
  | jq -r .token)

# 模板路径
curl -X POST http://localhost:8080/agent/workflow/proposals \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{
    "slug": "weekly-summary",
    "name": "每周摘要",
    "template_id": "llm-summarize-notify",
    "slots": {
      "prompt": "Summarize open tasks",
      "model": "default-mock:text",
      "notify_tool": "llm.chat",
      "notify_args": { "model": "default-mock:text", "messages": [{"role":"user","content":"ping"}] }
    }
  }'

# 取 proposal_id → confirm → invoke workflow.weekly-summary
```
