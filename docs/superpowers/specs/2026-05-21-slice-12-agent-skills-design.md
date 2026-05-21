# Slice 12 — Agent Skills 设计 Spec

> Status: Draft — 待评审
> Author: planning session (2026-05-21)
> Related: 主 spec §4/§5、Slice 5（Agent Engine）、Slice 7/11（Memory）、HANDOFF §3.2（会话注入）
> Implementation plan: `docs/superpowers/plans/2026-05-21-slice-12-agent-skills.md`

## 1. 概述

**Agent Skills** 是可版本化的**程序性知识包**（`SKILL.md` + 可选 `references/`），用于指导 Agent **如何**完成任务，而不是替代 Tool Bus **执行**任务。

本切片在 PCA 中引入 Skills 子系统，与现有能力分工如下：

| 能力 | 职责 | 是否执行副作用 |
|---|---|---|
| **Tool Bus** | 调沙箱 / LLM / 记忆等 | ✅ |
| **Memory** | 用户/Agent 沉淀的**事实与偏好**（可向量检索） | 仅 CRUD |
| **Profile** | 角色人设 + 工具白名单 + 步数上限 | 否 |
| **Skill（本切片）** | SOP、规范、检查清单、领域最佳实践 | 否（只注入 context） |
| **Workflow（未来）** | 确定性多步 DAG | 通过 Tool 节点执行 |

**核心叙事**

- **标准化**：租户可预装「React 性能」「Go 代码审查」等 Skill，降低对话随机性。
- **可运维**：Skill 来自 Git 目录或 DB，可审计、可禁用；与 Cursor `npx skills` 生态格式兼容（MVP 先读 `SKILL.md` frontmatter）。
- **不膨胀 Tool Bus**：Skill 不注册为 `tools/invoke` 除非未来显式做 `skill.run`（本切片不做）。

**分三期交付（同一切片编号，按子阶段发布）**

| 阶段 | 代号 | 交付物 |
|---|---|---|
| P0 | **12a — Filesystem Skills** | 启动扫描 `skills/` → Profile 绑定 → Agent/Session 注入 |
| P1 | **12b — Tenant Skills API** | DB 存储 + admin CRUD + 租户启用表 + Web UI 开关 |
| P2 | **12c — Ecosystem Sync** | 可选对接 `skills.sh` / Git URL 同步、版本与校验 |

**本 spec 以 12a 为验收边界**；12b/12c 只写接口预留，不阻塞 12a 合并。

**明确不在 12a 范围**

- External MCP adapter（另切片）
- Skill 自动生成 / NL 编译（属 Workflow Authoring）
- Skill 内容向量化检索（可用 Memory 或 12c；12a 仅按 id/name 绑定）
- `skill.*` 可执行工具（12a 可选只读 `skill.list` 推迟到 12b）
- 与 `npx skills` 安装器打通（12c）

---

## 2. 前置条件

- Slice 5（Agent Engine + Profile）已交付
- Slice 6（Session + WS）已交付
- Slice 7/11（Memory）已交付 — **不依赖** vector，但注入钩子设计需与「未来 memory 自动注入」共用
- Slice 10（Observability）已交付 — 新增 `pca_skill_injections_total` 等指标

建议 Slice 11（Vector Memory）commit 后再开工 12a，避免并行改 `agent.Engine` / session 路径。

---

## 3. 与 Memory / Profile 的边界（ADR）

### ADR-64 — Skill 是只读程序性知识，不进 Tool Bus 执行路径

- Skill 正文**永不**经 `Bus.Invoke` 执行。
- Agent 仍通过 `memory.save` 写**个案**；Skill 写**标准**。
- 禁止把整份 Skill 同步进 `memories` 表（避免 vector dedup 与双源维护）；若需「用户改写版 SOP」，用 `memory.lesson` + tag `derived-from-skill:<id>`。

### ADR-65 — 注入点固定在 Agent Run 的 system 层

- 在 `agent.Engine.Run` 拼 `messages` 时，顺序为：
  1. `Profile.SystemPrompt`（短角色说明）
  2. **Resolved Skills**（0..N 块，每块一个 system message 或合并为单块 `## Skill: <name>`）
  3. （未来）Memory 自动注入块
  4. 用户/助手历史消息
- 不修改 tool schema；不改变 `ToolAllowlist` 语义。

### ADR-66 — 12a 解析 Cursor-compatible SKILL.md frontmatter

最小字段（与社区惯例对齐）：

```yaml
---
name: vercel-react-best-practices
description: React and Next.js performance optimization guidelines from Vercel Engineering.
---
```

- `name`：全局唯一 id（`[a-z0-9][a-z0-9-]*`，最长 64）
- `description`：给 LLM / UI 的摘要；**不**用于 12a 自动路由（路由见 ADR-67）
- body：Markdown 正文，注入时原样（经长度截断）

### ADR-67 — 12a 路由：Profile 静态绑定 + Session 可选覆盖

| 来源 | 优先级 | 说明 |
|---|---|---|
| `Session.skill_ids`（可选列/API） | 最高 | 创建会话时指定；空则回落 Profile |
| `Profile.SkillIDs []string` | 中 | 代码或 config 注册 profile 时写死 |
| `skills.default_enabled`（config） | 低 | 全局默认 Skill 列表 |

**不做** 12a 的「根据用户消息 embedding 匹配 Skill」（与 Memory 检索混淆，放 12c 或独立切片）。

### ADR-68 — Token 预算硬上限，超限 fail-soft

- 配置 `skills.max_injected_chars`（默认 24000，约 6k tokens 量级）与 `skills.max_skills_per_run`（默认 5）。
- 超限时按 Profile/Session 列表顺序截断，并打 `warn` 日志 + audit 事件 `skill.inject.truncated`。
- 不允许因 Skill 注入导致整次 Run 失败（与 Memory embed 硬失败策略区分）。

---

## 4. 架构

```
+------------------------------------------------------------------+
| 配置 / 存储                                                       |
|  skills.dirs[] (glob)     # 12a: 本地目录，递归找 **/SKILL.md      |
|  skills table (optional)  # 12b: 租户级 DB                         |
+------------------------------------------------------------------+
                  |
                  v
+------------------------------------------------------------------+
| internal/skills/                                                  |
|  Parser       — frontmatter + body                               |
|  Registry     — name -> SkillMeta + content hash                 |
|  Resolver     — (tenant, user, profile, session) -> []Skill      |
|  Injector     — BuildSystemBlocks(skills) -> []ChatMessage       |
+------------------------------------------------------------------+
                  | used by
                  v
+------------------------------------------------------------------+
| agent.Engine.Run                                                  |
|  Profile prompt + Injector + history -> modelgw.Chat             |
+------------------------------------------------------------------+
                  ^
                  | optional future
+------------------------------------------------------------------+
| session.Service.SendMessage — session.skill_ids 传入 RunInput      |
+------------------------------------------------------------------+
```

**与 HANDOFF「会话起始 memory 注入」共用扩展点**

建议在 `internal/agent` 定义：

```go
// ContextComposer 在 Engine 内组合 system 层；12a 只实现 Skill 部分。
type ContextComposer interface {
    ComposeSystem(ctx context.Context, in ComposeInput) ([]modelgw.ChatMessage, ComposeMeta, error)
}
```

Slice 13（memory auto-inject）向同一接口追加，避免 `engine.go` 反复改动。

---

## 5. 数据模型

### 5.1 12a — 无 DB（纯文件）

目录约定（可多个根）：

```
/skills/                          # 或 config: skills.dirs
  platform/                       # 平台内置
    vercel-react-best-practices/
      SKILL.md
  tenant-overrides/               # 可选，12b 前用挂载卷模拟
    ...
```

`SkillMeta`（内存索引）：

```go
type SkillMeta struct {
    ID          string    // frontmatter name
    Description string
    Version     string    // 文件 content SHA256 前 12 hex（变更检测）
    SourcePath  string    // 审计用，不暴露给 LLM
    CharCount   int
}
```

### 5.2 12b — DB（预留）

```sql
-- migration 0011_skills.up.sql（12b，本文档仅预留）
CREATE TABLE skills (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id    UUID NOT NULL REFERENCES tenants(id),
  skill_key    TEXT NOT NULL,          -- 逻辑名，租户内唯一
  description  TEXT NOT NULL,
  body         TEXT NOT NULL,
  content_hash TEXT NOT NULL,
  enabled      BOOLEAN NOT NULL DEFAULT true,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, skill_key)
);

CREATE TABLE profile_skills (
  tenant_id    UUID NOT NULL,
  profile_name TEXT NOT NULL,
  skill_key    TEXT NOT NULL,
  sort_order   INT NOT NULL DEFAULT 0,
  PRIMARY KEY (tenant_id, profile_name, skill_key)
);
```

### 5.3 Session 扩展（12a 可选列）

```sql
-- migration 0011_session_skills.up.sql
ALTER TABLE sessions ADD COLUMN skill_ids TEXT[] DEFAULT '{}';
```

空数组 = 使用 Profile 默认；非空 = 本会话覆盖。

---

## 6. 配置

`config/config.example.yaml` 新增：

```yaml
skills:
  enabled: true
  dirs:
    - "./skills"              # 相对 server 工作目录；compose 可挂载卷
  default_skill_ids: []        # 全局默认，如 ["platform-coding-standards"]
  max_injected_chars: 24000
  max_skills_per_run: 5
  hot_reload: false            # dev: true 时每 60s 重扫；prod: false
```

环境变量：`PCA_SKILLS_ENABLED`、`PCA_SKILLS_DIRS`（逗号分隔）等。

---

## 7. 接口

### 7.1 Agent `RunInput` 扩展

```go
type RunInput struct {
    // ... existing ...
    SkillIDs []string `json:"skill_ids,omitempty"` // 显式覆盖；WS/REST 传入
}
```

`session.Service` 在 `SendMessage` 时把 `sessions.skill_ids` 填入 `RunInput`。

### 7.2 REST（12a 只读）

| Method | Path | 鉴权 | 说明 |
|---|---|---|---|
| GET | `/skills` | Bearer | 列出当前租户可见 Skill（meta only，无 body 或 `?include=body`） |
| GET | `/skills/{id}` | Bearer | 单条 meta + body（admin 或本人调试） |

12b 增加：`POST/PUT/DELETE /skills`（admin）、`PUT /profiles/{name}/skills`。

### 7.3 内部工具（12b，12a 不做）

| 工具名 | 作用 |
|---|---|
| `skill.list` | Agent 查看可用 Skill 摘要 |
| `skill.describe` | 拉取单个 Skill body（受 token 限制） |

理由：12a 靠 Profile/Session 预绑定即可；动态发现放 12b。

---

## 8. Profile 变更

```go
type Profile struct {
    Name          string
    SystemPrompt  string
    ToolAllowlist []string
    SkillIDs      []string // 新增；12a
    MaxSteps      int
}
```

`DefaultCodingProfile()` 示例：

```go
SkillIDs: []string{"platform-coding-standards"},
ToolAllowlist: []string{
    "fs.read", "fs.write", "fs.list", "fs.glob",
    "grep", "shell.exec",
    "llm.chat", "llm.embed",
    "memory.save", "memory.search", "memory.list", "memory.delete",
},
```

（顺带修正：coding profile 白名单应包含 memory.*，与 main 注册一致。）

---

## 9. 注入格式（LLM 可见）

单 Skill 合并为一块 system message（推荐，减少 message 条数）：

```text
You are a coding agent operating inside a private development platform.
...

## Active Skills

### Skill: platform-coding-standards
<description line>

<markdown body, possibly truncated>

### Skill: vercel-react-best-practices
...
```

审计 metadata（`skill.inject` 事件）记录：`skill_ids[]`、`chars_injected`、`truncated: bool`。

---

## 10. 安全与多租户

| 风险 | 12a 处置 |
|---|---|
| Prompt 注入（恶意 SKILL.md） | 仅 admin 可写 `skills/` 挂载；12b 仅 admin API 写库 |
| 跨租户泄露 | 12a 全局目录 = 平台级；租户隔离等 12b `tenant_id` |
| Token 放大攻击 | `max_injected_chars` + `max_skills_per_run` |
| 路径遍历 | Parser 只接受 `skills.dirs` 下 `**/SKILL.md`，拒绝 `..` |

---

## 11. 可观测性

| 指标 | 类型 | 标签 |
|---|---|---|
| `pca_skill_load_total` | Counter | `outcome=ok\|parse_error` |
| `pca_skill_injections_total` | Counter | `truncated=true\|false` |
| `pca_skill_injected_chars` | Histogram | — |

Trace span：`skill.resolve`、`skill.inject`（`agent.run` 子 span）。

Audit action：`skill.inject`（target=session_id，metadata: skill_ids, chars, truncated）。

---

## 12. 测试策略

| 层级 | 内容 |
|---|---|
| 单元 | Parser（合法/缺 frontmatter/超长）、Resolver 优先级、Injector 截断 |
| 集成 | 临时 `skills/` 目录 + Engine httptest：断言发往 modelgw 的 messages 含 Skill 标题 |
| E2E | 在 `test-e2e.sh` 增加 [40-42]：挂载 demo skill → chat 断言 mock 收到 system 含 marker；session 级 skill_ids 覆盖 |

Demo skill 建议：`skills/e2e/e2e-marker/SKILL.md`，body 含唯一字符串 `E2E_SKILL_MARKER_V1`。

---

## 13. 实施任务分解（12a）

详细步骤、文件清单、验收标准见 **`docs/superpowers/plans/2026-05-21-slice-12-agent-skills.md`**（Task 0–10）。

| Task | 内容 | 估时 |
|---|---|---|
| 0 | 设计 spec 定稿 | 0.25d |
| 1 | Parser + 类型 + 错误哨兵 | 0.5d |
| 2 | Registry + Resolver | 0.5d |
| 3 | Injector | 0.25d |
| 4 | Config + main 启动加载 | 0.25d |
| 5 | ContextComposer + Engine | 0.5d |
| 6 | Profile + Session `skill_ids` | 0.5d |
| 7 | `GET /skills` API | 0.25d |
| 8 | Metrics + Audit + Dockerfile | 0.25d |
| 9 | E2E 42 步 + README + demo skills | 0.5d |
| 10 | 全量回归 | 0.25d |

**合计约 3–4 人日**（含 review）。

### 推荐 commit 切分

见 implementation plan §推荐 commit 切分（5 个 commits）。

---

## 14. 12b / 12c 路线图（简表）

| 能力 | 12b | 12c |
|---|---|---|
| 租户 CRUD API + Web UI | ✅ | |
| admin 审批 / enabled 开关 | ✅ | |
| `skill.list` / `skill.describe` 工具 | ✅ | |
| Git / skills.sh 同步 worker | | ✅ |
| 按描述 embedding 自动选 Skill | | ✅（可选） |
| Skill 签名 / 版本 pin | | ✅ |

---

## 15. 与主 spec 目标的对齐

| 主 spec 目标 | Skill 切片贡献 |
|---|---|
| 对话即编程 | 注入编码规范，减少胡编乱造 |
| 沉淀即资产 | Skill 包 = 可 Git 管理的资产；Workflow 仍为确定性资产 |
| 越用越聪明 | Memory 存个案；Skill 存标准；二者互补 |
| 智能流程编排 | Skill 指导 ReAct；Workflow 执行 DAG；Tool 叶子执行 |

---

## 16. 开放问题（评审时拍板）

1. **12a 是否包含 `GET /skills`？** 建议包含（利于 Web UI 后续展示）。
2. **system 合并为一条还是多条 message？** 建议一条（省 token overhead）。
3. **平台 `skills/` 是否打进 Docker 镜像？** 建议 `COPY skills /app/skills` + compose 可覆盖挂载。
4. **coding profile 是否默认启用 `vercel-react-best-practices`？** 建议仅 `platform-coding-standards`（短），重型 Skill 由租户 opt-in。

---

## 附录 A — 与 Cursor Skills CLI 的关系

| Cursor 生态 | PCA 12a |
|---|---|
| `SKILL.md` + YAML frontmatter | ✅ 解析兼容 |
| `npx skills add owner/repo@skill` | ❌ 12c 再议（可落盘到 `skills/`） |
| `~/.agents/skills` 全局目录 | ❌ 不读用户主目录；仅服务端 `skills.dirs` |
| Agent 自动 `Read skill` | PCA 用注入替代，无需模型再读文件 |

## 附录 B — 示例 `platform-coding-standards` 摘要

```markdown
---
name: platform-coding-standards
description: Private Coding Agent platform conventions for tools, sandbox, and memory.
---

- Always pass sandbox_id to fs.* and shell.exec tools.
- Prefer memory.search before claiming user preferences.
- Do not exfiltrate secrets; never log JWT or API keys.
- Stop with a concise summary when the task is done.
```

（正文控制在 ~2KB 内，便于默认注入。）
