import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { useState } from 'react'

import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ApiError, api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth'
import type {
  ConnectorCatalogResponse,
  ConnectorRecipeStatus,
  CreateMcpServerRequest,
  McpServer,
} from '@/types/api'

export function Connectors() {
  const token = useAuthStore((s) => s.token)
  const qc = useQueryClient()
  const [error, setError] = useState<string | null>(null)
  const [installId, setInstallId] = useState<string | null>(null)
  const [installURL, setInstallURL] = useState('')
  const [installToken, setInstallToken] = useState('')

  const catalogQ = useQuery({
    queryKey: ['connector-catalog'],
    queryFn: () => api<ConnectorCatalogResponse>('/admin/connectors/catalog', { token }),
    enabled: !!token,
  })

  const installMut = useMutation({
    mutationFn: (body: CreateMcpServerRequest) =>
      api<McpServer>('/admin/mcp-servers', {
        method: 'POST',
        token,
        body: JSON.stringify(body),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['connector-catalog'] })
      qc.invalidateQueries({ queryKey: ['mcp-servers'] })
      qc.invalidateQueries({ queryKey: ['tools'] })
      setInstallId(null)
      setInstallURL('')
      setInstallToken('')
      setError(null)
    },
    onError: (e) => setError(humanError(e)),
  })

  function openInstall(rec: ConnectorRecipeStatus) {
    if (rec.kind !== 'mcp') return
    setInstallId(rec.id)
    setInstallURL(rec.setup_url_hint ?? '')
    setInstallToken('')
    setError(null)
  }

  function submitInstall(rec: ConnectorRecipeStatus) {
    const url = installURL.trim()
    if (!url || !rec.mcp_slug) return
    const body: CreateMcpServerRequest = {
      slug: rec.mcp_slug,
      name: rec.name,
      url,
      auth_type: rec.auth_type_default === 'bearer' ? 'bearer' : 'none',
      enabled: true,
    }
    if (body.auth_type === 'bearer' && installToken.trim()) {
      body.auth_token = installToken.trim()
    }
    installMut.mutate(body)
  }

  return (
    <div className="flex h-full flex-col gap-4 overflow-auto p-6">
      {error && (
        <Card className="border-destructive">
          <CardContent className="py-3 text-sm text-destructive">{error}</CardContent>
        </Card>
      )}
      <Card>
        <CardHeader>
          <CardTitle>连接器目录</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-2 text-sm text-muted-foreground">
          <p>
            连接器让 Agent 与外部系统通信，且<strong className="text-foreground">无需沙箱出网</strong>。
            MCP 类连接器注册后工具名为 <code className="text-xs">mcp.&lt;slug&gt;.&lt;tool&gt;</code>，
            可在工作流模板的「通知工具」槽位中选择。
          </p>
          <p>
            HTTP 拉取使用 server 侧 <code className="text-xs">http.fetch</code>（见{' '}
            <code className="text-xs">config.connectors.http_fetch</code>）。
            详细部署说明见仓库 <code className="text-xs">docs/CONNECTORS.md</code>。
          </p>
          <Link to="/admin/mcp-servers" className="text-primary hover:underline">
            管理 MCP 服务 →
          </Link>
        </CardContent>
      </Card>

      {catalogQ.isLoading && <p className="text-sm text-muted-foreground">加载目录…</p>}
      {catalogQ.error && (
        <p className="text-sm text-destructive">{(catalogQ.error as Error).message}</p>
      )}

      <ul className="grid gap-4 md:grid-cols-2">
        {(catalogQ.data?.recipes ?? []).map((rec) => (
          <li key={rec.id}>
            <Card>
              <CardHeader className="pb-2">
                <CardTitle className="text-base">{rec.name}</CardTitle>
              </CardHeader>
              <CardContent className="flex flex-col gap-2 text-sm">
                <p className="text-muted-foreground">{rec.description}</p>
                <StatusBadge rec={rec} />
                {rec.tools.length > 0 && (
                  <p className="font-mono text-xs text-muted-foreground">
                    工具：{rec.tools.join(', ')}
                  </p>
                )}
                {rec.kind === 'mcp' && !rec.installed && (
                  <>
                    {installId === rec.id ? (
                      <div className="flex flex-col gap-2 border-t pt-2">
                        <div className="flex flex-col gap-1">
                          <Label>MCP URL</Label>
                          <Input
                            value={installURL}
                            onChange={(e) => setInstallURL(e.target.value)}
                            placeholder={rec.setup_url_hint}
                          />
                        </div>
                        {rec.auth_type_default === 'bearer' && (
                          <div className="flex flex-col gap-1">
                            <Label>Bearer Token（可选）</Label>
                            <Input
                              type="password"
                              value={installToken}
                              onChange={(e) => setInstallToken(e.target.value)}
                            />
                          </div>
                        )}
                        <div className="flex gap-2">
                          <Button
                            size="sm"
                            disabled={installMut.isPending || !installURL.trim()}
                            onClick={() => submitInstall(rec)}
                          >
                            {installMut.isPending ? '安装中…' : '确认安装'}
                          </Button>
                          <Button size="sm" variant="ghost" onClick={() => setInstallId(null)}>
                            取消
                          </Button>
                        </div>
                      </div>
                    ) : (
                      <Button size="sm" variant="secondary" onClick={() => openInstall(rec)}>
                        安装 MCP
                      </Button>
                    )}
                  </>
                )}
                {rec.kind === 'mcp' && rec.installed && rec.server_id && (
                  <Link
                    to="/admin/mcp-servers"
                    className="text-xs text-primary hover:underline"
                  >
                    在 MCP 管理中编辑 / 测试
                  </Link>
                )}
                {rec.kind === 'http_fetch' && !rec.installed && (
                  <p className="text-xs text-muted-foreground">
                    在 server 配置中设置 <code>connectors.http_fetch.enabled: true</code> 并配置{' '}
                    <code>allow_hosts</code>。
                  </p>
                )}
              </CardContent>
            </Card>
          </li>
        ))}
      </ul>
    </div>
  )
}

function StatusBadge({ rec }: { rec: ConnectorRecipeStatus }) {
  if (rec.kind === 'http_fetch') {
    return rec.installed ? (
      <span className="text-xs text-green-600">http.fetch 已启用</span>
    ) : (
      <span className="text-xs text-amber-600">未启用</span>
    )
  }
  if (!rec.installed) {
    return <span className="text-xs text-amber-600">未安装</span>
  }
  if (!rec.enabled) {
    return <span className="text-xs text-amber-600">已安装，已禁用</span>
  }
  return <span className="text-xs text-green-600">已安装并启用</span>
}

function humanError(e: unknown): string {
  if (e instanceof ApiError) {
    try {
      const j = JSON.parse(e.body) as { error?: string; detail?: string }
      return j.error ? `${j.error}${j.detail ? ': ' + j.detail : ''}` : e.message
    } catch {
      return e.body || e.message
    }
  }
  return e instanceof Error ? e.message : String(e)
}
