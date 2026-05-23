# Slice 22 — K8s + Production Security Implementation Plan

> **Goal:** audit hash chain、Snapshot/MinIO、seccomp、trivy CI、K8sDriver、Helm；E2E 65+。
>
> **拆分:** 22 体量过大，按 19/21 模式拆为四段顺序落地：
> - **22a — Audit Hash Chain** ✅ 已落地（HEAD `7968a77`，E2E 步骤 64）
> - **22b — Snapshot → MinIO** ✅ 已落地（HEAD `a533657`，E2E 步骤 65）
> - **22c — seccomp + trivy CI**（pending）
> - **22d — K8sDriver + Helm chart**（pending）

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

## 22c — seccomp + trivy CI（pending）

- [ ] `sandbox/image/seccomp.json` 默认 profile（拒绝 mount/ptrace/keyctl 等）
- [ ] DockerDriver 加 `SecurityOpt: ["seccomp=…"]`
- [ ] `.github/workflows/security.yml`：`trivy image pca/sandbox:base`，CRITICAL/HIGH 阈值
- [ ] `docs/SECURITY.md` 列 seccomp 行为
- [ ] E2E 步骤 66（可选）：exec `mount` 应被拒绝

## 22d — K8sDriver + Helm（pending）

- [ ] `internal/sandbox/k8s_driver.go` 实现 `Runtime`（Pod = sandbox）
- [ ] `deploy/helm/pca` chart：Deployment + Service + ConfigMap + Secret
- [ ] kind nightly workflow（拉起单节点 → 跑 e2e 子集）
- [ ] `docs/DEPLOY-K8S.md`

---

**非目标（22 全段）：** 多区域 HA（P2+）；Merkle tree / 外部 Notary（22a v2+）
