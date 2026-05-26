# 工作流可视化编辑器 — 开源组件选型

> **日期**：2026-05-24 · **结案**：2026-05-26（方案 A / SWD 为唯一设计器画布）  
> **背景**：Slice 20 已用 `WorkflowDesign` compile/decompile；本文件记录选型结论。  
> **约束（不可变）**：运行时真相仍是 Go `WorkflowDoc` / `dsl_yaml`（见 [Slice 20 设计](./superpowers/specs/2026-05-25-workflow-visual-editor-design.md) P1）。  
> **已实施（21e）**：`SequentialWorkflowDesignerPane` + `swdAdapter` + `swdToolboxDynamic`；右侧 `WorkflowDesignStepPanel`（ArgsForm / ExprPicker）；设计器 **仅 SWD 顺序画布**。  
> **已移除（21e-c）**：`@workflowbuilder/sdk`、`WorkflowBuilderCanvas`；概览/提案流程图改为 `@xyflow/react` + dagre（`WorkflowGraphPreview`）。  
> **集成方案（详细）**：[SWD-INTEGRATION-PLAN.md](./SWD-INTEGRATION-PLAN.md)

---

## 1. 本项目需要什么

| 能力 | 说明 |
|------|------|
| 顺序步骤 | `tool` → `assign` → `if` → 分支内顺序步骤（与 `e2e-mock-chain.yaml` 一致） |
| 非自由画布 | 不是任意 DAG/数据流图，而是**有顺序的流水线 + 条件分支** |
| 参数表单 | 工具 args、assign 表达式、if 条件；工具列表来自 `tool-schemas` |
| 嵌入现有 React 页 | `/workflows` 设计器 Tab，Tailwind/shadcn 风格 |
| 适配层 | 编辑器 JSON ↔ `WorkflowDesign` ↔ `design.Compile` → YAML |

不适合直接照搬：**n8n 全栈**、**BPMN**、**Rete 数据流**、**ComfyUI 图**。

---

## 2. 候选对比（GitHub / 生态）

| 项目 | Stars≈ | 协议 | 与 DSL 匹配度 | 嵌入成本 | 结论 |
|------|--------|------|---------------|----------|------|
| [@xyflow/react](https://github.com/xyflow/xyflow)（React Flow） | 30k+ 生态 | MIT | 中（需自建顺序/分支语义） | **已集成** | 继续深化即可 |
| [nocode-js/sequential-workflow-designer](https://github.com/nocode-js/sequential-workflow-designer) | ~1.4k | MIT | **高**（顺序 + switch/if） | 中（适配层 + 换 UI） | **首选替换画布** |
| [nocode-js/sequential-workflow-editor](https://github.com/nocode-js/sequential-workflow-editor) | 配套 | MIT | 高 | 低（与上者配套） | 表单/校验生成器 |
| [didi/LogicFlow](https://github.com/didi/LogicFlow) | ~11k | Apache-2.0 | 中（通用流程图） | 高（MobX/Preact 体系） | 国内成熟但迁移重 |
| [retejs/rete](https://github.com/retejs/rete) | ~12k | MIT | 低（socket 数据流） | 高 | 不适合顺序 DSL |
| [n8n-io/n8n](https://github.com/n8n-io/n8n) | ~190k | 可持续许可证 | 高（但 n8n JSON） | **极高**（Vue 单体） | 只借鉴 UX，不嵌入 |
| [navid-kianfar/kerdar](https://github.com/navid-kianfar/kerdar) | 新项目 | MIT | n8n 向 | 中 | 社区与成熟度不足 |
| [koolii/react-workflow-editor](https://github.com/koolii/react-workflow-editor) | <10 | MIT | n8n 格式 | 中 | 实验性，不推荐生产 |

---

## 3. 推荐方案

### 方案 A（推荐）：**Sequential Workflow Designer + 保留现有后端**

**包**：`sequential-workflow-designer` + `sequential-workflow-designer-react`（MIT）

**理由**：

1. **语义对齐**：原生「顺序 + 占位符插入 + 多分支 switch」，与 `steps[]` / `if.then/else` 同构，比 React Flow 自由连线更贴 DSL。
2. **开箱能力**：工具箱拖入、步骤选中、撤销栈、只读模式、步骤变更回调、**可关掉自带 editor 面板**（用现有 `ArgsForm` / `ExprPicker`）。
3. **与现有架构兼容**：编辑器产出 JSON `definition`；新增 `internal/webui/src/lib/swdAdapter.ts`：`definition ↔ WorkflowDesign`，仍走 `POST /admin/workflows/design/compile`。
4. **可选增强**：[`sequential-workflow-editor`](https://github.com/nocode-js/sequential-workflow-editor) 按工具 schema 生成 step editor（减少手写表单）。

**Demo**：[React Demo](https://nocode-js.github.io/sequential-workflow-designer/react-app/) · [Multi-conditional Switch](https://nocode-js.github.io/sequential-workflow-designer/examples/multi-conditional-switch.html)（对应 if 多分支）

**注意**：

- Pro 功能（复制粘贴、文件夹等）为付费；MIT 版对 P0/P1 足够。
- 样式需引入 `designer.css` / `designer-light.css`，与 shadcn 并存要做一层主题覆盖。

### 方案 B（保守）：**继续 @xyflow/react，吸收成熟模式**

**理由**：已在 `WorkflowGraphCanvas` 投入 20b（选中、插入、排序）；React Flow 为 Stripe/Typeform 等在用的事实标准。

**建议只补**（不换库）：

- [`NodeToolbar`](https://reactflow.dev/api-reference/components/node-toolbar)、`onConnect` 约束（禁止任意连线，仅顺序）
- 官方 [Workflow 示例](https://reactflow.dev/showcase) 或社区模板

**缺点**：顺序 + if 合并/插入规则需长期自维护；UX 弱于 SWD 的「流水线」心智。

### 不推荐

- **整包嵌入 n8n 前端**：Vue2/3 技术栈、许可证与体积、与 Go DSL 双轨，成本 > 重写。
- **LogicFlow**：能力强，但另一套渲染与模型，和现有 React Flow 重复投资。
- **Rete.js**：适合节点编程，不适合「工作流步骤表」。

---

## 4. 集成架构（方案 A）

```text
┌─────────────────────────────────────────────────────────┐
│ WorkflowDesigner (React)                                 │
│  ┌─────────────────────┐  ┌──────────────────────────┐  │
│  │ SequentialWorkflow   │  │ 现有右侧表单              │  │
│  │ Designer (画布)      │  │ ToolPicker/ArgsForm/     │  │
│  │                      │  │ ExprPicker               │  │
│  └──────────┬──────────┘  └────────────┬─────────────┘  │
│             │ definition JSON            │ WorkflowDesign │
│             └──────────┬─────────────────┘                │
│                        ▼                                  │
│                 swdAdapter.ts                             │
│                        ▼                                  │
│              design.Compile → dsl_yaml (Go, 不变)          │
└─────────────────────────────────────────────────────────┘
```

**适配要点**：

| SWD 概念 | 本项目 |
|----------|--------|
| `sequence[]` 顶层 | `design.steps` |
| `switch` / 多分支 step | `kind: if` + `then`/`else` |
| 普通 task step | `kind: tool` / `assign` |
| `properties` | `inputs` / step `args` |
| `onDefinitionChanged` | debounce → `compileMut` |

**保留**：`design/decompile`、`tool-schemas`、试运行、NL 草案导入、专家 YAML Tab。

**可删除/降级**：自研 `workflowDesignTree` 插入逻辑、部分 20b 画布代码（由 SWD 接管）。

---

## 5. 实施切片（方案 A — 状态 2026-05-26）

| 切片 | 内容 | 状态 |
|------|------|------|
| **21e-POC** | SWD React、`e2e-mock-chain` round-trip | ✅ |
| **21e-a** | SWD 默认画布 + 选中联动右侧表单 | ✅ |
| **21e-b** | MCP 动态工具箱 + ArgsForm / ExprPicker / assign / if | ✅ |
| **21e-c** | 移除 Builder SDK；`WorkflowGraphPreview` 只读图 | ✅ |
| **21e-d** | OS 深浅色跟随 SWD `theme`；设计器操作说明 | ✅ |
| **21e-b+** | `sequential-workflow-editor` | ⏸ 暂缓（见 `swdEditorBridge.ts`，右栏表单为唯一路径） |

---

## 6. 决策建议

| 若目标是… | 选择 |
|-----------|------|
| **最快稳定、少迁移风险** | 方案 B（深化 React Flow） |
| **产品化流水线 UX、愿做适配层** | **方案 A（Sequential Workflow Designer）** |
| **与 n8n 互通** | 不嵌入 n8n；仅导出/借鉴；继续自有 DSL |

**团队建议**：采用 **方案 A** 作 Slice **21e**；当前 Slice 20 的表单层（`ArgsForm`/`ExprPicker`/`compile`）**全部保留**，只替换「画布 + 步骤树」为 SWD。

---

## 7. 参考链接

- Sequential Workflow Designer: https://github.com/nocode-js/sequential-workflow-designer  
- React 包装: https://www.npmjs.com/package/sequential-workflow-designer-react  
- Sequential Workflow Editor: https://github.com/nocode-js/sequential-workflow-editor  
- React Flow: https://reactflow.dev/  
- LogicFlow: https://github.com/didi/LogicFlow  
- Rete.js: https://github.com/retejs/rete  
- n8n（仅参考）: https://github.com/n8n-io/n8n  
