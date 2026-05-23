# Slice 19c — Template Market + Version Diff

> **状态**：✅ 已交付（2026-05-24）  
> **依赖**：19b NL 模板 catalog、19d 只读流程图

## Delivered

| 组件 | 说明 |
|------|------|
| `POST /agent/workflow/templates/:id/preview` | 渲染模板 DSL（空 slots 时用 `ExampleSlots`） |
| `GET /admin/workflow/proposals` | Admin 列表（status/limit/offset） |
| `/workflows` 模板市场 | `WorkflowTemplateMarket`：浏览 5 内置模板、填 slot、图预览、一键创建 |
| `/workflows` 版本 diff | `YamlDiffPanel`：编辑 DSL 时相对已保存版本行级 diff |
| `/admin/workflow-proposals` | 待审批/草案/已发布/已拒绝；批准/驳回 + 迷你流程图 |

## 验收

- [x] `go test ./internal/workflow/... -count=1`
- [x] `cd internal/webui && npm test && npm run build`
- [x] 无新 E2E 步号（L1/L2 + 手工冒烟；compose 仍 78/78）

## 非目标（仍出栈）

- 外部模板 registry / Git 安装
- Workflow 历史表 diff（仍靠单行 `version` + audit）
- 模板 classify embedding（P2）

## 参考

- [`docs/WORKFLOW.md`](../../WORKFLOW.md) §8.6
- [`docs/SLICE-VERIFICATION.md`](../../SLICE-VERIFICATION.md) §19c
