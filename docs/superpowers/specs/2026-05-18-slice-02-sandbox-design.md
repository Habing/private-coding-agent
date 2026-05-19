# Slice 2 — Sandbox Runtime + DockerDriver 设计

| 字段 | 值 |
|---|---|
| 文档日期 | 2026-05-18 |
| 状态 | Draft — 待用户复核 |
| 范围 | 私有化 AI 编码 Agent 平台 — P0 MVP 的第 2 个切片 |
| 前置 | **Slice 1.5（mini-plan）** 必须先完成 3 条加固（audit ctx detached、`db.Migrate` 加 ctx、拒绝默认 JWT secret） |

---

## 1. 概述

本切片交付 **服务器端沙箱运行时** —— 一个隔离的 Linux 容器，Agent 在其中读写文件、执行命令。沙箱是后续 Slice（Tool Bus 的内置 `shell.exec` / `fs.read|write`、Agent Engine 的"代码代理"能力）的基础设施。

**核心叙事**：
- 一个 `internal/sandbox` Go 包，对外暴露 `Runtime` 接口
- `DockerDriver` 是 Slice 2 唯一实现；未来 `K8sDriver` 满足同一接口
- 一套 HTTP API（同 Slice 1 服务器、JWT 鉴权）让 curl / 测试脚本可直接玩
- 默认胖镜像 `pca/sandbox:base`（Go/Node/Python/CLI 全装）
- 默认隔离：非 root、`--internal` 网络、rootfs 只读、cgroup 资源限、cap drop

**不在 Slice 2 范围**：
- Snapshot / Restore 持久化（接口留位 `ErrNotImplemented`，等 MinIO 引入后实现）
- 沙箱预热池（warm pool）—— 优化，留后续
- 命令白名单 / 黑名单（属 Tool Bus 策略层）
- 配额检查（Slice 1 Tenant.CheckQuota 已就位，但 Slice 2 暂不联动）

## 2. 前置条件 — Slice 1.5 加固

Slice 2 严格依赖 Slice 1.5 完成的 3 项加固：

1. **`audit.Middleware` 用 detached context + 5s timeout**：沙箱请求结束后 audit 写库不能用已取消的 request ctx，否则大量审计丢失
2. **`db.Migrate(ctx, dsn)` 接受 ctx**：Slice 2 启动期会跑 reconcile，需要可超时
3. **启动期拒绝默认 JWT secret**：避免误以 `change-me-in-production` 上生产

Slice 1.5 走独立的 plan + subagent 执行（约 1-3 个 commit），完成后再开 Slice 2 实施。

## 3. 核心需求

| 维度 | 决策 |
|---|---|
| 交付形态 | `internal/sandbox` Go 包 + 一套 HTTP API |
| 沙箱镜像 | 胖镜像 `pca/sandbox:base`（debian 12-slim + git/curl/jq/rg/Go/Node/Python/build-essential） |
| Exec 语义 | 单一同步接口；Stdout/Stderr 合计 ≤ 256 KB（超截断）；超时 ctx 控制 |
| 工作区 | 每会话独立 tmpfs `/workspace`，不跨会话持久化 |
| 网络 | 默认 `--internal`（deny-all 出网），可显式 `bridge` 放开（dev/测试用） |
| 资源 | 默认 1 CPU / 512 MB / 256 pids（创建时可覆盖到上限 4 CPU / 4 GB） |
| 安全 | non-root（uid 10001）、rootfs 只读、cap drop all + 最小白名单、no-new-privileges、seccomp default |
| Snapshot | 接口保留、实现返 `ErrNotImplemented`（501） |
| 持久化 | `sandbox_sessions` 表（PG）记录元数据；进程重启走 reconcile |

非功能：
- 创建沙箱 P95 < 4 s（镜像已缓存）
- Exec 短命令 P50 < 100 ms
- 单实例支持 ≥ 50 并发沙箱
- 多租户隔离：sandbox_id 跨租户访问统一 404

## 4. 整体架构

```
+------------------------------------------------------------+
|  HTTP 层  (gin)                                             |
|   POST   /sandbox/sessions                                  |
|   GET    /sandbox/sessions/{id}                             |
|   DELETE /sandbox/sessions/{id}                             |
|   POST   /sandbox/sessions/{id}/exec                        |
|   GET    /sandbox/sessions/{id}/files?path=...              |
|   PUT    /sandbox/sessions/{id}/files?path=...              |
|   POST   /sandbox/sessions/{id}/snapshot   → 501            |
+------------------------------------------------------------+
                          | uses
                          v
+------------------------------------------------------------+
|  internal/sandbox/                                          |
|   Runtime (interface)                                       |
|     Create / Get / Destroy / Exec / ReadFile / WriteFile   |
|     / Snapshot                                              |
|                                                             |
|   DockerDriver  实现 Runtime,使用 docker SDK                |
|                                                             |
|   SessionRepo (PG) 持久化沙箱元数据                          |
|                                                             |
|   Reconciler 启动期一次性扫描已死容器                        |
|                                                             |
|   path.go / validate.go 纯函数校验层                         |
+------------------------------------------------------------+
                          | uses
                          v
+------------------------------------------------------------+
|  数据层                                                     |
|   PostgreSQL: sandbox_sessions 表                           |
|   Docker daemon: /var/run/docker.sock (Linux) /             |
|                  //./pipe/docker_engine (Windows host)      |
+------------------------------------------------------------+
```

横切：所有 HTTP 入口共用 Slice 1 已有的 `audit.Middleware` + `auth.Middleware` + otelgin。

## 5. 接口与数据模型

### 5.1 公共类型 (`internal/sandbox/types.go`)

```go
type Status string
const (
    StatusPending    Status = "pending"
    StatusRunning    Status = "running"
    StatusDestroying Status = "destroying"
    StatusDestroyed  Status = "destroyed"
    StatusFailed     Status = "failed"
)

type Sandbox struct {
    ID          uuid.UUID
    TenantID    uuid.UUID
    ProjectID   *uuid.UUID
    OwnerUserID uuid.UUID
    Status      Status
    Image       string
    CreatedAt   time.Time
    UpdatedAt   time.Time
    DestroyedAt *time.Time
}

type CreateOpts struct {
    TenantID    uuid.UUID
    OwnerUserID uuid.UUID
    ProjectID   *uuid.UUID
    Image       string
    Resources   ResourceLimits
    Network     NetworkMode
    Env         map[string]string
    Labels      map[string]string
}

type ResourceLimits struct {
    CPUs      float64
    MemoryMB  int64
    PIDsLimit int64
}

type NetworkMode string
const (
    NetworkInternal NetworkMode = "internal"
    NetworkBridge   NetworkMode = "bridge"
    NetworkNone     NetworkMode = "none"
)

type ExecOpts struct {
    Cmd        []string
    WorkingDir string
    Env        map[string]string
    Stdin      []byte
    TimeoutSec int
}

type ExecResult struct {
    ExitCode   int
    Stdout     []byte
    Stderr     []byte
    Truncated  bool
    DurationMS int64
    TimedOut   bool
}

var (
    ErrSandboxNotFound      = errors.New("sandbox not found")
    ErrSandboxNotReady      = errors.New("sandbox not running")
    ErrPathOutsideWorkspace = errors.New("path outside /workspace")
    ErrNotImplemented       = errors.New("not implemented")
    ErrTooLarge             = errors.New("payload too large")
)
```

**默认值**：
- `Image = "pca/sandbox:base"`
- `Network = NetworkInternal`
- `Resources = {CPUs: 1.0, MemoryMB: 512, PIDsLimit: 256}`
- `ExecOpts.WorkingDir = "/workspace"`
- `ExecOpts.TimeoutSec = 60`（上限 600）
- 资源上限：CPUs ≤ 4，MemoryMB ≤ 4096，PIDsLimit ≤ 1024

### 5.2 Runtime 接口 (`internal/sandbox/runtime.go`)

```go
type Runtime interface {
    Create(ctx context.Context, opts CreateOpts) (*Sandbox, error)
    Get(ctx context.Context, id uuid.UUID) (*Sandbox, error)
    Destroy(ctx context.Context, id uuid.UUID) error
    Exec(ctx context.Context, id uuid.UUID, opts ExecOpts) (*ExecResult, error)
    ReadFile(ctx context.Context, id uuid.UUID, path string) ([]byte, error)
    WriteFile(ctx context.Context, id uuid.UUID, path string, data []byte) error
    Snapshot(ctx context.Context, id uuid.UUID) (string, error)
}
```

所有方法协程安全。`Destroy` 幂等。读写文件 1 MB 上限，路径必须在 `/workspace`。

### 5.3 数据库表 — Migration 0004

```sql
CREATE TABLE sandbox_sessions (
    id            UUID PRIMARY KEY,
    tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    owner_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    project_id    UUID,
    container_id  TEXT,
    image         TEXT NOT NULL,
    status        TEXT NOT NULL,
    network_mode  TEXT NOT NULL,
    cpus          REAL NOT NULL DEFAULT 1.0,
    memory_mb     BIGINT NOT NULL DEFAULT 512,
    pids_limit    BIGINT NOT NULL DEFAULT 256,
    labels        JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    destroyed_at  TIMESTAMPTZ
);
CREATE INDEX sandbox_sessions_tenant_status_idx
    ON sandbox_sessions(tenant_id, status);
```

### 5.4 HTTP API

| 方法 | 路径 | Body / Query | 成功响应 |
|---|---|---|---|
| POST | `/sandbox/sessions` | `{image?, project_id?, resources?, network?, env?, labels?}` | 201 `{id, status, image, created_at, ...}` |
| GET | `/sandbox/sessions/{id}` | – | 200 `{id, tenant_id, owner_user_id, status, image, network_mode, resources, created_at, updated_at, destroyed_at}` |
| DELETE | `/sandbox/sessions/{id}` | – | 204 |
| POST | `/sandbox/sessions/{id}/exec` | `{cmd:[...], working_dir?, env?, stdin_base64?, timeout_sec?}` | 200 `{exit_code, stdout_base64, stderr_base64, truncated, duration_ms, timed_out}` |
| GET | `/sandbox/sessions/{id}/files?path=...` | – | 200 `{content_base64, size}` |
| PUT | `/sandbox/sessions/{id}/files?path=...` | `{content_base64}` | 204 |
| POST | `/sandbox/sessions/{id}/snapshot` | – | 501 `{error:"not_implemented"}` |

**统一约束**：
- 所有路径要 JWT；`tenant_id` 和 `user_id` 从 claims 取
- 创建时 `owner_user_id = claims.user_id`、`tenant_id = claims.tenant_id`
- 操作前查表带 `WHERE tenant_id = $claims_tid`；不命中统一 404（不区分"不存在 vs 越权"）
- 全部经 `audit.Middleware`，action = `sandbox.<method>`，target = sandbox_id
- 文件 / Exec IO 用 base64（避免 JSON 二进制问题）
- 文件 / Exec 输出上限：1 MB（文件）、合计 256 KB（exec 输出）

### 5.5 沙箱基础镜像 `pca/sandbox:base`

独立 Dockerfile：`sandbox/image/Dockerfile`

```dockerfile
FROM debian:12-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
      git curl wget ca-certificates jq tree ripgrep make \
      golang nodejs npm python3 python3-pip \
      build-essential \
    && rm -rf /var/lib/apt/lists/*
RUN useradd -m -u 10001 -s /bin/bash sandbox
USER sandbox
WORKDIR /workspace
CMD ["sleep", "infinity"]
```

镜像大小预估 ~1.2 GB。docker-compose 通过 `build:` 字段在首次启动时构建（或 CI 中预 build 上 registry，本地 pull）。

### 5.6 容器运行时配置（DockerDriver.Create 行为）

```
image:        pca/sandbox:base（CreateOpts 可覆盖）
user:         sandbox (uid=10001)
working_dir:  /workspace
rootfs:       read-only
volumes:
  /workspace = tmpfs (默认；snapshot 实装后改 named volume)
  /tmp       = tmpfs, size=1G
network:      --internal (NetworkInternal) / bridge / none
resources:    --cpus / --memory / --pids-limit 按 CreateOpts
cap_drop:     ALL
cap_add:      CHOWN, DAC_OVERRIDE, SETUID, SETGID, FOWNER
security_opt: no-new-privileges, seccomp=default(Docker default profile)
labels:
  pca.tenant_id      = <uuid>
  pca.sandbox_id     = <uuid>
  pca.owner_user_id  = <uuid>
stop_signal:  SIGTERM, grace 5s → SIGKILL
```

## 6. 数据流

### 6.1 创建沙箱（POST /sandbox/sessions）

```
1. handler 解 body + 校 JWT claims
2. CreateOpts 填默认 + 范围校验
3. SessionRepo.Insert(status=pending, container_id=NULL)
4. DockerDriver.containerCreate (image / labels / mounts / resources / security)
5. ContainerStart
6. SessionRepo.UpdateRunning(container_id)
7. 返 201

失败路径:
- 4/5 失败 → ContainerRemove(force) 清理 + SessionRepo.MarkFailed
- 总 ctx timeout 30s
```

### 6.2 执行命令（POST .../exec）

```
1. handler 解 body + 校 cmd/timeout 范围
2. SessionRepo.Get(id, tenant_id) → 不命中 404
3. 状态 != running → 409 not_ready
4. ContainerExecCreate(cmd, env, working_dir, tty=false)
5. ContainerExecAttach (stream stdout+stderr)
6. 读流 → byte buffer，超 128 KB/路截断
7. ctx 取消 / TimeoutSec 到 → ContainerKill
8. ContainerExecInspect → ExitCode
9. 返 200 + ExecResult JSON (stdout/stderr base64)
```

### 6.3 文件读写

```
WriteFile:
  1. 校 path (filepath.Clean → strings.HasPrefix("/workspace/"))
  2. 校 size ≤ 1MB
  3. 构造 tar stream (单 entry)
  4. CopyToContainer(containerID, "/", tarStream)
  5. 返 204

ReadFile:
  1. 校 path
  2. CopyFromContainer(containerID, "/workspace/<path>") → tar
  3. 取单 entry，校 size ≤ 1MB（流式计数）
  4. base64 编码返 200
```

### 6.4 销毁（DELETE）

```
1. Redis SETNX 锁 "sandbox:destroy:<id>" TTL=30s
2. SessionRepo.Get → 404 / 已 destroyed 直接 204 (幂等)
3. SessionRepo.UpdateStatus(destroying)
4. ContainerStop(timeout=5s)（先 SIGTERM 再 SIGKILL）
5. ContainerRemove(force=true, volumes=true)
6. SessionRepo.UpdateStatus(destroyed) + destroyed_at=now()
7. 释放锁
8. 返 204
```

### 6.5 Reconcile（启动期）

```go
reconciler.Run(ctx) // 在 main run() 中、HTTP server 启动前调用
```

```
1. SELECT * FROM sandbox_sessions WHERE status IN ('pending','running','destroying')
2. 对每条:
   a. ContainerInspect(container_id)
      - not exists / exited / dead → SessionRepo.MarkDestroyed
      - running → 保留
   b. updated_at < now() - 24h && status != running → 强 Destroy
3. ready.Store(true)
```

Docker 连不上时跳过 reconcile，readyz 持续 503 直至 Docker 可达。

### 6.6 横切

- 每次 `Runtime` 调用经 `audit.Middleware`（HTTP 层）+ OTel span（实施层）
- audit metadata 记 `container_id / cmd[0] / exit_code / timed_out / truncated / duration_ms`
- **不**记 stdin / stdout / stderr 内容（防泄密）
- 完整 cmd line 走 OTel span（可采样），不入审计

## 7. 错误处理

### 7.1 HTTP 入口

| 故障 | 状态码 / body |
|---|---|
| 缺/坏 JWT | 401 `missing_token` / `invalid_token` |
| 沙箱不存在或跨租户 | 404 `not_found`（不区分） |
| 请求体 JSON 不合法 | 400 `bad_request` |
| 参数越界 (path 越出 / cmd 空 / timeout > 600 / 资源越上限) | 400 `validation: <字段>` |
| 文件大小超 1 MB / Exec 输出超 256 KB（写入时） | 413 `payload_too_large` |
| 路径越界 | 400 `path_outside_workspace` |
| Snapshot | 501 `not_implemented` |

### 7.2 Runtime / Docker 层

| 故障 | 处置 |
|---|---|
| Docker daemon 不可达 | 503 `runtime_unavailable`；`/readyz` 联动返 503 |
| 镜像 pull 失败 | 500 `image_unavailable` |
| ContainerCreate 失败 | sandbox_sessions 置 `failed`，500 `runtime_error` |
| 启动总超时 (30s) | force remove 兜底，置 failed，504 `timeout` |
| Exec timeout | 200 + `TimedOut=true`（不是 HTTP 错误） |
| OOM kill | 200 + `ExitCode=137`（不是 HTTP 错误）+ server log 标记 |
| 容器意外死亡 | reconcile 标 destroyed；后续操作返 404 |
| Copy IO 失败 | 500 `io_error` |

### 7.3 状态机

| 当前状态 | Exec/IO | Destroy |
|---|---|---|
| pending | 409 not_ready | 等到 running 或直接清理 |
| running | OK | OK |
| destroying | 409 destroying | 加锁等已有 Destroy 完成 |
| destroyed | 404 | 204（幂等） |
| failed | 409 sandbox_failed | 204（尝试清残留） |

### 7.4 并发

- Destroy：Redis SETNX 锁
- Exec：不加锁（Docker 自身支持）
- 同沙箱 Exec + Destroy：Destroy 抢到锁后 Kill 进行中的 Exec，Exec 返 `ExitCode=-1`

### 7.5 安全 / 越权

- 跨租户访问统一 404（防枚举）
- 沙箱内不挂 docker.sock，cap drop 充分
- 路径用 `filepath.Clean` + 前缀校验，二次走 tar 不 follow 符号链接
- fork bomb 等由 PIDsLimit + memory cgroup 兜底

## 8. 测试策略

### 8.1 单元测试

| 文件 | 覆盖 |
|---|---|
| `path_test.go` | 路径校验（正常 / .. / 绝对 / 越界 / 符号链接） |
| `validate_test.go` | ExecOpts / CreateOpts 范围校验 |
| `types_test.go` | 状态机合法 / 非法转换 |
| `sessionrepo_test.go` | dockertest PG：CRUD + 租户过滤 |

### 8.2 集成测试（build tag `docker_integration`）

`internal/sandbox/docker_driver_test.go`：

- Create / Get / Destroy（含幂等、并发）
- Exec 各种情况：成功、非零、stderr、timeout、截断、OOM
- File read/write round trip / 太大 / 路径越界 / 中间目录自动创建
- NetworkInternal 隔离验证（curl 失败）
- NetworkBridge 放开验证（curl 成功）
- Snapshot 返 ErrNotImplemented

运行：
```bash
go test -tags=docker_integration ./internal/sandbox/...
```

CI 阶段先 build `pca/sandbox:base`，再跑集成测试。

### 8.3 HTTP handler 测试

`handler_test.go` — 手写 mock Runtime（接口已有），覆盖：
- 鉴权、参数校验、错误映射、状态码、JSON 字段

### 8.4 E2E

PowerShell 脚本 `deploy/compose/test-e2e.ps1`：登录 → 建沙箱 → 写文件 → exec cat → 销毁 → 校 404。

入仓但**不入门禁**（手动跑，记录于 README 验收步骤）。

### 8.5 性能基线（informational）

- Create P95 < 4 s
- Exec `echo` P50 < 100 ms
- Read/Write 1 MB P50 < 200 ms
- Destroy P50 < 500 ms

### 8.6 安全（P0 基线）

- docker.sock 不可达
- NetworkInternal 出网失败
- 路径穿越被拒
- fork bomb 被 PIDsLimit 兜住

更激进的 seccomp 逃逸 / CVE 测试留 Slice 9。

## 9. 阶段规划（Slice 2 内部 Task 拆解）

| Task | 内容 |
|---|---|
| 0 | `sandbox/image/Dockerfile` + 构建脚本；`.dockerignore` 调整 |
| 1 | `internal/sandbox/types.go` — 全部公共类型 + 错误哨兵 + 默认值常量 + 单元测 |
| 2 | migration 0004_create_sandbox_sessions + `SessionRepo` + dockertest 集成测 |
| 3 | `internal/sandbox/path.go` 路径校验 + 单元测 |
| 4 | `internal/sandbox/validate.go` ExecOpts/CreateOpts 校验 + 单元测 |
| 5 | `internal/sandbox/runtime.go` Runtime 接口 |
| 6 | `internal/sandbox/docker_driver.go` 骨架 + 构造器 |
| 7 | `DockerDriver.Create` + 集成测 |
| 8 | `DockerDriver.Get` + `Destroy`（幂等） + 集成测 |
| 9 | `DockerDriver.Exec`（含 timeout / 截断） + 集成测 |
| 10 | `DockerDriver.ReadFile` / `WriteFile`（tar 流） + 集成测 |
| 11 | `DockerDriver.Snapshot` 返 ErrNotImplemented + 单元测 |
| 12 | `internal/sandbox/handler.go` HTTP handlers + mock-runtime 单元测 |
| 13 | main.go 装配（注入 Runtime、注册路由）+ `/readyz` 检测 Docker |
| 14 | docker-compose 更新（挂 docker.sock、预构建 base 镜像）+ 调 server depends |
| 15 | `internal/sandbox/reconciler.go` + 单元/集成测 |
| 16 | E2E 脚本 + README 更新 + 验收清单 |

执行顺序：0 → 1 → 2 → 3 → 4 → 5 → 6 → 7 → 8 → 9 → 10 → 11 → 12 → 13 → 14 → 15 → 16（subagent-driven 全串行）。

## 10. 验收清单

- [ ] **Slice 1.5 已完成**（前置）
- [ ] `go test ./...` 全 PASS（含 `-tags=docker_integration`）
- [ ] `go build ./...` 无错
- [ ] `pca/sandbox:base` 镜像构建成功
- [ ] `docker compose up -d --build` 后 `/healthz` 与 `/readyz` 均 200
- [ ] `deploy/compose/test-e2e.ps1` 端到端跑通
- [ ] `audit_log` 表含 `sandbox.*` action 行
- [ ] `sandbox_sessions` 表含 destroyed 与 active 状态行
- [ ] OTel trace 包含 `sandbox.create` / `sandbox.exec` 等 span
- [ ] 所有 Task commit、git tree clean

## 11. 风险与开放问题

| 风险 | 缓解 |
|---|---|
| Docker socket 在 server 容器内的挂载（compose `volumes: /var/run/docker.sock:/var/run/docker.sock`）等于把 server 容器提权到主机 root | 接受（私有化部署语境合理）；后续切片可考虑用 dind 或 rootless-docker，但 P0 简单优先 |
| 沙箱镜像 1.2 GB 拉取慢 / 初次 build 慢 | 文档化 "首次 build 预计 10 min"；CI 可预 build 上 registry |
| Windows 主机：docker.sock 实际是 `//./pipe/docker_engine`，挂载语法不同 | docker-compose 文件区分 host 类型；Linux + macOS Desktop 用 unix socket，Windows Desktop 用 npipe |
| 容器内 fork bomb 仍可能拖累主机调度（即便 cgroup 限） | PIDsLimit + cpus 限 + 主机级监控告警 |
| `/workspace` tmpfs，沙箱重启数据丢 | 文档化；Snapshot 接口预留（未来加 MinIO） |
| Docker SDK 升级 breaking change | 锁定 minor 版本，在 go.mod 钉死 |

## 12. ADR 摘要

| ID | 决策 | 理由 |
|---|---|---|
| ADR-11 | `Runtime` 接口在 `internal/sandbox` 包内定义 | Go 习惯：消费方定义接口（HTTP handler 消费） |
| ADR-12 | HTTP API 同进程暴露而非独立服务 | P0 简化、Slice 1 已具备 gin 框架 |
| ADR-13 | 胖镜像 `pca/sandbox:base`（debian + Go/Node/Python） | 覆盖编码场景 80%，冷启动快 |
| ADR-14 | Exec 同步语义 | P0 简单，长命令靠 ctx 超时；流式留后续 |
| ADR-15 | 默认 `--internal` 网络（无外网） | 安全优先；显式 `bridge` 切换 |
| ADR-16 | rootfs 只读 + /workspace tmpfs | 每会话隔离，无残留 |
| ADR-17 | Snapshot 接口留位、实现 stub | 等 MinIO 引入后实装 |
| ADR-18 | 跨租户访问统一 404 | 防租户枚举 |
| ADR-19 | `failed` 沙箱记录保留 7 天 | 审计 / 排障可见性 |
| ADR-20 | Reconcile 在启动期一次性扫描 | 进程崩溃后清残留；不做后台周期 reconciler（YAGNI） |

## 13. 开放问题

1. CI 跑 `docker_integration` 测试时如何分发 `pca/sandbox:base` 镜像？build-on-CI vs push-to-registry——尚未决定（GitLab CI / GitHub Actions 选型未拍板）。
2. 是否需要在 Slice 2 给沙箱内进程加 SBOM / 漏洞扫描（trivy）？倾向不做（Slice 9 加固期处理）。
3. 沙箱基础镜像的工具链版本如何维护？打 tag `pca/sandbox:base-2026.05` 还是 `:latest`？倾向语义化版本 + 月度 tag。

---

**审核状态**：草稿，待用户复核。
