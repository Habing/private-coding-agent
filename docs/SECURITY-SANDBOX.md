# 沙箱安全模型

> 适用范围：MVP-P1（DockerDriver）。K8sDriver / seccomp / trivy / 镜像签名留给 [`p1-full-enterprise-design.md`](superpowers/specs/2026-05-21-p1-full-enterprise-design.md) 切片 22。

沙箱是本项目最大的攻击面：Agent 的 shell.exec / fs.write 一旦失控，宿主机即被波及。本文交代当前 DockerDriver 的硬化措施、已知坑、以及哪些必须等切片 22。

## 1. 威胁模型

| 攻击者 | 能力 | 防线 |
|--------|------|------|
| 同租户的合法用户 | 任意 prompt + tool call | tenant_id 隔离、quota、audit |
| 跨租户的合法用户 | 拿到的 JWT 只有自己 tenant_id | 所有 repo query 强制 `WHERE tenant_id=$claims`；沙箱 label 也带 tenant_id |
| 沙箱内逃逸者 | 在沙箱内执行任意命令 | 见 §2 容器硬化；目前无 seccomp，依赖 cap 限制 |
| 拿到宿主 SSH 的人 | 直接读 docker.sock | 见 §3——这是 MVP 阶段最大的妥协 |
| 网络攻击者 | 从外网打 server | TLS 应在反向代理层；OIDC + JWT；rate limit |

## 2. 容器硬化（当前实现）

`internal/sandbox/docker_driver.go` 的 `createAndStartContainer` 钉死了一组安全 HostConfig，**不可被 API 调用方覆盖**：

| 字段 | 值 | 作用 |
|------|----|----|
| `ReadonlyRootfs` | `true` | rootfs 不可写；攻击者无法持久化 |
| `Tmpfs["/workspace"]` | `size=1g,uid=10001,gid=10001` | Agent 唯一可写目录；销毁即清 |
| `Tmpfs["/tmp"]` | `size=1g` | 软件常用临时区；同上 |
| `CapDrop` | `["ALL"]` | 默认丢弃所有 Linux capability |
| `CapAdd` | `CHOWN, DAC_OVERRIDE, SETUID, SETGID, FOWNER` | 仅为让 `npm install`/`pip install` 之类工作；不含 `NET_ADMIN`/`SYS_ADMIN`/`SYS_PTRACE` |
| `SecurityOpt` | `no-new-privileges:true` | 阻断 setuid 提权链 |
| `Memory` | 默认 512MB，上限 4GB | OOM kill 兜底 |
| `NanoCPUs` | 默认 1.0，上限 4.0 | CPU 配额 |
| `PidsLimit` | 默认 256，上限 1024 | fork bomb 防护 |
| `NetworkMode` | 默认 `internal`（compose 自建网络，无外网），可选 `bridge`（外网，dev 用）或 `none`（气隙） |

默认 / 上限常量在 `internal/sandbox/types.go`，校验在 `internal/sandbox/validate.go`。API 传入 0 → 默认；超 `Max*` → 直接 400 拒绝。

> **`shell.exec` 与 `fs.write` 内部都强制走 `/workspace` 前缀**，越界返回 `ErrPathOutsideWorkspace`（`internal/sandbox/types.go`）。

## 3. docker.sock 妥协（MVP 阶段已知）

`deploy/compose/docker-compose.yml` 给 server 容器挂了 `/var/run/docker.sock`，并以 `user: "0:0"` 启动。**这等价于把宿主 root 权限交给 server 进程。** 这是 MVP 阶段交付密度的妥协，明确写在 [`HANDOFF.md`](../HANDOFF.md) §4.2 的环境注意事项里。

风险与缓解：

| 风险 | 缓解（MVP） | 终极方案（切片 22） |
|------|-------------|---------------------|
| Server 进程被 RCE → 通过 sock 拿宿主 root | 1) Server 与沙箱跑在同台单租户主机；2) Server 镜像 distroless 缩攻击面；3) /metrics、/audit 路径 admin-only | 切到 K8sDriver——server 进程经 ServiceAccount 仅有 `pods.create/exec` 权限，没有 sock |
| 共享 sock 让某沙箱镜像在 build 阶段污染其他沙箱 | sandbox 镜像走 `pca/sandbox:base`，build 由 `sandbox-image-builder` 一次性预热 | trivy 扫描 + 签名校验（切片 22） |
| 没有 rootless docker | 接受；试点环境 docker host 只跑 PCA | rootless docker 或 K8s + nonroot UID |

**如果客户环境对 docker.sock 不可接受，必须等切片 22。** 不要试图把 server 切到 nonroot 而保留 sock 挂载——会拿不到 sock 权限。

## 4. 网络隔离

`networkModeFor` 把 API 的 `NetworkMode` 映射为 Docker 网络：

| 值 | Docker 实际网络 | 用途 | 安全后果 |
|----|-----------------|------|----------|
| `internal`（默认） | compose 自建网络 `pca_internal`（`internal: true`） | 沙箱之间可通信，但**无 internet 出口** | 推荐生产默认；模型/记忆/外部依赖应走 server 中转，不让沙箱直接出网 |
| `bridge` | 默认 bridge | 能拉公网包 | **仅 dev**；生产开 = 沙箱可外联，等价于把 RCE 当代理 |
| `none` | `--network=none` | 完全气隙 | 跑 lint/编译之类纯本地任务首选 |

server 容器自己接的是 compose 的默认 `default` 网络（能跑 postgres/redis/jaeger），与沙箱共享的 `pca_internal` 是另一张网。沙箱**默认看不见** server 的 postgres——除非有人显式把它接进来。

**生产建议**：把宿主的 docker default bridge 限到内网网段，外网走显式 egress 代理；或者干脆 `iptables` 把沙箱 cgroup 拦死。

## 5. 文件读写边界

- `fs.read / fs.write / fs.list / fs.glob / grep` 都走 `sandbox.Driver` 接口，路径强制 `/workspace` 前缀，`..` 被拒
- 写入大小上限 `MaxFileSize = 1 MB`（types.go）；exec stdout/stderr 上限 `MaxStreamBytes = 128 KB` 每流
- exec 超时上限 `MaxExecTimeoutSec = 600`s；默认 60s

更深的细节看 `internal/sandbox/handler.go` 与 `internal/toolbus/tools/fs_*.go`。

## 6. 生命周期 + Reconciler

启动期 `sandbox.RunReconciler`（`cmd/server/main.go`）会：

1. 扫 `sandbox_sessions` 表里 `status in (pending, running, destroying)` 的所有行
2. 对每行查 docker 实际容器是否存在
3. 容器消失 → DB 状态改 `destroyed`（防止"server 重启后 DB 一直显示 running"造成 quota 死锁）

每次 `POST /sandbox/sessions` 与 `DELETE /sandbox/sessions/:id` 都写 audit（`sandbox.create` / `sandbox.destroy`）。审计 metadata 仅记 sandbox_id + image，不记 exec 命令原文（见 [`README.md`](../README.md) 审计章节的 PII 最小化原则）。

## 7. 配额耦合

```yaml
quota:
  sandbox_max_active: 5         # 每 tenant 同时存活沙箱
  tool_invoke_per_minute: 120   # 每 (tenant, user) 每分钟
```

`sandbox_max_active` 是**资源耗尽防线**——没有它，恶意租户可以拿一个被压泄露的 JWT 在 1s 内拉满宿主内存。MVP 阶段强烈建议 ≥ 1（即使是 1 也能挡住"agent 失控 spawn 100 个沙箱"）。

429 触发后写 audit `quota.reject{kind="sandbox.active"}` + Prometheus `pca_quota_rejects_total{kind="sandbox.active"}`。

## 8. 镜像供应链

当前 sandbox 镜像 `pca/sandbox:base` 由 `sandbox/image/Dockerfile` 本地构建，**未做签名 / 未做扫描**。生产建议（MVP 阶段手动）：

- `docker scan pca/sandbox:base` 或 `trivy image pca/sandbox:base`，把高危 CVE 补完再 push 到私有 registry
- 用 sha256 digest 而非 tag 钉死生产 server 配置中的 `sandbox.default_image`
- 镜像内**默认非 root**（uid=10001/gid=10001）——见 `sandbox/image/Dockerfile`；不要在客户环境改成 root 镜像

自动化的 trivy + signing 在切片 22 落。

## 9. 已知未做（明确出栈到切片 22）

| 项 | 当前状态 | 切片 22 落点 |
|----|----------|-------------|
| seccomp profile | docker 默认 profile（已收紧 capabilities） | 自定义 profile，禁止 `mount/ptrace/userfaultfd` 等 |
| AppArmor / SELinux | 跟随宿主 | 显式 profile |
| 镜像 trivy 扫描 | 手动 | CI gate |
| Snapshot / rootfs immutability check | ReadonlyRootfs 是唯一防线 | dm-verity 或 image digest pin |
| K8s + ServiceAccount 替换 docker.sock | 未做 | K8sDriver 上线 |
| egress 策略 | NetworkMode 三档 + compose 网络 | NetworkPolicy / egress proxy |
| audit hash chain | 朴素 append | 区块链式 SHA chain |

**所以**：MVP-P1 的沙箱**适合"内部工程师试点 + 单租户/可信多租户"**，不适合"公网开放 + 不可信用户"。要后者请等切片 22。

## 10. 生产上线建议

按风险等级排序：

1. **宿主隔离** —— PCA server 跑在独立物理/虚拟机；不要和 prod DB / Kubernetes master 同机
2. **网络** —— `sandbox.default_network: internal`（compose 已默认）；显式 egress proxy；阻断沙箱网段外联
3. **资源** —— `quota.sandbox_max_active` 按主机 RAM 算（每沙箱默认 512MB，建议预留 30% headroom）
4. **审计** —— Loki/ELK 接 `audit_log` 表 + container json log；告警规则盯 `sandbox.exec` 异常频次
5. **镜像** —— 每月 rebuild + trivy 扫；用 digest 而非 tag
6. **凭据** —— 沙箱内**不放生产凭据**；如需对接外部 API，走 server 中转工具（`llm.chat` / `memory.*` 这类）
7. **docker.sock** —— 如果客户合规说"docker.sock 不行"——等切片 22 / K8sDriver；不要尝试自己改

## 11. 验证

```bash
# 沙箱以非 root 跑
docker inspect $(docker ps -q --filter label=pca.sandbox_id) \
  --format '{{.Config.User}} {{.HostConfig.ReadonlyRootfs}} {{.HostConfig.CapDrop}}'
# 期望: 10001:10001 true [ALL]

# 沙箱无外网
docker exec $(docker ps -q --filter label=pca.sandbox_id) \
  sh -c 'curl -m 3 -fsS https://example.com || echo OFFLINE_OK'
# 期望: OFFLINE_OK (当 NetworkMode=internal)

# Reconciler 工作
docker kill $(docker ps -q --filter label=pca.sandbox_id | head -1)
# 等几秒,然后:
curl -fsS -H "Authorization: Bearer $TOK" http://localhost:8080/sandbox/sessions/<id>
# 期望: status=destroyed (而非 running)

# 配额工作
for i in $(seq 1 10); do
  curl -X POST -H "Authorization: Bearer $TOK" http://localhost:8080/sandbox/sessions \
    -d '{"image":"pca/sandbox:base"}' -H 'Content-Type: application/json' &
done; wait
# 期望: 多数请求 429 + body 含 quota_exceeded / sandbox.active
```

E2E 步骤 43 自动跑配额验证；步骤 4-8 跑沙箱生命周期。
