# Private Coding Agent

私有化部署的 AI 编码 Agent 平台。

## 切片进度

- [x] **切片 1：Foundation**（本文档）
- [ ] 切片 2：Sandbox
- [ ] 切片 3：Model Gateway
- [ ] 切片 4：Tool Bus + Internal MCP
- [ ] 切片 5：Agent Engine
- [ ] 切片 6：Session API + WebSocket
- [ ] 切片 7：Memory (basic)
- [ ] 切片 8：Web Frontend
- [ ] 切片 9：Integration & Audit

## 本地开发

```powershell
# 单元 + 集成测试 (会拉 postgres 镜像)
go test ./...

# 本地直接跑
copy config\config.example.yaml config\config.yaml
go run ./cmd/server --config config\config.yaml
```

## docker-compose 启动

```powershell
cd deploy\compose
copy .env.example .env
docker compose up -d --build
curl http://localhost:8080/healthz
```

## 关键端点

| 方法 | 路径 | 鉴权 | 说明 |
|---|---|---|---|
| GET | /healthz | - | 健康检查 |
| GET | /readyz | - | 就绪检查 |
| POST | /auth/login | - | 登录拿 JWT |
| GET | /me | Bearer | 当前用户身份 |

## 配置

见 `config/config.example.yaml`。所有字段可用 `PCA_<UPPER>_<UPPER>` 环境变量覆盖（点号换下划线）。
