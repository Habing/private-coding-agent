# 私有化部署的 AI 编码 Agent 平台 —— 设计文档

| 字段 | 值 |
|---|---|
| 文档日期 | 2026-05-18（2026-05-21 修订 §1.1–1.2 / §4.0 / §4.6，ADR-69–72） |
| 状态 | Draft — 待用户复核 |
| 作者 | Brainstorming 会话 |
| 类型 | 设计 / Spec |

---

## 1. 概述

本项目目标是构建一个**可私有化部署**的多租户 **AI 编码 Agent 平台**，对标 Claude Code / Cursor / Cline，差异化定位是**数据不出企业内网 + 工作流沉淀 + 长期记忆**。

核心叙事：
- **对话即编程**：终端用户用自然语言驱动 AI 完成读代码、改代码、跑测试、提 PR 全流程
- **沉淀即资产**：一次性任务可保存为可复用工作流；可视化编排由 N8N 补位
- **越用越聪明**：用户/项目/组织三层记忆，加上反思 Agent，行为持续优化
- **零外泄**：本地模型 + 沙箱执行 + 内网部署，代码与对话不出企业边界

### 1.1 主 Agent（调度中枢）

### 1.2 四类能力（产品目标）

平台以 **主 Agent** 为统一对话入口：理解用户意图，在 §1.2 四类能力之间做路由与编排，汇总结果并经 Session 流式返回。主 Agent 在实现上由 **Session Orchestrator**（会话层）+ **Agent Engine**（ReAct 推理层）+ **编排策略**（演进中的显式路由）构成；**凡产生副作用的执行**仍收敛到 **Tool Bus**（ADR-2），避免绕开审计、配额与租户隔离。

主 Agent 当前已交付会话层与推理层（`coding` 单 Profile）；显式编排策略（Tool / Workflow / Sub-Agent / Skill 选择）规划为 P1+（见 §4.0.1）。

```
                    ┌─────────────────────────────┐
                    │  主 Agent（调度中枢）          │
                    │  Session Orchestrator        │
                    │  + Agent Engine (ReAct)      │
                    └──────────────┬──────────────┘
           ┌──────────┼──────────┬──────────┬──────────┐
           ▼          ▼          ▼          ▼          ▼
      ┌────────┐ ┌──────────┐ ┌────────┐ ┌─────────────┐
      │ Tools  │ │Workflows │ │ Skills │ │ Sub-Agents  │
      │ 执行   │ │ 编排     │ │ 规范   │ │ 业务专家    │
      └───┬────┘ └────┬─────┘ └───┬────┘ └──────┬──────┘
          │           │           │             │
          └───────────┴───────────┴─────────────┘
                              │
                              ▼
                    ┌─────────────────────────────┐
                    │  Tool Bus（唯一执行入口）     │
                    └─────────────────────────────┘
```

§1.2 定义主 Agent 可调度的四类能力（非四套独立产品）：

| 能力 | 一句话 | 主 Agent 怎么用 | 是否走 Tool Bus 执行 |
|---|---|---|---|
| **Tools（工具）** | 原子能力：读文件、跑命令、调模型、读写记忆 | ReAct 中 `tool_call` → `Invoke` | ✅ 是（叶子执行） |
| **Workflows（智能编排）** | 可版本化的多步 DAG：分支、循环、并行、等待 | 调用已发布 `workflow.<id>` 或触发器调度 | ✅ 是（引擎节点最终调 Bus） |
| **Skills（自我优化）** | 可安装的 SOP / 规范 / 检查清单（`SKILL.md`） | 会话/Run 前注入 system，指导如何选工具与步骤 | ❌ 否（只读 context，见 Slice 12 spec） |
| **Sub-Agents（业务专家）** | 领域人设 + 专用工具集 + 可选独立上下文 | 委派子 Run（Profile 或 `agent.delegate`）→ 结构化结果回灌 | ✅ 子 Agent 内部仍经 Bus |

**与核心叙事的对应**：

| 核心叙事 | 主要由哪类能力承载 |
|---|---|
| 对话即编程 | 主 Agent + **Tools** + Sandbox |
| 沉淀即资产 | **Workflows**（+ N8N）；成功路径可提炼为 Skill（远期） |
| 越用越聪明 | **Skills** + Memory + Reflection Agent |
| 零外泄 | 横切：沙箱、Model Gateway、多租户、审计 |

**交付状态（截至 Slice 11，供规划对照）**：

| 能力 | 已交付（PCA 仓库） | 规划中 |
|---|---|---|
| Tools | Tool Bus 12 内置工具；Sandbox；Model Gateway | External MCP；`http.*` / `git.*` 等 |
| Workflows | — | Workflow Service + Engine；`workflow.*` 工具；N8N Adapter (P1) |
| Skills | — | Slice 12：文件系统 `SKILL.md` 注入（见 `2026-05-21-slice-12-agent-skills-design.md`） |
| Sub-Agents | 单 Profile `coding` + ReAct | 多 Profile 注册表；`agent.delegate`；专家工具白名单 |
| 主 Agent 调度层 | Session + WS + ReAct | 显式路由（Tool / Workflow / Sub-Agent / Skill 选择）；Slice 16 概念 |

**设计边界（四类能力互不替代）**：

1. **Skill 不执行** — 不得把 Skill 正文当作 tool 入参旁路执行；优化靠版本升级与 Reflection 写 Memory，不靠 Skill 调沙箱。
2. **Workflow 管确定性** — 高频、步骤稳定的流程沉淀为 DAG；探索性任务留在主 Agent ReAct。
3. **Sub-Agent 是受限 Engine Run** — 不是第二个 Tool Bus；委派有独立 `max_steps` 与上下文，结果以 observation 回灌主 Agent。
4. **Memory ≠ Skill** — Memory 存个案事实与偏好；Skill 存团队/平台标准；禁止整份 Skill 同步进 `memories` 表（见 Slice 12 ADR-64）。

## 2. 核心需求

| 维度 | 决策 |
|---|---|
| 形态 | Web 应用（多端可选 CLI/IDE 插件） |
| 能力范围 | 主 Agent 调度四类能力：Tools、Workflows、Skills、Sub-Agents（§1.1–1.2）；含 MCP 扩展 |
| 代码工作区 | 云端沙箱（服务器内每会话独立容器/Pod） |
| 使用规模 | 企业多租户（组织 / 项目 / 用户三级） |
| LLM 后端 | 本地（Ollama/vLLM）+ 远程 API（Claude/OpenAI/DeepSeek/…）双路 |
| 后端技术栈 | Go（Gin/Fiber + 自研 Agent 编排） |
| 部署拓扑 | 两阶段：P0 docker-compose；P1+ Kubernetes |

非功能需求：

- 首 token 延迟 P95 < 1.5s（本地模型）
- 沙箱冷启 < 3s（含预热池）
- 沙箱单会话工具调用延迟 P95 < 500 ms
- 单实例支持 ≥ 50 并发会话；K8s 阶段水平扩展
- 多租户严格隔离：数据 / 计算 / MCP / 记忆 / 工作流不跨租户
- 全链路审计、不可篡改

## 3. 整体架构

```
+---------------------------------------------------------------------+
|  接入层  Web UI (React)  + (可选) CLI                                |
|         HTTPS REST + WebSocket(SSE)                                 |
+---------------------------------------------------------------------+
|  应用层  API Gateway (Gin/Fiber)                                     |
|    +- Auth (JWT + OIDC/LDAP)                                        |
|    +- Tenant / Project Manager                                      |
|    +- Session Orchestrator                                          |
|    |     +- Memory Loader (起始注入)                                 |
|    +- Workflow Service (Store / Validator / Trigger / Version/Fork) |
|    +- N8N Adapter (P1, 可选)                                         |
+---------------------------------------------------------------------+
|  执行层                                                              |
|    +- Agent Engine (ReAct, 多 profile)                               |
|    |     +- Coding Agent profile                                    |
|    |     +- Workflow Authoring Agent profile                        |
|    +- Workflow Engine (DAG, 控制流: if/loop/parallel/wait/subflow)   |
|    +- Reflection Agent (P1, 异步)                                    |
|    +- Tool Bus  ----- 唯一调用入口 -------------------------+         |
|    |   +- Internal MCP Servers (builtin)                   |         |
|    |   |    fs.* / shell.* / llm.* / http.* / git.*        |         |
|    |   |    memory.* / workflow.<published>                |         |
|    |   +- External MCP Servers (用户/租户接入)              |         |
|    +- Sandbox Runtime <interface>                          |         |
|    |      +- DockerDriver (POC)                            |         |
|    |      +- K8sDriver (生产)                               |         |
|    +- Model Gateway (OpenAI 兼容协议聚合)                   |         |
+---------------------------------------------------------------------+
|  数据层                                                              |
|    +- PostgreSQL (用户/租户/项目/会话/消息/工作流/记忆/审计/配额)      |
|    +- pgvector (记忆/RAG 向量)                                       |
|    +- Redis (流式状态/限流/锁/Working Memory)                         |
|    +- MinIO/S3 (沙箱快照/上传文件/日志归档)                           |
+---------------------------------------------------------------------+
|  N8N Subsystem (P1, 可选, 每租户独立实例)                             |
|    Workflow Editor + 400+ Integrations + Custom Nodes (我们的)        |
+---------------------------------------------------------------------+
横切: AuthN/AuthZ · Audit · OpenTelemetry · ConfigCenter
```

**四个关键抽象**（+ 主 Agent 中枢）：

0. **主 Agent** — Session Orchestrator + Agent Engine；协调 §1.2 四类能力（详见 §4.0）
1. **`SandboxRuntime` 接口** — 屏蔽 Docker / K8s 差异，支撑两阶段部署
2. **`Tool Bus` 统一调用** — 内置能力 + 外部 MCP + 工作流 + N8N 工作流，统一为 MCP 协议
3. **`Model Gateway`** — 内部全部使用 OpenAI Chat Completions 协议，下游适配本地/远程多种后端

## 4. 组件清单

### 4.0 主 Agent（调度中枢）与四类能力

本节把 §1.1–§1.2 的产品目标落到组件边界：谁是中枢、四类能力各由哪些包/接口承载、以及它们如何汇合到 Tool Bus。

#### 4.0.1 主 Agent（调度中枢）

| 组件 | 职责 | 当前实现 | 目标演进 |
|---|---|---|---|
| **Session Orchestrator** | 会话生命周期、消息持久化、WebSocket/SSE 流、配额入口 | `internal/session`（Slice 6 ✅） | 会话级 `skill_ids`、Memory 起始注入钩子 |
| **Agent Engine（主实例）** | ReAct：LLM 决策 → tool_call → 观察 → 直至 final；多 Profile | `internal/agent`（Slice 5 ✅） | 接入 ContextComposer（Skill + Memory）；调度策略 |
| **编排策略（规划中）** | 根据意图选择：直接 Tools / 调用 Workflow / 委派 Sub-Agent / 加载哪些 Skill | 隐式（模型自选 tool） | `internal/agent` 或 `internal/orchestrator`：路由表 + 审计 |

主 Agent **不替代** Workflow Engine 的 DAG 执行，也**不替代** Tool Bus 的 schema 校验与调用记录；它负责**决策与汇总**。

典型一次用户消息的路径：

```
用户消息 → Session.SendMessage
         → [ContextComposer] Profile prompt + Skills + (未来) Memory 块
         → Agent.Engine.Run (ReAct)
              ├─ tool_call → Tool Bus → Sandbox / ModelGW / memory.*
              ├─ (未来) workflow.<id> → Workflow Engine → 节点再调 Bus
              └─ (未来) agent.delegate → Sub-Agent Engine.Run → 结果回灌
         → 事件流持久化 + WS 推送
```

#### 4.0.2 四类能力与组件映射

| 能力 | 核心组件 | 存储 / 配置 | 对外表面 | 切片 / 文档 |
|---|---|---|---|---|
| **Tools** | Tool Bus；Internal/External MCP；Sandbox；Model Gateway | `tool_invocations`；providers 表 | `GET /tools`，`POST /tools/invoke` | Slice 4 ✅ |
| **Workflows** | Workflow Service；Workflow Engine；N8N Adapter (P1) | `workflows` 表；DSL 版本库 | `workflow.<id>` 注册为工具；触发器 API | 主 spec §6；未实施 |
| **Skills** | Skills Registry / Resolver / Injector；ContextComposer | `skills.dirs[]`；`(未来)` `skills` 表 | `GET /skills`；注入 system（无 invoke） | Slice 12 规划 |
| **Sub-Agents** | Agent Engine 多 Profile；(未来) Delegate 工具 | Profile 注册表；`(未来)` 专家元数据 | `profile` 会话字段；`agent.delegate` | Slice 14 概念；现仅 `coding` |

**汇合点 — Tool Bus**（ADR-2）：

| 调用方 | 调用 Bus 的方式 |
|---|---|
| 主 Agent Engine | `tool_calls` → `Bus.Invoke` |
| Workflow Engine 节点 | `tool` / `agent.run` / `subflow` 节点实现内调 Bus |
| Sub-Agent Engine | 与主 Agent 相同，工具白名单按 Profile 过滤 |
| Web / 管理端 | `POST /tools/invoke` 直连（调试与集成） |

**不经过 Bus 的路径**（须单独审计与 token 预算）：

| 路径 | 说明 |
|---|---|
| Skill 注入 | 只增加 system message 字符数；审计 action `skill.inject` |
| Memory 自动注入（规划） | 只读块拼入 context；不写沙箱 |
| Model Gateway 直连流式 | Agent 内 LLM 轮次；计量走 `model_usage` |

#### 4.0.3 与 Memory、Reflection 的关系

| 子系统 | 归类 | 说明 |
|---|---|---|
| Memory Service | 主 Agent 的**上下文燃料**（事实/偏好），不是第五类能力 | 经 `memory.*` **工具**读写；自动注入属 Loader，非 Skill |
| Reflection Agent (P1) | **自我优化**的异步补充 | 提议写入 Memory；不替代 Skill 包管理 |

### 4.1 接入层

| 组件 | 职责 | 对外接口 | 依赖 |
|---|---|---|---|
| Web Frontend | 会话、项目、工作流市场、记忆管理、流式渲染 | 浏览器 | API Gateway |
| API Gateway | 路由、CORS、限流、WS 握手升级、统一鉴权拦截器 | REST + WebSocket | 应用层各模块 |

### 4.2 应用层

| 组件 | 职责 | 主要接口 |
|---|---|---|
| AuthN/AuthZ | Token 颁发与校验、RBAC | `Authenticate`, `Authorize` |
| Tenant Manager | 组织、配额（QPS/token/沙箱数/存储） | `GetTenant`, `CheckQuota`, `ConsumeQuota` |
| Project Manager | 项目 CRUD、成员、Git 关联 | `CreateProject`, `ListMembers` |
| Session Orchestrator | 会话生命周期、消息持久化、流通道 | `StartSession`, `SendMessage`, `Stream` |
| Memory Loader | 会话起始按 token 预算注入相关记忆 | `LoadForSession(session) -> contextBlock` |
| Workflow Service | DSL 存储、版本、Fork、触发器调度、Dry-Run | `Save`, `Publish`, `Trigger`, `Fork` |
| N8N Adapter (P1) | N8N 工作流发现、注册为 MCP 工具、SSO 桥、Webhook 回调 | `SyncWorkflows`, `InvokeN8nFlow` |

### 4.3 执行层

| 组件 | 职责 | 主要接口 |
|---|---|---|
| Agent Engine | ReAct 循环、上下文压缩、停止判定、多 profile 切换 | `Run(session, msg) -> stream<Event>` |
| Workflow Engine | DAG 执行；控制流原语 `if`/`loop`/`parallel`/`wait`/`subflow`/`assign` | `Execute(dsl, inputs) -> stream<Event>` |
| Reflection Agent (P1) | 异步消费会话事件，提议记忆条目 | `Reflect(sessionId) -> []MemoryProposal` |
| Tool Bus | 统一 MCP dispatcher，租户 scope 校验，schema 缓存 | `ListTools(tenant)`, `Invoke(tool, args)` |
| Internal MCP Servers | builtin：fs/shell/llm/http/git/memory/workflow/util | MCP 协议 |
| External MCP Manager | 加载租户配置的 MCP server，心跳与重连 | `RegisterServer`, `RemoveServer` |
| Sandbox Runtime | 接口：Create/Destroy/Exec/Read/Write/Snapshot | `SandboxRuntime` |
| DockerDriver | 用 Docker SDK 实现 SandboxRuntime；专用 bridge / volume / seccomp | 实现接口 |
| K8sDriver | client-go 调度 Pod；Namespace 隔离 / NetworkPolicy / PSA Restricted | 实现接口 |
| Model Gateway | OpenAI Chat Completions 协议聚合 + 路由 + token 计量 | `ChatCompletion(stream)`, `Embeddings` |
| Memory Service | 持久化结构化 + 向量记忆；暴露为内部 MCP server `memory.*` | `Save`, `Search`, `List`, `Update`, `Delete`, `Link` |

### 4.4 数据层

| 组件 | 用途 |
|---|---|
| PostgreSQL | 用户/租户/项目/会话/消息/工作流/记忆元数据/审计/配额 |
| pgvector | 记忆与 RAG 向量索引（默认；规模大可换 Qdrant） |
| Redis | 流状态、限流计数、分布式锁、Working Memory |
| MinIO/S3 | 工作区快照、上传文件、长期日志归档 |

### 4.5 边界守则

1. 包级强约束：`internal/sandbox` 不允许 import `internal/session`，反向通过接口注入
2. 每个组件独立 Go package，对外只导出接口和 DTO
3. 跨组件调用通过事件总线（POC: channel；K8s 阶段: NATS）和接口注入
4. 拆服务优先级：**Agent Engine → Sandbox Runtime → Model Gateway**（最易成瓶颈）

### 4.6 四类能力 — 组件映射与接口

四类能力（§1.2）在组件层的落点如下。执行类能力最终汇入 **Tool Bus**；非执行类能力汇入 **Context Composer** 或独立 Engine Run。

#### 4.6.1 Tools（调用工具）

| 组件 | 职责 | 主要接口 | 状态 |
|---|---|---|---|
| Tool Bus | 校验 schema、审计 sha256、调度 Tool | `ListTools(tenant)`, `Invoke(tool, args)` | ✅ |
| Internal MCP Servers | builtin 工具实现 | `fs.*` `shell.*` `llm.*` `memory.*` 等 | ✅ |
| External MCP Manager | 租户接入第三方 MCP | `RegisterServer`, `RemoveServer` | ⏳ |
| Sandbox Runtime | Tools 中 fs/shell 的底层 | `SandboxRuntime` | ✅ |
| Model Gateway | Tools 中 llm.* 的底层 | `ChatCompletion`, `Embeddings` | ✅ |
| Memory Service | Tools 中 memory.* 的底层 | `Save`, `Search`, … | ✅ |

HTTP：`GET /tools`、`POST /tools/invoke`。Agent 通过 OpenAI `tool_calls` 触发。

#### 4.6.2 Workflows（智能编排）

| 组件 | 职责 | 主要接口 | 状态 |
|---|---|---|---|
| Workflow Service | DSL 存储、版本、触发器、Dry-Run | `Save`, `Publish`, `Trigger`, `Fork` | ⏳ |
| Workflow Engine | DAG 执行；控制流原语 | `Execute(dsl, inputs) -> stream<Event>` | ⏳ |
| Workflow Authoring Agent | NL → DSL 的 Meta-Agent Profile | `Run` + ToolBus 查工具/流程 | ⏳ |
| N8N Adapter (P1) | 外部可视化编排，注册为 `n8n.<name>` | `SyncWorkflows`, `InvokeN8nFlow` | ⏳ |

发布后工作流注册为 Tool Bus 工具 `workflow.<id>`（ADR-4），主 Agent 与 Sub-Agent 均可调用。控制流（`if`/`loop`/`parallel`/`wait`/`subflow`）**仅**在 Workflow Engine，不经 MCP 抽象（ADR-3）。

#### 4.6.3 Skills（自我优化）

| 组件 | 职责 | 主要接口 | 状态 |
|---|---|---|---|
| Skill Registry | 扫描 `skills/**/SKILL.md`，索引 meta | `LoadFromDirs`, `Get(id)` | 📋 Slice 12 |
| Skill Resolver | Profile / Session / 默认列表解析 | `Resolve(ResolveInput) -> []*Skill` | 📋 Slice 12 |
| Skill Injector | 渲染 `## Active Skills` system 块；token 预算 | `BuildSystemMessages` | 📋 Slice 12 |
| Context Composer | Engine 拼 message 前调用 | `ComposeSystem(ctx, in)` | 📋 Slice 12 |

Skills **不**实现 `Tool.Invoke`。自我优化的动态路径：Reflection Agent 写 `memory.lesson`；运营升级 Skill 包版本；未来 Session → Skill 提炼（12c+）。

REST（规划）：`GET /skills`、`GET /skills/{id}`。配置：`skills.dirs`、`skills.max_injected_chars`（见 Slice 12 spec）。

#### 4.6.4 Sub-Agents（业务专家）

| 组件 | 职责 | 主要接口 | 状态 |
|---|---|---|---|
| Agent Profile | 专家人设、工具白名单、绑定 SkillIDs、MaxSteps | `Profile` 注册表 | 🟡 仅 `coding` |
| Agent Engine | 专家独立 ReAct Run | `Run(in, yield)` | ✅ 机制已有 |
| `agent.delegate` 工具（规划） | 主 Agent 委派子任务，结构化回灌 | `Invoke({profile, task, sandbox_id?})` | ⏳ |
| Sub-Agent Registry（规划） | 租户可启用专家列表、配额 | `ListAgents(tenant)` | ⏳ |

模式：
- **Profile 型（P0）**：用户或 Session 指定 `profile=security-auditor`，一次 Engine Run 即专家。
- **委派型（P1）**：主 Agent `tool_call` → `agent.delegate` → 子 Run（独立上下文压缩）→ `role=tool` 结果回主会话。
- **并行型（P2）**：主 Agent 并行多个 Sub-Agent，结果合并（对齐 Task/subagent 模式）。

Sub-Agent 可挂载专属 **Skills** 包；执行仍经 **Tool Bus**，沙箱 `sandbox_id` 由主会话或委派参数传入。

#### 4.6.5 四类能力协作（主 Agent 视角）

| 用户意图类型 | 主 Agent 优先路径 | 参与能力 |
|---|---|---|
| 探索性改代码、查日志 | ReAct + Tools | Tools, Skills（规范）, Memory（检索） |
| 标准化发布/巡检/集成 | 调用 `workflow.<id>` | Workflows → Tools |
| 需安全/DBA/合规深度审查 | `agent.delegate` 或切换 Profile | Sub-Agents → Tools, Skills |
| 遵守团队编码/Review 规范 | 预装 Skill 注入 | Skills（无执行） |

## 5. 数据流与典型时序

### 5.1 会话启动 — 沙箱按需分配

- 用户 → API Gateway → Session Orchestrator
- `Tenant.CheckQuota` 通过后 `Sandbox.Create`
- 容器/Pod 启动慢路径走预热池
- 返回 `sessionId + wsURL`，WS 接入二次鉴权

### 5.2 单轮消息 — ReAct 流式循环

```
LLM 决策 -> tool_call -> Tool Bus -> [Internal MCP | External MCP | Sandbox]
        <- observation
        -> LLM 再决策
        -> ... -> final tokens (流式)
```

- 每一步发事件给 Session Orchestrator，推 WS 给前端
- 工具结果 > 50 KB 截断 + 写对象存储 + LLM 摘要，前端可展开原文
- 上下文逼近窗口时压缩（系统提示 + 近 N 轮 + 中段摘要）

### 5.3 模型网关路由

- 内部全用 OpenAI Chat Completions 协议
- 路由顺序：租户配置 `routing` → 模型显式指定 → 兜底默认
- 出网/本地链路按租户隔离，可强制"本地模型 only"
- Token 用量出口拦截，写计费/审计

### 5.4 沙箱生命周期

```
Idle -> Allocated -> Running -> (Paused) -> Released -> Reclaimed
```

- 资源限制：cpus / memory / pids-limit
- 网络：专用 bridge，默认 deny-all，租户白名单出网
- 文件：/workspace 独立 volume，/tmp tmpfs，rootfs 只读
- 用户：非 root，userns-remap，seccomp，no-new-privileges
- K8s 阶段：Namespace 隔离 + NetworkPolicy + PSA Restricted + 可选 gVisor/Kata

### 5.5 NL → Workflow 编译流

```
User NL -> Workflow Authoring Agent (元 Agent)
        -> 调 ToolBus.list_tools / WorkflowStore.list
        -> 生成 DSL Draft
        -> 渲染人类可读视图 + 副作用高亮
        -> 用户操作:
              [确认] -> 校验 -> 存储 -> 注册为 MCP 工具 -> 触发器生效
              [让 AI 改] -> 用户 NL feedback -> 重新生成
              [Dry Run] -> 沙箱内试运行 (mutating 工具走 mock)
              [手动编辑] -> DSL Editor (YAML)
```

## 6. NL → Workflow 子系统

### 6.1 用户体验保证

1. **绝不静默执行**：AI 生成的 DSL 必经 UI 确认才入库与触发
2. **副作用透明化**：写 issue / push / 发邮件 / 调收费 API 等步骤在确认页高亮
3. **可回退**：版本化发布；触发器可暂停；运行历史完整审计

### 6.2 Workflow DSL（YAML）

```yaml
id: <kebab-case-id>
name: <human readable>
version: <int>
description: <string>
inputs:
  <name>: { type: <string|int|bool|object|array>, default: <value> }
triggers:
  - type: cron|manual|webhook|event
    <type-specific args>
steps:
  - id: <step id>
    use: <mcp tool name>           # 例: gitlab.list_repos / workflow.<id>
    args: { <param>: ${expr} }
    on_error: fail|retry(N)|continue|rollback
    timeout: <duration>
  - id: <loop step>
    foreach: ${expr<list>}
    as: <var>
    steps: [...]
  - id: <branch>
    if: ${expr<bool>}
    then: [...]
    else: [...]
  - id: <parallel>
    parallel:
      - <branch A>
      - <branch B>
outputs:
  <name>: ${expr}
```

### 6.3 复用机制

- 发布 = 版本化 + 自动注册到 Tool Bus 为 `workflow.<id>`
- 可见范围：private / project / tenant
- Fork：克隆别人的 workflow 改造，版本号回到 v1
- **Agent 可调 Workflow**（自然语言找到）；**Workflow 可调 Agent**（`agent.run` 节点）；**Workflow 可嵌套 Workflow**（subflow）

### 6.4 编辑器与 Dry-Run

- 三联视图：流程图 / 步骤列表 / 原始 YAML
- Dry-Run：沙箱里跑一次，`mutating: true` 的工具走 mock/dry-run 适配，捕获预期副作用
- 通过 Dry-Run 后方可"发布"

### 6.5 控制流原语（不可经 MCP 抽象）

`if`/`switch`/`loop`/`foreach`/`parallel`/`wait`/`wait_event`/`assign`/`map`/`subflow`

理由：图结构操作、长时挂起、表达式求值在引擎层实现成本更低、性能更好。

## 7. 记忆子系统

### 7.1 三层记忆

| 层 | 作用域 | 寿命 | 后端 |
|---|---|---|---|
| Working Memory | 单次会话 | 会话结束失效 | Redis |
| User Memory | 单用户跨会话 | 长期 | PG + pgvector |
| Project Memory | 项目成员共享 | 长期 | PG + pgvector + 可选 Markdown |
| Tenant Memory | 整个组织 | 长期 | PG + pgvector + Markdown |

### 7.2 四种条目类型

| type | 用途 | 写入触发 | 读取时机 |
|---|---|---|---|
| profile | 用户/项目画像 | 对话提取 | 每次会话起始 |
| preference | 偏好/规约 | 显式说明 / 被纠正 | 决策前检索 |
| knowledge | 事实知识 | 探索发现 / 用户告知 | 任务相关时检索 |
| lesson | 经验教训 | 失败/成功事后反思 | 类似任务时检索 |

元数据：`type, scope, source, confidence, last_used_at, verified, tags, source_msg_id`。

### 7.3 学习信号

| 信号 | 采集 | 用途 |
|---|---|---|
| 显式 | 用户"记住 X" / 给会话评分 / 修正 Agent 输出 | 直接写 preference / lesson |
| 行为 | 接受/拒绝工具调用、撤销改动、AI 输出 vs 用户最终提交编辑距离 | 后台 Reflection Agent 汇总为 lesson |
| 任务结果 | 工作流/会话最终是否完成预定目标 | 写 lesson，调节相关记忆 confidence |

### 7.4 读写策略

- **会话起始注入**：profile（always）+ 高频 preference + 项目相关 knowledge 摘要；总量按 token 预算（默认 4k）
- **运行时按需**：Agent / Workflow 节点显式调 `memory.search`
- **写入触发**：用户显式 → 即时；Agent 识别值得记的事实 → 即时；Reflection Agent 异步提议 → 默认进用户审核队列（高 confidence ≥ 0.85 可自动入库）

### 7.5 质量保证

- 写入时向量相似度 ≥ 0.92 触发合并/更新而非新增
- `last_used_at` 长期未访问 + 低 `confidence` 归档
- 冲突解决：与既有矛盾时触发对话审核
- confidence 衰减：成功使用 +，被推翻 -

### 7.6 隐私与控制（UI 提供）

- 浏览所有记忆 / 按 type, scope 过滤
- 编辑、删除单条
- 一键导出（JSON / Markdown）
- 一键清空当前 scope（"忘记我"）
- Agent 行为引用某条记忆时，UI 可溯源

### 7.7 Memory 作为内部 MCP

工具：`memory.save | memory.search | memory.list | memory.update | memory.delete | memory.link`。
Agent / Workflow / UI 共用同一接口；Workflow 可读可写。

## 8. N8N 集成（P1，可选）

### 8.1 定位

互补不替代。AI 生成走自研 DSL；可视化手工创建走 N8N；两者经 Tool Bus 统一对外。

### 8.2 桥接

- **N8N → 我们**：N8N Adapter 通过 N8N REST API 发现 workflows，注册为 `n8n.<workflow_name>` MCP 工具，调用即 POST webhook
- **我们 → N8N**：发布自定义 N8N 节点包 `@org/n8n-nodes-openclaw`，含 `Call Agent` / `Run AI-DSL Workflow` / `Memory Search/Save` / `Sandbox Exec`

### 8.3 多租户

每租户独立 N8N 实例，K8s 阶段以 Operator/Helm sub-chart 形式动态 provision；SSO 通过 OIDC 透传。

### 8.4 许可证

N8N 当前为 Sustainable Use License。**仅以独立服务方式集成、不修改 N8N 代码、原始 LICENSE/NOTICE 随包交付**，自定义节点单独 npm 包发布。商业转售场景需法务复核或选用替代品（Activepieces / Windmill）。

### 8.5 UI

工作流市场统一展示两类工作流，标签 `source: ai-dsl | n8n`；编辑入口分流（AI 走我们的确认 UI；N8N 走嵌入 iframe 画布）。

## 9. 错误处理与失败模式

通用原则：**失败可见、可重试、可回滚、可定位**。

### 9.1 LLM 层

| 故障 | 处置 |
|---|---|
| 超时 / 网络抖动 | 指数退避重试 ≤ 3 次 → 切备用模型 |
| Token 超限 | 触发上下文压缩；仍超限则截断并提示 |
| tool_call JSON 解析失败 | 错误回灌单轮自纠；2 次失败则结构化重写 |
| 幻觉工具名 | 返回未知工具 + 可用清单提示 |
| Token 配额耗尽 | 入口拦截，明确提示，未扣费 |

### 9.2 工具 / MCP 层

| 故障 | 处置 |
|---|---|
| MCP server 不可达 | 摘除并标记 unavailable；后台重连 |
| 工具调用异常 | 错误回灌 LLM 决策 |
| 参数 schema 校验失败 | ToolBus 入口拒发 |
| 结果超大 | 截断 + 写对象存储 + 摘要 |
| 副作用工具失败 | Workflow 按 `on_error` 策略；Agent 自主决策 |

### 9.3 沙箱层

| 故障 | 处置 |
|---|---|
| 启动失败 | 重试 1 次 → 失败提示；预热池补位 |
| OOM / CPU 超时 | 当前命令失败回灌；连续 3 次建议提升 quota |
| 僵尸 / 网络断 | 标记 unhealthy → 迁移至新沙箱（有快照）或终止保留日志 |
| 长时任务卡住 | `task_id + wait_task` 协程挂起，超时强返回 timeout |
| 数据丢失风险 | 周期快照；异常退出后从最近快照恢复 |

### 9.4 Workflow 层

| 故障 | 处置 |
|---|---|
| DSL 校验失败 | 编辑器内联报错；回到 Authoring Agent 重生成 |
| Dry-Run 失败 | 阻止发布；原因展示 |
| 运行时节点失败 | 按 `on_error`: `fail|retry|continue|rollback` |
| 触发器迟到/重叠 | 单实例 + 分布式锁；策略 `skip|queue|parallel` |
| 子流死循环 | 全局深度上限（默认 5）+ step 上限 |

### 9.5 Memory 层

| 故障 | 处置 |
|---|---|
| 向量后端不可达 | 降级为元数据查询，UI 警示 |
| Reflection 异常 | 进死信队列，不阻塞主流程 |
| 冲突无法自动解决 | 进审核队列；UI 弹通知 |
| 反复学错 | 被否决次数高的记忆自动降权或归档 |

### 9.6 平台 / 多租户层

| 故障 | 处置 |
|---|---|
| 配额耗尽 | 入口返回 "配额已满"，告警 |
| DB/Redis 故障 | 已起会话只读续流；新建被拒；告警 |
| 上游模型集体不可用 | fallback；最终降级为"只可查不可写" |
| MCP 越权 | 强制租户 scope 校验，拒绝并审计 |

### 9.7 可观测与可恢复（横切）

- 每次决策写结构化事件：`session_id + step_id + tool + args_hash + result + duration + tokens + cost`
- OpenTelemetry 端到端 trace
- 审计 append-only，定期归档
- 失败可重放：保存到失败点的全部上下文（含沙箱快照、消息历史）

## 10. 测试策略

比例约 70/20/8/2 = 单元 / 集成 / E2E / 在线。

### 10.1 单元

- 每个 Go package 独立单测，接口层 mock
- DSL Validator / 表达式解析 / 配额计算 / 记忆冲突解决：≥ 80% 覆盖
- LLM 调用层：golden file + replay，CI 不调外网

### 10.2 集成

- Sandbox 真起 Docker，跑创建→exec→写文件→快照→销毁
- 起 reference MCP server（stdio + SSE）联调 Tool Bus
- Workflow Engine 用 fake MCP 工具集跑各种控制流
- 每版本 DB 迁移前后向测试

### 10.3 Agent / LLM 行为

- 任务套件（Bench）：N 个真实编码任务，通过率作为门禁
- Workflow 编译 bench：N 个 NL → 期望 DSL，跑 Authoring Agent 看一致性
- 模型/prompt 变更必须跑 bench

### 10.4 E2E

- Playwright 关键路径
- 多租户隔离专项越权用例
- 大量数据：长会话（>100 轮）、大文件（>10 MB）、并发会话（≥ 50）

### 10.5 安全

- 沙箱逃逸用例集（mount / capabilities / sysctl / docker socket）
- Prompt 注入（文件内容 / MCP 结果劫持）
- 跨租户越权
- 拒绝服务（循环调用 / 上下文炸药 / 超长 stream）

### 10.6 性能

- 关键 SLI：首 token P95 < 1.5s；工具调用 P95 < 500 ms；沙箱冷启 < 3s
- 基准压测：50 / 200 / 500 并发

### 10.7 在线

- Canary 5% 流量 24h
- 模型/prompt 1% 双跑对比
- UI 内反馈通道（👍/👎）进 Reflection 信号池

## 11. 阶段规划

### P0 — MVP（核心可用）

- Auth：本地账号 + JWT（OIDC/LDAP 延后到 P1）
- 数据层**全部带 `tenant_id`**，结构上 multi-tenant ready；P0 阶段默认部署为单租户（一个默认租户），不暴露租户管理 UI；目的是先把核心路径跑通而不背运维负担
- 项目 / 会话 / 消息 / 工作区
- 主 Agent：Session + ReAct Engine + Tool Bus 内置 MCP（fs/shell/llm/memory 等）
- Skills（Slice 12）：平台 `SKILL.md` 注入；Workflows / Sub-Agent 委派延后 P1
- Sandbox Runtime + DockerDriver
- Model Gateway（Ollama + 1 个外部 API 兼容）
- Memory Service（user/project 显式记忆 + 起始注入，无 Reflection Agent）
- Web UI（会话、流式、文件浏览、设置）
- docker-compose 一键部署
- 基础审计（append-only 表 + 关键事件埋点）

### P1 — 企业可用

- 多租户 + OIDC/LDAP SSO + 配额
- NL → Workflow 全流程（Authoring Agent / DSL / 确认 UI / Store / Trigger / Dry-Run）
- 已发布工作流暴露为 MCP 工具（**Workflows** 能力闭环）
- 多 Sub-Agent Profile + `agent.delegate`；租户 Skill 库（12b）；编排路由层
- Reflection Agent + 记忆冲突合并 + Memory Management UI
- N8N Subsystem（每租户独立实例 + Adapter + 自定义节点包）
- K8s Helm chart + K8sDriver
- 完整审计与 OpenTelemetry

### P2 — 高级能力

- Tenant Memory + 跨项目知识共享审批流
- 工作流"AI 协助创建（在 N8N 画布上）"
- 记忆质量看板
- Webhook / Event 触发器系统
- 工作流可视化简易编辑器（我们自家的，作为 N8N 替代选项）

### P3 — 探索

- 行为信号驱动的 LoRA 微调（需 ML 团队）
- 跨租户企业知识库（合规审批）
- 自主 Agent 群体协作（multi-agent）

## 12. 风险与开放问题

| 风险 | 影响 | 缓解 |
|---|---|---|
| Go 生态 LangChain/Agent 库不如 Python 成熟 | 开发慢 | 选定 `cloudwego/eino` 或基于 Go 直调 OpenAI 协议自研薄层 |
| MCP 协议在 Go 的实现成熟度 | 与外部 MCP 联调风险 | 早期 POC 阶段实测主要 MCP server 的兼容性 |
| 沙箱多租户安全 | 一旦逃逸全盘崩溃 | gVisor/Kata 二级隔离；持续 Red Team |
| 本地模型能力上限 | Agent 表现差 | Model Gateway 路由策略允许租户配置"强 Agent 任务路由到 API 模型" |
| N8N 商业许可不确定性 | 商业化受阻 | 准备 Activepieces / Windmill 替代方案 |
| Workflow DSL 表达力与稳定性的平衡 | AI 生成质量波动 | 严格 schema + 控制原语白名单 + Bench 守门 |
| Reflection Agent 自动写记忆引发漂移 | 行为劣化 | 默认进审核队列；置信度 + 否决率监控 |
| pgvector 性能上限 | 记忆/RAG 慢 | 监控指标；超过阈值切 Qdrant |

开放问题：

1. 用户跳 N8N 编辑器是 iframe 嵌入还是独立标签页？iframe 的 CSP 配置要早确认
2. 沙箱里默认装哪些工具链版本（Go / Node / Python / 主流 CLI）？需出"基线沙箱镜像"规格
3. Workflow DSL 是否要支持人工审批节点（HITL）？P1 还是 P2？
4. 内部 MCP server 是 in-process 还是独立进程？前者性能好，后者更"标准"
5. P0 默认沙箱基础镜像由谁维护（运维基线 vs 由用户/项目自带 Dockerfile）？

## 13. 关键设计决策记录（ADR 摘要）

| ID | 决策 | 理由 |
|---|---|---|
| ADR-1 | 模块化单体起步，渐进拆分 | 匹配两阶段部署节奏；POC 期开发效率最高 |
| ADR-2 | Tool Bus 统一抽象，内置能力也 MCP 化 | 唯一扩展点；Agent / Workflow / UI 共用一份工具集 |
| ADR-3 | 控制流原语保留在 Workflow Engine | 图操作 / 长时挂起 / 表达式求值，引擎层成本更低 |
| ADR-4 | Workflow 发布后自动注册为 MCP 工具 | 解锁组合复用：Agent 调 Workflow、Workflow 嵌套 |
| ADR-5 | Memory 作为内部 MCP 服务 | 与 Tool Bus 统一边界，三处共用 |
| ADR-6 | 三层记忆 + 显式/行为/任务三类学习信号 | "越用越聪明"的技术路径 |
| ADR-7 | N8N 作为对等服务集成，不进我们的进程 | 规避许可证 + 技术栈不一致问题 |
| ADR-8 | 默认 pgvector 而非 Qdrant | 减少私有化部署外部依赖 |
| ADR-9 | YAML DSL + JSON Schema 校验 | 人类可读、Git 友好、AI 生成稳定 |
| ADR-10 | OpenAI Chat Completions 作为内部统一模型协议 | 生态最广，下游适配成本低 |
| ADR-69 | 主 Agent 为唯一用户对话调度中枢 | 统一会话、审计、配额；四类能力为其挂载件 |
| ADR-70 | 四类能力分工：Tools 执行 / Workflows 确定性编排 / Skills 只读注入 / Sub-Agents 委派推理 | 避免 Memory、Skill、Workflow 职责重叠 |
| ADR-71 | 副作用仅经 Tool Bus；Skills 不经 Invoke | 安全与审计单点；与 MCP 扩展模型一致 |
| ADR-72 | Sub-Agent 首阶段用 Profile；委派用 `agent.delegate` 工具（P1+） | 复用现有 Engine，渐进增强调度层 |

## 附录 A：术语表

- **主 Agent（Main Agent）** — 用户会话的调度中枢：Session Orchestrator + Agent Engine +（演进）Orchestration Router
- **四类能力** — Tools、Workflows、Skills、Sub-Agents；见 §1.2 / §4.0 / §4.6
- **Skills** — `SKILL.md` 程序性知识包，注入 system，不经 Tool Bus 执行
- **Sub-Agent** — 业务专家：独立 Profile 或 `agent.delegate` 委派的受限 Engine Run
- **Orchestration Router** — 主 Agent 显式路由层（规划），选择 ReAct / Workflow / 专家 / Skill 集
- **Agent Engine** — ReAct 风格的 LLM 工具调用循环
- **MCP** — Model Context Protocol，模型与外部能力的标准协议
- **Tool Bus** — 我们自研的 MCP dispatcher，统一所有工具调用入口
- **Workflow Authoring Agent** — 专用 Meta-Agent，自然语言生成工作流 DSL
- **Sandbox Runtime** — 沙箱运行时抽象接口，含 Docker / K8s 两实现
- **Model Gateway** — 模型调用统一网关，OpenAI 兼容协议
- **Reflection Agent** — 异步消费会话事件，提炼记忆条目的元 Agent

## 附录 B：建议的目录结构（实施参考）

```
/cmd/server                main 入口
/internal/auth             鉴权与 RBAC
/internal/tenant           租户与配额
/internal/project          项目
/internal/session          会话编排 + Memory Loader
/internal/agent            Agent Engine + profiles + Context Composer
/internal/skills           Skill Registry / Resolver / Injector (Slice 12+)
/internal/orchestrator     Orchestration Router (P1+, 规划)
/internal/workflow         Workflow Service + Engine + Validator
/internal/reflection       Reflection Agent (P1)
/internal/toolbus          Tool Bus + Internal MCP servers
/internal/mcp              MCP client/server 通用库
/internal/sandbox          Sandbox Runtime 接口 + DockerDriver / K8sDriver
/internal/modelgw          Model Gateway
/internal/memory           Memory Service
/internal/n8nadapter       N8N Adapter (P1)
/internal/audit            审计
/internal/telemetry        OTel
/internal/config           配置加载
/pkg/dsl                   Workflow DSL 类型 / 解析 / 校验
/pkg/proto                 跨模块 DTO 与事件定义
/web                       前端 (React)
/deploy/compose            docker-compose 模板
/deploy/helm               Helm chart (P1)
/docs                      文档与 ADR
```

---

**审核状态**：草稿，待用户复核。
