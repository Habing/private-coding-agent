# 沙箱安全模型

> 适用范围：MVP-P1（DockerDriver）。K8sDriver / 镜像签名留给 [`p1-full-enterprise-design.md`](superpowers/specs/2026-05-21-p1-full-enterprise-design.md) 切片 22d。seccomp profile + trivy CI gate 自 22c 起已交付（见 §2 / §9）。

沙箱是本项目最大的攻击面：Agent 的 shell.exec / fs.write 一旦失控，宿主机即被波及。本文交代当前 DockerDriver 的硬化措施、已知坑、以及哪些必须等切片 22。

## 1. 威胁模型

| 攻击者 | 能力 | 防线 |
|--------|------|------|
| 同租户的合法用户 | 任意 prompt + tool call | tenant_id 隔离、quota、audit |
| 跨租户的合法用户 | 拿到的 JWT 只有自己 tenant_id | 所有 repo query 强制 `WHERE tenant_id=$claims`；沙箱 label 也带 tenant_id |
| 沙箱内逃逸者 | 在沙箱内执行任意命令 | 见 §2 容器硬化：CapDrop ALL + 自定义 seccomp profile（禁 `mount/ptrace/keyctl/bpf/userfaultfd/perf_event_open` 等）防御纵深 |
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
| `SecurityOpt` | `no-new-privileges:true` + `seccomp=<自定义 profile>` | 阻断 setuid 提权链；seccomp 在 syscall 层拒绝 `mount/umount/pivot_root/ptrace/process_vm_*/keyctl/add_key/bpf/init_module/kexec_load/userfaultfd/perf_event_open` 等约 16 个高危调用（即便 CapDrop 配置错也兜底）。profile 由 `//go:embed internal/sandbox/seccomp.json` 内嵌，server 启动期一次加载。可通过 `PCA_SANDBOX_SECCOMP_ENABLED=false` 应急回退到 Docker 默认 profile |
| `Memory` | 默认 512MB，上限 4GB | OOM kill 兜底 |
| `NanoCPUs` | 默认 1.0，上限 4.0 | CPU 配额 |
| `PidsLimit` | 默认 256，上限 1024 | fork bomb 防护 |
| `NetworkMode` | 默认 `internal`（compose 自建网络，无外网），可选 `bridge`（外网，dev 用）或 `none`（气隙） |

默认 / 上限常量在 `internal/sandbox/types.go`，校验在 `internal/sandbox/validate.go`。API 传入 0 → 默认；超 `Max*` → 直接 400 拒绝。

> **`shell.exec` 与 `fs.write` 内部都强制走 `/workspace` 前缀**，越界返回 `ErrPathOutsideWorkspace`（`internal/sandbox/types.go`）。

### 2.1 K8s 部署等价表（切片 22d1）

当 `cfg.Sandbox.Driver="k8s"` 时，`internal/sandbox/k8s_driver.go` 的 `buildPod` 把以上 Docker HostConfig 1:1 映射到 Pod spec — 同样钉死、不可被 API 调用方覆盖：

| Docker 字段 | K8s Pod 字段 | 备注 |
|------|----|----|
| `ReadonlyRootfs=true` | `securityContext.readOnlyRootFilesystem=true` | 容器级别 |
| `Tmpfs["/workspace"]` | `volumes[].emptyDir{medium:Memory, sizeLimit:1Gi}` + `volumeMount` | tmpfs 在 mount namespace 内 |
| `Tmpfs["/tmp"]` | 同上，独立 emptyDir | — |
| `CapDrop=ALL` + 5 add | `securityContext.capabilities.{drop:[ALL], add:[CHOWN,DAC_OVERRIDE,SETUID,SETGID,FOWNER]}` | 完全一致 |
| `no-new-privileges:true` | `securityContext.allowPrivilegeEscalation=false` | 语义等价 |
| `seccomp=<embedded JSON>` | `securityContext.seccompProfile.type=Localhost` + `localhostProfile=<cfg.Sandbox.K8s.SeccompLocalhostProfile>` | 留空时退化 `RuntimeDefault`；Localhost 要求 profile 已通过 DaemonSet/镜像烘焙到每个 node 的 `/var/lib/kubelet/seccomp/` |
| `NanoCPUs` | `resources.{requests,limits}.cpu` | requests==limits → Guaranteed QoS，沙箱不能噪声邻居 |
| `Memory` | `resources.{requests,limits}.memory` | 同上 |
| `PidsLimit` | **22d1 暂未映射** — K8s 1.31+ alpha gate，待 22d-v2 | DockerDriver 路径不受影响 |
| `NetworkMode` | `labels["pca.network"]=internal\|bridge\|none` + `DNSPolicy` 切换 | 真实隔离由 22d2 Helm chart 内的 NetworkPolicy YAML 实现 |
| run as 10001 | `securityContext.{runAsUser:10001, runAsGroup:10001, runAsNonRoot:true}` | Pod 与容器级别都设 |
| n/a | `automountServiceAccountToken=false`（默认） | 仅当 `cfg.Sandbox.K8s.ServiceAccount` 非空才挂 SA token |
| n/a | `restartPolicy=Never` | Pod 死掉 → 由调用方 Destroy+Create 重建，避免 kubelet 静默重启丢失 tmpfs |

K8sDriver.Snapshot 在 22d1 阶段直接返回 `ErrSnapshotDisabled`（admin /snapshot 路由 503）；kaniko-based 方案排在 22d-v2。

## 3. docker.sock 妥协（MVP 阶段已知）

`deploy/compose/docker-compose.yml` 给 server 容器挂了 `/var/run/docker.sock`，并以 `user: "0:0"` 启动。**这等价于把宿主 root 权限交给 server 进程。** 这是 MVP 阶段交付密度的妥协，明确写在 [`HANDOFF.md`](../HANDOFF.md) §4.2 的环境注意事项里。

风险与缓解：

| 风险 | 缓解（MVP） | 终极方案（切片 22） |
|------|-------------|---------------------|
| Server 进程被 RCE → 通过 sock 拿宿主 root | 1) Server 与沙箱跑在同台单租户主机；2) Server 镜像 distroless 缩攻击面；3) /metrics、/audit 路径 admin-only | 切到 K8sDriver——server 进程经 ServiceAccount 仅有 `pods.create/exec` 权限，没有 sock |
| 共享 sock 让某沙箱镜像在 build 阶段污染其他沙箱 | sandbox 镜像走 `pca/sandbox:base`，build 由 `sandbox-image-builder` 一次性预热 | trivy 扫描 + 签名校验（切片 22） |
| 没有 rootless docker | 接受；试点环境 docker host 只跑 PCA | rootless docker 或 K8s + nonroot UID |

**如果客户环境对 docker.sock 不可接受，必须等切片 22。** 不要试图把 server 切到 nonroot 而保留 sock 挂载——会拿不到 sock 权限。

### 3.1 已落地的替代路径（切片 22d1/22d2）

K8sDriver + Helm chart（`deploy/helm/pca/`）已交付，构成 docker.sock 妥协的**对照消除**路径：

- server Pod 通过 ServiceAccount 调 kube-apiserver；**集群内不需要 docker.sock**
- chart RBAC Role 仅绑 `pca-sandboxes` ns，verbs 是 `pods{create,get,list,delete}` + `pods/exec{create}` + `pods/log{get}`——server 进程被 RCE 时，攻击者拿到的 SA token 也只能在 sandbox ns 里建/exec Pod，**拿不到 nodes、secrets、cluster-admin**
- server Pod 自身 securityContext：`runAsNonRoot=true` + `readOnlyRootFilesystem=true` + `cap drop ALL` + `seccompProfile RuntimeDefault`，与 compose 形态的 root server 形成对比
- NetworkPolicy `pca-sandbox-internal` 把 sandbox 出站锁到 release ns 内的 server pod；外网 egress 在 nightly e2e（`./deploy/helm/pca/test/kind-e2e.sh`）实证被拒

部署细节见 [`DEPLOY-K8S.md`](DEPLOY-K8S.md)；客户环境对 docker.sock 不可接受时走 K8s 形态。

## 4. 网络隔离

`networkModeFor` 把 API 的 `NetworkMode` 映射为 Docker 网络：

| 值 | Docker 实际网络 | 用途 | 安全后果 |
|----|-----------------|------|----------|
| `internal`（默认） | compose 自建网络 `pca_internal`（`internal: true`） | 沙箱之间可通信，但**无 internet 出口** | 推荐生产默认；模型/记忆/外部 HTTP 走 **server 中转**（`http.fetch`、MCP），不让沙箱直接出网 |
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

## 9. 已知未做（明确出栈到切片 22 / P2）

| 项 | 当前状态 | 后续落点 |
|----|----------|-------------|
| seccomp profile | ✅ 22c：自定义 profile，禁 `mount/ptrace/keyctl/bpf/userfaultfd/perf_event_open` 等 16 个 syscall | profile runtime override / 外挂文件（22c-v2，按需） |
| 镜像 trivy 扫描 | ✅ 22c：GitHub Actions `.github/workflows/security.yml`，PR + push to main 触发；CRITICAL 阻塞 merge / HIGH 仅 warn | nightly schedule + server 镜像扫描（22c-v2） |
| AppArmor / SELinux | 跟随宿主 | 显式 profile（P2） |
| audit hash chain | ✅ 22a：每条 audit 记录链式 SHA-256（`prev_hash → curr_hash`），admin `/audit/verify` 端点全表校验 | — |
| Snapshot / rootfs immutability check | ReadonlyRootfs 是唯一防线；22b Sandbox→MinIO snapshot 仅做导出，不做完整性校验 | dm-verity 或 image digest pin（22c-v2 / P2） |
| K8s + ServiceAccount 替换 docker.sock | ✅ 22d1 K8sDriver + 22d2 Helm chart（kind nightly 6 步）；compose 默认仍 `driver=docker` | compose 切 k8s driver（按需） |
| egress 策略 | ✅ 22d2 NetworkPolicy（internal/none + server 出站 allowlist）；compose 仍 NetworkMode 三档 | egress proxy（P2） |
| 镜像 cosign 签名 | 未做 | 22c-v2 / P2 |

**所以**：MVP-P1 的沙箱**适合「内部工程师试点 + 单租户/可信多租户」**，不适合「公网开放 + 不可信用户」。22c 起 syscall 与镜像 CVE 层都有了硬控；**K8s 路径**见 [`docs/DEPLOY-K8S.md`](DEPLOY-K8S.md)（Helm + NetworkPolicy）；compose 试点仍可用 docker.sock。

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

# seccomp profile 已注入（22c）
docker inspect $(docker ps -q --filter label=pca.sandbox_id) \
  --format '{{range .HostConfig.SecurityOpt}}{{println .}}{{end}}'
# 期望两行: no-new-privileges:true / seccomp={"defaultAction":"SCMP_ACT_ERRNO",...}

# 沙箱内 mount 被 seccomp 拒（22c）
docker exec $(docker ps -q --filter label=pca.sandbox_id) \
  sh -c 'mount -t tmpfs none /tmp/x 2>&1; echo exit=$?'
# 期望: stderr 含 "Operation not permitted" 且 exit != 0

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

E2E 步骤 43 自动跑配额验证；步骤 4-8 跑沙箱生命周期；步骤 66 跑 seccomp mount 拒绝实证 + 非危险 syscall 正常工作回归（22c）。
