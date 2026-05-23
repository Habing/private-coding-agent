# K8s / Helm 部署 Runbook

> 适用范围：Full-P1 切片 22d2。Helm chart 路径 `deploy/helm/pca`，appVersion `22d2`。compose 形态见 [`DEPLOY.md`](DEPLOY.md)。

切片 22d1 把 `K8sDriver` 写进了 server 二进制（pod-per-sandbox + SPDY exec）；22d2 把它打成 Helm chart 让真集群能跑起来。本文从"为什么要选 K8s 形态"讲到"升级 / 回滚 / 排错"。

---

## 1. 选 K8s 还是 compose

| 维度 | compose | K8s / Helm |
|------|---------|------------|
| Sandbox 隔离 | `--privileged=false` + seccomp + cap-drop **但** 挂 `docker.sock` 给 server——server RCE 可逃逸 | 沙箱是 Pod；server 通过 SA + RBAC 调 kube-apiserver，**无** docker.sock。**replace了 docker.sock 妥协**——见 [`SECURITY-SANDBOX.md`](SECURITY-SANDBOX.md#3-沙箱实现) §3 |
| HA / 多节点 | 单机 | 原生 |
| NetworkPolicy | 不支持 | `pca.network=internal/bridge/none` Pod-label 自动应用 |
| 升级 | `docker compose pull && up -d` 单点中断 | `helm upgrade` 滚动 |
| 运维门槛 | 低 | 需要 K8s 1.30+、Helm 3.13+、RWO StorageClass |
| 适用 | ≤ 50 并发、单机内网 | 生产试点；HA；多租户多 namespace |

**经验线**：5 个并发用户以下 compose 没问题；超过就该切 K8s。

---

## 2. 前置条件

- Kubernetes ≥ 1.30（kindnetd / Calico / Cilium 都验过 NP v1）
- Helm ≥ 3.13
- RWO StorageClass（若启 chart 自带 PG/Redis）
- 镜像 `pca-server:<tag>` + `pca/sandbox:base` 已在每个 Node 拉到（私有 registry pullSecret，或 kind/k3s 的 `kind load docker-image`）
- 可选：cert-manager + Ingress controller，用于把 server 暴露到集群外（chart 只暴露 ClusterIP）
- 可选：sealed-secrets / external-secrets，用于管 jwt/db 密钥

集群侧 K8sDriver 不依赖的：CSI 快照、Prometheus operator、Service mesh。

---

## 3. 镜像准备

```bash
# server 二进制
docker build -t <your-registry>/pca-server:0.22.2 .

# sandbox 镜像（debian:12-slim + non-root 1000:1000）
docker build -t <your-registry>/pca/sandbox:base ./sandbox/image
```

push 后用 `<image>@sha256:...` digest 锁定，而非 `:latest`。原因：每个 sandbox Pod 都 pull 一次，registry 抖动 / image-rewrite 会让"5 月跑过的回归"和"6 月线上行为"不一致。

> 切片 22 后续会上 cosign + SLSA provenance；22d2 不强制。

---

## 4. 快速开始（minimal install）

```bash
# 1. 两个 namespace。release ns 由 helm install 决定；sandbox ns 由 chart 创建（rbac.createSandboxNamespace=true）
kubectl create namespace pca-system

# 2. 随机 JWT 密钥（生产请用 sealed-secrets）
JWT=$(openssl rand -base64 48 | tr -d '=+/' | head -c 48)

# 3. 安装
helm install pca ./deploy/helm/pca \
  --namespace pca-system \
  --set secrets.jwtSecret="$JWT" \
  --set image.repository=<your-registry>/pca-server \
  --set image.tag=0.22.2 \
  --set sandbox.image=<your-registry>/pca/sandbox:base \
  --wait --timeout 5m

# 4. 验证
kubectl -n pca-system wait --for=condition=available --timeout=180s deploy/pca-server
kubectl -n pca-system port-forward svc/pca-server 8080:8080 &
curl -s http://localhost:8080/healthz | jq .
# 期望：{"status":"ok","sandbox":{"driver":"k8s",...}}
```

`/healthz` 的 `sandbox.driver` 字段是切片 22d1 加的——若它显示 `docker` 说明 ConfigMap 没渲染到 Pod，先排 `kubectl -n pca-system describe deploy/pca-server`。

---

## 5. values.yaml 关键字段速查

| 字段 | 默认 | 含义 |
|------|------|------|
| `image.repository` / `image.tag` | `pca-server` / Chart.AppVersion | server 镜像；生产用 digest |
| `secrets.jwtSecret` | `""` | **必填，>=32 字符**；chart 渲染时 fail |
| `secrets.existing` | `""` | 非空时引用预存 Secret，跳过 chart-managed Secret（推荐用 sealed-secrets） |
| `postgres.enabled` | `true` | 启 chart 自带 PG（StatefulSet+PVC）；生产建议改 `false` + `postgres.externalDsn` |
| `redis.enabled` | `true` | 启 chart 自带 Redis；生产推荐外部 |
| `sandbox.image` | `pca/sandbox:base` | 沙箱镜像；务必改私有 registry digest |
| `sandbox.network` | `internal` | `internal\|bridge\|none`；K8sDriver 给 Pod 打 `pca.network=<v>` label，对应 NP 模板生效 |
| `networkPolicy.enabled` | `true` | 总开关；`false` → server NP + sandbox NP 都不渲染 |
| `networkPolicy.allowExternalEgress` | `true` | server pod 是否能出公网（LLM provider / OIDC）；air-gap 改 `false` |
| `rbac.sandboxNamespace` | `pca-sandboxes` | sandbox Pod 落的 ns；**必须**等于 `config.sandbox.k8s.namespace`（chart 在 _helpers.tpl 里 assert） |
| `config.sandbox.k8s.seccompLocalhostProfile` | `""` | 留空 → RuntimeDefault；非空 → Localhost（需提前 push 到所有 node `/var/lib/kubelet/seccomp/`） |
| `config.sandbox.k8s.podReadyTimeoutSec` | `60` | K8sDriver Create 等 Pod Ready 上限；NodePool 拉新镜像慢时拉大 |

完整字段列表见 `deploy/helm/pca/values.yaml`。

---

## 6. 生产 checklist

- [ ] `secrets.existing` 指向 sealed-secrets / external-secrets 管理的 Secret，**不要** 把 jwtSecret/dbPassword 写在 chart values 里
- [ ] `image.tag` 改成 `@sha256:...` digest
- [ ] `postgres.enabled=false` + `postgres.externalDsn` 指向托管 PG（开了 `pgvector` 扩展）
- [ ] `redis.enabled=false` + `redis.externalAddr` 指向托管 Redis
- [ ] `networkPolicy.enabled=true` + `sandbox.network=internal`（默认）
- [ ] `sandbox.image` 改私有 registry digest；该镜像经 trivy 扫过（参考 [`SECURITY-SANDBOX.md`](SECURITY-SANDBOX.md) §6）
- [ ] `config.sandbox.k8s.seccompLocalhostProfile` 部署到每台 node 后填路径
- [ ] `resources.limits` 按实际负载调（默认 2cpu/1Gi 对 ≤ 10 并发 OK）
- [ ] 部署前 `pg_dump`，记录回滚点（migration forward-only，见 §8）
- [ ] OIDC：`config.auth.localEnabled=false` + 完整 oidc 块；`OIDC_CLIENT_SECRET` 走 secrets（见 [`DEPLOY.md`](DEPLOY.md#31-认证)）
- [ ] 监控：当前 chart **不** 集成 ServiceMonitor（22d-v2 上）；如已有 prom-operator，可手动写 ServiceMonitor 对接 `/metrics`（需 `config.observability.metricsToken`）

---

## 7. 升级

```bash
helm upgrade pca ./deploy/helm/pca \
  --namespace pca-system \
  -f values-prod.yaml \
  --wait --timeout 10m
```

- migrations 是 forward-only：server 启动跑 `internal/db/migrations` 目录里的新文件——只往下不往回
- 升级期 server Pod 滚动重启；**已创建的沙箱 Pod 不受影响**（K8sDriver 与 server 进程是 detached——SPDY exec stream 会断，下次 invoke 重新打开）
- 升级前确保 `helm upgrade --dry-run --debug` 渲染无误
- ConfigMap 变更触发 `checksum/config` 注解变化 → 自动滚动

---

## 8. 回滚

```bash
helm rollback pca <revision> -n pca-system
```

**铁律**：DB schema 不可回滚。`helm rollback` 只能把 server 二进制回滚到上个 image tag——**只有在新版 migration 还没跑过、或新 migration 与旧版兼容**时安全。

不安全的回滚 = 旧 server 二进制读不懂新 schema → 报错重启循环。处理方式：
1. 先 `pg_dump` 当前库
2. `helm rollback`
3. 若旧 server 启动失败，恢复 `pg_dump` 备份；再 `helm upgrade` 回新版查根因

---

## 9. Troubleshooting

| 症状 | 排查 |
|------|------|
| `Pod ImagePullBackOff` | `kubectl describe pod`；先确认 `image.tag` 在 registry 存在；privateregistry 缺 `image.pullSecrets` |
| server `CrashLoopBackOff`，日志含 `jwt_secret too short` | `secrets.jwtSecret` 漏了或 <32 字符；用 `kubectl -n pca-system get secret pca-secret -o yaml` 看实际值 |
| server 启动报 `config.sandbox.k8s.namespace=... != rbac.sandboxNamespace=...` | helm 渲染期被 `_helpers.tpl` assert 拦下；改两处一致 |
| `POST /sandbox/sessions` 报 `forbidden`，server 日志 `pods is forbidden` | RBAC Role/RoleBinding 没建好；`kubectl -n pca-sandboxes get role,rolebinding`；`rbac.create=true` 是不是被你关了 |
| sandbox Pod 卡 `Pending` | `kubectl -n pca-sandboxes describe pod <name>`；常见原因：node 没镜像 / NetworkPolicy 把 image pull egress 切了 / Resource quota 满 |
| sandbox 能 curl 外网（NetworkPolicy=internal 失效） | 检查 CNI 是否支持 NP v1（kindnetd / Calico / Cilium ✅；flannel ❌）；`kubectl -n pca-sandboxes get netpol` 看到 internal NP 存在 |
| 沙箱里 SPDY exec 立刻断开 | `pods/exec` verb 是否在 Role 里；`kubectl auth can-i create pods/exec -n pca-sandboxes --as=system:serviceaccount:pca-system:pca-server` 应当返回 `yes` |
| `helm upgrade` 卡在 `Waiting for...` | server readinessProbe `/readyz` 失败；`kubectl logs` 看 PG 连接、migration 进度 |

---

## 10. 本地实验 — kind

```bash
# 创集群（kindest/node:v1.30.0，单节点，kindnetd CNI 支持 NetworkPolicy）
kind create cluster --name pca-local --config deploy/helm/pca/test/kind-config.yaml

# build + load 镜像
docker build -t pca-server:kind-latest .
docker build -t pca/sandbox:base ./sandbox/image
kind load docker-image pca-server:kind-latest --name pca-local
kind load docker-image pca/sandbox:base       --name pca-local

# install
kubectl create namespace pca-system
helm install pca ./deploy/helm/pca \
  -n pca-system \
  -f ./deploy/helm/pca/values-kind.yaml \
  --wait --timeout 5m

# 验证
kubectl -n pca-system wait --for=condition=available --timeout=180s deploy/pca-server
./deploy/helm/pca/test/kind-e2e.sh
# 期望：kind 1–6 全 PASS，含 NetworkPolicy=internal 实证

# 清理
kind delete cluster --name pca-local
```

CI 上的 `.github/workflows/kind-nightly.yml` 每天 03:17 UTC 跑同一脚本；手动触发 `gh workflow run kind-nightly.yml` 也行。

Compose 全量 **78 步** E2E（docker driver）见 `.github/workflows/compose-e2e.yml`（PR/push main + 每日 04:30 UTC）。

Helm `values.yaml` 已与 compose `config.example.yaml` 对齐 **workflow trigger** 与 **orchestrator `nl-workflow-author`** 规则（configmap 渲染）。

`values-kind.yaml` 关键差异：`image.pullPolicy=Never`（依赖 kind load）、storageClassName=`standard`（kind 自带）、resources 缩小、log_level=debug、`secrets.jwtSecret` 是公开测试值（**禁止生产用**）。

---

## 未列入 22d2 的（22d-v2/22e 候选）

- ServiceMonitor + PrometheusRule（chart 留 `serviceMonitor.enabled` stub）
- HPA + PodDisruptionBudget（server 单实例 MVP）
- Ingress / TLS termination（chart 只暴露 ClusterIP）
- `K8sDriver.Snapshot` 实现 + MinIO 部署
- Multi-arch image build
- chart cosign sign + helm OCI registry push
- 全量 67 步 e2e 在 kind 上跑（需 mock-provider/oidc/mcp/minio 全 chart 化）
