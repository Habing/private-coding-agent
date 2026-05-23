# Slice 19d — Workflow Visualization（只读流程图）

> **状态**：✅ 已交付（2026-05-24）  
> **依赖**：Slice 19a Engine、19b Web UI、19b NL Authoring  
> **编号说明**：**19c** 保留给可选「模板市场 + 版本 diff」；本切片使用 **19d**，避免与 roadmap 中 19c 含义冲突。

## Context

管理员与对话用户在审阅 workflow DSL 时需要 **结构化的只读视图**（if/foreach/parallel 分支、工具链顺序），但不需要 N8N 式可视化编辑器。本切片把已解析的 DSL 转为 Graph IR，经 REST 暴露，并在 Web UI 渲染为 React Flow 流程图。

**已确认边界：**

1. **只读** — 拖拽/连线编辑不在范围；YAML 仍是唯一编辑面
2. **Graph 来自 Parse** — 可视化不要求 Validate 通过（预览 API 对草稿友好）
3. **无新 E2E 步号** — L1/L2 + 手工冒烟；compose 仍 **75/75**

**非目标：** 可视化编辑器、run 态节点高亮（可后续 19d-v2）、Mermaid 导出、独立 E2E 步骤 76+。

## Delivered

| 组件 | 说明 |
|------|------|
| `internal/workflow/graph.go` | `GraphFromYAML` / `GraphFromDoc` → nodes + edges（sequential / branch / parallel） |
| `internal/workflow/graph_test.go` | 线性 / if / parallel 单测 |
| Admin REST | `POST /admin/workflows/graph-preview`（body `dsl_yaml`）；`GET /admin/workflows/:slug/graph` |
| Agent REST | `GET /agent/workflow/proposals/:id/graph`（proposal 卡片用） |
| `WorkflowGraphCanvas` | React Flow + `@dagrejs/dagre` 布局；`compact` 模式 |
| `/workflows` 编辑页 | YAML \| 流程图 \| 元数据 三栏；DSL 防抖 ~400ms 调 graph-preview |
| `WorkflowProposalCard` | dry_run 通过后嵌入 220px 迷你图 |

**Commits：** `833f110`（后端）、`d192a77`（WebUI）

## Graph IR（JSON）

```json
{
  "meta": { "id": "greet", "name": "Greet" },
  "inputs": [{ "name": "who", "type": "string" }],
  "outputs": [{ "name": "said", "expr": "${vars.msg}" }],
  "nodes": [
    { "id": "__start__", "kind": "start", "label": "开始" },
    { "id": "build", "kind": "assign", "label": "assign", "detail": "msg" },
    { "id": "__end__", "kind": "end", "label": "结束" }
  ],
  "edges": [
    { "from": "__start__", "to": "build", "type": "sequential" },
    { "from": "build", "to": "__end__", "type": "sequential" }
  ]
}
```

**节点 kind：** `start` / `end` / `tool` / `assign` / `if` / `foreach` / `parallel` / `wait`  
**边 type：** `sequential` | `branch`（label `then`/`else`）| `parallel`（label 分支序号）

## 验收

- [x] `go test ./internal/workflow/... -run Graph -count=1`
- [x] graph-preview / slug graph / proposal graph handler 单测
- [x] `cd internal/webui && npm test && npm run build`
- [x] admin `/workflows` 编辑视图右侧流程图随 YAML 更新
- [x] 聊天 `WorkflowProposalCard` 展示迷你流程图

## 与后续切片

| 切片 | 接口 |
|------|------|
| **19c（可选）** | 模板市场 UI；可复用 graph-preview 展示模板 DSL |
| **19d-v2（可选）** | `workflow_runs` 执行态 overlay；run 详情页高亮当前 step |
| **Slice 24 Triggers** | 图上标注 cron/webhook 触发点（只读） |

## 参考

- 用户手册：[`docs/WORKFLOW.md`](../../WORKFLOW.md) §9
- 验收：[`docs/SLICE-VERIFICATION.md`](../../SLICE-VERIFICATION.md) §19d
