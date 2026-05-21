# P1 Full — 全量企业可用设计 Spec

> Status: Approved — 在 MVP-P1 完成后执行（2026-05-21）  
> Related: 主 spec §11、[`2026-05-21-p1-mvp-enterprise-design.md`](2026-05-21-p1-mvp-enterprise-design.md)、[`docs/P1-ROADMAP.md`](../../P1-ROADMAP.md)

## 1. 目标

在 **MVP-P1（Slice 13～17）** 完成后，兑现主设计 **§11 P1 — 企业可用** 的剩余能力：

| 主 spec §11 项 | Full P1 切片 |
|----------------|--------------|
| NL → Workflow + `workflow.*` | 19（可拆 19a/19b） |
| 多 Sub-Agent + `agent.delegate` + 编排路由 | 18 + 21 |
| Reflection + 记忆合并 + （Memory UI 已在 16） | 20 |
| N8N（可选） | 23 |
| K8s Helm + K8sDriver | 22 |
| 完整审计与 OTel | 22 + 各切片增量 |

**MVP 已覆盖**：多租户 schema 运营化、OIDC、配额、session-sandbox、Skills 12b、Memory 注入/UI。

---

## 2. 前置条件

- MVP-P1 完成检查表全部勾选（E2E 55）
- 无 P0 回归：E2E 1～55 仍 PASS

---

## 3. 切片摘要

### Slice 18 — Sub-Agents + `agent.delegate`

- Profile 注册表：`review`、`research`、`workflow-authoring`（最小 2 个）
- 工具 `agent.delegate`：`{profile, task, sandbox_id?, max_steps?}`
- 子 Run 独立上下文；结果 `role=tool` 回灌
- Session/Web：`profile` 字段可切换
- **E2E 56**：delegate 一轮成功

### Slice 19 — Workflow Engine

- 表 `workflows`、`workflow_versions`、`workflow_runs`
- DSL：YAML + JSON Schema；节点 `tool`、`if`、`loop`、`parallel`、`assign`、`wait`（P0 子集）
- 发布 → `workflow.<id>` 注册 Tool Bus
- Authoring Agent profile + 确认 UI（可 19b）
- Dry-Run：mutating 工具 mock
- **E2E 57～60**：保存 → 发布 → invoke → 审计

### Slice 20 — Reflection Agent

- 异步 worker（Redis stream 或 PG outbox）
- `Reflect(sessionId) -> []MemoryProposal`
- Admin 审核 API + UI；默认进审核队列
- 冲突合并：同 tag/type 去重、confidence 升权
- **E2E 61**：approve proposal → search 命中

### Slice 21 — Orchestration Router + External MCP

- Router：显式策略选择 Tool / Workflow / Skill / Delegate（可配置规则 + 未来 ML）
- External MCP：`mcp_servers` 表、注册、心跳、`ListTools` 租户过滤
- 可选内置 `http.get` / `git.clone`（按 spec 优先级）
- **E2E 62～63**

### Slice 22 — K8s + 生产安全

- `K8sDriver` 实现 `sandbox.Runtime`
- Helm chart：`server`、`postgres`、`redis`、`sandbox` RBAC
- seccomp profile、沙箱镜像 trivy CI
- Snapshot → MinIO；长会话恢复 API
- audit hash chain（若未在 13 做）
- **E2E 64+**：kind 烟测或 nightly job

### Slice 23 — N8N（可选）

- `internal/n8nadapter`：发现 workflow → `n8n.<name>`
- 自定义节点包文档；每租户实例部署指南
- 工作流市场 `source: n8n | ai-dsl`
- **法务**：Sustainable Use License 合规确认后再开发

---

## 4. Full P1 完成检查表

- [ ] Slice 18～22 完成（23 标记 skipped 或 done）
- [ ] E2E **≥70** 步 PASS
- [ ] 主 spec §1.2 能力表 Workflows / Sub-Agents 为 ✅
- [ ] ADR：HITL 节点 P1 vs P2 有结论
- [ ] Helm 文档 + 生产 runbook

---

## 5. 与 P2 边界

以下 **不在 Full P1**，见主 spec §11 P2：

- Tenant Memory + 跨项目共享审批
- N8N 画布 AI 协助创建
- 记忆质量看板
- Webhook/Event 触发器系统
- 自研工作流可视化编辑器（N8N 替代）

---

## 6. Plan 索引

见 [`docs/P1-ROADMAP.md`](../../P1-ROADMAP.md) Full P1 表。
