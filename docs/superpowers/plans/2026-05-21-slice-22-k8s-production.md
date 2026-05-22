# Slice 22 — K8s + Production Security Implementation Plan

> **Goal:** audit hash chain、Snapshot/MinIO、seccomp、trivy CI、K8sDriver、Helm；E2E 64+。
>
> **拆分:** 22 体量过大，按 19/21 模式拆为四段顺序落地：
> - **22a — Audit Hash Chain** ✅ 已落地（HEAD `7968a77`，E2E 步骤 64）
> - **22b — Snapshot → MinIO**（pending）
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

## 22b — Snapshot → MinIO（pending）

- [ ] compose 加 `minio` service + MinIO SDK 依赖
- [ ] `DockerDriver.Snapshot(sessionID)`：commit 容器 → push 到 MinIO 对象存储 → 元数据写 `sandbox_snapshots` 表
- [ ] `POST /sandbox/sessions/{id}/snapshot` + `GET /sandbox/snapshots`
- [ ] E2E 步骤 65：create sandbox → snapshot → 列表里看到 → 删 sandbox 后 snapshot 仍可用

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
