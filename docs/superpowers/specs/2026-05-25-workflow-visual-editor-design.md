# Slice 20 — 工作流可视化设计器（表单 + 流程图）Design

> **Status:** 草案（2026-05-25）  
> **Depends on:** Slice 19a Engine、19b NL/模板、19c 模板市场、19d 只读流程图、21b MCP Bus  
> **Parent:** [`WORKFLOW-APP-ROADMAP.md`](../../WORKFLOW-APP-ROADMAP.md) · [`MCP-WORKFLOW-PLATFORM-PLAN.md`](../../MCP-WORKFLOW-PLATFORM-PLAN.md)  
> **Plan:** [`../plans/2026-05-25-slice-20-workflow-visual-editor.md`](../plans/2026-05-25-slice-20-workflow-visual-editor.md)

---

## 1. 问题

P0 验证（`e2e-mock-chain`）证明 **引擎 + MCP + 分支** 可用，但 WebUI 仍要求用户进入 **专家模式粘贴 YAML**。业务用户期望：

| 用户期望 | 现状（19d） |
|----------|-------------|
| 像流程图一样搭自动化 | 流程图 **只读**，不能拖节点改逻辑 |
| 选工具、填表即可 | 需手写 `use:` / `args:` / `${steps.*}` |
| 看不见 YAML | YAML 是唯一可靠编辑面 |
| 与 MCP 连接器一体 | MCP 在另一菜单注册，工作流里手填 `mcp.*` 字符串 |

**目标一句话：** 用户操作 **可视化设计模型（选 + 填）**；YAML 仅作 **持久化与执行 IR**，对用户无感。

---

## 2. 设计原则

| # | 原则 |
|---|------|
| P1 | **单一执行真相**：运行时仍只认 `WorkflowDoc` / `dsl_yaml`（19a 不变） |
| P2 | **编辑模型与 IR 分离**：UI 编辑 `WorkflowDesign`（JSON）；保存时 **编译** 为 YAML |
| P3 | **渐进增强**：先表单步骤列表（2a），再画布联动（2b）；专家 YAML 降级为「高级」 |
| P4 | **工具必选清单内**：`use` 从租户 `GET /tools` + 连接器 catalog 下拉，禁止裸填未注册名 |
| P5 | **可往返**：打开已有 workflow 时 **解析 YAML → Design**（能解析多少算多少，解析失败则提示进高级模式） |
| P6 | **NL 汇入设计器**：`workflow.propose` 产物导入设计器微调，而非让用户改 YAML |

---

## 3. 架构

```text
┌─────────────────────────────────────────────────────────────┐
│  WebUI /workflows                                            │
│  ┌──────────────┐  ┌──────────────┐  ┌─────────────────────┐ │
│  │ 模板市场(C)  │  │ 设计器(新)   │  │ 高级 YAML(折叠)     │ │
│  │ 填 slot      │  │ 步骤+表单    │  │ 双向同步/只读导出   │ │
│  └──────┬───────┘  └──────┬───────┘  └──────────┬──────────┘ │
└─────────┼─────────────────┼─────────────────────┼───────────┘
          │                 │                     │
          ▼                 ▼                     ▼
   template.Render    design.Compile()      Parse + Validate
          │                 │                     │
          └─────────────────┴─────────────────────┘
                            ▼
                     dsl_yaml (DB)
                            ▼
                     Engine.Execute + graph-preview (只读展示)
```

**与 19d 关系：** `GraphFromDoc` 继续用于 **预览**；编辑走 `WorkflowDesign`，保存后重新生成 Graph。

---

## 4. 编辑模型 `WorkflowDesign`（JSON）

存于浏览器状态；可选 P2 持久化 `design_json` 列。v1 **不落库**，仅随 `PUT /admin/workflows` 提交前在客户端持有。

```json
{
  "id": "e2e-mock-chain",
  "name": "Mock 状态巡检",
  "description": "",
  "inputs": [
    {
      "name": "scenario",
      "type": "string",
      "default": "degraded",
      "widget": "select",
      "options": ["ok", "degraded"],
      "label": "巡检场景"
    }
  ],
  "steps": [
    {
      "id": "status",
      "kind": "tool",
      "tool": "mcp.e2e-mock.fetch_status",
      "args": [
        { "name": "scenario", "value": "${inputs.scenario}", "valueKind": "expr" }
      ]
    },
    {
      "id": "pick",
      "kind": "assign",
      "assignments": [
        {
          "var": "health",
          "expr": "${steps.status.output.content.0.text}",
          "label": "健康状态原文"
        }
      ]
    },
    {
      "id": "gate",
      "kind": "if",
      "condition": { "left": "${vars.health}", "op": "eq", "right": "degraded" },
      "then": [
        {
          "id": "record",
          "kind": "tool",
          "tool": "mcp.e2e-mock.record_event",
          "args": [
            { "name": "kind", "value": "inspect", "valueKind": "literal" },
            { "name": "detail", "value": "${vars.health}", "valueKind": "expr" }
          ]
        },
        {
          "id": "alert",
          "kind": "tool",
          "tool": "mcp.e2e-mock.echo",
          "args": [
            { "name": "text", "value": "ALERT: system degraded", "valueKind": "literal" }
          ]
        }
      ],
      "else": [
        {
          "id": "ok_msg",
          "kind": "tool",
          "tool": "mcp.e2e-mock.echo",
          "args": [
            { "name": "text", "value": "OK: system healthy", "valueKind": "literal" }
          ]
        }
      ]
    }
  ],
  "outputs": [
    { "name": "health", "expr": "${vars.health}" },
    { "name": "branch", "expr": "${vars.health}" }
  ]
}
```

### 4.1 字段约定

| 字段 | 说明 |
|------|------|
| `valueKind` | `literal` \| `expr` — 字面量 vs `${...}` 表达式 |
| `condition` | v1 仅支持 **二元比较**（left / op / right），op ∈ `eq` `ne` `gt` `lt` … |
| `widget` | inputs 表单控件：`text` `select` `number` `boolean` `json`（高级） |

### 4.2 v1 支持的节点（设计器）

| kind | UI |
|------|-----|
| `tool` | 工具下拉 + 动态 args 表单 |
| `assign` | 变量名 + 表达式（提供插入助手） |
| `if` | 条件下拉/比较器 + then/else 子步骤列表 |

**延后：** `foreach` / `parallel` / `wait` — 仍在高级 YAML 编辑。

---

## 5. 编译器 `design.Compile` / `design.Decompile`

新包：`internal/workflow/design/`

| 函数 | 职责 |
|------|------|
| `Compile(d *WorkflowDesign) (yaml string, error)` | Design → `WorkflowDoc` → YAML 文本 |
| `Decompile(doc *WorkflowDoc) (*WorkflowDesign, error)` | 尽力还原；不支持的节点记入 `warnings[]` |
| `ValidateDesign(d *WorkflowDesign, tools []string) error` | 工具名在 Bus 中存在、id 唯一、条件合法 |

**条件编译示例：**

```text
{ left: "${vars.health}", op: "eq", right: "degraded" }
  → if: ${vars.health == "degraded"}
```

**工具 args：** 按 `valueKind` 输出 YAML 标量或带引号的字符串；`expr` 原样写入。

**Decompile 限制（需在 UI 明示）：**

- 复杂 `if` 表达式（`&&`、函数调用）→ 降级显示为「自定义条件」，仅高级 YAML 可改
- `foreach` / `parallel` → 标记为「不受支持，请用高级模式」

---

## 6. API

### 6.1 新增（建议）

| Method | Path | 说明 |
|--------|------|------|
| `POST` | `/admin/workflows/design/compile` | body `{ design }` → `{ dsl_yaml, warnings }` |
| `POST` | `/admin/workflows/design/decompile` | body `{ dsl_yaml }` → `{ design, warnings }` |
| `GET` | `/admin/workflows/tool-schemas` | 聚合 `GET /tools` 的 name + inputSchema + IsMutating（供表单生成） |

v1 也可 **纯前端编译**（将 `design` Go 逻辑 WASM/TS 端口成本高）→ **推荐后端 compile**，保证与引擎 Parse 一致。

### 6.2 现有（不变）

- `PUT /admin/workflows/:slug` 仍接收 `dsl_yaml`（由设计器 compile 后提交）
- `POST .../graph-preview` 仍接收 `dsl_yaml` 或改为接收 `design` 先 compile 再 graph（二选一）

---

## 7. WebUI 信息架构

### 7.1 `/workflows` 编辑视图（默认）

```text
[ 设计器 ] [ 流程图 ] [ 试运行 ]     （专家 YAML ▾ 折叠）
────────────────────────────────────
左侧：步骤列表（+ 添加工具 / 分支 / 赋值）
中间：当前步骤属性表单
右侧：流程图预览（19d，随 design 防抖 compile 后刷新）
底部：工作流 inputs / outputs 表单
```

**不再默认展示 YamlEditor。**

### 7.2 添加步骤向导

1. 选择类型：「调用工具」「设置变量」「条件分支」
2. 工具：分组下拉（MCP / 内置 / workflow.*），来源 `tool-schemas` + catalog 推荐
3. 参数：按 JSON Schema 渲染（string/number/boolean/enum）；需要引用时点「插入变量」→ `inputs.*` / `vars.*` / `steps.<id>.output...` 树

### 7.3 表达式助手（降低 `${}` 恐惧）

弹层树形选择：

```text
inputs
  └ scenario
vars
  └ health
steps
  └ status
      └ output
          └ content.0.text
```

选中后生成 `${...}` 填入字段。v1 **不强制**用户手写表达式。

### 7.4 试运行（迭代 2c 最小版）

右栏 `InvokePanel`：

- `inputs` 由 `WorkflowDesign.inputs` 生成 **表单**（如 `scenario` 下拉）
- 高级用户可切换「JSON 模式」

---

## 8. 与现有路径的衔接

| 路径 | 衔接方式 |
|------|----------|
| **模板市场 (C)** | 创建后进入设计器；slots 已填的步骤可展开编辑 |
| **NL propose (B)** | 卡片增加「在设计器中打开」→ decompile DSL |
| **专家 YAML** | 「高级」Tab；修改后提示「同步到设计器」或覆盖确认 |
| **MCP 连接器** | 工具下拉仅显示已注册；未安装 MCP 时引导至 `/admin/connectors` |
| **审批 / Dry-Run** | 不变；compile 后走现有 validate + dry_run |

---

## 9. 内置模板扩展（快速赢）

新增模板 **`mock-inspect`**（P0 配套），slots：

| slot | 类型 | 说明 |
|------|------|------|
| `scenario` | select | ok / degraded |
| `alert_text` | string | 告警文案 |
| `ok_text` | string | 正常文案 |

渲染结果等价于当前 `e2e-mock-chain.yaml`。用户 **模板市场填 3 项** 即可，无需设计器也能完成验证实验。

---

## 10. 分阶段交付

| 阶段 | 范围 | 用户感知 |
|------|------|----------|
| **20a** | `design` 包 + compile/decompile + 设计器 Tab（步骤列表 + 工具/分支表单）+ `tool-schemas` API + 默认隐藏 YAML | 可零 YAML 搭建 `e2e-mock-chain` 同级逻辑 |
| **20b** | 画布点击选节点 ↔ 表单联动；拖放添加顺序边（不引入新节点类型） | 流程图可点击编辑 |
| **20c** | inputs 试运行表单；outputs 只读展示 | 试运行不手写 JSON |
| **20d（可选）** | DB `design_json` 列；保存双写；冲突以 design 为准 | 跨设备编辑草稿 |

---

## 11. 非目标（本切片不做）

- n8n 级任意节点拖拽、自定义 JS、子图市场
- 运行时节点高亮（属 19d-v2）
- 可视化编辑 `triggers` cron 表达式（仍文本 + 校验器）
- 替换 NL 建流；设计器是 **精修** 层

---

## 12. 风险与对策

| 风险 | 对策 |
|------|------|
| Decompile 丢信息 | `warnings[]` + 高级 YAML 兜底 |
| MCP 输出路径难填（`content.0.text`） | 表达式助手 + 常用 MCP 的「输出字段预设」 |
| 前后端 DSL 不一致 | compile 必须走后端 Parse/Validate |
| 范围膨胀 | v1 仅 tool/assign/if；其余进高级 |

---

## 13. 验收标准

> **状态（2026-05-26）**：Slice 20 + 21e 已交付；主画布为 SWD，概览图为 `WorkflowGraphPreview`。

### 20a（最小可交付）

- [x] 后端 `go test ./internal/workflow/...`（`design_*`）
- [x] WebUI：不打开 YAML 即可创建并发布与 `e2e-mock-chain` 等价的工作流
- [x] 试运行 `scenario=ok|degraded` 结果与现手动 YAML 一致（SSE `invoke/stream`）
- [x] `decompile(e2e-mock-chain.yaml)` 无 error，warnings 为空或已文档化
- [x] 模板 `mock-inspect` 一键创建

### 20b / 21e

- [x] 画布选中步骤 ↔ 右侧 `WorkflowDesignStepPanel` 联动
- [x] 概览 Tab 只读 DAG（`WorkflowGraph` → `WorkflowGraphPreview`）

---

## 14. 参考实现触点

| 现有代码 | 复用方式 |
|----------|----------|
| `internal/workflow/template/render.go` | slots → steps 的渲染模式借鉴 |
| `internal/workflow/graph.go` | compile 后 `GraphFromDoc` 预览 |
| `WorkflowGraphPreview` | 概览/提案只读 DAG（dagre + React Flow；21e-c 已移除可编辑 Builder 画布） |
| `SequentialWorkflowDesignerPane` + `swdAdapter` | 默认设计器画布（Slice 21e） |
| `Connectors.tsx` / `GET /tools` | 工具下拉数据源 |
| `deploy/compose/examples/e2e-mock-chain.yaml` | 黄金样例 / 回归测试 |

---

## 15. 变更日志

| 日期 | 说明 |
|------|------|
| 2026-05-25 | 初版：表单设计器 + 编译 IR + 分阶段路线图 |
| 2026-05-26 | 验收勾选更新；主画布 SWD；§14 触点改为 `WorkflowGraphPreview` |
