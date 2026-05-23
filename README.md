# Private Coding Agent

私有化部署的 AI 编码 Agent 平台。

## 切片进度

- [x] 切片 1：Foundation
- [x] 切片 1.5：Foundation Hardening
- [x] 切片 2：Sandbox Runtime + DockerDriver
- [x] 切片 3：Model Gateway
- [x] 切片 4：Tool Bus + Internal MCP
- [x] 切片 5：Agent Engine
- [x] 切片 6：Session API + WebSocket
- [x] 切片 7：Memory (basic)
- [x] 切片 8：Web Frontend
- [x] 切片 9：Audit Deepening
- [x] 切片 10：Observability (OTel spans + Prometheus + structured logs)
- [x] 切片 11：Vector Memory (pgvector cosine search + 0.92 dedup)
- [x] 切片 12：Agent Skills (SKILL.md registry + Profile/Session/Run 路由 + 系统消息注入)

### P1 — 企业可用（规划已落盘，实施中）

**路线图：** [`docs/P1-ROADMAP.md`](docs/P1-ROADMAP.md) · **Compose 试点运维：** [`docs/P2-COMPOSE-PILOT.md`](docs/P2-COMPOSE-PILOT.md)

#### MVP-P1（企业试点）— 切片 13～17

- [x] 切片 13：Enterprise Foundation（provider 租户隔离、quota、JWT logout）
- [x] 切片 14：Session ↔ Sandbox 强绑定（创建会话自动建沙箱）
- [x] 切片 15：SSO (OIDC)
- [x] 切片 16：Enterprise Web（沙箱文件浏览、Memory 注入/UI）
- [x] 切片 17：Skills 12b（租户 Skill API + Admin UI）

#### Full P1 — 切片 18～23

- [x] 切片 18：Sub-Agents + `agent.delegate`（review / research / workflow-authoring profile + 父子 Agent 协作）
- [x] 切片 19a：Workflow Engine（YAML DSL + DAG executor + `workflow.<slug>` 注册到 ToolBus + Dry-Run mock mutating tools）
- [x] 切片 19b（Web UI）：Workflows & Tools Web UI（`/workflows` admin 管理页 + `/toolbox` 工具浏览页 + `GET /tools` 暴露 `mutating` 标志）
- [x] 切片 19b（NL Authoring）：自然语言建流 B+C（模板 catalog + `workflow.propose` + 对话确认卡片 + member 审批链 + E2E 70–75）
- [x] 切片 19d：Workflow Visualization（DSL → 只读流程图；admin graph API + `/workflows` 预览 + Proposal 迷你图）
- [x] 切片 20：Reflection Agent（异步 worker + `memory_proposals` 表 + admin 审核 + auto-approve 阈值 + WebUI `/admin/memory-proposals`）
- [x] 切片 21a：Orchestration Router（YAML 规则 + 命中后注入 routing hint system msg + `pca_orchestrator_routes_total` + audit `orchestrator.route`）
- [x] 切片 21b：External MCP Manager（`mcp_servers` 表 + 2024-11-05 JSON-RPC client + Manager 心跳 + `mcp.<slug>.<tool>` 注册到 ToolBus + `/admin/mcp-servers` REST + WebUI + mock-mcp 容器）
- [x] 切片 22a：Audit Hash Chain（SHA-256 链式 `audit_log.prev_hash/entry_hash` + `GET /audit/verify` admin 防篡改校验）
- [x] 切片 22b：Snapshot → MinIO（compose minio + `DockerDriver.Snapshot` + restore）
- [x] 切片 22c：seccomp + trivy CI（沙箱 seccomp profile + GitHub Actions 镜像扫描）
- [x] 切片 22d：K8sDriver + Helm chart（Pod = sandbox + kind nightly）
- [ ] 切片 23：N8N 集成（**跳过** — 非硬需求；Full P1 核心已完成）

**Compose 试点（P2 运维）**：备份/restore、workflow retention、Reflection 队列、re-embed、snapshot restore — 见 [`docs/P2-COMPOSE-PILOT.md`](docs/P2-COMPOSE-PILOT.md)。生产化演练清单：[`docs/PILOT-RUNBOOK.md`](docs/PILOT-RUNBOOK.md)。

## 本地开发

```powershell
# 单元 + 集成测试 (含 dockertest 拉 PG)
go test ./...

# 集成测试 (含真 Docker)
docker build -t pca/sandbox:base ./sandbox/image
go test -tags=docker_integration ./internal/sandbox/...

# 本地直接跑
copy config\config.example.yaml config\config.yaml
go run ./cmd/server --config config\config.yaml
```

## docker-compose 启动

```powershell
docker build -t pca/sandbox:base ./sandbox/image   # 首次必须
cd deploy\compose
copy .env.example .env
# 接真实 LLM：在 .env 填入 DASHSCOPE_API_KEY=sk-...（见 deploy/compose/QWEN.md）
docker compose up -d --build
curl http://localhost:8080/healthz
```

### 阿里云 Qwen 3.6 Plus

- 迁移 `0012` 注册 provider `dashscope` → 百炼 OpenAI 兼容端点
- 对话模型：`dashscope:qwen3.6-plus`
- 详细步骤：[deploy/compose/QWEN.md](deploy/compose/QWEN.md)

## 端到端验证

```powershell
cd deploy\compose
./test-e2e.sh    # Git Bash / WSL，推荐（75 步全量；含切片 13–20 + 19b NL 70–75）
# pwsh ./test-e2e.ps1   # 仅覆盖早期切片，完整验收请用 .sh
```

每切片完成后的增量步号与 L1/L2 命令见 [`docs/SLICE-VERIFICATION.md`](docs/SLICE-VERIFICATION.md)。  
P0 Gate：**E2E 1～42**；切片 13 起增量 **43～48**（quota+logout / session-sandbox / OIDC / memory inject / sandbox files）。详见 [`HANDOFF.md`](HANDOFF.md) §3.0。

## 关键端点

| 方法 | 路径 | 鉴权 | 说明 |
|---|---|---|---|
| GET | /healthz | - | 健康检查 |
| GET | /readyz | - | 就绪检查 |
| POST | /auth/login | - | 登录拿 JWT（`auth.local_enabled=true` 时） |
| GET | /auth/oidc/login | - | 发起 OIDC 登录（重定向 IdP） |
| GET | /auth/oidc/callback | - | OIDC 回调，返回 PCA JWT JSON |
| POST | /auth/logout | Bearer | 吊销当前 JWT（`jti` 入 Redis 黑名单） |
| GET | /me | Bearer | 当前用户身份 |
| POST | /sandbox/sessions | Bearer | 创建沙箱 |
| GET | /sandbox/sessions/{id} | Bearer | 查询沙箱 |
| DELETE | /sandbox/sessions/{id} | Bearer | 销毁沙箱 |
| POST | /sandbox/sessions/{id}/exec | Bearer | 执行命令 |
| GET | /sandbox/sessions/{id}/files?path=... | Bearer | 读文件 |
| PUT | /sandbox/sessions/{id}/files?path=... | Bearer | 写文件 |
| POST | /sandbox/sessions/{id}/snapshot | Bearer | (501) |
| POST | /v1/chat/completions | Bearer | OpenAI 兼容,支持 stream |
| POST | /v1/embeddings | Bearer | OpenAI 兼容 |
| GET | /tools | Bearer | 列出已注册工具（每项含 `mutating bool`，由 `toolbus.Mutating` 接口推导） |
| POST | /tools/invoke | Bearer | 调用 tool |
| POST | /agent/run | Bearer | ReAct 循环,返回 events 数组 (非流式) |
| GET  | /agent/profiles | Bearer | 列出可用 Profile（name + description） |
| POST | /sessions | Bearer | 创建会话 |
| GET  | /sessions | Bearer | 列出当前用户会话 |
| GET  | /sessions/{id} | Bearer | 查询会话 |
| DELETE | /sessions/{id} | Bearer | 归档会话 |
| GET  | /sessions/{id}/messages | Bearer | 列出会话消息 |
| GET  | /sessions/{id}/ws | Bearer | WebSocket 流: 发 user_message,收 event/done/error |
| POST | /memories | Bearer | 创建一条记忆 |
| GET  | /memories | Bearer | 列出当前用户记忆,可按 ?type=&tag=&q= 过滤 |
| GET  | /memories/{id} | Bearer | 查询单条记忆 |
| PUT  | /memories/{id} | Bearer | 更新 content / tags / type |
| DELETE | /memories/{id} | Bearer | 删除一条记忆 |
| GET | /admin/memory-proposals | Bearer (admin) | 列出 Reflection 候选（`?status=pending\|approved\|auto_approved\|rejected&owner_user_id=&limit=&offset=`） |
| GET | /admin/memory-proposals/{id} | Bearer (admin) | 查询单条 proposal |
| POST | /admin/memory-proposals/{id}/approve | Bearer (admin) | 审批通过，body 可选 `{type?,content?,tags?}` 覆盖；走 `memory.Service.Create`，复用 0.92 dedup |
| POST | /admin/memory-proposals/{id}/reject | Bearer (admin) | 驳回，body 可选 `{reason?}`；`memory_id=NULL` |
| GET | /audit | Bearer (admin) | 查询审计日志,支持 action/user_id/from/to/min_status/max_status/limit/offset 过滤 |
| GET | /metrics | Bearer (admin 或 scrape token) | Prometheus exposition,`pca_*` 指标 |
| GET | / | - | SPA 首页（embed 进二进制） |
| GET | /login, /sessions/{id}, /audit | - | SPA 前端路由，由 NoRoute fallback 返回 index.html |

## 内部 MCP 工具

8 个基础工具 + 4 个记忆工具 + 1 个 sub-agent 委派 + 4 个 workflow admin 工具 = 17 个（通过 `GET /tools` 列出）：

- `fs.read / fs.write / fs.list / fs.glob` 沙箱内文件读写
- `grep` 沙箱内全文搜索
- `shell.exec` 沙箱内执行命令
- `llm.chat / llm.embed` 调 Model Gateway
- `memory.save / memory.search / memory.list / memory.delete` 持久化记忆（User scope）
- `agent.delegate` 委派子 Run 给另一个 Profile（仅 coding profile 持有；详见「Sub-Agents 与 Profile」）
- `workflow.create / workflow.update / workflow.list / workflow.get` admin-only；Agent 在会话里起 Workflow DSL 草稿、改 DSL、查看现有 workflow。**Publish / delete 仍只在 admin REST**——人在 loop 里做发布动作（详见「Workflow 子系统」）

## Sub-Agents 与 Profile

Slice 18 把"单 Profile"扩成"父子 Agent + 多 Profile 协作"。

**4 个内置 Profile**（`GET /agent/profiles` 列出）：

| Profile | 工具白名单 | 用途 |
|---------|-----------|------|
| `coding` | 全 12 个基础/记忆工具 + `agent.delegate` | 默认；可写沙箱、可委派 |
| `review` | fs.read / fs.list / fs.glob / grep / memory.search / memory.list / llm.chat | 只读评审，不改沙箱 |
| `research` | llm.chat / llm.embed / memory.{search,list,save} | 资料检索 + 记忆沉淀，不碰沙箱 |
| `workflow-authoring` | llm.chat / memory.search / fs.read / fs.glob / grep | Slice 19 SKILL.md 作者助手（预热接入） |

**`agent.delegate` 工具**：input `{profile, task, max_steps?}`；父 Run 一次调用，子 Run 跑完返回 `{result, sub_steps, status, sub_tool_calls}`。

不变量：
- 子 Run **继承父会话的 sandbox_id**（通过 `internal/agent.RunCtx` 经 ctx 透传，不靠 LLM 推断）
- 递归深度上限 = **1**（ctx 计数 + 子 Profile 白名单不含 `agent.delegate`，双保险）
- 子 Run 的 `assistant_delta / tool_call / tool_result` **不外泄**到父客户端流；父客户端只看到 delegate 的 `tool_call` + `tool_result`
- 子 Run 强制继承父 TenantID/UserID/Model；配额走原 quota 中间件自然兜底
- 审计：`agent.delegate.start` + `agent.delegate.complete`（含 `parent_profile / sub_profile / sub_steps / status / duration_ms`）
- OTel：子 `agent.run` span 自动嵌套在父 `tool.invoke{tool=agent.delegate}` span 下

## Workflow 子系统

Slice 19a 把高频确定流程从 ReAct 推理里下沉成可版本化、可发布的 YAML DSL DAG。发布后 `workflow.<slug>` 自动成为一条 ToolBus 工具，Agent 通过 `tool_call`、用户通过 `POST /tools/invoke`、其他 workflow 通过 `tool: use: workflow.<other>` 都能触发——subflow 自然成立，不需要专门节点 kind。

**6 类节点：**

| Kind | 触发字段 | 行为 |
|------|----------|------|
| `tool` | `use:` | bus.Invoke；DryRun + Mutating → mock JSON `{"dry_run":true,"tool":"...","input":...}` |
| `assign` | `assign:` | 表达式求值后写入 vars |
| `if` | `if: / then: / else:` | bool 真假分支 |
| `foreach` | `foreach: / as: / steps:` | 对 expr 求值得 list，逐项迭代；`vars[as]=item` |
| `parallel` | `parallel: [[...],[...]]` | 各分支独立 goroutine；wait-all；first-error-cancels-siblings |
| `wait` | `wait: 100ms` | ctx-aware time.Sleep |

修饰字段：`timeout`（默认 60s）、`on_error: fail|continue`。

**表达式：** `${inputs.x}` / `${vars.y}` / `${steps.<id>.output[.path]}` / `${steps.<id>.error}`；运算符 `== != < > <= >= && || !`。

**Admin REST：**

```
POST   /admin/workflows                     # 创建（published=false）
GET    /admin/workflows                     # 列出（list 不返 dsl_yaml）
GET    /admin/workflows/:slug               # 详情
PUT    /admin/workflows/:slug               # 更新；version+=1；强制 published=false
DELETE /admin/workflows/:slug               # 已发布则 bus.Unregister
POST   /admin/workflows/:slug/publish       # 校验 + Bus.Register
POST   /admin/workflows/:slug/unpublish     # Bus.Unregister
POST   /admin/workflows/:slug/invoke        # body {inputs, dry_run?}
GET    /admin/workflows/:slug/runs          # 最近 N 次 run
POST   /admin/workflows/graph-preview       # body {dsl_yaml} → Graph JSON（Slice 19d）
GET    /admin/workflows/:slug/graph         # 已保存 workflow 的流程图
```

Agent proposal：`GET /agent/workflow/proposals/:id/graph`（对话卡片迷你图，member+）。

**Dry-Run：** `internal/toolbus.Mutating` 可选接口；fs.write / shell.exec / memory.save / memory.delete / agent.delegate 实现并返回 true。Engine 在 `dry_run=true` 时拦截 mutating 节点返回 mock JSON，Run 行 `dry_run=true` 落表但不动副作用。

**关键不变量：** ① published == 在 Bus 中（startup republish 兜底）；② PUT 强制 unpublish；③ 跨租户隔离（`workflow.<slug>` 在 Bus 全局，但 WorkflowTool.Invoke 在 boundary 拒绝跨租户）；④ MaxSteps=200 / MaxParallelFanout=8 / MaxNestingDepth=8 三层兜底；⑤ Engine 错误降级为 `ExecutionResult{status: failed}`，只有系统错才返回 Go error。

**审计 7 个 action：** `workflow.admin.{create,update,delete,publish,unpublish}` + `workflow.invoke.{start,complete}`。

**OTel：** `workflow.execute` 顶 span + `workflow.step{id,kind,dry_run}` 每节点子 span。

**完整使用说明：** [`docs/WORKFLOW.md`](docs/WORKFLOW.md)（DSL 例子 + REST API + Agent 触发 + subflow + Dry-Run + 不变量）。

## 记忆子系统

User-scope 持久化记忆，配套 4 个 MCP 工具与 REST 表面。

**检索路径**（`memory.search` / `POST /memories/search`）：

| `mode` | 行为 |
|---|---|
| `vector`（默认） | Gateway 算 query embedding → cosine `<=>` 排序 → 按 score 降序返回 |
| `keyword` | 退回 Slice 7 的 ILIKE + tag overlap + type 过滤 |
| 未设 + query 空 | 自动 keyword（保留 filter-only 场景） |
| 未设 + query 非空 + `embed_on_write=true` | 自动 vector |

`score` 字段仅在 vector path 出现（cosine 相似度，[-1, 1]）。

**去重**（`memory.save` / `POST /memories`）：
- Create 前先对 query embedding 做 top-1 cosine 比对
- 命中 `memory.dedup_threshold`（默认 0.92）→ 不写新行，touch 既有行 `last_used_at`
- 工具响应携带 `created` bool（false 表示 dedup hit），REST 状态码 200 vs 201

**Embedding 维度固定 1536**：迁移列 `vector(1536)` 与 `internal/memory.EmbeddingDim` 必须一致。切换不同维度的模型（如 bge-base 768）需要新 migration + 重建数据；本切片不做运行时维度切换。

**运维 kill switch**：`memory.embed_on_write=false` 时 Create 不算 embedding，Search 永远走 keyword（Slice 7 行为）。

**ivfflat 召回**：`internal/db.Connect` 在每条连接上 `SET ivfflat.probes = 100`（默认 1，仅扫一个聚类，在小数据集上易漏召）。代价可控——单 (tenant, user) 记忆量不大；行数涨上去之后索引依然有效。改为更高的 `lists` 时需要同步上调 probes 以维持「全聚类扫描」语义。

```yaml
memory:
  embedding_model: "default-mock:text"   # 百炼: dashscope:text-embedding-v4（1536 维）
  dedup_threshold: 0.92                  # 0 关闭去重
  embed_on_write: true
```

dev / compose 默认走 mock provider 的 deterministic 1536-d 向量（sha256 → L2-normalize），同 input 必出同向量。切到真实模型后老向量与新向量不可比，需重建。

## Reflection 子系统

Slice 20 把"会话经验沉淀"做成自动闭环：归档会话 → 异步 worker → LLM 抽取候选 → `memory_proposals` 落表 → admin 审核 / 自动通过 → 走 `memory.Service.Create` 复用 0.92 cosine dedup 写入 `memories`。

**流程：**

1. `DELETE /sessions/:id` 走 `session.Service.ArchiveSession`，末尾把 `{tenant, user, session}` 推入 in-process `chan reflection.Job`（buffer 256，best-effort：channel 满即 `outcome=dropped` 计数 + warn，不阻塞归档）。
2. `reflection.Worker` 主循环抽 job → 起独立 goroutine（5min ctx 超时）→ 调 `reflection.Reflector.Run`。
3. `Reflector` 拉会话最近 ≤20 条消息（单条截 ≤500 字符），拼上含 `REFLECTION_TASK_V1` 的 system prompt 调 Model Gateway。返回必须是 JSON 数组 `[{type, content, tags, confidence}]`，最多 3 条；解析失败 → audit `reflection.session.failed`，不写任何 proposal。
4. 解析成功：每条 proposal 写一行 `memory_proposals`。`confidence ≥ reflection.auto_approve_threshold`（默认 0.85）→ 状态直接 `auto_approved` + 同步 `memory.Service.Create(Source=reflection)`，命中 dedup 时 `memory_id` 指向既有行；否则 `pending`。
5. Admin 通过 `/admin/memory-proposals` 审批：approve 走同一 `memory.Service.Create` 路径（支持 `{type?,content?,tags?}` 覆盖），reject 留 `memory_id=NULL`。

**Admin REST**（`auth.RequireAdmin` + 租户过滤）：

```
GET    /admin/memory-proposals?status=pending|approved|auto_approved|rejected&owner_user_id=&limit=&offset=
GET    /admin/memory-proposals/:id
POST   /admin/memory-proposals/:id/approve   # 可选 body {type?, content?, tags?}
POST   /admin/memory-proposals/:id/reject    # 可选 body {reason?}
```

**配置**（`reflection.*`，env 前缀 `PCA_REFLECTION_*`）：

```yaml
reflection:
  enabled: true                        # false 时不构造 worker / 不挂 admin handler
  model: "default-mock:gpt-4o"         # 生产: dashscope:qwen3.6-plus
  auto_approve_threshold: 0.85         # 0 关闭 auto-approve（全走审核）
  max_messages_per_session: 20
  max_chars_per_message: 500
  worker_buffer: 256
  worker_timeout: 5m
```

**审计 5 个 action：** `reflection.session.{complete,failed}` + `memory.proposal.{create,approve,reject}`。  
**Metric：** `pca_reflection_proposals_total{outcome=created|auto_approved|approved|rejected|dropped|llm_failed}`。  
**OTel：** `reflection.session` 顶 span + `reflection.llm` / `reflection.persist` 子 span。

**Web 入口：** `/admin/memory-proposals`（admin only）— 4 个状态 tab、表格列出 proposal、点 Approve 弹对话框可覆盖 type/content/tags 后再提交，Reject 直接生效。

**v1 不做**：① 服务重启时 in-flight job 丢失（用户重 archive 一次即可，监控 `outcome=dropped`）；② 失败重试队列；③ proposal TTL 自动 reject；④ 普通用户自审视图（仅 admin）。outbox / Redis stream 是 P2 议题。

## Orchestration Router

Slice 21a 在 Agent Engine 的 ReAct 循环之前插了一个**轻量规则引擎**：每次 `agent.Engine.Run` 启动时拿最近一条 user 消息 + profile name 跑一遍配置里的规则；命中则把 `suggest.hint` 作为 system message 注入到 skills/memory 之后、user 历史之前 —— LLM 看得到，但**ReAct 自己决定是否采纳**（shadow + hint，不强路由）。

**关键点：**

- **规则在 YAML 不在 DB**：`orchestrator.rules` 段一行一规则，启动期编译 regex；非法 regex / 空 match 块 → 启动失败（与 reflection 阈值非法同位）。
- **总是 audit + 计 metric**：命中或未命中都发一行 `orchestrator.route` + 增计 `pca_orchestrator_routes_total{outcome,target_type}`，便于 shadow 期统计覆盖率与误报率。
- **三档输入控制**：`enabled: false` 完全短路（不构造引擎、不发 audit）；`inject_hint: false` 仍计算 Decision + audit + metric，但不写 system msg（纯 shadow 模式）；`default_hint: ""` 非空则在所有规则未命中时作为兜底 hint 注入。
- **不验证 target 真实存在**：rule 可以提前编排尚未发布的 workflow / 不存在的 profile，避免循环依赖。
- **不消耗 ReAct 步数**：规则引擎是纯函数，不调 LLM、不发 HTTP，P95 < 50 µs。

**Rule schema**（`config.example.yaml`）：

```yaml
orchestrator:
  enabled: true
  inject_hint: true
  default_hint: ""
  rules:
    - name: code-review-suggest
      match:
        profile: ["coding"]              # 留空 = 不限 profile
        content_regex: "(?i)code review|审查"
        # content_contains: "..."        # 也可以；regex 与 contains 任一命中即可
      suggest:
        type: workflow                   # tool | workflow | sub_agent | skill
        target: code-review              # 工具名 / workflow slug / profile 名
        hint: |
          Routing hint: this looks like a code review task. Consider
          invoking workflow.code-review if published, or delegate to
          agent.delegate(profile="review", ...).
```

env override：`PCA_ORCHESTRATOR_ENABLED` / `PCA_ORCHESTRATOR_INJECT_HINT`（规则只走 YAML）。

**Audit 单 action：** `orchestrator.route`（target=`suggest.target` 或空；metadata `matched / rule_name / type / matched_on / profile`）。  
**Metric：** `pca_orchestrator_routes_total{outcome=hit|no_match, target_type=tool|workflow|sub_agent|skill|""}`。

**21a 不做**：① 强路由 / bypass ReAct（v2+ 议题）；② ML / embedding-based routing（P2+）；③ 路由器自己 invoke workflow / 强制选 skill（仅注入提示）；④ WebUI 路由器配置页（v1 走 YAML）；⑤ 外部 MCP（拆到 21b）。

## External MCP Manager（Slice 21b）

Slice 21b 让平台能把**外部** MCP（Model Context Protocol）server 注册进 ToolBus：admin 在 `/admin/mcp-servers` 填一个 HTTP MCP URL + 可选 Bearer + 自定义 header，保存后该 server 暴露的所有 tool 立刻以 `mcp.<slug>.<tool>` 出现在 `GET /tools` 列表和 `agent.run` 的 tool_calls 候选中。Agent 调用 `mcp.<slug>.echo` 透明转发到外部 server 的 `tools/call`，结果原样返给 ReAct。

**关键设计：**

- **完全对齐 MCP 官方 2024-11-05 wire spec**：JSON-RPC 2.0，每次 invoke 走 `initialize → tools/call` 短连接（stateless，简化错误恢复）；`tools/list` 只在显式 refresh 时调，启动期靠 `tools_cache` JSONB 列直接 republish（远端宕机不阻塞启动）。
- **HTTP-only**：stdio transport 推到 P2（需要进程管理 + 重启策略）。本地 stdio MCP server 可用社区 `mcp-proxy` 转 HTTP 后接入。
- **静态 Bearer + 自定义 header**：v1 不做 OAuth flow / token 自动旋转；与 `providers.api_key` 同档明文存 DB，API 响应统一 redact 为 `"***"`，audit metadata 只保留 `sha256[:8]` 指纹。
- **租户隔离 + 防 slug 冲突**：每条 `mcp_servers` 行绑定一个 tenant；`mcpTool.Invoke` 在调用时检查 `runCtx.TenantID == server.TenantID`，跨租户调用直接拒绝（防御性二保险，与 workflow 同款 Unregister-then-Register 占位竞争）。
- **destructiveHint → IsMutating**：从 `tools/list` 的 `annotations.destructiveHint` 读取；缺省保守为 `true`，前端可显示红色徽标提醒。
- **心跳与 cache 解耦**：60s 一次 `Ping`（即 initialize）更新 `last_seen_at` / `last_error`，不刷工具列表；admin 点 refresh 才真正 `tools/list` 并重建 Bus prefix `mcp.<slug>.`。

**Admin REST `/admin/mcp-servers`**（admin role + tenant scope）：

| Method | Path | 说明 |
|--------|------|------|
| POST | `/admin/mcp-servers` | 创建 + 立即 `Initialize+ListTools+Register` |
| GET | `/admin/mcp-servers` | 列出（auth_token redact 为 `"***"`） |
| GET | `/admin/mcp-servers/:id` | 详情 |
| PUT | `/admin/mcp-servers/:id` | 更新（url/auth 改了 → 重新 Initialize+Register） |
| DELETE | `/admin/mcp-servers/:id` | 删除 + Bus.Unregister |
| POST | `/admin/mcp-servers/:id/refresh` | 显式刷新工具 |
| POST | `/admin/mcp-servers/:id/test` | 仅 Initialize（不写库不动 Bus），body 可覆盖 DB 行用来探测候选 URL |
| POST | `/admin/mcp-servers/:id/enable` / `/disable` | 切换 enabled + Register/Unregister |

`cfg.MCP.Enabled=false` 时 Manager 不构造，但 AdminHandler 仍挂载（每条路由返回 `503 mcp_disabled`，让 WebUI 区分"关闭"与"故障"）。

**Audit / Metric：**

- 6 个 action：`mcp.admin.{create,update,delete,refresh,enable,disable}` + `mcp.tool.invoke`（target=`mcp.<slug>.<tool>`，metadata `{server_id,latency_ms,error?}`）
- 3 个 metric：`pca_mcp_invocations_total{server,tool,outcome=success|error|tenant_mismatch}`、`pca_mcp_invocation_duration_seconds{server,tool}`、`pca_mcp_heartbeat_total{server,outcome=success|fail}`

env override：`PCA_MCP_ENABLED` / `PCA_MCP_HEARTBEAT_INTERVAL` / `PCA_MCP_INVOKE_TIMEOUT` / `PCA_MCP_LIST_TOOLS_TIMEOUT`。compose 同时跑 `mock-mcp:8083`（`internal/mcp/mockserver`，单工具 `echo`），E2E 步 63 走完整 register → tools/list → invoke 链路。

**21b 不做**：① stdio transport（推 P2）；② OAuth flow / token 自动旋转；③ MCP `prompts/list` / `resources/list` / `sampling`（只做 tools）；④ WebSocket / 长连接 session（每次 invoke 都重新 initialize）；⑤ 工具粒度授权（v1 = server 级 enabled 开关 + tenant 隔离 + admin only）；⑥ 跨租户共享 server（每条行强绑 tenant）。

## Audit Hash Chain（Slice 22a）

Slice 22a 把 `audit_log` 改造为防篡改：每行写入时计算 `entry_hash = SHA-256(prev_hash ‖ canonical(entry))`，下一行的 `prev_hash` 指向上一行的 `entry_hash`，整张表形成一条 SHA-256 链。任何 DBA 或入侵者 `UPDATE` 任意一行后，该行及其后所有行的链校验都会失败。

写入路径（`internal/audit/repo.go::Append`）在 `BeginTx → pg_advisory_xact_lock(hashtext('audit_log')) → SELECT 上一行 entry_hash → INSERT 含 prev/entry → COMMIT` 序列内完成。`pg_advisory_xact_lock` 跨 goroutine、跨副本（22d K8s 多 pod 时）串行化所有 Append，保证链不分叉。`occurred_at` 在哈希前截到 microsecond 精度，与 PG `timestamptz` 实际存储字节对齐，避免 verify 时哈希输入与存储不一致。

Canonical 编码用 ASCII Record Separator（0x1E）分隔字段：

```
prev (32B raw bytes)
  || RS || occurred_at_rfc3339nano_utc
  || RS || tenant_id_or_empty
  || RS || user_id_or_empty
  || RS || action
  || RS || target
  || RS || method
  || RS || path
  || RS || status
  || RS || duration_ms
  || RS || canonical_metadata_json
```

`json.Marshal(map[string]any)` 对 key 升序排列，所以 metadata 天然 canonical。nil UUID 编码为空串，与 `*uuid.UUID(uuid.Nil)` 区分。

验证端点 `GET /audit/verify`（admin-only，挂在 `auth.Middleware + RequireAdmin` 组）流式遍历 audit_log，对每行重算 `Hash(running_prev, entry)` 与存储值比对，首个不匹配立刻返回：

```json
{
  "ok": false,
  "rows_checked": 233,
  "chain_start_id": 1,
  "first_broken_id": 234,
  "reason": "entry_hash_mismatch",
  "expected_hash": "abc...",
  "actual_hash": "def..."
}
```

`reason` 取值为 `prev_hash_mismatch`（链指针被改，常见于 DELETE 中间行）或 `entry_hash_mismatch`（行字段被改）。

`from_id` query 参数（≥0）有两种语义：
- `from_id=0`（默认）：全表 verify，首链行 `prev_hash` 必须等于 32 零字节（genesis）
- `from_id>0`：suffix verify，信任首行 prev_hash 做种子，只校验从该 id 之后的链内部一致性。用于跳过已知断点确认后续没继续被动

迁移 `0021_audit_hash_chain` 给 `prev_hash` / `entry_hash` 都设了 32 零字节的 DEFAULT，所以迁移前已存在的行（pre-chain）两列都是零；verify 把这些行计入 `pre_chain_rows` 跳过，从首个非零 entry_hash 行开始走链。链保护从 22a 部署后的第一次 Append 起算，pre-chain 段不在 22a 范围内。

**22a 不做**：① Merkle tree / Notary 外部 anchoring（v2+，当前线性链对单租户够用）；② 跨租户独立子链（v1 全局单链，租户隔离靠 List 过滤）；③ 哈希算法可配置（v1 硬编码 SHA-256）；④ 自动 verify 定时任务 + 报警（v1 admin 显式调用）；⑤ 链断点修复 / 重置工具（v1 检测出篡改后人工介入）；⑥ KMS / HSM 数字签名（v2+）。

## Agent Skills

Cursor 风格的"程序化知识"子系统：每个 Skill 是一个 `SKILL.md` 文件（YAML frontmatter + Markdown body），启动期由 `internal/skills.Registry` 递归扫描 `skills.dirs` 加载，Engine 在每次 Run 之前把当次选中的 Skill body 合并成一条系统消息注入。

注入来源的优先级（高 → 低）：

1. Run 级 — `POST /agent/run` body 里的 `skill_ids`
2. Session 级 — `sessions.skill_ids` 列（POST /sessions 时设置）
3. Profile 级 — `Profile.SkillIDs`（如 `coding` 默认带 `platform-coding-standards`）
4. Config 默认 — `skills.default_skill_ids`

高优先级非空即覆盖低优先级（不合并）。每次注入受 `max_skills_per_run`（默认 5）与 `max_injected_chars`（默认 24000）双重约束，超出按顺序截断并在 metrics + audit 里标 `truncated=true`。

只读端点：

- `GET /skills` — 列出已加载的 Skill 元信息（不含 body）
- `GET /skills/:id?include=body` — 显式拿单条 body

Skills 不是工具，不走 Tool Bus；Agent 模型直接在系统消息里读到。生产部署时通过 `skills.dirs` 挂载内部知识库。`skills.enabled=false` 退化为切片 12 之前的纯 Profile 行为。

### 租户 Skill（切片 17）

文件系统注册表是平台级的。租户希望覆写或追加 Skill 时走 DB：`skills` 表按 `(tenant_id, skill_key)` 存放；`tenant_profile_skills` 让租户覆盖 Profile 默认绑定。Resolver 解析时按租户拉一份启用的 DB Skill，与 FS 注册表合并，**同 key 时 DB 胜出**，从而支持"按租户改写 platform-coding-standards"。

管理 API（admin only，挂在 `auth.RequireAdmin` 之后）：

- `POST/GET/PUT/DELETE /admin/skills[/:key]` — 租户 Skill CRUD。`skill_key` 限制为 `^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$`，body 上限 64KB。列表默认省略 body；`?include=body` 显式拿。
- `GET/PUT /admin/profiles/:name/skills` — 设/取该 Profile 在当前租户下的 skill_keys（覆盖 in-code `Profile.SkillIDs`，空数组 = 退回 in-code 默认）。

所有写操作通过 `audit.Sink` 写入 `skill.admin.{create,update,delete,profile_bind}` 审计条目。Web 端入口在 TopBar → "Skills"（仅 admin 可见，路径 `/admin/skills`）。

## Web Frontend

React + Vite + Tailwind + shadcn/ui SPA，源码在 `internal/webui/`，由 `go:embed` 打进 server 二进制。登录、会话列表、Chat（含 WebSocket 流式 + 工具调用折叠卡）。

### Web 入口（页面清单）

| 路由 | 鉴权 | 用途 |
|---|---|---|
| `/login` | - | 本地账号 / OIDC 登录 |
| `/` | 登录用户 | Home：会话列表 + 新建（含 profile + skill_ids 选择） |
| `/sessions/{id}` | 登录用户 | Chat：WS 流式 + 工具调用卡 + 沙箱文件浏览侧栏 |
| `/memories` | 登录用户 | 用户记忆 CRUD + 标签过滤 |
| `/toolbox` | 登录用户 | 只读列出全部 internal 工具；mutating 工具有红色徽标 |
| `/audit` | admin | 审计日志查询 |
| `/admin/skills` | admin | 租户 Skill CRUD + Profile binding（切片 17） |
| `/workflows` | admin | Workflow DSL CRUD（Monaco YAML + **只读流程图预览** Slice 19d）+ publish/unpublish + invoke（含 dry_run）+ 最近 runs 抽屉 |
| `/admin/memory-proposals` | admin | Reflection 候选审核（切片 20） |
| `/admin/mcp-servers` | admin | 外部 MCP 管理（切片 21b） |

`/workflows` 与 `/toolbox` 是切片 19b 新增；19d 在编辑页增加只读流程图。Monaco Editor 通过 CDN worker，首屏 bundle 不含编辑器主体。

### 本地开发（前后端分离）

```powershell
# 终端 1: 起后端 (:8080)
go run ./cmd/server --config config\config.yaml

# 终端 2: 起 vite dev server (:5173, 自动 proxy /api /sessions /tools /auth ... 到 :8080)
cd internal\webui
npm install
npm run dev
# 浏览器访问 http://localhost:5173
```

### 生产构建（单二进制）

```powershell
make build         # = make web + go build -o bin/server ./cmd/server
.\bin\server --config config\config.yaml
# 浏览器访问 http://localhost:8080
```

docker-compose 路径会自动跑多阶段 build：`node:20-alpine` 先 `npm run build`，再 `COPY --from=web` 进 Go build。

## 审计

两层并存：

1. **HTTP 访问层** — `audit.Middleware` 给每个请求落一行 `action=http_request`，覆盖全量访问日志。
2. **业务事件层** — 关键 handler/service 显式落领域事件，便于按动作过滤。固定 action 枚举（命名规范：`<domain>.<verb>[.<outcome>]`）：

| Action | Target | Metadata 关键字段 |
|---|---|---|
| `auth.login.success` | user email | `user_id`, `tenant_slug`, `role` |
| `auth.login.failure` | user email | `reason` (`bad_credentials` / `wrong_tenant`) |
| `sandbox.create` | sandbox_id | `image` |
| `sandbox.destroy` | sandbox_id | — |
| `session.create` | session_id | `model`, `profile` |
| `session.archive` | session_id | — |
| `session.ws.open` | session_id | — |
| `session.ws.close` | session_id | `duration_ms` |
| `tool.invoke.error` | tool_name | `error_class` |

`GET /audit` 仅对 `role=admin` 开放（`auth.RequireAdmin` 中间件 + 独立 router group），且永远按 `cl.TenantID` 过滤——不存在跨租户视图。`audit.Sink` 写盘 detached + 5s 超时，业务路径不会被审计写阻塞。

PII 最小化：metadata 不存 prompt 内容、不存 shell 命令原文、不存文件内容，仅存 ID/计数/错误类目；`auth.login.failure` 的 target 字段记 email 是为支持追失败登录所必须。

## 可观测性

三层闭环：

1. **结构化日志** — `internal/logx` 包装 `slog`，JSON handler 默认。`logx.FromCtx(ctx)` 自动注入 `request_id` / `trace_id` / `span_id` / `tenant_id` / `user_id`。日志格式与 level 由 `observability.log_format` / `observability.log_level` 控制（或 `PCA_OBSERVABILITY_LOG_FORMAT` / `_LOG_LEVEL`）。
2. **Trace** — OTel + `otelgin` 自动根 span；以下路径打了手工子 span，便于在 Jaeger 看链路：

   | Span 名 | 包 | 关键属性 |
   |---|---|---|
   | `agent.run` / `agent.step` | `internal/agent` | `agent.model`、`agent.profile`、`agent.max_steps`、`agent.step_index`、`agent.finish_reason` |
   | `tool.invoke` | `internal/toolbus` | `tool.name`、`tool.outcome`、`tool.duration_ms`、`tool.error_class` |
   | `model.chat` / `model.chat_stream` / `model.embed` | `internal/modelgw` | `model.id`、`model.prompt_tokens`、`model.completion_tokens`、`model.input_count` |
   | `sandbox.create` / `sandbox.exec` / `sandbox.destroy` | `internal/sandbox` | `sandbox.image`、`sandbox.id`、`sandbox.exec.cmd_len`、`sandbox.exec.exit_code`、`sandbox.exec.timed_out` |

   compose 启动时把 OTLP 指向 `jaeger:4317`，访问 <http://localhost:16686> 查 trace。

3. **指标 (Prometheus)** — `GET /metrics` 暴露 10 个 `pca_*` 指标：

   | 指标 | 类型 | 标签 |
   |---|---|---|
   | `pca_http_requests_total` | Counter | `method`、`route`、`status_code` |
   | `pca_http_request_duration_seconds` | Histogram | `method`、`route` |
   | `pca_tool_invocations_total` | Counter | `tool`、`outcome` |
   | `pca_tool_invocation_duration_seconds` | Histogram | `tool` |
   | `pca_model_calls_total` | Counter | `model`、`kind`、`outcome` |
   | `pca_model_call_duration_seconds` | Histogram | `model`、`kind` |
   | `pca_model_tokens_total` | Counter | `model`、`direction` (in/out) |
   | `pca_sandbox_active` | UpDownCounter | — |
   | `pca_ws_connections_active` | UpDownCounter | — |
   | `pca_sessions_created_total` | Counter | `profile` |

   `route` 来自 gin 的模板路径（带 `:id` 占位符）保证 cardinality 有界。`/metrics`、`/healthz`、`/readyz` 不进 `pca_http_*` 指标也不进 `audit_log`。

   鉴权双通道：标准 admin JWT 或 `observability.metrics_token` 静态 bearer。Prom scraper 走静态 token（JWT TTL 会让定时抓取过期）。compose 的 `prometheus.yml` 已配好；UI <http://localhost:9090>。**生产环境必须改 `PCA_OBSERVABILITY_METRICS_TOKEN` 与 `prometheus.yml` 里的 token。**

## 配置

见 `config/config.example.yaml`。所有字段可用 `PCA_<UPPER>_<UPPER>` 环境变量覆盖（点号换下划线）。

### 配额与限流（切片 13）

| 配置项 | 环境变量 | 说明 |
|--------|----------|------|
| `quota.llm_tokens_per_day` | `PCA_QUOTA_LLM_TOKENS_PER_DAY` | 每租户+用户每日 LLM token 上限（chat + embeddings） |
| `quota.sandbox_max_active` | `PCA_QUOTA_SANDBOX_MAX_ACTIVE` | 每租户同时活跃沙箱数（pending/running/destroying） |
| `quota.tool_invoke_per_minute` | `PCA_QUOTA_TOOL_INVOKE_PER_MINUTE` | 每租户+用户每分钟 `POST /tools/invoke` 次数 |
| `rate_limit.per_minute` | `PCA_RATE_LIMIT_PER_MINUTE` | 每租户+用户每分钟受保护 HTTP 请求数 |

超限返回 **HTTP 429**。工具/模型网关：`type=rate_limit_error`、`code=quota_exceeded`；沙箱创建：`{"error":"quota_exceeded","kind":"sandbox.active",...}`。

`deploy/compose` 默认 `PCA_QUOTA_SANDBOX_MAX_ACTIVE=1` 以便 E2E 步骤 43 验证沙箱配额；生产请按容量调大。

### OIDC SSO（切片 15）

| 配置项 | 环境变量 |
|--------|----------|
| `auth.oidc.enabled` | `PCA_AUTH_OIDC_ENABLED` |
| `auth.oidc.issuer` | `PCA_AUTH_OIDC_ISSUER` |
| `auth.oidc.client_id` | `PCA_AUTH_OIDC_CLIENT_ID` |
| `auth.oidc.client_secret_env` | 指向的环境变量名（如 `OIDC_CLIENT_SECRET`） |
| `auth.oidc.redirect_url` | `PCA_AUTH_OIDC_REDIRECT_URL` |
| `auth.local_enabled` | `PCA_AUTH_LOCAL_ENABLED` |

Keycloak / Azure AD 示例见 [`deploy/compose/OIDC.md`](deploy/compose/OIDC.md)。
