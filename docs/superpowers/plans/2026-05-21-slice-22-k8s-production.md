# Slice 22 — K8s + Production Security Implementation Plan

> **Goal:** audit hash chain、Snapshot/MinIO、seccomp、trivy CI、K8sDriver、Helm；E2E 65+。
>
> **拆分:** 22 体量过大，按 19/21 模式拆为四段顺序落地：
> - **22a — Audit Hash Chain** ✅ 已落地（HEAD `7968a77`，E2E 步骤 64）
> - **22b — Snapshot → MinIO** ✅ 已落地（HEAD `a533657`，E2E 步骤 65）
> - **22c — seccomp + trivy CI** ✅ 已落地（HEAD `5425039`，E2E 步骤 66）
> - **22d1 — K8sDriver Runtime + fake-client L1** ✅ 已落地（HEAD `7981073` + C3 closeout，E2E 步骤 67）
> - **22d2 — Helm chart + kind nightly + DEPLOY-K8S.md** ✅ 已落地（HEAD `48bf11c` + C3 closeout；compose E2E 步号不增，kind 6 步 nightly 独立 PASS）

**Design:** Full P1 spec §22 + HANDOFF 技术债

**Depends on:** Slice 14、MVP-P1

---

## 22a — Audit Hash Chain ✅

> 落地：commits `83a4882` → `aa4a663` → `7968a77`；E2E **[64/64]** PASS

- [x] `internal/audit/hash.go` — `Canonical(prev, e)` + `Hash(prev, e)`（SHA-256，RS=0x1E 分隔，json.Marshal 对 map[string]any 按 key 排序保证 canonical metadata）
- [x] migration `0021_audit_hash_chain`：`audit_log` 加 `prev_hash BYTEA NOT NULL` + `entry_hash BYTEA NOT NULL`，DEFAULT 32 零字节（pre-chain 行兼容）
- [x] `Repo.Append`：`BeginTx → pg_advisory_xact_lock(hashtext('audit_log')) → SELECT prev_hash → 计算 hash → INSERT 含 prev/entry → COMMIT`；`occurred_at` 截到 microsecond 与 PG timestamptz 字节对齐
- [x] `Repo.Verify(ctx, fromID)` + `GET /audit/verify`（admin-only）；流式遍历，skip pre-chain 行，逐行校验 prev_hash 指针 + 重算 entry_hash，首个不匹配返回 `first_broken_id + reason ∈ {prev_hash_mismatch, entry_hash_mismatch}`
- [x] dockertest：genesis row 全零 prev、顺序写 5 链接正确、并发 20×10 链不分叉、tampered metadata/prev_hash 都被检测、`fromID>0` suffix verify、pre-chain 跳过、空表 vacuous ok
- [x] httptest：admin 200 / member 403 / 无 token 401 / `from_id` 传透 / bad input 400 / repo 500
- [x] E2E 步骤 64：verify clean → SQL 篡改 metadata → verify failed at target id → 还原 → verify ok again；幂等
- [x] README + HANDOFF + SLICE-VERIFICATION 更新

**22a 不做：** Merkle tree / Notary anchoring、跨租户独立子链、哈希算法可配置、自动 verify 定时任务、链断点修复工具、KMS/HSM 签名（全部 v2+）

## 22b — Snapshot → MinIO ✅

> 落地：commits `950d6c4` → `ea7efd5` → `a533657` → (C4 docs)；E2E **[65/65]** PASS

- [x] migration `0022_sandbox_snapshots`：`sandbox_snapshots` 表 + FK `session_id REFERENCES sandbox_sessions(id) ON DELETE SET NULL` + 复合索引 `(tenant_id, created_at DESC)` + `(session_id)`
- [x] `internal/sandbox/snapshot_repo.go`：tenant-scoped Insert/Get/List/Delete；`ErrSnapshotNotFound` sentinel；dockertest 覆盖 tenant 隔离 + FK SET NULL 行为 + `session_id` 过滤
- [x] `internal/objstore/`（新包）：`minio-go/v7` 封装 `Client.{EnsureBucket,Put,Stat}`；`Put(reader, size=-1, PartSize=64MiB)` 启动 multipart 流式；`New` 校验 endpoint+bucket 非空
- [x] `internal/sandbox/docker_driver.go` Snapshot 真实实现：`ContainerCommit(Pause=true)` → `ImageSave` → `objstore.Put` 直传 → optional `ImageRemove` → `SnapshotRepo.Insert`；镜像 tag `pca-snapshot-<sessionID>:<unix_ts>`
- [x] `internal/sandbox/runtime.go` Snapshot 改造：释放 pgx 连接再做上传，最后重新 Acquire 写 DB；防止慢上传长时占用连接池
- [x] compose 加 `minio` service（pin `RELEASE.2025-04-08T15-41-24Z` + healthcheck + 命名卷 `miniodata` + 端口 9000/9001）；server `depends_on: minio: service_healthy`
- [x] `SnapshotConfig{Enabled,Endpoint,Bucket,AccessKey,SecretKey,Region,UseSSL,Prefix,KeepLocalImage}` + `PCA_SNAPSHOT_*` env + defaults + config_test
- [x] `cmd/server/main.go` 启动期 wiring：`if cfg.Snapshot.Enabled { objstore.New → EnsureBucket → SetSnapshotDeps → WithSnapshotRepo }`；disabled 三条路由统一 503 `snapshot_disabled`
- [x] `internal/sandbox/handler.go`：`POST /sandbox/sessions/:id/snapshot` 替换 not_implemented + `GET /sandbox/snapshots`（`?session_id=` `?limit=`）+ `GET /sandbox/snapshots/:id`；3 个 audit action `sandbox.snapshot.{create,list,get}` 全 `audit.Detached`
- [x] handler httptest：Create 201 DTO / Disabled 503 / NotReady 409 / NotFound 404 / ListDisabledNoRepo 503 / GetDisabledNoRepo 503 / ListNoAuth 401
- [x] E2E 步骤 65：create sandbox → write `/workspace/marker.txt` → POST snapshot 断言 object_key 前缀 + size>1000 + image_ref → list filtered by session_id → DELETE sandbox → GET snapshot 仍 200 且 `session_id=null` → audit `sandbox.snapshot.create` 含 target=SNAP_ID
- [x] README + HANDOFF + SLICE-VERIFICATION + 22 plan 更新

**22b 不做（留 22b-v2）：** restore-from-snapshot（基于快照新建沙箱）、`DELETE /sandbox/snapshots/:id` 路由（repo 已留 Delete 方法）、presigned URL 下载（走 MinIO console）、自动 GC / retention policy、多区域复制 / KMS 加密、异步 job 队列

## 22c — seccomp + trivy CI ✅

> 落地：commits `fe87ad0` → `5425039` → (C3 closeout)；E2E **[66/66]** PASS

- [x] `internal/sandbox/seccomp.json` 派生自 Docker default profile `moby/v25.0.5/profiles/seccomp/default.json`，沿用 `defaultAction: SCMP_ACT_ERRNO` + 显式 allowlist，**物理移除** allow 名单中 16 类约 40 个高危 syscall：文件系统命名空间逃逸（`mount/umount/umount2/pivot_root/name_to_handle_at/open_by_handle_at/mount_setattr/move_mount/open_tree/fsconfig/fsmount/fsopen/fspick`）、debugger / 进程内省（`ptrace/process_vm_readv/process_vm_writev/process_madvise/pidfd_getfd/kcmp`）、内核 keyring（`keyctl/add_key/request_key`）、内核扩展（`bpf/init_module/delete_module/finit_module/create_module`）、内核启动（`kexec_load/kexec_file_load`）、Dirty COW 原语（`userfaultfd`）、内核 perf（`perf_event_open`）、其他（`fanotify_init/lookup_dcookie/quotactl/quotactl_fd/setdomainname/sethostname/syslog/iopl/acct`）；保留 `setns/unshare/clone3` 满足现代 glibc / Node.js 启动需求
- [x] `internal/sandbox/seccomp.go`：`//go:embed seccomp.json` + `LoadSeccompProfile() (string, error)` 启动期一次性解析校验（`defaultAction == "SCMP_ACT_ERRNO"`、`syscalls` 数组非空），返回原始 JSON 字符串供 driver 直接拼 SecurityOpt
- [x] `internal/sandbox/docker_driver.go`：`DockerDriverConfig.SeccompProfile string` 字段；helper `securityOpts(profile)` 维持 `no-new-privileges:true` baseline，profile 非空时追加 `"seccomp="+profile`；空字符串等价禁用（不退化 Docker default）
- [x] `internal/config/config.go`：新增 `SandboxConfig{SeccompEnabled bool}` 顶层段（首次新增 `sandbox.*` 配置树）+ `v.SetDefault("sandbox.seccomp_enabled", true)` 在 `ReadInConfig` 前注册以让 viper.AutomaticEnv 绑定 `PCA_SANDBOX_SECCOMP_ENABLED`
- [x] `cmd/server/main.go` boot wiring：`if cfg.Sandbox.SeccompEnabled { seccompJSON, err := sandbox.LoadSeccompProfile(); slog.Info/Warn ... }` → 传入 `DockerDriverConfig.SeccompProfile`；profile 损坏只 warn 不阻 boot（降级 == 等价禁用）
- [x] L1 测试：`seccomp_test.go` TestLoadSeccompProfile_Parses + TestSeccompProfile_DeniesDangerousSyscalls（21 个高危 syscall 不在 allow 集 — drift detection）+ TestSeccompProfile_AllowsCommonSyscalls（26 个常用 syscall 在 allow 集 — over-trim detection）；config_test 覆盖默认 true + `PCA_SANDBOX_SECCOMP_ENABLED=false` env override
- [x] `.github/workflows/security.yml`（仓库首个 GitHub Actions workflow）：触发 PR + push to main 且 path filter `sandbox/image/**` / `.github/workflows/security.yml` / `.trivyignore`；流程 checkout → `docker build pca/sandbox:base ./sandbox/image` → `aquasecurity/trivy-action@master` CRITICAL（exit-code=1 阻塞 merge, `vuln-type: os,library`, `ignore-unfixed: true`）→ HIGH（`if: always()`, exit-code=0 仅 table）
- [x] `.trivyignore` placeholder 含使用说明（默认空，CVE 白名单仅在 HIGH→CRITICAL 升级且确认不可达时加）
- [x] `docs/SECURITY-SANDBOX.md` §1 适用范围澄清 22c 已交付 + §1 威胁模型表沙箱内逃逸防线补 seccomp + §2 SecurityOpt 行扩展 + §9 已知未做表 seccomp/trivy/audit hash chain 标 ✅ + §11 加 SecurityOpt inspect + docker exec mount 拒绝实证
- [x] E2E 步骤 66：(a) 建沙箱 → (b) exec `mount -t tmpfs none /tmp/seccomp-probe` 期望 `exit_code != 0` + stderr 含 EPERM 字样 → (c) 回归保护 `sh -c "echo ok > /workspace/seccomp-probe && cat"` 期望 exit=0 + stdout=`ok` → (d) destroy
- [x] HANDOFF + SLICE-VERIFICATION + 22 plan 更新

**22c 不做（留 22c-v2）：** profile runtime override（外挂文件路径 / per-tenant profile）、server 镜像 trivy 扫描（专注 sandbox base image）、AppArmor / SELinux profile（P2）、镜像 cosign 签名（22d 或 P2）、nightly trivy schedule（仅 PR + push to main 触发）

## 22d1 — K8sDriver Runtime + fake-client L1 ✅

> 落地：commits `1feb268` → `7981073` → (C3 closeout)；E2E **[67/67]** PASS

- [x] `internal/sandbox/k8s_driver.go` 实现 `sandbox.Runtime`（Pod = sandbox；Create/Get/Destroy + waitForPodReady 轮询 phase + ImagePullBackOff/ErrImagePull/CreateContainerError/PodFailed 早死 + timeout reaper Pods.Delete 回收）；`buildPod` 全字段对齐 DockerDriver 硬化矩阵（runAsUser/Group=10001 runAsNonRoot、readOnlyRootFilesystem、CapDrop ALL + 5 add、allowPrivilegeEscalation=false、SeccompProfile Localhost\|RuntimeDefault、emptyDir{medium:Memory,sizeLimit:1Gi} /workspace+/tmp、requests==limits → Guaranteed QoS、restartPolicy=Never、automountServiceAccountToken=false 默认）；Snapshot 在 tenant scope check 通过后直接返回 `ErrSnapshotDisabled`（K8sDriver.Snapshot 实现留 22d-v2 kaniko/K8s-native）；Destroy 复用 redis 锁 + lua release + DetachSession（K8sDriver 暴露 `SetSnapshotRepo` setter）
- [x] `internal/sandbox/k8s_driver_exec.go` SPDY exec 经 `k8sExecer` test seam（`newSPDYExecer` 真实实现 wraps `remotecommand.NewSPDYExecutor` + `StreamWithContext`；非零退出经 `utilexec.CodeExitError.ExitCode` 还原）+ `MaxStreamBytes` 截断
- [x] `internal/sandbox/k8s_driver_fs.go` tar-pipe ReadFile/WriteFile 走 SPDY exec（复用 `fs_common.go` 抽出的 `buildWriteTarStream` / `parseReadTarStream` / `stripWorkspacePrefix`）
- [x] `internal/sandbox/fs_common.go` 新增 — DockerDriver/K8sDriver 共享 tar helper；`docker_driver_fs.go` 改用 helper，行为零变化
- [x] `internal/sandbox/k8s_driver_test.go` 13 个 fake-clientset L1：pod 元数据 + securityContext 全字段 + seccomp 三态 + Guaranteed QoS canonical Quantity（`"2"` / `"1Gi"`）+ pod-ready timeout 回收 + tenant 隔离 + destroy 幂等 + Pods.Delete reactor 计数 + DetachSession 调用 + snapshot disabled + snapshot tenant scope 优先 + NetworkMode `internal`/`bridge`/`none` 三态 label + DNSPolicy `ClusterFirst`/`None` 切换 + exec stream signature 编译检查
- [x] `internal/config/config.go`：`SandboxConfig` 扩 `Driver string`（默认 `"docker"`） + `K8s SandboxK8sConfig{Namespace,InCluster,Kubeconfig,ServiceAccount,SeccompLocalhostProfile,PodReadyTimeoutSec}`；`applySlice22dDefaults` 在 `Load()` 末尾验证 `Driver in {"docker","k8s"}`，非法 fail-fast；viper.AutomaticEnv 绑定 `PCA_SANDBOX_DRIVER` / `PCA_SANDBOX_K8S_*`；config_test 覆盖默认/env/非法
- [x] `cmd/server/main.go` boot 期 switch：`docker` → 走 DockerDriver + Reconciler；`k8s` → `buildK8sRestConfig`（InCluster=true `rest.InClusterConfig` / false `clientcmd.NewDefaultClientConfigLoadingRules`+ ExplicitPath）→ `kubernetes.NewForConfig` → `NewK8sDriver`；Reconciler 在非 docker driver 下跳过；`SetSnapshotDeps` 改类型断言保护（fallback `SetSnapshotRepo`）；`httpx.Deps.Info` 注入 `{"sandbox":{"driver":...}}`
- [x] `internal/httpx/{server,health}.go`：`Deps.Info map[string]any` 新增 + `/healthz` body 合并；`server_test.go` 加 `TestHealthz_InfoMerged`
- [x] `config/config.example.yaml` 新增 `sandbox.driver` + `sandbox.k8s.*` 段附详细说明
- [x] `go.mod` 加 `k8s.io/{api,apimachinery,client-go} v0.32.0`（兼容 K8s 1.30/1.31/1.32）；`go mod tidy` 干净；vendored 增量 ~30MB go.sum / ~10MB binary
- [x] `docs/SECURITY-SANDBOX.md` §2.1 新增 K8s 部署等价表（Docker HostConfig → Pod spec 一一映射，含 PidsLimit 22d-v2 注 + NetworkMode 22d2 NetworkPolicy 注）
- [x] E2E 步骤 67：boot 后 `curl /healthz | jq -r '.sandbox.driver'` 必须 `=="docker"`（compose 默认）；K8s 真跑（in-cluster exec/files/destroy + NetworkPolicy 实证）留 22d2 kind nightly
- [x] HANDOFF + SLICE-VERIFICATION + 22 plan 更新

**22d1 不做（留 22d2 / 22d-v2）：** Helm chart（22d2）、kind nightly + DEPLOY-K8S.md（22d2）、NetworkPolicy YAML（22d2 chart）、K8sDriver.Snapshot 实现（22d-v2 kaniko / K8s-native）、PidsLimit Pod spec（22d-v2，K8s 1.31+ alpha gate）、RuntimeClass kata/gvisor 选择字段（22d-v2）、in-cluster watch 重连退避（22d-v2）、K8s 模式 reconciliation（22d-v2 用 informer/watch 替换 sandbox.RunReconciler）

## 22d2 — Helm chart + kind nightly + DEPLOY-K8S.md ✅

> 落地：commits `74781e7` → `48bf11c` → (C3 closeout)；compose `./test-e2e.sh` 1–67 不变；`.github/workflows/kind-nightly.yml` workflow_dispatch + 03:17 UTC schedule，`deploy/helm/pca/test/kind-e2e.sh` 6 步全 PASS

- [x] `deploy/helm/pca/Chart.yaml`（appVersion `22d2`，kubeVersion `>=1.30.0`）+ `values.yaml` 生产默认 + `values-kind.yaml` nightly overrides + `README.md` 一页速查
- [x] 13 个模板：`_helpers.tpl`（含 `pca.assertions` 渲染期硬拦截：jwtSecret ≥32 字符、namespace 一致性、sandbox.network ∈ internal|bridge|none、driver ∈ docker|k8s）+ `namespace.yaml`（rbac.createSandboxNamespace 时托管 sandbox ns）+ `serviceaccount.yaml` + `rbac.yaml`（Role scope 限定 `rbac.sandboxNamespace`，verbs 仅 `pods{create,get,list,delete}` + `pods/exec{create}` + `pods/log{get}`）+ `configmap.yaml`（1:1 镜射 config.example.yaml；故意省略 db.dsn/redis.addr）+ `secret.yaml`（gated by `not .Values.secrets.existing`）+ `service.yaml`（ClusterIP:8080）+ `deployment.yaml`（PCA_DB_DSN 走 Pod-spec env `$(PCA_DB_PASSWORD)` 展开 + PCA_REDIS_ADDR + PCA_AUTH_JWT_SECRET + securityContext runAsNonRoot=65532+readOnlyRootFilesystem+capDrop ALL+seccompProfile RuntimeDefault）+ `postgres.yaml`（StatefulSet + PVC，gated）+ `redis.yaml`（Deployment + optional PVC，gated）+ `networkpolicy-server.yaml`（出站 allowlist：DNS + kube-apiserver + chart PG/Redis + 公网由 allowExternalEgress 控制）+ `networkpolicy-sandbox-internal.yaml`（podSelector `pca.network=internal`，egress 仅 release ns server pod）+ `networkpolicy-sandbox-none.yaml`（deny-all）
- [x] `.github/workflows/kind-nightly.yml`：cron `17 3 * * *`（避 :00/:30 高峰）+ workflow_dispatch；步骤：checkout → setup-buildx → build server + sandbox images（gha cache）→ helm/kind-action@v1 kindest/node:v1.30.0 单节点 → `kind load docker-image` → azure/setup-helm@v4 v3.14.4 → 创 namespace + `helm install --wait --timeout 5m` → `kubectl wait deploy/pca-server` → 跑 `kind-e2e.sh` → failure 时 dump pods/logs/events/netpols
- [x] `deploy/helm/pca/test/kind-config.yaml` 单 control-plane 节点
- [x] `deploy/helm/pca/test/kind-e2e.sh`（+x，bash strict mode）6 步：(1) psql exec bootstrap demo user 到 PG StatefulSet —— 复用 compose e2e 的 bcrypt hash；(2) `kubectl port-forward svc/pca-server :18080` + /healthz 等就绪；(3) /auth/login → /sandbox/sessions 必返回 `status=running` 且 `kubectl -n pca-sandboxes get pods` 非空（实证 K8sDriver buildPod 落到正确 ns）；(4) PUT /files + POST /exec via SPDY 双向 round-trip `hello kind`；(5) NetworkPolicy=internal 实证：`curl https://1.1.1.1` 退出码必 != 0；(6) DELETE session → 再 exec 必 404
- [x] `docs/DEPLOY-K8S.md` 新增 10 段：部署形态选择（compose vs K8s 对照表，指向 SECURITY-SANDBOX §3 docker.sock 妥协对照）+ 前置条件 + 镜像准备 + 快速开始 + values 字段速查 + 生产 checklist（sealed-secrets / digest / external PG/Redis / seccompLocalhostProfile / OIDC）+ 升级（rolling，沙箱 Pod 不受影响）+ 回滚（DB schema forward-only 警告）+ Troubleshooting + 本地 kind 实验
- [x] `docs/DEPLOY.md` §1 形态表加 "K8s / Helm（22d2 ✅）" 行 + 顶部范围声明指向 DEPLOY-K8S
- [x] `docs/SECURITY-SANDBOX.md` §3.1 新增 K8sDriver + chart RBAC 已替换 docker.sock 妥协路径的注，列出 server SA scope 限制 + Pod securityContext + NetworkPolicy 实证手段
- [x] HANDOFF + SLICE-VERIFICATION + 本 plan 更新；compose `./test-e2e.sh` 1–67 不变（回归保护）

**22d2 不做（留 22d-v2 / 22e）：** ServiceMonitor + PrometheusRule（chart 留 stub）、HPA / PodDisruptionBudget、Ingress / TLS（chart 仅 ClusterIP）、K8sDriver.Snapshot + MinIO chart（22d-v2 kaniko-based）、multi-arch image build、helm chart cosign sign / OCI registry push（22e）、kind 上跑全量 67 步 e2e（需 mock-provider/mock-oidc/mock-mcp/minio 全 chart 化，超 22d2 体量）

---

**非目标（22 全段）：** 多区域 HA（P2+）；Merkle tree / 外部 Notary（22a v2+）
