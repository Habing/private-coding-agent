# 域：`data-prep`（公共能力 · 文件入参 · AI 降噪）

> **类型**：公共能力域（P1 首域）  
> **上级**：[`MCP-TOOL-ROADMAP.md`](../MCP-TOOL-ROADMAP.md) · [`MCP-WORKFLOW-PLATFORM-PLAN.md`](../MCP-WORKFLOW-PLATFORM-PLAN.md)

---

## 1. 目标

提供可复用的 **AI 数据降噪 / 清洗** 能力，供后续业务工作流在前置节点调用。

| 决策 | 选择 |
|------|------|
| **数据来源** | **文件**：批次文件进入 `/data/inbox` |
| **清洗方式** | **AI（LLM）**：在 **MCP 服务内**调用模型，对记录做语义级降噪（非主路径规则引擎） |
| **PCA 角色** | 注册 MCP、工作流编排、NL 建流、触发与审计；清洗逻辑与模型调用封装在 MCP 内 |

**非目标（P1）**：用固定规则替代 AI（空行过滤、去重等仅可作为 AI 前后的**可选**轻量预处理，见 §3.2）。

---

## 2. 数据流

```text
  [文件落地]                         [MCP 容器 mcp-data-prep]
        │                                      │
        ▼                                      ▼
   /data/inbox/                    load_file(path) → records[]
   batch.json           ───►       ai_denoise_records
        │                            (LLM + 任务说明 / schema)
        │                                      │
        ▼                                      ▼
   /data/outbox/                   write_file → batch.clean.json
   batch.clean.json
```

| 目录 | 说明 |
|------|------|
| `/data/inbox` | 原始文件（只读挂载） |
| `/data/outbox` | AI 清洗后的结构化结果 |

工作流 **inputs** 示例：

```json
{
  "source_file": "batch-2026-05-24.json",
  "output_file": "batch-2026-05-24.clean.json",
  "cleaning_instructions": "去掉重复和明显噪声记录，统一日期为 ISO8601，保留 id/name/amount 字段"
}
```

`cleaning_instructions` 可由 NL 建流时从用户原话写入，体现 **「用 AI 洗数」** 而非写死规则。

---

## 3. MCP 设计

### 3.1 slug 与模型

| 项 | 值 |
|----|-----|
| **slug** | `data-prep` |
| **Bus** | `mcp.data-prep.<tool>` |
| **模型** | MCP 进程内配置（如 `DASHSCOPE_API_KEY` + 与 PCA 相同的 OpenAI 兼容端点），**不**依赖对话 Session 的 ReAct |

这样「公共能力」在运行时仍走 **Tool Bus → MCP → LLM**，与平台模型网关策略可后续对齐（P2：MCP 改调 PCA 内网代理）。

### 3.2 Tool 清单（P1）

| tool_name | 说明 | destructiveHint | 典型 input |
|-----------|------|-----------------|------------|
| `load_file` | 读 inbox，解析为 `records[]`（可限制条数/采样） | false | `path`, `format`, `max_records?` |
| `ai_denoise_records` | **核心**：将 `records` + 自然语言/结构化指令送 LLM，返回清洗后 `records[]` + `changelog` | false | `records`, `instructions`, `output_schema?`, `model?` |
| `ai_denoise_file` | 便捷：path + instructions，内部 load → ai_denoise → 返回结果（可不写盘） | false | `path`, `instructions`, `format` |
| `summarize_run` | 清洗前后对比摘要（条数、剔除原因归类，供审批卡片） | false | `before`, `after`, `changelog` |
| `write_file` | 将结果写入 outbox | **true** | `path`, `records`, `format` |

**可选（P1 末期 / P2）**：`preflight_trim` — 仅做体积截断、UTF-8 校验等非语义预处理，**不**替代 `ai_denoise_records`。

### 3.3 LLM 调用约定（实现参考）

1. **结构化输出**：要求模型只返回 JSON（`records` 数组 + `removed` 说明），便于工作流 `${steps.*.output}` 引用。  
2. **分块**：大文件按 `max_records` 或 token 预算分批调用 `ai_denoise_records`，MCP 内 merge。  
3. **Prompt 模板**：服务内维护 system 模板；`instructions` 来自 workflow inputs（NL 可生成）。  
4. **合规**：文件内容会出网到 LLM 提供商（即便内网 DashScope）— 试点需确认数据分级；可 P2 增加「仅本地模型」开关。

---

## 4. 工作流（路线 2）

### 4.1 标准管道 `data-prep-pipeline`（AI 版）

```yaml
id: data-prep-pipeline
name: AI 文件数据降噪
description: 从 inbox 读文件，经 MCP 内 LLM 语义清洗后写入 outbox

inputs:
  source_file: { type: string }
  output_file: { type: string }
  cleaning_instructions:
    type: string
    default: "移除无效与重复记录，修正明显格式错误，保持业务字段语义一致"

steps:
  - id: load
    use: mcp.data-prep.load_file
    args:
      path: ${inputs.source_file}
      format: json

  - id: denoise
    use: mcp.data-prep.ai_denoise_records
    args:
      records: ${steps.load.output.records}
      instructions: ${inputs.cleaning_instructions}

  - id: summary
    use: mcp.data-prep.summarize_run
    args:
      before: ${steps.load.output.records}
      after: ${steps.denoise.output.records}
      changelog: ${steps.denoise.output.changelog}

  - id: write
    use: mcp.data-prep.write_file
    args:
      path: ${inputs.output_file}
      records: ${steps.denoise.output.records}
      format: json

outputs:
  output_file: ${inputs.output_file}
  stats: ${steps.summary.output}
  changelog: ${steps.denoise.output.changelog}
```

节点少、语义集中在 **`ai_denoise_records`**，符合「AI 清洗」叙事；NL 拆节点时仍可拆成 load / denoise / write 三步。

### 4.2 与「工作流里直接 llm.chat」的边界

| 方式 | 说明 |
|------|------|
| **推荐（本域）** | `mcp.data-prep.ai_*`：清洗 prompt、分块、schema、文件 IO 封装在 MCP，Bus 上能力是 **「AI 降噪服务」** |
| 不推荐作为主路径 | 工作流 `load` → `llm.chat` → `write`：清洗散落在 DSL 里，难复用、难版本化、难给业务域统一前置 |

业务域（P2）应引用 **`mcp.data-prep.*`**，而不是各自写一段 `llm.chat` 洗数。

### 4.3 NL 建流

用户说法示例：

> 读取 inbox 的 sales.json，用 AI 去掉噪声和重复项，字段名统一成英文 snake_case，结果写到 outbox。

→ `workflow.propose` 生成 DSL：`cleaning_instructions` 写用户意图，`steps` 使用 `mcp.data-prep.ai_denoise_records`。

这与平台优势一致：**NL 定义 AI 任务，发布后为确定性 workflow（指令可版本化）**。

---

## 5. 部署

```yaml
services:
  mcp-data-prep:
    image: <registry>/mcp-data-prep:0.1.0
    environment:
      - LLM_BASE_URL=${DASHSCOPE_BASE_URL}
      - LLM_API_KEY=${DASHSCOPE_API_KEY}
      - LLM_MODEL=${DATA_PREP_MODEL:-qwen-plus}
      - MAX_RECORDS_PER_CHUNK=200
    volumes:
      - ./data-prep/inbox:/data/inbox:ro
      - ./data-prep/outbox:/data/outbox:rw
    ports:
      - "8085:8085"
```

PCA：`slug=data-prep`，`url=http://mcp-data-prep:8085/` → Refresh。

---

## 6. Dry-Run 与成本

| 项 | 行为 |
|----|------|
| Workflow `dry_run=true` | `write_file` mock；`ai_denoise_*` 建议 **mock 固定样本** 或短路，避免审批阶段大量调 LLM |
| Proposal Dry-Run | 卡片展示将调用的 tool 列表；可注明「生产 run 将消耗 LLM token」 |
| 配额 | MCP 内 LLM 调用暂不走 PCA `quota`（P2 可经内网 Model Gateway 统一计量） |

---

## 7. P1 完成标准

> **验收状态**：MCP 与 compose 样例已存在；下列为 **域 P1 运营验收**（非 Slice 20/21e 文档范围），按部署环境逐项勾选。

- [ ] `load_file` + `ai_denoise_records` + `write_file` 实现，LLM 返回可解析 JSON  
- [ ] 样例脏文件 → outbox 干净文件，changelog 可读  
- [ ] PCA 注册，`mcp.data-prep.*` invoke 成功  
- [ ] `data-prep-pipeline` 发布，runs 含 `stats` / `changelog`  
- [ ] NL propose 一条（`cleaning_instructions` 来自用户原话）  

---

## 8. 变更日志

| 日期 | 说明 |
|------|------|
| 2026-05-24 | 初版：文件入参 + MCP 内清洗 |
| 2026-05-24 | **修正**：清洗主路径为 **AI（LLM）**，非规则引擎；工具改为 `ai_denoise_*` |
