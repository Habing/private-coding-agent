# Slice 22 — K8s + Production Security Implementation Plan

> **Goal:** K8sDriver、Helm、seccomp、trivy CI、Snapshot/MinIO；E2E **64+**。

**Design:** Full P1 spec §22 + HANDOFF 技术债

**Depends on:** Slice 14、MVP-P1

---

## Outline

- [ ] `internal/sandbox/k8s_driver.go` 实现 `Runtime`
- [ ] `deploy/helm/pca` chart
- [ ] seccomp default profile + 文档
- [ ] CI：`trivy image pca/sandbox:base`
- [ ] Snapshot API 实现（MinIO SDK）
- [ ] audit hash chain（可选本切片）
- [ ] kind 集成测试或 nightly workflow
- [ ] `docs/DEPLOY-K8S.md`

**非目标：** 多区域 HA（P2+）
