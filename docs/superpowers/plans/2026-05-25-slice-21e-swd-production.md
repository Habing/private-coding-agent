# Slice 21e — SWD 生产化（实施计划）

> **状态**：**已完成**（2026-05-26；21e-b+ 暂缓）  
> **方案**：[SWD-INTEGRATION-PLAN.md](../../SWD-INTEGRATION-PLAN.md)  
> **设计**：[Slice 20 可视化设计](../specs/2026-05-25-workflow-visual-editor-design.md)  
> **依赖**：`internal/workflow/design_*`、`design/compile|decompile`、`GET /admin/workflows/tool-schemas`

---

## 目标

将 **Sequential Workflow Designer（SWD）** 从「画布 Beta」推进为 **默认工作流编辑画布**，并复用已有 `ArgsForm` / `ExprPicker` / compile 链路；最终下线 Workflow Builder SDK 双轨维护。

**不变量**（不可破）：

```text
用户编辑 → WorkflowDesign → POST design/compile → dsl_yaml → Go WorkflowDoc
SWD Definition 仅 UI 中间表示，不新增持久化格式。
```

---

## 现状快照（2026-05-26）

| 项 | 状态 |
|----|------|
| `swdAdapter` round-trip（`e2e-mock-chain`） | ✅ |
| SWD 唯一设计器画布 | ✅ |
| 右侧 `WorkflowDesignStepPanel` + ArgsForm / ExprPicker | ✅ |
| 动态 MCP 工具箱 | ✅ `swdToolboxDynamic.ts` |
| 概览只读流程图 | ✅ `WorkflowGraphPreview`（无 Builder SDK） |
| OS 深浅色 → SWD theme | ✅ `usePrefersColorScheme` |
| `sequential-workflow-editor` | ⏸ `swdEditorBridge.ts` 记录暂缓原因 |

---

## 任务勾选归档

| 切片 | 任务 | 状态 |
|------|------|------|
| **21e-a** | A1 左 SWD + 右 `WorkflowDesignStepPanel` | ✅ |
| | A2 `selectedStepId` 上浮 | ✅ |
| | A3 tool / assign / if 右栏表单 | ✅ |
| | A4 SWD 内置 editor 精简 | ✅ |
| | A5 `canvasMode` 双画布 localStorage | **N/A**（21e-c 已移除 Builder 切换） |
| | A6 单测 + 手工 e2e-mock-chain | ✅ |
| **21e-b** | B1 动态工具箱 | ✅ |
| | B2 tool-schemas API | ✅ |
| | B3 `validatorConfiguration` 未知 tool | ⏸ 待办 |
| | B4 assign / if 表单（含预设） | ✅ |
| | B5 `swdToolboxDynamic` + `swdAdapter` 测试 | ✅（深层 if 在 round-trip 用例中覆盖） |
| | B5+ `sequential-workflow-editor` | ⏸ 见 `swdEditorBridge.ts` |
| **21e-c** | C1 概览图 → `WorkflowGraphPreview` | ✅ |
| | C2 删除 Builder SDK 与相关文件 | ✅ |
| | C3 Playwright / debug 脚本扩展 | ⏸ 可选 |
| | C4 SWD / WORKFLOW / 选型文档 | ✅ |
| **21e-d** | 主题 / 帮助文案 | ✅ |
| | 工具箱 i18n 全覆盖 | ~ 节点标题已中文化；SWD 内置英文保留 |

---

## 切片总览

| 切片 | 名称 | 状态 |
|------|------|------|
| **21e-a** | 默认画布 + 选中联动 | ✅ |
| **21e-b** | 工具箱 + schema 表单 | ✅ |
| **21e-c** | 清理与回归 | ✅ |
| **21e-d** | 主题 / 帮助 | ✅ |

---

## 21e-a — 默认画布 + 选中联动（归档）

#### A1 — 布局

- [x] `WorkflowDesignStepPanel.tsx` + `WorkflowDesigner` 横排布局
- [x] 工作流级字段在画布上方

#### A2 — 选中联动

- [x] `SequentialWorkflowDesignerPane`：`selectedStepId` / `onSelectedStepIdChange`
- [x] SWD `onSelectedStepIdChanged`

#### A3 — 右侧表单

- [x] `GET /admin/workflows/tool-schemas`
- [x] tool → `ArgsForm` + `ToolPicker`
- [x] assign → 变量列表 + 表达式预设
- [x] if → 条件 UI；then/else 仅画布

#### A4 — 精简 SWD editor

- [x] 无 tool/assign/if JSON 双轨

#### A5 — 画布模式

- [x] **已取消**：仅 SWD，无 `canvasMode` / Builder Tab

#### A6 — 测试

- [x] `SequentialWorkflowDesignerPane.test.tsx`
- [x] `WorkflowDesignStepPanel.test.tsx`

---

## 21e-b — 动态工具箱（归档）

- [x] B1 `swdToolboxDynamic.ts` + fallback `PCA_SWD_TOOLBOX`
- [x] B2 tool-schemas `useQuery`
- [ ] B3 `validatorConfiguration` 未知 tool 标红
- [x] B4 assign / if 完善（含 `workflowExprPresets`）
- [x] B5 `swdToolboxDynamic.test.ts`、`swdAdapter` round-trip
- [~] B5+ `sequential-workflow-editor` — **暂缓**（`swdEditorBridge.ts`）

---

## 21e-c — 清理（归档）

- [x] C1 `WorkflowGraphPreview`（dagre + @xyflow/react）
- [x] C2 删除 `WorkflowBuilderCanvas`、`workflowBuilderAdapter`、SDK 依赖
- [ ] C3 Playwright / `debug-workflow-designer.py` 扩展（可选）
- [x] C4 文档结案

---

## 21e-d — 体验（归档）

- [x] `designer-dark.css` + `theme` 跟随 OS
- [x] 设计器 Tab 顶部帮助文案
- [~] 工具箱分组中文化（节点 `workflowStepKindZh` 已覆盖主路径）

---

## 整体验收

| 级别 | 命令 / 行为 | 状态 |
|------|-------------|------|
| L1 | `go test ./internal/workflow/... -run Design` | ✅ |
| L2 | `cd internal/webui && npm test && npm run build` | ✅ |
| L3 | 设计器编辑 `e2e-mock-chain` → 保存 → 试运行 | ✅ |
| L3 | NL 草案 →「在设计器中打开」 | ✅ |

> **回滚说明**：21e-c 后已无 Builder 分支；回滚需 git revert 相关 PR。

---

## 非目标（本切片）

- 不改 Go DSL 语法（仅 `tool` / `assign` / `if`）
- 不购 SWD Pro、不嵌 n8n / LogicFlow
- 不把 SWD Definition 写入 DB

---

## 参考

- [SWD-INTEGRATION-PLAN.md](../../SWD-INTEGRATION-PLAN.md)
- [WORKFLOW-EDITOR-LIBRARY-EVALUATION.md](../../WORKFLOW-EDITOR-LIBRARY-EVALUATION.md)
- 样例 DSL：`deploy/compose/examples/e2e-mock-chain.yaml`
- 适配层：`internal/webui/src/lib/swdAdapter.ts`
