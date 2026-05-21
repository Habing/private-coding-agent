# Slice 12 — Agent Skills (12a Filesystem + Injection) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 交付 `internal/skills` 包 + Agent/Session 注入链路：启动扫描 `skills/**/SKILL.md` → Profile/Session 解析 → `agent.Engine.Run` 将 Active Skills 合并进 system message；只读 `GET /skills`；E2E 扩至 **42 步**。

**Architecture:** Skills 是**只读程序性知识**，不经 Tool Bus。`Registry` 在进程启动（或 hot_reload）时索引文件；`Resolver` 按 ADR-67 优先级产出 id 列表；`Injector` 渲染 `## Active Skills` 块并 enforce char/skill 上限；`agent.ContextComposer`（12a 仅 Skill 实现）在拼 messages 时插入 Profile prompt 之后、历史之前。Session 表增加 `skill_ids TEXT[]`；`POST /agent/run` 与 WS 路径透传 `RunInput.SkillIDs`。

**Tech Stack:**
- Go 1.25+、gin、testify、google/uuid（沿用）
- `gopkg.in/yaml.v3` 解析 frontmatter（若项目尚无，新增 direct dep；或手写 `---` 分隔最小解析避免新依赖 — **推荐手写** 仅支持 `name`/`description` 两行，与 Cursor 最小子集一致）
- 标准库 `crypto/sha256`、`path/filepath`、`io/fs`

**Design spec:** `docs/superpowers/specs/2026-05-21-slice-12-agent-skills-design.md`

---

## Context

主 spec 要求 Agent / Workflow 共用 Tool Bus，但**不解决**「按什么 SOP 调工具」。Slice 7/11 的 Memory 存**事实与偏好**；Profile 只有短 system prompt。Cursor 生态的 **Agent Skills**（`SKILL.md`）是成熟的程序性知识格式，PCA 12a 在服务端复用该格式并注入 LLM context，无需模型再 `Read` 文件。

**用户已确认 12a 边界（见 design spec §1）：**
1. 存储：**仅文件系统** `skills.dirs[]`；DB/API 写操作推到 12b
2. 路由：**Profile.SkillIDs + Session.skill_ids + config default**；不做 embedding 自动选 Skill
3. 注入：**单条合并 system message**（`## Active Skills`）；超限 truncate，不 fail Run
4. 工具：**不**新增 `skill.*` invoke 工具（12b）
5. 顺带修正：`DefaultCodingProfile` ToolAllowlist 补上 `memory.*`

---

## 非目标（12a 明确出栈）

- 租户级 DB `skills` 表、admin CRUD、Web UI 管理页（12b）
- `npx skills` / skills.sh 同步 worker（12c）
- Skill 向量化检索、与 Memory dedup 打通
- External MCP adapter
- 会话起始 **Memory** 自动注入（Slice 13；仅预留 `ContextComposer` 接口）
- Workflow Engine / NL→Workflow
- 把 Skill 注册为 `tools/invoke` 可执行项

---

## 关键不变量

1. **Skill 永不 Invoke** — 无代码路径 `bus.Invoke("skill.*")`。
2. **解析安全** — 只遍历 `skills.dirs` 下名为 `SKILL.md` 的文件；`filepath.Clean` + 拒绝跳出 root 的路径。
3. **id 唯一** — 重复 `name` 时后者覆盖前者并 `slog.Warn`（记录 source path）。
4. **未知 id 跳过** — Resolver 里配置的 id 在 Registry 不存在时跳过并 warn，不 500。
5. **disabled 全局** — `skills.enabled=false` 时 Registry 空操作、Injector 返回空、Engine 行为与 slice 11 一致。
6. **审计不含正文** — `skill.inject` metadata 只记 `skill_ids`、`chars_injected`、`truncated`；不记 body。
7. **mock 测试可观测** — 增强 mock-provider：若 system message 含 `E2E_SKILL_MARKER_V1`，在 assistant 回复中回显 `skill-marker-ok`（仅测试用，见 Task 9）。

---

## Architecture

```
启动 main()
  -> skills.NewRegistry(cfg) -> LoadFromDirs(dirs)  // 索引 SKILL.md
  -> skills.NewResolver(registry, cfg)
  -> agent.NewSkillComposer(resolver, cfg) implements ContextComposer
  -> agent.NewEngine(gw, bus, profiles, composer)

Engine.Run(in)
  -> composer.ComposeSystem(ctx, {Tenant, User, Profile, SkillIDs from in})
       -> resolver.Resolve(...) -> []*Skill
       -> injector.Build(profilePrompt, skills) -> []ChatMessage
  -> append in.Messages...
  -> ReAct loop (unchanged)

POST /sessions { skill_ids?: [] }
  -> sessions.skill_ids 落库

WS / POST /agent/run
  -> RunInput.SkillIDs = session.SkillIDs (session 优先) 或 body 覆盖
```

**Resolve 优先级（高 → 低）：**
1. `RunInput.SkillIDs` 非空（agent/run body 或 WS 未来扩展）
2. `Session.SkillIDs` 非空（PG `TEXT[]`）
3. `Profile.SkillIDs`
4. `config.skills.default_skill_ids`

---

## File Structure

```
skills/                                    Task 8: 平台 + e2e demo
  platform/platform-coding-standards/SKILL.md
  e2e/e2e-marker/SKILL.md

internal/skills/
  types.go                 Task 1
  errors.go                Task 1
  parser.go                Task 1
  parser_test.go           Task 1
  registry.go              Task 2
  registry_test.go         Task 2
  resolver.go              Task 2
  resolver_test.go         Task 2
  injector.go              Task 3
  injector_test.go         Task 3
  config.go                Task 4 (SkillsConfig struct, validation)

internal/agent/
  composer.go              Task 5: ContextComposer + ComposeInput/Meta
  composer_test.go         Task 5
  engine.go                Task 5: wire composer
  profile.go               Task 6: SkillIDs + memory.* allowlist
  profile_test.go          Task 6
  types.go                 Task 5: RunInput.SkillIDs

internal/skills/handler.go       Task 7
internal/skills/handler_test.go  Task 7

internal/db/migrations/
  0011_session_skill_ids.up.sql    Task 6
  0011_session_skill_ids.down.sql  Task 6

internal/session/
  types.go                 Task 6: Session.SkillIDs, CreateRequest.SkillIDs
  repo.go                  Task 6: CRUD columns
  repo_test.go             Task 6
  service.go               Task 6: pass SkillIDs to RunInput
  service_test.go          Task 6

internal/config/config.go        Task 4
config/config.example.yaml       Task 4

internal/metrics/metrics.go      Task 8: pca_skill_* counters
internal/audit/                  Task 8: document skill.inject action in README

cmd/server/main.go               Task 4,5,7: wire registry/resolver/composer/handler

internal/modelgw/mockserver/main.go  Task 9: e2e marker echo

deploy/compose/test-e2e.sh       Task 9: [40-42], sed 39->42
Dockerfile                       Task 8: COPY skills /app/skills

README.md                        Task 9
HANDOFF.md                       Task 9
docs/superpowers/specs/2026-05-21-slice-12-agent-skills-design.md  Task 0: Status -> Approved
```

---

## 工作分解

### Task 0 — 设计 spec 定稿

**Files:** `docs/superpowers/specs/2026-05-21-slice-12-agent-skills-design.md`

- [ ] 评审 §16 开放问题并更新 Status 为 Approved
- [ ] 确认本 plan 与 spec ADR-64..68 一致

---

### Task 1 — Parser + 类型 + 错误哨兵

**Files:**
- Create `internal/skills/types.go`
- Create `internal/skills/errors.go`
- Create `internal/skills/parser.go`
- Create `internal/skills/parser_test.go`

**`types.go` 核心：**

```go
type Document struct {
    ID          string // frontmatter name
    Description string
    Body        string // markdown after frontmatter
    SourcePath  string // absolute or rel, audit only
}

type Skill struct {
    Document
    Version   string // sha256(body)[:12]
    CharCount int
}
```

**`errors.go` 哨兵：**

```go
var (
    ErrInvalidSkillID   = errors.New("skills: invalid skill id")
    ErrInvalidFrontmatter = errors.New("skills: invalid frontmatter")
    ErrPathEscape       = errors.New("skills: path outside skills root")
)
```

**`parser.go` 行为：**
- 读文件；必须以 `---\n` 开头；解析到下一个 `\n---\n` 为 YAML 块（仅允许 `name:`、`description:` 键，用简单行解析或 `yaml.v3`）
- `name` 校验：`^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$` 或单字符 `[a-z0-9]`
- body = 剩余内容 trim
- `ParseFile(path string) (*Document, error)`

**测试用例：**
- 合法最小 SKILL.md
- 缺 `name` → error
- 非法 id `Bad_Name` → error
- 无 frontmatter → error

- [ ] **Step 1:** 写 `parser_test.go`（红灯）
- [ ] **Step 2:** 实现 `parser.go`（绿灯）
- [ ] **Step 3:** `go test ./internal/skills/... -count=1`

---

### Task 2 — Registry + Resolver

**Files:**
- Create `internal/skills/registry.go` + `registry_test.go`
- Create `internal/skills/resolver.go` + `resolver_test.go`
- Create `internal/skills/config.go`

**Registry：**

```go
type Registry struct {
    mu    sync.RWMutex
    byID  map[string]*Skill
}

func (r *Registry) LoadFromDirs(dirs []string) (loaded int, errs []error)
// filepath.WalkDir each dir; only SKILL.md; Clean + HasPrefix(root)
// duplicate id: warn + overwrite

func (r *Registry) Get(id string) (*Skill, bool)
func (r *Registry) List() []SkillMeta // sorted by id, no body
```

**Resolver：**

```go
type ResolveInput struct {
    ProfileSkillIDs []string
    SessionSkillIDs []string
    RunSkillIDs     []string
    DefaultSkillIDs []string
}

func (res *Resolver) Resolve(in ResolveInput) []*Skill
// 按优先级取第一个非空 []string 作为 ordered ids
// 去重保序；Get 失败 skip；截断到 MaxSkillsPerRun
```

**config.go：**

```go
type Config struct {
    Enabled          bool
    Dirs             []string
    DefaultSkillIDs  []string
    MaxInjectedChars int
    MaxSkillsPerRun  int
    HotReload        bool
}
func DefaultConfig() Config { ... } // 24000, 5, enabled true
```

- [ ] Registry 单测：临时目录两个 skill、重复 id、路径逃逸拒绝
- [ ] Resolver 单测：session 覆盖 profile、未知 id 跳过、max 5 截断

---

### Task 3 — Injector

**Files:** `internal/skills/injector.go` + `injector_test.go`

```go
type InjectResult struct {
    Messages   []modelgw.ChatMessage // 0 or 1 system blocks
    SkillIDs   []string
    CharCount  int
    Truncated  bool
}

func BuildSystemMessages(profilePrompt string, skills []*Skill, maxChars int) InjectResult
```

渲染模板（单条 system）：

```text
{profilePrompt}

## Active Skills

### Skill: {id}
{description}

{body}
...
```

- 从 profilePrompt 开始累计字符；每个 skill 一节；超 `maxChars` 时截断 body 并 `Truncated=true`
- `profilePrompt` 为空时仍可有 Active Skills 块
- 无 skills 时：若 profilePrompt 非空返回 1 条 system；否则返回空

- [ ] 单测：0 skill、2 skill 合并、超长 truncated、顺序与输入一致

---

### Task 4 — Config + main 启动加载

**Files:**
- Modify `internal/config/config.go` — 加 `Skills SkillsConfig \`mapstructure:"skills"\``
- Modify `config/config.example.yaml` — `skills:` 段（见 design spec §6）
- Modify `cmd/server/main.go`

**main 伪代码：**

```go
var skillRegistry *skills.Registry
var skillResolver *skills.Resolver
if cfg.Skills.Enabled && len(cfg.Skills.Dirs) > 0 {
    skillRegistry = skills.NewRegistry()
    n, errs := skillRegistry.LoadFromDirs(cfg.Skills.Dirs)
    for _, e := range errs { slog.Warn("skills.load", "err", e) }
    slog.Info("skills.loaded", "count", n)
}
skillResolver = skills.NewResolver(skillRegistry, cfg.Skills)
composer := agent.NewSkillComposer(skillResolver, cfg.Skills)
engine := agent.NewEngine(gw, bus, profiles, composer)
```

- [ ] `PCA_SKILLS_DIRS` 逗号分隔验证
- [ ] `skills.enabled=false` 时 composer 为 noop

---

### Task 5 — ContextComposer + Engine 接入

**Files:**
- Create `internal/agent/composer.go` + `composer_test.go`
- Modify `internal/agent/engine.go`
- Modify `internal/agent/types.go` — `RunInput.SkillIDs []string`

**接口：**

```go
type ComposeInput struct {
    TenantID        uuid.UUID
    UserID          uuid.UUID
    Profile         Profile
    RunSkillIDs     []string
    SessionSkillIDs []string
}

type ComposeMeta struct {
    SkillIDs   []string
    CharCount  int
    Truncated  bool
}

type ContextComposer interface {
    ComposeSystem(ctx context.Context, in ComposeInput) ([]modelgw.ChatMessage, ComposeMeta, error)
}

type SkillComposer struct { resolver *skills.Resolver; cfg skills.Config }
// noopComposer for disabled
```

**Engine.Run 改动（替换原 90-97 行逻辑）：**

```go
sysMsgs, meta, err := e.composer.ComposeSystem(ctx, ComposeInput{
    TenantID: in.TenantID, UserID: in.UserID,
    Profile: profile, RunSkillIDs: in.SkillIDs,
    SessionSkillIDs: in.SessionSkillIDs, // 新增 RunInput 字段
})
messages := append(sysMsgs, in.Messages...)
// 可选：runSpan.SetAttributes(skill_ids, chars, truncated)
```

`RunInput` 新增：

```go
SessionSkillIDs []string `json:"-"` // 由 session service 填，不来自 JSON
SkillIDs        []string `json:"skill_ids,omitempty"` // POST /agent/run 显式覆盖
```

- [ ] `engine_test.go`：mock composer 或 fake registry 断言第一条 system 含 `Active Skills`
- [ ] `skills.enabled=false` 时与旧行为 byte-equal（仅 profile prompt）

---

### Task 6 — Profile + Session skill_ids

**Files:**
- Modify `internal/agent/profile.go` — `SkillIDs []string`；`DefaultCodingProfile` 加 `SkillIDs: []string{"platform-coding-standards"}` 与 memory.* allowlist
- Create migration `0011_session_skill_ids.up.sql` / `.down.sql`
- Modify `internal/session/types.go` — `Session.SkillIDs []string`, `CreateRequest.SkillIDs []string`
- Modify `internal/session/repo.go` — INSERT/SELECT/SCAN `skill_ids`
- Modify `internal/session/service.go` — `SendMessage` / `Run` 填 `RunInput.SessionSkillIDs`
- Modify `internal/session/repo_test.go` + `service_test.go`

**Migration:**

```sql
-- 0011_session_skill_ids.up.sql
ALTER TABLE sessions ADD COLUMN skill_ids TEXT[] NOT NULL DEFAULT '{}';
```

**Create session API：**

```json
POST /sessions
{ "model": "...", "profile": "coding", "title": "...", "skill_ids": ["e2e-marker"] }
```

空数组或未传 → `{}` → Resolver 回落 Profile。

- [ ] dockertest：创建 session 带 skill_ids，Get 读回一致
- [ ] `go test ./internal/session/... ./internal/agent/... -count=1`

---

### Task 7 — GET /skills 只读 API

**Files:**
- Create `internal/skills/handler.go` + `handler_test.go`
- Modify `cmd/server/main.go` — `skills.NewHandler(registry).Register(protected)`

| 路由 | 行为 |
|---|---|
| `GET /skills` | `{"skills":[{id,description,version,char_count},...]}` |
| `GET /skills/:id` | 单条 meta；`?include=body` 时加 `body`（供调试） |
| 未知 id | 404 |

- [ ] handler_test：httptest 列表、include=body、404

---

### Task 8 — Metrics + Audit + Dockerfile

**Files:**
- Modify `internal/metrics/metrics.go`

```go
skillLoadTotal = meter.Int64Counter("pca_skill_load_total", ...)
skillInjectionsTotal = meter.Int64Counter("pca_skill_injections_total",
    metric.WithDescription("..."), /* attr truncated */)
skillInjectedChars = meter.Int64Histogram("pca_skill_injected_chars", ...)
```

- Registry.LoadFromDirs：parse error → `pca_skill_load_total{outcome=parse_error}`
- Injector：inject 后 record chars + truncated

**Audit：** 在 `session.Service.SendMessage` 成功 compose 后（或 Engine 内）：

```go
audit.Record(ctx, "skill.inject", sessionID, metadata: skill_ids, chars, truncated)
```

若项目用显式 `Recorder` 接口，对齐 slice 9 模式（查 `session` 是否已有 audit 调用点；若无则在 Engine 末尾记一次 per Run）。

- [ ] Modify `Dockerfile`：`COPY skills /app/skills`
- [ ] `deploy/compose/docker-compose.yml` 可选 volume 覆盖 `./skills:/app/skills:ro`

---

### Task 9 — Mock E2E 增强 + test-e2e + 文档

**Files:**
- Modify `internal/modelgw/mockserver/main.go`
- Modify `deploy/compose/test-e2e.sh`
- Modify `README.md`
- Modify `HANDOFF.md`
- Create `skills/platform/platform-coding-standards/SKILL.md`
- Create `skills/e2e/e2e-marker/SKILL.md`

**mockserver 逻辑（仅 default-mock）：** 扫描 messages 中 role=system 的 content，若包含 `E2E_SKILL_MARKER_V1`，非流式回复 `skill-marker-ok`（替代 `hello from mock`）；流式同理。其他请求保持原行为。

**E2E 新步骤（先 `sed` 全文 `39` → `42`）：**

```bash
# [40/42] GET /skills lists e2e-marker
curl -fsS http://localhost:8080/skills -H "Authorization: Bearer $TOK" \
  | jq -e '.skills[] | select(.id=="e2e-marker")'

# [41/42] POST /agent/run with session carrying skill_ids -> mock echoes marker
SESS2=$(curl -fsS -X POST http://localhost:8080/sessions \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"model":"default-mock:gpt-4o","profile":"coding","title":"skill-e2e","skill_ids":["e2e-marker"]}')
SID2=$(echo "$SESS2" | jq -r .id)
RUN3=$(curl -fsS -X POST http://localhost:8080/agent/run \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d "{\"model\":\"default-mock:gpt-4o\",\"profile\":\"coding\",\"session_id\":\"$SID2\",\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}]}")
# 若 agent/run 不支持 session_id，改为 CreateRequest 已带 skill_ids 的 session + 用 WS 或扩展 run body:
# 更简单路径：agent/run body 直接 "skill_ids":["e2e-marker"] 若 Task 5 支持
TEXT3=$(echo "$RUN3" | jq -r '.events[-1].text')
[[ "$TEXT3" == "skill-marker-ok" ]] || exit 1

# [42/42] GET /audit?action=skill.inject has row
HITS=$(curl -fsS -H "Authorization: Bearer $TOK" \
  "http://localhost:8080/audit?action=skill.inject&limit=5" | jq '.entries | length')
[[ "$HITS" -ge 1 ]] || exit 1
```

**实施注意：** 若 `POST /agent/run` 当前无 `session_id`，[41] 使用 `RunInput.skill_ids` 直传：

```json
{"model":"default-mock:gpt-4o","profile":"coding","skill_ids":["e2e-marker"],"messages":[...]}
```

并在 handler 绑定 `SkillIDs` 到 `RunInput`。

- [ ] `platform-coding-standards` body < 2KB
- [ ] `e2e-marker` body 含唯一串 `E2E_SKILL_MARKER_V1`
- [ ] README：Skills 小节 + 配置表 + 端点 `GET /skills`
- [ ] HANDOFF：Slice 12 完成态、42 步 e2e

- [ ] **验收：** `cd deploy/compose && ./test-e2e.sh` → `E2E PASS`

---

### Task 10 — 全量回归

- [ ] `go test ./... -count=1`
- [ ] `go vet ./...`
- [ ] `go build -o bin/server ./cmd/server`（或 `make build` 若需 webui）
- [ ] 可选：`go test -tags=docker_integration ./internal/session/... -count=1`

---

## 推荐 commit 切分

| # | Message | Tasks |
|---|---|---|
| 1 | `feat(skills): parser, registry, resolver, config` | 1-2, 4(partial) |
| 2 | `feat(skills): injector and agent context composer` | 3, 5 |
| 3 | `feat(session): skill_ids column and run wiring` | 6 |
| 4 | `feat(skills): GET /skills, metrics, audit, docker skills dir` | 7-8 |
| 5 | `feat(e2e,docs): mock skill marker, e2e 42 steps, README` | 9-10 |

---

## 12b / 12c  backlog（本 plan 不实施）

| ID | 内容 |
|---|---|
| 12b-1 | migration `skills` + `profile_skills` 表 |
| 12b-2 | Admin `POST/PUT/DELETE /skills` |
| 12b-3 | `skill.list` / `skill.describe` tools |
| 12b-4 | Web UI Skills 管理页 |
| 12c-1 | Git / skills.sh sync worker |
| 12c-2 | 按 description embedding 自动选 Skill |
| 12c-3 | Skill 签名与 version pin |

---

## 风险与缓解

| 风险 | 缓解 |
|---|---|
| Skill 太大撑爆 context | `max_injected_chars` + truncated 指标 |
| 恶意 SKILL.md 提示注入 | 仅服务端目录；12b 加审批 |
| Engine 改动回归 | composer 单测 + 全量 agent/session 测试 |
| Docker 镜像体积 | platform skill 保持短小；大 skill 租户挂载卷 |
| `POST /agent/run` 无 session 关联 | 12a 用 body `skill_ids`；session WS 用 `SessionSkillIDs` |

---

## 开放问题（实施前拍板）

1. **`POST /agent/run` 是否接受 `skill_ids`？** — 计划：**是**（E2E 最简单）；`session_id` 可选后续。
2. **frontmatter 解析库：** — 计划：**最小手写解析** `name`/`description` 两行；若需完整 YAML 再引 `yaml.v3`。
3. **Audit 记录点：** — 计划：`Engine.Run` 在 compose 后记 `skill.inject`（session_id 从 RunInput 可选字段传入，WS 路径有）。

---

## 验收清单（12a Done Definition）

- [ ] `skills/` 目录随镜像发布；compose 可挂载覆盖
- [ ] `coding` profile 默认注入 `platform-coding-standards`
- [ ] `GET /skills` 返回已加载 meta
- [ ] Session `skill_ids` 覆盖 profile 默认
- [ ] `pca_skill_injections_total` 在 `/metrics` 可见
- [ ] `test-e2e.sh` **42/42 PASS**
- [ ] README + HANDOFF 更新
- [ ] 设计 spec Status = Approved
