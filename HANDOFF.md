# 项目交接文档 (HANDOFF.md)

| 字段 | 值 |
|---|---|
| 项目名 | Private Coding Agent — 私有化部署的 AI 编码 Agent 平台 |
| 项目根 | `F:\project\private-coding-agent` |
| Git module | `github.com/yourorg/private-coding-agent` |
| 当前日期 | 2026-05-23 |
| 当前 HEAD | `689466a` *(Compose Pilot #11–#15：backup/restore 脚本、workflow retention、Reflection 持久队列、admin re-embed、Docker snapshot restore + workspace sidecar tar；E2E **69/69**)* |
| P1 规划 | **已落盘** — [`docs/P1-ROADMAP.md`](docs/P1-ROADMAP.md) |
| 工作区状态 | MVP-P1 17 ✅；Full-P1 18–22d2 ✅；Compose Pilot #11–#15 ✅；compose E2E **69/69** + kind nightly 6 步独立 |
| 下一阶段 | **Slice 19b** NL 建流（Task 1–2 ✅）→ Task 3 HTTP/工具 → 生产化演练 [`docs/PILOT-RUNBOOK.md`](docs/PILOT-RUNBOOK.md) |

---

## 1. 当前已完成的工作

### 1.1 设计与规划阶段

- 主设计 spec：`docs/superpowers/specs/2026-05-18-private-ai-coding-agent-design.md`
- 每切片独立 spec + plan，全部落盘到 `docs/superpowers/{specs,plans}/`
- P0 切片 1～12 路线已覆盖；P1 见 [`docs/P1-ROADMAP.md`](docs/P1-ROADMAP.md)

### 1.2 已交付切片（P0：切片 1～12）

| 切片 | 状态 | 内容摘要 |
|---|---|---|
| 1 — Foundation | ✅ | Go 骨架 + PG 迁移 + JWT + Gin + OTel + audit_log + compose |
| 1.5 — Hardening | ✅ | ctx-aware Migrate / detached audit / JWT secret 校验 |
| 2 — Sandbox | ✅ | `SandboxRuntime` + `DockerDriver` + Redis 锁 + Reconciler |
| 3 — Model Gateway | ✅ | 多 provider + SSE + ProviderRegistry |
| 4 — Tool Bus | ✅ | 12 内置工具 + `tool_invocations` |
| 5 — Agent Engine | ✅ | ReAct + tool_calls + 上下文压缩 |
| 6 — Session API + WS | ✅ | `/sessions` REST + WebSocket + messages |
| 7 — Memory (basic) | ✅ | `/memories` REST + memory.* 工具 |
| 8 — Web Frontend | ✅ | React SPA embed |
| 9 — Audit 加固 | ✅ | admin `/audit` + 领域事件 |
| 10 — Observability | ✅ | OTel + Prometheus + 结构化日志 |
| 11 — Vector Memory | ✅ | pgvector + 0.92 dedup |
| 12 — Agent Skills (12a) | ✅ | SKILL.md 注入 + `GET /skills` + E2E 40–42 |

### 1.3 测试与验收（P0）

| 维度 | 状态 |
|---|---|
| `go test ./...` | 预期全 PASS |
| `go vet ./...` | 干净 |
| `go build ./...` | 干净 |
| E2E `test-e2e.sh` | 全量 **69 步**（P0 1–42 + MVP-P1 43–49 + Full-P1 50–67 + Compose Pilot 68–69） |

### 1.4 Gate G1 收口（2026-05-21）

P0 至 Slice 12 全部 push 之后，工作区仍堆了一批跨切片的杂活；本次 Gate 收口拆成 6 个 Conventional Commits 顺序 push：

| Commit | 内容 |
|---|---|
| `6fadd41` | `feat(agent,modelgw,session)`: 流式 Agent 循环 + WS 断连不杀 in-flight run（`context.WithoutCancel`） |
| `70e8cdf` | `feat(db,compose)`: DashScope provider 注册（0012/0013 迁移）+ Qwen env 透传 |
| `7b1dec6` | `feat(webui)`: `assistant_delta` 实时气泡 + 默认模型切到 `dashscope:qwen3.6-plus` |
| `a014d26` | `docs(p1)`: 路线图 + MVP/Full spec + slice 13–23 plan + HANDOFF 刷新 |
| `5690ca9` | `chore`: ignore `.claude/` 与 `deploy/compose/*.json` 临时载荷 |
| `bd21e6d` | `test(e2e)`: 启动期 TRUNCATE 让 vector dedup（步骤 39）跨 run 幂等 |

**Gate 状态**：G1–G4 全部 ✅，`main` 与 `origin/main` 同步，`test-e2e.sh` 42/42 PASS。

---

## 2. 系统能力快照（用户视角）

### 2.1 端点表

| 方法 | 路径 | 鉴权 | 说明 |
|---|---|---|---|
| GET | /healthz, /readyz | - | 健康检查 |
| POST | /auth/login | - | 登录拿 JWT |
| GET | /me | Bearer | 当前身份 |
| POST/GET/DELETE | /sandbox/sessions[/{id}] | Bearer | 沙箱生命周期 |
| POST | /sandbox/sessions/{id}/exec | Bearer | 沙箱内命令 |
| GET/PUT | /sandbox/sessions/{id}/files | Bearer | 沙箱内读写 |
| POST | /v1/chat/completions, /v1/embeddings | Bearer | OpenAI 兼容（支持 SSE） |
| GET | /tools | Bearer | 列出 12 个内置工具 |
| POST | /tools/invoke | Bearer | 调用工具 |
| POST | /agent/run | Bearer | ReAct 循环（非流） |
| POST/GET/DELETE | /sessions[/{id}] | Bearer | 会话生命周期 |
| GET | /sessions/{id}/messages | Bearer | 历史 |
| GET | /sessions/{id}/ws | Bearer (URL token shim) | WebSocket 流式 |
| POST/GET/PUT/DELETE | /memories[/{id}] | Bearer | 记忆 CRUD |
| GET | /audit | Bearer (admin) | 审计日志查询 |
| GET | /metrics | Bearer (admin 或 scrape token) | Prometheus exposition |
| GET | /, /login, /sessions/{id}, /audit | - | SPA shell（NoRoute fallback） |

### 2.2 内部 MCP 工具（17 个）

- `fs.read / fs.write / fs.list / fs.glob / grep / shell.exec`（沙箱内）
- `llm.chat / llm.embed`（Model Gateway 透传）
- `memory.save / memory.search / memory.list / memory.delete`（User scope；Slice 11 后 `memory.search` 支持 `mode=vector|keyword`，`memory.save` 响应携带 `created` bool）
- `agent.delegate`（仅 coding profile；委派子 Run；详见 Slice 18）
- `workflow.create / workflow.update / workflow.list / workflow.get`（admin-only；Agent 在会话里起草 / 改 DSL；publish/delete/invoke 仍只在 admin REST。`coding` + `workflow-authoring` profile 持有）

### 2.3 持久化表（10 个迁移）

```
0001 tenants
0002 users
0003 audit_log
0004 sandbox_sessions
0005 providers
0006 model_usage
0007 tool_invocations
0008 sessions + messages
0009 memories
0010 memories.embedding (vector(1536)) + ivfflat partial cosine index   ← Slice 11
```

### 2.4 配置（`config/config.example.yaml`）

```yaml
server:        port / mode / ws_allowed_origins
db:            dsn
redis:         addr
auth:          jwt_secret / jwt_ttl
telemetry:     service_name / otlp_endpoint
observability: log_format / log_level / metrics_token
memory:        embedding_model / dedup_threshold / embed_on_write   ← Slice 11
```

env 覆盖：`PCA_<SECTION>_<FIELD>`，例如 `PCA_MEMORY_DEDUP_THRESHOLD=0.92`

---

## 3. P1 执行计划（已批准）

**总路线图**：[`docs/P1-ROADMAP.md`](docs/P1-ROADMAP.md)  
**MVP Spec**：[`docs/superpowers/specs/2026-05-21-p1-mvp-enterprise-design.md`](docs/superpowers/specs/2026-05-21-p1-mvp-enterprise-design.md)  
**Full Spec**：[`docs/superpowers/specs/2026-05-21-p1-full-enterprise-design.md`](docs/superpowers/specs/2026-05-21-p1-full-enterprise-design.md)  
**验收**：[`docs/SLICE-VERIFICATION.md`](docs/SLICE-VERIFICATION.md)

### 3.0 Gate（开工 Slice 13 前）

| ID | 项 | 状态 |
|----|-----|------|
| G1 | 工作区未提交改动 commit | ✅ `bd21e6d` 已 push |
| G2 | 本 HANDOFF 与 HEAD 一致 | ✅ |
| G3 | E2E **42/42** + `go test ./...` | ✅（2026-05-21 验证） |
| G4 | P0 缺口归属确认（文件浏览→16，自动沙箱→14） | ✅ 已写入 spec |

### 3.1 MVP-P1（切片 13～17）— 企业试点

| 切片 | 状态 | E2E | Plan |
|------|------|-----|------|
| 13 Enterprise Foundation | ✅ | 43–44 | `plans/2026-05-21-slice-13-enterprise-foundation.md` |
| 14 Session↔Sandbox | ✅ | 45 | `plans/2026-05-21-slice-14-session-sandbox-binding.md` |
| 15 SSO (OIDC) | ✅ | 46 | `plans/2026-05-21-slice-15-sso-oidc.md` |
| 16 Enterprise Web | ✅ | 47–48 | `plans/2026-05-21-slice-16-enterprise-web.md` |
| 17 Skills 12b | ✅ | 49 | `plans/2026-05-21-slice-17-skills-12b.md` |

**MVP 完成后**：E2E **55** 步；可对外「企业试点」。

### 3.2 Full P1（切片 18～23）— 主 spec §11 字面

| 切片 | 状态 | E2E | 说明 |
|------|------|-----|------|
| 18 Sub-Agents + delegate | ✅ | 50 | review/research/workflow-authoring profile + `agent.delegate` + ctx-based RunCtx |
| 19a Workflow Engine | ✅ | 57–60 | YAML DSL + DAG executor (tool/assign/if/foreach/parallel/wait) + `workflow.<slug>` 注册到 ToolBus + Dry-Run mock mutating tools |
| 19b Workflows & Tools Web UI | ✅ | — | `/workflows` admin（Monaco YAML 编辑器 + CRUD + publish/invoke/dry_run + runs 抽屉）+ `/toolbox`（只读工具列表 + Mutating 徽标）；`GET /tools` 暴露 `mutating bool` |
| 20 Reflection | ✅ | 61 | `session.archive` → in-process worker → Reflector LLM 抽取 → `memory_proposals`（pending/auto_approved）→ admin `/admin/memory-proposals` 审核 → `memory.Service.Create`（0.92 dedup 复用）；mock-provider `REFLECTION_TASK_V1` canned JSON；5 个 audit action + `pca_reflection_proposals_total{outcome=…}`；WebUI `/admin/memory-proposals` |
| 21a Orchestration Router | ✅ | 62 | `internal/orchestrator` 规则引擎 + `agent.Engine.WithRouter` Shadow + Hint 注入；YAML 规则 `match{profile,content_regex,content_contains}` + `suggest{type,target,hint}`；audit `orchestrator.route` + `pca_orchestrator_routes_total{outcome,target_type}`；mock-provider `ORCHESTRATOR_E2E_HINT_DELIVERED` canned → `"orchestrator-hint-ok"` |
| 21b External MCP Manager | ✅ | 63 | 0020 `mcp_servers` 表 + `internal/mcp` HTTP JSON-RPC client (2024-11-05) + Manager 心跳 + `mcp.<slug>.<tool>` Bus 注册 + `/admin/mcp-servers` CRUD + `mock-mcp:8083` 容器 + WebUI `/admin/mcp-servers`；6 audit + 3 metric |
| 22a Audit Hash Chain | ✅ | 64 | 0021 `audit_log` 加 `prev_hash/entry_hash BYTEA NOT NULL` + `internal/audit/hash.go` SHA-256 canonical（RS=0x1E 分隔，map[string]any 自然有序）+ `Repo.Append` 走 `BeginTx → pg_advisory_xact_lock(hashtext('audit_log')) → SELECT prev → INSERT`（跨 goroutine/副本不分叉）+ `Repo.Verify(ctx, fromID)` 流式 prev/entry 双校验 + `GET /audit/verify` admin-only；E2E 64 篡改后定位 first_broken_id |
| 22b Snapshot → MinIO | ✅ | 65 | 0022 `sandbox_snapshots(tenant_id,user_id,session_id NULL on FK delete,object_key,size_bytes,image_ref,metadata)` + `SnapshotRepo` + dockertest 6 例 / `internal/objstore` wraps minio-go/v7（`PartSize=64<<20`, `objectSize=-1` 流式 multipart）/ `DockerDriver.Snapshot` 真实：`ContainerCommit(Pause=true)` → `ImageSave` → `objstore.Put` → `SnapshotRepo.Insert` → 可选 `ImageRemove`（KeepLocalImage=false 默认）/ key 布局 `{prefix?}/{tenant}/{session}/{rfc3339nano}.tar` / `SnapshotConfig{Enabled,Endpoint,Bucket,AccessKey,SecretKey,Region,UseSSL,Prefix,KeepLocalImage}` + `PCA_SNAPSHOT_*` env / handler `POST /sandbox/sessions/:id/snapshot`（替换 501）+ `GET /sandbox/snapshots(?session_id=&limit=)` + `GET /sandbox/snapshots/:id`，3 个 `sandbox.snapshot.{create,list,get}` audit Detached / disabled posture：未 `SetSnapshotDeps` → 503 `snapshot_disabled` / compose `minio:RELEASE.2025-04-08T15-41-24Z` + 命名卷 `miniodata` + healthcheck + server `depends_on minio:service_healthy` / E2E 65 create→write file→snapshot→list→destroy→still visible w/ session_id=null→audit |
| 22c seccomp + trivy CI | ✅ | 66 | `internal/sandbox/seccomp.json` 派生自 Docker default profile（v25.0.5），从 allow 名单移除 `mount/umount/umount2/pivot_root/name_to_handle_at/open_by_handle_at/mount_setattr/move_mount/open_tree/fsconfig/fsmount/fsopen/fspick/ptrace/process_vm_readv/process_vm_writev/process_madvise/pidfd_getfd/kcmp/keyctl/add_key/request_key/bpf/init_module/delete_module/finit_module/create_module/kexec_load/kexec_file_load/userfaultfd/perf_event_open/fanotify_init/lookup_dcookie/quotactl/quotactl_fd/setdomainname/sethostname/syslog/iopl/acct` 共 16 类约 40 个危险 syscall；保留 `setns/unshare/clone3` 满足现代 glibc / Node.js；`//go:embed seccomp.json` + `LoadSeccompProfile` 启动期解析校验 `defaultAction==SCMP_ACT_ERRNO`；`DockerDriverConfig.SeccompProfile` 注入 `SecurityOpt: seccomp=<json>`（helper `securityOpts(profile)` 保留 no-new-privileges:true）；`SandboxConfig.SeccompEnabled` 默认 true、`PCA_SANDBOX_SECCOMP_ENABLED=false` 应急回退到 Docker 默认 profile；GitHub Actions `.github/workflows/security.yml` 在 PR + push to main 触发 sandbox/image/** 路径变更，trivy `aquasecurity/trivy-action@master` 双 job — CRITICAL exit-code=1 阻塞 merge / HIGH exit-code=0 仅 table；`.trivyignore` placeholder；SECURITY-SANDBOX.md §1 适用范围 + §2 SecurityOpt 表 + §9 已知未做表（seccomp/trivy/audit hash chain 标 ✅）+ §11 验证（SecurityOpt inspect + mount 拒绝实证）；E2E 66 mount → EPERM + sh+echo+cat 回归 |
| 22d1 K8sDriver Runtime | ✅ | 67 | `internal/sandbox/k8s_driver{,_exec,_fs}.go` 实现 Runtime（Pod=sandbox，硬化全对齐 DockerDriver；SPDY exec；tar-pipe FS；Snapshot=`ErrSnapshotDisabled`）+ fake-clientset 13 L1 单测 / `SandboxConfig.Driver` 默认 docker + K8sConfig + `Load()` 非法 driver fail-fast / main.go boot switch（docker→DockerDriver+reconciler、k8s→buildK8sRestConfig→clientset→K8sDriver、skip reconciler）+ `SetSnapshotDeps` 类型断言保护 / `httpx.Deps.Info` + /healthz 暴露 `sandbox.driver` / `k8s.io/{api,apimachinery,client-go} v0.32.0` |
| 22d2 Helm chart + kind nightly | ✅ | — | `deploy/helm/pca` chart（Chart.yaml + values.yaml prod 默认 + values-kind.yaml + README + 13 模板：_helpers/namespace/serviceaccount/rbac scope=pca-sandboxes + Role pods{create,get,list,delete}+pods/exec{create}+pods/log{get} + configmap 故意省略 db.dsn/redis.addr + secret + service + deployment（PCA_DB_DSN 走 Pod env `$(PCA_DB_PASSWORD)`，readOnlyRootFilesystem+runAsNonRoot=65532）+ postgres StatefulSet+PVC + redis Deployment+PVC + 3 张 NetworkPolicy 模板（server 出站 allowlist；sandbox-internal podSelector pca.network=internal 仅出 release ns；sandbox-none deny-all））+ `.github/workflows/kind-nightly.yml` cron 17 3 UTC + workflow_dispatch（kind 单节点 → helm install → port-forward → 6 步 kind-e2e.sh：psql bootstrap user / login+create→Pod 在 pca-sandboxes ns / PUT files+POST exec via SPDY round-trip / NP=internal curl 外网拒 / DELETE+404）+ `docs/DEPLOY-K8S.md` 10 段 + `docs/DEPLOY.md` §1 加 K8s 行 + `docs/SECURITY-SANDBOX.md` §3.1 注 docker.sock 妥协对照消除 |
| 23 N8N（可选） | ⏭️ **跳过** | — | 非硬需求；Full P1 核心已完成 |

### 3.3 技术债 ↔ 切片映射

| 项 | 归入 |
|----|------|
| `providers.tenant_id`、quota、rate limit、JWT logout、HTTP 超时 | **13** |
| session 自动建沙箱 | **14** |
| Memory 自动注入、Memory UI、沙箱文件浏览 | **16** |
| Tenant Skills DB | **17** |
| audit hash chain | **22a** ✅ |
| Snapshot → MinIO | **22b** ✅ |
| seccomp、trivy | **22c** ✅ |
| K8sDriver | **22d1** ✅ |
| Helm chart + kind nightly + DEPLOY-K8S.md | **22d2** ✅ |
| Reflection、Workflow、delegate、N8N | **18–23** |
| Compose 试点 #11 备份 / #14 runs 保留 / #15 Reflection 队列 | **Compose Pilot** ✅ → [`docs/P2-COMPOSE-PILOT.md`](docs/P2-COMPOSE-PILOT.md) |
| Compose 试点 #12 re-embed admin | **Compose Pilot** ✅ |
| Compose 试点 #13 snapshot restore | **Compose Pilot** ✅（Docker only） |
| Hybrid 检索、Project/Tenant memory | **P2** /  backlog |
| 历史 re-embed admin | 见 Compose Pilot #12 |

---

## 4. 当前遇到的问题

### 4.1 阻塞性问题

- **Slice 22d2** 已交付（`deploy/helm/pca/` Helm chart：Chart.yaml + values.yaml（prod 默认；secrets.jwtSecret 非空必填 ≥32 字符，否则 _helpers.tpl `pca.assertions` 渲染期 fail）+ values-kind.yaml（image.tag=kind-latest, pullPolicy=Never, jwtSecret 测试值, storageClassName=standard, resources 缩小, log_level=debug, quota.sandboxMaxActive=2）+ 13 模板：_helpers.tpl 含 5 条 assert（jwtSecret 长度、`config.sandbox.k8s.namespace==rbac.sandboxNamespace`、sandbox.network∈internal|bridge|none、driver∈docker|k8s）；namespace.yaml gated by `rbac.createSandboxNamespace`；serviceaccount.yaml；rbac.yaml Role scope 限定 `pca-sandboxes` ns，verbs 仅 pods{create,get,list,delete}+pods/exec{create}+pods/log{get}（server SA 在自己 ns 无任何 K8s API 权限）；configmap.yaml 1:1 mirror config.example.yaml 但**故意省略 db.dsn / redis.addr**（viper 不解析 `$(VAR)` YAML 插值，DSN/Addr 走 Pod-spec env `$(PCA_DB_PASSWORD)` 在 K8s admission 期展开 + viper.AutomaticEnv 让 PCA_DB_DSN/PCA_REDIS_ADDR env 覆盖 config）；secret.yaml gated by `not .Values.secrets.existing`（推荐 sealed-secrets / external-secrets 外管）；service.yaml ClusterIP:8080；deployment.yaml `checksum/config` 注解触发 ConfigMap rollout + securityContext runAsNonRoot=65532 / readOnlyRootFilesystem / capDrop ALL / seccompProfile RuntimeDefault + extraEnv 列表+ nodeSelector/tolerations/affinity 透传；postgres.yaml StatefulSet+volumeClaimTemplates（gated, pgvector:pg16）；redis.yaml Deployment+optional PVC（gated, redis:7-alpine）；networkpolicy-server.yaml 出站 allowlist：DNS(53) + kube-apiserver(443/6443) + chart-managed PG(5432)/Redis(6379) + 公网由 `allowExternalEgress` 控制；networkpolicy-sandbox-internal.yaml podSelector `pca.network=internal`，egress 仅允 release ns 的 server pod + DNS；networkpolicy-sandbox-none.yaml deny-all（air-gap）。`.github/workflows/kind-nightly.yml` cron `17 3 * * *`（避 :00/:30 高峰）+ workflow_dispatch：docker/build-push-action@v6 双 image gha cache → helm/kind-action@v1 kindest/node:v1.30.0 → `kind load docker-image` → azure/setup-helm@v4 v3.14.4 → `helm install --wait --timeout 5m` → `kubectl wait deploy/pca-server` → 跑 kind-e2e.sh → failure dump pods/logs/events/netpols。`deploy/helm/pca/test/kind-config.yaml` 单 control-plane 节点。`deploy/helm/pca/test/kind-e2e.sh`（+x，bash strict mode）6 步：(1) `kubectl exec` 进 PG StatefulSet 跑 psql bootstrap demo@example.com 用户（bcrypt hash 同 compose e2e）；(2) `kubectl port-forward svc/pca-server :18080` + 等 /healthz 就绪；(3) /auth/login → /sandbox/sessions 必返 `status=running` 且 `kubectl -n pca-sandboxes get pods` 非空（实证 K8sDriver buildPod 落到正确 ns）；(4) PUT /files + POST /exec via SPDY 双向 round-trip `hello kind`；(5) NetworkPolicy=internal 实证：`curl https://1.1.1.1` 必失败；(6) DELETE session → 再 exec 必 404。`docs/DEPLOY-K8S.md` 新增 10 段（部署形态选择 / 前置 / 镜像 / 快速开始 / values 速查 / 生产 checklist / 升级 / 回滚 / Troubleshooting / kind 本地实验）。`docs/DEPLOY.md` §1 形态表加 "K8s / Helm（22d2 ✅）" 行 + 顶部范围声明指向 DEPLOY-K8S。`docs/SECURITY-SANDBOX.md` §3.1 新增 K8sDriver + chart RBAC 已替换 docker.sock 妥协路径的对照消除注。compose `./test-e2e.sh` 1–67 不变（回归保护）。Full P1 22 全段完成；剩 23（可选）。
- **Slice 22d1** 已交付（`internal/sandbox/k8s_driver{,_exec,_fs}.go` 实现 Runtime 第二个 driver — Pod = sandbox，buildPod 全对齐 DockerDriver 硬化矩阵（runAsUser/Group=10001 runAsNonRoot、readOnlyRootFilesystem、CapDrop ALL + 5 add、allowPrivilegeEscalation=false、SeccompProfile Localhost\|RuntimeDefault、emptyDir{medium:Memory,1Gi} /workspace+/tmp、Guaranteed QoS requests==limits、restartPolicy=Never、automountServiceAccountToken=false 默认）；SPDY remote-exec 经 `k8sExecer` test seam（`newSPDYExecer` 真实实现 / fake-clientset 单元只做 signature 检查）；tar-pipe ReadFile/WriteFile 复用 fs_common；Snapshot 直接 `ErrSnapshotDisabled`（tenant scope check 优先以保 no-enumeration 契约）；waitForPodReady 轮询 phase+waiting reason，timeout 后回收 Pod；Destroy 复用 redis 锁 + lua release + DetachSession。`internal/sandbox/fs_common.go` 抽出 `buildWriteTarStream`/`parseReadTarStream`/`stripWorkspacePrefix` 给两 driver 共用，docker_driver_fs.go 行为零变化。13 个 fake-clientset L1 单测：pod spec 元数据 + securityContext + seccomp 三态 + Guaranteed QoS resources + pod-ready timeout 回收 + tenant 隔离 + destroy 幂等 + Pods.Delete reactor 计数 + snapshot disabled + snapshot tenant scope 优先 + network mode label + DNSPolicy 切换 + exec stream 编译检查。`SandboxConfig.Driver` 默认 `docker` + `SandboxK8sConfig{Namespace,InCluster,Kubeconfig,ServiceAccount,SeccompLocalhostProfile,PodReadyTimeoutSec}` + `applySlice22dDefaults` 非法 driver `Load()` fail-fast + `PCA_SANDBOX_K8S_*` env 绑定。`cmd/server/main.go` boot 期 switch：docker → 老路径 + reconciler；k8s → `buildK8sRestConfig`（InCluster=true `rest.InClusterConfig` / false `clientcmd.NewDefaultClientConfigLoadingRules` + ExplicitPath）→ `kubernetes.NewForConfig` → `NewK8sDriver`；reconciler 在非 docker driver 下跳过。`SetSnapshotDeps` 改类型断言保护（K8sDriver 暴露 `SetSnapshotRepo` fallback 让 Destroy 还能 DetachSession）。`httpx.Deps.Info` 新增 + /healthz body 合并 `{"sandbox":{"driver":"..."}}`。`k8s.io/{api,apimachinery,client-go} v0.32.0` 入 go.mod / go.sum。E2E step 67 校验 `sandbox.driver=docker`。Full P1 剩 22d2 + 23。
- **Slice 22c** 已交付（`internal/sandbox/seccomp.json` 派生自 Docker default profile v25.0.5、从 allow 名单移除 ~16 类危险 syscall、保留 setns/unshare/clone3 + `//go:embed` 内嵌 + `LoadSeccompProfile` 启动期解析校验 + `DockerDriverConfig.SeccompProfile` 注入 `SecurityOpt: seccomp=<json>` + `SandboxConfig.SeccompEnabled` 默认 true / `PCA_SANDBOX_SECCOMP_ENABLED=false` 应急回退 + `.github/workflows/security.yml` trivy CRITICAL fail / HIGH warn + `.trivyignore` placeholder + SECURITY-SANDBOX.md §1/§2/§9/§11 改写 + E2E 66 mount→EPERM + sh+echo+cat 回归）。
- **Slice 22b** 已交付（0022 `sandbox_snapshots` 表 + `SnapshotRepo` + dockertest 6 例 + `internal/objstore` 包 wraps minio-go/v7 + `DockerDriver.Snapshot` commit→save→put→insert 真实实现 + 3 个 audit Detached + compose minio service + E2E 65 destroy 后 snapshot 仍可读 + session_id null）。
- **Slice 22a** 已交付（`internal/audit/hash.go` SHA-256 canonical + 0021 `audit_log` 加 `prev_hash/entry_hash` + `Repo.Append` 走 `pg_advisory_xact_lock` 串行化 + `GET /audit/verify` admin 防篡改校验，E2E 64 篡改 metadata → `entry_hash_mismatch` 定位 + 还原幂等）。
- **Slice 21b** 已交付（`internal/mcp` 2024-11-05 JSON-RPC client + Manager 心跳 + `mcp.<slug>.<tool>` Bus 注册 + `/admin/mcp-servers` CRUD + WebUI + `mock-mcp` 容器 + E2E 63）。
- **Slice 21a** 已交付（`internal/orchestrator` 规则引擎 + `agent.Engine.WithRouter` Shadow + Hint 注入 + audit `orchestrator.route` + counter + mock-provider canned + E2E 62）。
- **Slice 17** 已交付（租户 Skill `skills`/`tenant_profile_skills` 表 + `DBRepo` + `/admin/skills` CRUD + `/admin/profiles/:name/skills` 绑定 + Resolver 合并 FS/DB + `/admin/skills` Web UI + E2E 49）。MVP-P1 完成。
- WebUI 仍无独立沙箱入口；聊天经会话绑定沙箱，工具可继续显式传 `sandbox_id` 覆盖。
- **ivfflat 召回兜底**：`internal/db.Connect` 每条连接 `SET ivfflat.probes = 100`（默认 1 在 E2E 小数据集上漏召 → 步骤 47 `memory.inject` 偶发失败）。生产 lists 调大时需同步上调 probes。

### 4.2 环境注意事项

| 项 | 说明 |
|---|---|
| Go | Windows 全局 PATH 内可直接 `go version`；项目使用 Go 1.25+ 语法（pgvector-go/pgx 依赖） |
| GOPROXY | `https://goproxy.cn,direct` |
| Docker Desktop | 必须在跑（dockertest、E2E、compose 都依赖） |
| Postgres 镜像 | **Slice 11 起 postgres → `pgvector/pgvector:pg16`**（compose + 9 个 dockertest 文件）。数据卷格式兼容（pgvector 镜像基于 postgres:16），无需迁移 |
| Redis (本地) | dockertest 不自动起；跑 sandbox docker_integration 测试前手动 `docker run -d --rm --name pca-redis-test -p 6379:6379 redis:7-alpine` |
| Docker socket | server 容器以 user 0:0 挂 `/var/run/docker.sock`；distroless nonroot 妥协 |

### 4.3 Windows 特定坑

| 问题 | 规避 |
|---|---|
| PowerShell 5.1 `2>&1` 把 stderr 包成 `NativeCommandError` | E2E 用 `test-e2e.sh`（Git Bash） |
| 主机无 `jq` | E2E 内部已通过 docker run jq 绕过 |
| `LF will be replaced by CRLF` | 正常，可忽略 |

### 4.4 性能 / 资源

- 首次跑 dockertest 启 PG ~10-20s（pgvector 镜像比 postgres:16-alpine 大 ~130MB，首次 pull 慢一些）
- 全包测试（不带 docker_integration tag）~25-60s
- 全包测试 + docker_integration ~3-5 分钟
- E2E（67 步含切片 13–22d1；22d2 不增 compose 步号）~3-8 分钟（首次 build 镜像更久；22b 增 minio 服务首次 pull ~50MB；22c seccomp profile 内嵌不增加部署时间；22d1 仅多一次 /healthz GET）
- kind nightly（22d2）独立路径 ~10-20 分钟：image build（gha cache 命中后 ~3 min）+ kind 启动 ~1 min + helm install + wait Pods ~2 min + kind-e2e.sh 6 步 ~1 min；GH Actions timeout 25 min

---

## 5. 下一步建议

### 5.1 生产化演练（当前）

Full P1 核心 + Compose Pilot 已交付。按 [`docs/PILOT-RUNBOOK.md`](docs/PILOT-RUNBOOK.md) 在 compose 或 kind 环境跑：

1. **备份** — `deploy/compose/backup/backup.sh`
2. **恢复** — `restore.sh`（破坏性，仅试点环境）
3. **Re-embed SOP** — 换 embedding 模型 → restart → `POST /admin/memories/re-embed`
4. **回归** — `./test-e2e.sh` 69/69（可选）

```bash
cd deploy/compose && docker compose up -d
cd backup && ./backup.sh
# 见 PILOT-RUNBOOK.md §2–§3
```

### 5.2 已完成

1. ~~**Slice 13** — Foundation~~ ✅
2. ~~**Slice 14** — Session↔Sandbox~~ ✅
3. ~~**Slice 15** — OIDC~~ ✅
4. ~~**Slice 16** — Enterprise Web~~ ✅
5. ~~**Slice 17** — Skills 12b~~ ✅（MVP-P1 收口）
6. ~~**Slice 18** — Sub-Agents + delegate~~ ✅（Full-P1 起步）
7. ~~**Slice 19a** — Workflow Engine~~ ✅（YAML DSL + Bus.Register workflow.<slug> + Dry-Run）
8. ~~**Slice 19b** — Workflows & Tools Web UI~~ ✅（`/workflows` Monaco 编辑 + `/toolbox` 工具列表 + `GET /tools` mutating 标志）
9. ~~**Slice 20** — Reflection~~ ✅（异步 Reflector worker + `memory_proposals` 表 + admin 审核 + auto-approve 阈值 + 5 audit + mock-provider canned JSON + WebUI `/admin/memory-proposals`）
10. ~~**Slice 21a** — Orchestration Router~~ ✅（`internal/orchestrator` 规则引擎 + `agent.Engine.WithRouter` Shadow + Hint 注入 + `orchestrator.route` audit + `pca_orchestrator_routes_total` counter + mock-provider `ORCHESTRATOR_E2E_HINT_DELIVERED` canned）
11. ~~**Slice 21b** — External MCP Manager~~ ✅（`internal/mcp` 2024-11-05 JSON-RPC client + 0020 `mcp_servers` 表 + Manager 心跳 + `mcp.<slug>.<tool>` Bus 注册 + `/admin/mcp-servers` REST + WebUI + `mock-mcp` 容器 + 6 audit + 3 metric + E2E 63）
12. ~~**Slice 22a** — Audit Hash Chain~~ ✅（`internal/audit/hash.go` SHA-256 canonical RS=0x1E 分隔 + 0021 `audit_log.prev_hash/entry_hash` BYTEA + `Repo.Append` 走 `BeginTx + pg_advisory_xact_lock(hashtext('audit_log'))` 跨副本串行化 + `Repo.Verify(ctx, fromID)` 流式 prev/entry 双校验 + `GET /audit/verify` admin-only + E2E 64 篡改 metadata → `entry_hash_mismatch` 定位 + 还原幂等）
13. ~~**Slice 22b** — Snapshot → MinIO~~ ✅（0022 `sandbox_snapshots(session_id NULL on FK delete)` + `SnapshotRepo` + dockertest 6 例 + `internal/objstore` 包封 minio-go/v7 `Put` 流式 multipart `PartSize=64<<20` + `DockerDriver.Snapshot` 真实：`ContainerCommit(Pause=true)` → `ImageSave` → `objstore.Put` → `SnapshotRepo.Insert` → 可选 `ImageRemove` + `SnapshotConfig` + `PCA_SNAPSHOT_*` env + main.go gating `EnsureBucket` + `SetSnapshotDeps` + `Handler.WithSnapshotRepo` + `POST /sandbox/sessions/:id/snapshot` 替换 501 + `GET /sandbox/snapshots(?session_id=&limit=)` + `GET /sandbox/snapshots/:id` + 3 个 `sandbox.snapshot.{create,list,get}` audit Detached + compose `minio:RELEASE.2025-04-08T15-41-24Z` + 命名卷 `miniodata` + healthcheck + server `depends_on minio:service_healthy` + E2E 65 create→write file→snapshot→list→destroy→still visible w/ session_id=null→audit）
14. ~~**Slice 22c** — Seccomp + Trivy CI~~ ✅（`internal/sandbox/seccomp.json` 派生自 Docker default profile v25.0.5，allow 名单移除 `mount/umount/umount2/pivot_root/name_to_handle_at/open_by_handle_at/mount_setattr/move_mount/open_tree/fsconfig/fsmount/fsopen/fspick/ptrace/process_vm_readv/process_vm_writev/process_madvise/pidfd_getfd/kcmp/keyctl/add_key/request_key/bpf/init_module/delete_module/finit_module/create_module/kexec_load/kexec_file_load/userfaultfd/perf_event_open/fanotify_init/lookup_dcookie/quotactl/quotactl_fd/setdomainname/sethostname/syslog/iopl/acct` 共 16 类约 40 个高危 syscall；保留 `setns/unshare/clone3` + `//go:embed seccomp.json` + `LoadSeccompProfile` 启动期解析 `defaultAction==SCMP_ACT_ERRNO` 并返回 JSON 字符串 + `DockerDriverConfig.SeccompProfile` + helper `securityOpts(profile)` 注入 `SecurityOpt: ["no-new-privileges:true","seccomp=<json>"]` + `SandboxConfig.SeccompEnabled` 默认 true（viper.SetDefault 在 ReadInConfig 前注册以让 `PCA_SANDBOX_SECCOMP_ENABLED=false` 应急回退生效）+ `.github/workflows/security.yml` `aquasecurity/trivy-action@master` 双 job CRITICAL exit-code=1 阻塞 / HIGH exit-code=0 仅 table，触发 sandbox/image/** + workflow + .trivyignore 路径 PR + push to main + `.trivyignore` placeholder + SECURITY-SANDBOX.md §1 适用范围 + §2 SecurityOpt 表新行 + §9 已知未做表（seccomp/trivy/audit hash chain 标 ✅、snapshot/K8s/cosign/nightly 推 22d 或 22c-v2）+ §11 docker inspect SecurityOpt + docker exec mount → "Operation not permitted" + E2E 66 mount denial + `sh -c "echo ok > /workspace/seccomp-probe && cat"` 回归保护）
15. ~~**Slice 22d1** — K8sDriver Runtime + fake-client L1~~ ✅（`internal/sandbox/k8s_driver{,_exec,_fs}.go` Pod=sandbox + SPDY exec + tar-pipe FS + 13 fake-clientset L1；`SandboxConfig.Driver` + `SandboxK8sConfig`；`cmd/server/main.go` boot 期 driver switch；`httpx.Deps.Info` + /healthz `sandbox.driver`；`k8s.io/{api,apimachinery,client-go} v0.32.0`；E2E 步骤 67 校验 `sandbox.driver=docker`）
16. ~~**Slice 22d2** — Helm chart + kind nightly + DEPLOY-K8S.md~~ ✅（`deploy/helm/pca/` 13 模板 chart + RBAC scope=`pca-sandboxes`/verbs=pods+pods/exec+pods/log + PCA_DB_DSN 走 Pod env `$(VAR)` 展开 + 3 张 NetworkPolicy 模板 + `_helpers.tpl` 5 条渲染期 assert + `.github/workflows/kind-nightly.yml` 03:17 UTC + workflow_dispatch + `deploy/helm/pca/test/kind-e2e.sh` 6 步含 NetworkPolicy=internal 外网拒实证 + `docs/DEPLOY-K8S.md` 10 段 + `docs/DEPLOY.md` §1 K8s 行 + `docs/SECURITY-SANDBOX.md` §3.1 docker.sock 妥协对照消除注；compose E2E 1–67 不变）

每切片：读 plan → 实现 → 更新 `SLICE-VERIFICATION.md` + E2E 步号 → README 勾选。

### 5.3 Full P1 状态

按 [`docs/P1-ROADMAP.md`](docs/P1-ROADMAP.md)：**切片 18–22d2 全部落地**；**Slice 23（N8N）跳过** — Full P1 **核心完成**。Compose Pilot #11–#15 已收口（`689466a`）。

### 5.4 Compose 试点技术债（P2 运维）

Full P1 完成后、Slice 23 之前，单实例 compose **#11–#15 全部交付**（含 #12 re-embed、#13 snapshot restore）。计划与**每项改完必测**纪律见 [`docs/P2-COMPOSE-PILOT.md`](docs/P2-COMPOSE-PILOT.md)。

```bash
go test ./... -count=1
go vet ./...
cd deploy/compose && ./test-e2e.sh   # 69/69
```

**Compose 试点轨道已收口。**

### 5.5 已知"未做"的设计决策（留给后续）

- Embedding 维度切换工具链（换模型 = 清表 / 重新生成）
- 记忆 confidence / 衰减 / 重排（Reflection 切片）
- Project / Tenant scope memory（多用户共享层）
- Hybrid（vector + keyword RRF）检索融合
- Long-content chunking（当前假设 memory 内容短）

---

## 6. 重要设计决策（ADR 摘录）

### 6.1 架构层面

| ADR | 决策 | 出处 |
|---|---|---|
| ADR-1 | 模块化单体起步，渐进拆分微服务 | 主 spec §11 |
| ADR-2 | Tool Bus 统一抽象，内置能力也 MCP 化 | 主 spec §11 |
| ADR-3 | 控制流原语保留在 Workflow Engine 而非 MCP | 主 spec §11 |
| ADR-4 | Workflow 发布后自动注册为 MCP 工具 | 主 spec §11 |
| ADR-7 | N8N 作为对等服务集成 | 主 spec §11 |
| ADR-53 | pgvector 推后到 Slice 11（已兑现） | Slice 7 spec |
| ADR-58 | Embedding 维度硬编码 1536 | Slice 11 spec |
| ADR-59 | ivfflat lists=100 + partial WHERE embedding IS NOT NULL | Slice 11 spec |
| ADR-60 | Search default vector，`mode=keyword` 显式退回 | Slice 11 spec |
| ADR-61 | Create 0.92 dedup hit → touch + 返回原 id（200，不是 201） | Slice 11 spec |
| ADR-62 | Embedder 同步、失败拒绝；不静默落库无 embedding | Slice 11 spec |
| ADR-63 | Mock embedding sha256+L2-normalize 出 deterministic 1536-d | Slice 11 spec |

### 6.2 哲学层

| 原则 | 实践 |
|---|---|
| 安全先行 | 沙箱 cap_drop + 内网 + 资源限；token 不入库；audit 仅记 sha256 |
| 隔离明确 | `SandboxRuntime` / `Provider` / `Tool` / `Embedder` 接口屏蔽具体实现 |
| 不重复造轮子 | 复用 OpenAI / Anthropic / MCP / pgvector 协议 |
| 测试金字塔 | ~70% 单元 / ~20% 集成（dockertest+httptest） / ~8% E2E / ~2% 在线 |
| Detached ctx 写库 | audit / model_usage / tool_invocations 都用 5s detached ctx |
| Embedder 失败拒绝 | 不静默落库无 embedding 行（避免"看似入库但 vector search 不可见"的隐形分裂） |

---

## 7. 运行和测试命令

### 7.1 一次性环境准备

```powershell
go version            # 应显示 go1.25+
docker --version      # 应显示 Docker 28+
docker compose version
git --version
```

### 7.2 单元 + 集成测试

```bash
cd F:/project/private-coding-agent

# 全部测试（不含真 Docker 集成）
go test ./... -count=1

# vet / build
go vet ./...
go build ./...

# 真 Docker 集成测试
docker run -d --rm --name pca-redis-test -p 6379:6379 redis:7-alpine
go test -tags=docker_integration ./internal/sandbox/... -count=1 -timeout=180s
docker stop pca-redis-test
```

### 7.3 端到端（39 步）

```bash
cd F:/project/private-coding-agent/deploy/compose
docker compose down 2>&1 | tail -1
./test-e2e.sh
# 期望最后输出: E2E PASS
```

39 步覆盖：
- [1-8] 沙箱生命周期
- [9-12] model gateway（chat 非流/流 + embeddings + usage 校验）
- [13-18] tool bus + agent
- [19-21] sessions + WebSocket
- [22-25] memory CRUD + MCP round-trip
- [26-28] SPA fallback
- [29-32] audit
- [33-35] metrics
- [36-39] **Slice 11**：vector ranking / keyword 退回 / dedup hit / dedup miss

### 7.4 本地直接跑

```powershell
Copy-Item config\config.example.yaml config\config.yaml -Force
$env:PCA_AUTH_JWT_SECRET = "dev-only-replace-in-prod-7Hk2wQpL3xRnF8tEsCvBmAyZ"
go run ./cmd/server --config config\config.yaml
```

### 7.5 跑 compose 后手工调端点

```bash
TOK=$(curl -s -X POST http://localhost:8080/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"tenant":"default","email":"demo@example.com","password":"demo123"}' | jq -r .token)

# 创建一条记忆（自动算 embedding）
curl -X POST http://localhost:8080/memories \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"type":"preference","content":"user loves golang generics"}'

# 向量检索（default mode = vector）
curl -X POST http://localhost:8080/tools/invoke \
  -H "Authorization: Bearer $TOK" -H 'Content-Type: application/json' \
  -d '{"tool":"memory.search","input":{"query":"golang"}}'
```

### 7.6 查看持久化数据

```bash
cd F:/project/private-coding-agent/deploy/compose

# 验证 pgvector extension
docker compose exec postgres psql -U app -d app -c "\dx vector"

# memories 表 + embedding 列
docker compose exec postgres psql -U app -d app -c "\d memories"

# 看 LLM 用量（含 embeddings）
docker compose exec postgres psql -U app -d app -c \
  "SELECT provider_type, model, action, status, input_tokens, output_tokens \
   FROM model_usage ORDER BY occurred_at DESC LIMIT 10;"
```

### 7.7 git 状态总览

```bash
cd F:/project/private-coding-agent
git log --oneline | wc -l          # 应显示 139
git status --short                  # Slice 11 改动列表
git diff --stat HEAD                # 累计改动
```

---

## 附录：最近 30 条 commit

```
b2eb6bb docs(slice-10): observability section + plan archive
dec7c54 feat(compose): wire jaeger + prometheus + e2e 35 steps
ec179b8 feat(observability): OTel spans + Prometheus metrics
ed0b7ab feat(observability): structured logging + request-id middleware
3d3e0a2 feat(audit): slice 9 — admin audit query + domain event instrumentation
f626093 test(e2e): step 28 uses GET to assert API content-type
c064f09 fix(httpx): SPA fallback treats HEAD as GET
57955d1 docs: mark slice 8 complete and document web frontend
5a3b8c4 feat(server): embed web UI and serve SPA from Go binary
f041887 feat(webui): WebSocket chat streaming with tool-call cards
d2eac49 feat(web): chat page scaffold with history fetch and Composer
25cbb7e feat(web): session list with create/delete + Home shell
ca4625c feat(web): login page with auth flow
c21e0d8 feat(web): types, api client, auth store, router shell
cfc450a feat(web): Vite+React+TS+Tailwind+shadcn scaffold
fa07f5f feat(server): WS ?token= shim + SPA fallback for embedded web UI
9e3a7a4 docs(web): slice 8 design spec + formal plan
384f590 docs(memory): formal slice 7 implementation plan
53a451d docs: mark slice 7 complete and document /memories endpoints + memory tools
8d2a5fe test(e2e): extend to 25 steps with /memories CRUD + MCP round-trip
440a9a8 feat(memory): wire Service + REST handler + 4 MCP tools into main
82617e3 feat(memory): internal MCP tools memory.{save,search,list,delete}
a2a622f feat(memory): REST CRUD handler for /memories endpoints
cf27aa8 feat(memory): Service layer with validation + cross-tenant safety
3a43206 feat(memory): Repo with dockertest coverage (CRUD + search + last_used_at touch)
5235647 feat(db): migration 0009 memories table
e3623f2 docs(memory): design spec for slice 7 (basic memory)
0360877 fix(e2e): route websocat via compose network
5ddf401 docs(plan): slice 6 formal implementation plan
a5fd48b docs: mark slice 6 complete in README and add /sessions endpoints
```

完整历史：`git log --oneline | head -139`
