# 路线 1：MCP 工具开发路线图

> **上级方案**：[`MCP-WORKFLOW-PLATFORM-PLAN.md`](MCP-WORKFLOW-PLATFORM-PLAN.md)  
> **平台能力**：Slice 21b 外部 MCP Manager · [`CONNECTORS.md`](CONNECTORS.md)  
> **参考实现**：`internal/mcp/mockserver/`（单 tool 示例，试点需扩展为多 tool）

---

## 1. 目标

为工作流与 Agent 提供 **稳定、可发现、可审计** 的企业能力节点：

- **一个业务域 = 一个 MCP 服务（一个 Docker 部署）= 一个 PCA `slug`**
- **域内多个 tool**，注册为 `mcp.<slug>.<tool_name>`
- 工作流 **只编排**，不实现业务 API

---

## 2. 设计原则

| 原则 | 说明 |
|------|------|
| **域边界清晰** | `orders`、`notify`、`ticket` 等；避免 `orders-list`、`orders-upsert` 各起一个 MCP |
| **动词命名** | `list_pending`、`get_detail`、`upsert`、`close` |
| **Schema 优先** | `tools/list` 的 `description` + `inputSchema` 即 NL 的「API 文档」 |
| **读写分离标注** | 只读：`destructiveHint: false`；写入/外发：`true`（影响 Dry-Run） |
| **幂等与错误** | 写操作尽量幂等键；错误返回 MCP `isError` + 可读文本 |
| **租户隔离** | 服务内按 tenant 鉴权，或每租户独立 `base_url` + PCA 各行 |

---

## 3. 技术栈与协议

| 项 | 要求 |
|----|------|
| 协议 | MCP **2024-11-05**，JSON-RPC over **HTTP POST**（与 PCA Client 一致） |
| 方法 | `initialize`、`tools/list`、`tools/call` |
| 健康检查 | 建议 `GET /healthz`（compose 侧车惯例） |
| 语言 | Go / TypeScript 均可；团队统一即可 |
| 部署 | 容器 + 内网 Registry；**不**依赖 Agent 沙箱 `docker.sock` |

PCA 注册后工具名规则（`internal/mcp/mcp_tool.go`）：

```text
mcp.<server_slug>.<tool_name_from_tools_list>
```

---

## 4. 仓库与目录结构（建议）

在 monorepo 外或 `examples/mcp-services/` 下按域建目录：

```text
mcp-services/
  orders/
    README.md           # 域说明、tool 清单、鉴权
    cmd/server/main.go  # 或 src/index.ts
    Dockerfile
    openapi.yaml        # 可选：内部 REST 映射文档
  notify/
    ...
```

**不**把 MCP 业务逻辑放进 `internal/toolbus`（保持 Bus 只做调度）。

---

## 5. Tool 清单模板（每域必填）

复制下表作为 `mcp-services/<domain>/TOOLS.md`：

| tool_name | 说明 | 读/写 | 典型 input | 被哪些 workflow 引用 |
|-----------|------|-------|------------|---------------------|
| `list_pending` | 列出待处理项 | 读 | `status`, `limit` | `order-sync-app` |
| `upsert` | 写入 ERP | 写 | `items[]` | `order-sync-app` |
| … | | | | |

**试点规模**：首域 **8～12** 个 tool 覆盖该域 80% 自动化步骤即可。

---

## 6. 开发 → 上线 SOP

### 6.1 本地开发

1. 实现 `tools/list` / `tools/call`（可参考 `internal/mcp/mockserver/main.go` 扩展多 tool）。
2. `curl` 自测 JSON-RPC。
3. `docker build -t <registry>/mcp-<slug>:<ver> .`

### 6.2 部署（Compose / K8s）

| 环境 | 做法 |
|------|------|
| Compose | 参考 `deploy/compose/examples/slack-mcp.production.yaml` 增加 sidecar Service |
| K8s | Deployment + Service；NetworkPolicy 仅允许 PCA server 访问 |

### 6.3 PCA 注册

1. **连接器** `/admin/connectors` 或 **MCP 服务** `/admin/mcp-servers`：
   - `slug`: 与域一致（如 `orders`）
   - `url`: `http://mcp-orders:808x/`
   - `auth`: bearer / none（按环境）
2. **Test connection** → **Refresh tools**
3. 确认 `GET /tools`（admin）或 Agent 可见 `mcp.orders.*`

### 6.4 发版

| 步骤 | 动作 |
|------|------|
| 1 | 部署新镜像 |
| 2 | PCA Refresh（新增 tool 必须） |
| 3 | 更新 `TOOLS.md` 与工作流域对照表 |
| 4 | 通知路线 2 维护者：是否有 DSL 需改 `notify_args` 等 |

**Breaking change**：改必填字段前，先加新 tool 或兼容旧字段一版，再弃用。

---

## 7. 试点域（已选首域 + 后续业务域）

### 7.1 P1 首域：`data-prep`（公共能力 · 文件 → **AI** 降噪）✅

| 决策 | 内容 |
|------|------|
| 数据来源 | **文件**：`/data/inbox` 挂载卷投递（JSON/JSONL/CSV） |
| 清洗方式 | **AI（LLM）**：在 **MCP 服务内**调用模型做语义降噪（非规则引擎主路径） |
| 详细设计 | [`domains/data-prep.md`](domains/data-prep.md) |

| tool | 用途 |
|------|------|
| `load_file` | 从 inbox 读文件 → `records` |
| `ai_denoise_records` | **核心**：records + 自然语言指令 → LLM → 清洗后 records + changelog |
| `ai_denoise_file` | 可选便捷：path + instructions 一步 AI 清洗 |
| `summarize_run` | 清洗前后对比摘要（审批/可观测） |
| `write_file` | 写入 outbox（`destructiveHint: true`） |

工作流：`data-prep-pipeline`（load → **ai_denoise** → write）；`cleaning_instructions` 可由 NL 写入 inputs。P2 业务流此前置本域。

### 7.2 P2 暂缓：`ops-alert`（偏运维）

告警、指标、通知；可消费 `data-prep` 产出的干净文件。

### 7.3 P2 暂缓：`orders`（偏业务）

订单同步；管道：`data-prep` → `mcp.orders.*`。

---

## 8. P0：扩展 mock-mcp（1 周内）✅

在验证路线 2 之前，先扩展现有 mock（`internal/mcp/mockserver` + `deploy/compose/examples/e2e-mock-chain.yaml`）：

| tool | destructiveHint | 行为 |
|------|-----------------|------|
| `echo` | false | 已有 |
| `fetch_status` | false | 返回固定 JSON 状态 |
| `record_event` | true | 模拟写入，返回 `recorded: true` |

注册 `slug=e2e-mock` 后，路线 2 用 `tool-chain` 模板串联三个 `mcp.e2e-mock.*`。

---

## 9. 与路线 2 的交接物

每域 MCP 上线时，交付：

1. **Tool 清单**（上表）+ 示例 `tools/call` JSON  
2. **推荐 workflow 草图**（步骤级，非最终 DSL）  
3. **模板 slot 默认值**（`notify_tool`、`forward_tool` 等应填的 `mcp.<slug>.<tool>`）  

路线 2 据此做 NL 模板 / Skill，**不**反向要求平台改 Bus 命名。

---

## 10. 质量门禁

| 门禁 | 标准 |
|------|------|
| L1 | 单测 / 契约测试：`tools/list` 稳定 |
| L2 | `docker build` + 健康检查 |
| L3 | PCA 注册后 E2E：`mcp.<slug>.<tool>` invoke 成功 |
| 安全 | 凭证不进 DSL；仅 server 配置 / K8s Secret |
| 文档 | `TOOLS.md` + CONNECTORS 段落链接 |

---

## 11. 后续切片（可选产品化）

| ID | 内容 | 优先级 |
|----|------|--------|
| M1 | `examples/mcp-services/` 脚手架（Go MCP 模板） | P1 |
| M2 | CI：tag → build → deploy → Refresh webhook | P2 |
| M3 | 工具目录 Web UI（只读 catalog） | P3 |
| M4 | MCP 版本与 workflow 兼容性检查 | P3 |

---

## 12. 变更日志

| 日期 | 说明 |
|------|------|
| 2026-05-24 | 初版 |
| 2026-05-24 | P1 首域定为 `data-prep`（文件入参 + MCP 内清洗） |
| 2026-05-24 | 修正：`data-prep` 清洗主路径为 MCP 内 **AI/LLM**，非规则引擎 |
| 2026-05-25 | P0 完成：mock-mcp 三工具 + `e2e-mock-chain` 工作流示例 |
