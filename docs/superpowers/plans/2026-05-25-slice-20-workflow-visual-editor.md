# Slice 20 — Workflow Visual Editor（实施计划）

> **状态**：**已完成**（2026-05-26；主画布已由 Slice **21e** 升级为 SWD，见 [`SWD-INTEGRATION-PLAN.md`](../../SWD-INTEGRATION-PLAN.md)）  
> **设计**：[`../specs/2026-05-25-workflow-visual-editor-design.md`](../specs/2026-05-25-workflow-visual-editor-design.md)  
> **依赖**：19a Engine、19c 模板市场、19d Graph、21b MCP、连接器 catalog

---

## 目标

用户在工作流页 **选择工具 + 填写参数 + 看流程图**，YAML 由系统生成；专家 YAML 降为高级入口。

---

## Task 分解（归档勾选）

### Task 1 — `internal/workflow/design`（后端）

> 实现文件：`internal/workflow/design_*.go`（非单独 `design/` 子包）

- [x] `design_model.go` — `WorkflowDesign`, steps, args, condition
- [x] `design_compile.go` — Design → `WorkflowDoc` → YAML
- [x] `design_decompile.go` — Parse → Design（tool/assign/if）
- [x] `design_validate.go` — id 唯一、工具名校验
- [x] `design_test.go` / `design_validate_test.go` — `e2e-mock-chain.yaml` 往返

### Task 2 — Admin API

- [x] `POST /admin/workflows/design/compile`
- [x] `POST /admin/workflows/design/decompile`
- [x] `GET /admin/workflows/tool-schemas`
- [x] handler 单测 + [`WORKFLOW.md`](../../WORKFLOW.md) §9.4

### Task 3 — WebUI 设计器（20a）

- [x] `WorkflowDesigner.tsx` — SWD 画布 + compile/decompile 链路
- [x] `ToolPicker.tsx` — 分组下拉（MCP / 内置）
- [x] `ArgsForm.tsx` — 按 JSON Schema 动态表单
- [x] `ExprPicker.tsx` — inputs/vars/steps 树（Portal + 中文标签）
- [x] 条件/赋值 — `ExprField` + 二元条件 UI（`WorkflowDesignStepPanel`）
- [x] `Workflows.tsx` — 默认 Tab「设计器」；YAML 收进「高级」；YAML↔设计器同步提示
- [x] 保存：compile → `PUT` dsl_yaml
- [x] 打开：decompile 已有 yaml

### Task 4 — 模板 `mock-inspect`

- [x] `internal/workflow/template/catalog.go` — 模板 `mock-inspect`
- [x] `render.go` — 渲染为 e2e-mock-chain 等价 DSL
- [x] 模板市场显示；P0 验证无需粘贴 YAML

### Task 5 — 画布联动（20b）

> 主编辑路径已迁移至 SWD（21e）。概览/提案只读图现为 `WorkflowGraphPreview`。

- [x] 选中步骤 ↔ 侧栏表单联动（`selectedStepId` / `focusStepId`）
- [x] 概览 Tab `WorkflowGraph` → graph-preview 只读 DAG
- [~] 原 `WorkflowGraphCanvas` 可编辑插入 — **已移除**（21e-c）

### Task 6 — 试运行表单（20c）

- [x] `WorkflowInvokeControls` + `InvokeInputsForm`（`design.inputs`）
- [x] 保留 JSON 切换；SSE 流式执行结果（`invoke/stream`）

### 仍开放（增强，非阻塞 Slice 20 结案）

- [ ] SWD `validatorConfiguration` 未知 tool 标红
- [ ] Playwright 设计器端到端冒烟
- [ ] 见 [`slice-21e-swd-production.md`](2026-05-25-slice-21e-swd-production.md) 中 21e-b+ 暂缓项

---

## 验收

| 级别 | 命令 / 行为 | 状态 |
|------|-------------|------|
| L1 | `go test ./internal/workflow/... -run Design -count=1` | ✅ |
| L2 | `cd internal/webui && npm test && npm run build` | ✅ |
| L3 | 零 YAML 创建 e2e-mock 巡检流；ok/degraded 试运行 | ✅ |
| L3 | NL propose →「在设计器中打开」→ 发布 | ✅ |

**E2E：** 沿用 compose workflow 相关步骤；触发器见 Slice 24（76–78）。

---

## 非目标

见 design §11。

---

## 参考

- 设计全文：[`2026-05-25-workflow-visual-editor-design.md`](../specs/2026-05-25-workflow-visual-editor-design.md)
- 样例 DSL：[`deploy/compose/examples/e2e-mock-chain.yaml`](../../../deploy/compose/examples/e2e-mock-chain.yaml)
- 路线 2 路线图：[`docs/WORKFLOW-APP-ROADMAP.md`](../../WORKFLOW-APP-ROADMAP.md)
