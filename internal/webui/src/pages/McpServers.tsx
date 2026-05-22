import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'

import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ApiError, api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth'
import type {
  CreateMcpServerRequest,
  McpRefreshResponse,
  McpServer,
  McpServerListResponse,
  TestMcpConnectionRequest,
  UpdateMcpServerRequest,
} from '@/types/api'

export function McpServers() {
  const token = useAuthStore((s) => s.token)
  const qc = useQueryClient()
  const [newSlug, setNewSlug] = useState('')
  const [newName, setNewName] = useState('')
  const [newURL, setNewURL] = useState('')
  const [newAuthType, setNewAuthType] = useState<'none' | 'bearer'>('none')
  const [newToken, setNewToken] = useState('')
  const [selected, setSelected] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const listQ = useQuery({
    queryKey: ['mcp-servers'],
    queryFn: () => api<McpServerListResponse>('/admin/mcp-servers', { token }),
    enabled: !!token,
  })

  const createMut = useMutation({
    mutationFn: (body: CreateMcpServerRequest) =>
      api<McpServer>('/admin/mcp-servers', {
        method: 'POST',
        token,
        body: JSON.stringify(body),
      }),
    onSuccess: (s) => {
      qc.invalidateQueries({ queryKey: ['mcp-servers'] })
      setNewSlug('')
      setNewName('')
      setNewURL('')
      setNewToken('')
      setNewAuthType('none')
      setSelected(s.id)
      setError(null)
    },
    onError: (e) => setError(humanError(e)),
  })

  function submitCreate() {
    const slug = newSlug.trim()
    const name = newName.trim() || slug
    const url = newURL.trim()
    if (!slug || !url) return
    const body: CreateMcpServerRequest = {
      slug,
      name,
      url,
      auth_type: newAuthType,
      enabled: true,
    }
    if (newAuthType === 'bearer' && newToken.trim()) {
      body.auth_token = newToken.trim()
    }
    createMut.mutate(body)
  }

  return (
    <div className="flex h-full flex-col gap-4 overflow-auto p-6">
      {error && (
        <Card className="border-destructive">
          <CardContent className="flex items-center justify-between py-3 text-sm text-destructive">
            <span>{error}</span>
            <Button size="sm" variant="ghost" onClick={() => setError(null)}>
              关闭
            </Button>
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader>
          <CardTitle>注册外部 MCP Server</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-wrap items-end gap-3">
          <div className="flex flex-col gap-1">
            <Label htmlFor="mcp-slug">slug</Label>
            <Input
              id="mcp-slug"
              placeholder="kebab-case-slug"
              value={newSlug}
              onChange={(e) => setNewSlug(e.target.value)}
              className="w-56"
            />
          </div>
          <div className="flex flex-col gap-1 min-w-[180px]">
            <Label htmlFor="mcp-name">name（可选）</Label>
            <Input
              id="mcp-name"
              value={newName}
              onChange={(e) => setNewName(e.target.value)}
            />
          </div>
          <div className="flex flex-1 flex-col gap-1 min-w-[260px]">
            <Label htmlFor="mcp-url">url</Label>
            <Input
              id="mcp-url"
              placeholder="http://mcp.example.com:8083"
              value={newURL}
              onChange={(e) => setNewURL(e.target.value)}
            />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="mcp-auth">auth_type</Label>
            <select
              id="mcp-auth"
              className="h-9 rounded-md border bg-background px-2 text-sm"
              value={newAuthType}
              onChange={(e) =>
                setNewAuthType(e.target.value as 'none' | 'bearer')
              }
            >
              <option value="none">none</option>
              <option value="bearer">bearer</option>
            </select>
          </div>
          {newAuthType === 'bearer' && (
            <div className="flex flex-col gap-1 min-w-[200px]">
              <Label htmlFor="mcp-token">auth_token</Label>
              <Input
                id="mcp-token"
                type="password"
                value={newToken}
                onChange={(e) => setNewToken(e.target.value)}
              />
            </div>
          )}
          <Button
            size="sm"
            disabled={
              !newSlug.trim() || !newURL.trim() || createMut.isPending
            }
            onClick={submitCreate}
          >
            创建
          </Button>
        </CardContent>
      </Card>

      <Card className="flex-1">
        <CardHeader>
          <CardTitle>MCP Server 列表</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          {listQ.isLoading && (
            <p className="text-sm text-muted-foreground">加载中…</p>
          )}
          {listQ.error && (
            <p className="text-sm text-destructive">
              加载失败：{(listQ.error as Error).message}
            </p>
          )}
          {!listQ.isLoading && (listQ.data?.servers.length ?? 0) === 0 && (
            <p className="text-sm text-muted-foreground">
              还没有 MCP Server，用上方表单注册一个。
            </p>
          )}
          <ul className="flex flex-col gap-3">
            {(listQ.data?.servers ?? []).map((s) => (
              <ServerRow
                key={s.id}
                server={s}
                expanded={selected === s.id}
                onToggle={() => setSelected(selected === s.id ? null : s.id)}
                onError={setError}
              />
            ))}
          </ul>
        </CardContent>
      </Card>
    </div>
  )
}

function ServerRow({
  server,
  expanded,
  onToggle,
  onError,
}: {
  server: McpServer
  expanded: boolean
  onToggle: () => void
  onError: (msg: string | null) => void
}) {
  const token = useAuthStore((s) => s.token)
  const qc = useQueryClient()

  const refreshMut = useMutation({
    mutationFn: () =>
      api<McpRefreshResponse>(`/admin/mcp-servers/${server.id}/refresh`, {
        method: 'POST',
        token,
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['mcp-servers'] })
      onError(null)
    },
    onError: (e) => onError(humanError(e)),
  })

  const enableMut = useMutation({
    mutationFn: (enabled: boolean) =>
      api<McpServer>(
        `/admin/mcp-servers/${server.id}/${enabled ? 'enable' : 'disable'}`,
        { method: 'POST', token },
      ),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['mcp-servers'] })
      onError(null)
    },
    onError: (e) => onError(humanError(e)),
  })

  const deleteMut = useMutation({
    mutationFn: () =>
      api<void>(`/admin/mcp-servers/${server.id}`, {
        method: 'DELETE',
        token,
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['mcp-servers'] })
      onError(null)
    },
    onError: (e) => onError(humanError(e)),
  })

  function confirmDelete() {
    if (
      window.confirm(
        `确认删除 MCP server "${server.slug}"？相关 tool 将立即从 Bus 下线。`,
      )
    ) {
      deleteMut.mutate()
    }
  }

  const seenAt = server.last_seen_at
    ? new Date(server.last_seen_at).toLocaleString()
    : '未连接'

  return (
    <li className="rounded-md border">
      <div className="flex flex-wrap items-center justify-between gap-2 p-3">
        <div className="flex flex-col">
          <span className="font-mono text-sm">
            {server.slug}{' '}
            <span className="text-xs text-muted-foreground">
              ({server.tools_cache.length} tools)
            </span>
          </span>
          <span className="text-xs text-muted-foreground">
            {server.name} ·{' '}
            {server.enabled ? (
              <span className="text-green-600">enabled</span>
            ) : (
              <span>disabled</span>
            )}{' '}
            · last_seen: {seenAt}
            {server.last_error && (
              <span className="ml-2 text-destructive">
                err: {truncate(server.last_error, 60)}
              </span>
            )}
          </span>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button size="sm" variant="secondary" onClick={onToggle}>
            {expanded ? '收起' : '详情'}
          </Button>
          <Button
            size="sm"
            variant="ghost"
            disabled={refreshMut.isPending}
            onClick={() => refreshMut.mutate()}
          >
            刷新工具
          </Button>
          <Button
            size="sm"
            variant="ghost"
            disabled={enableMut.isPending}
            onClick={() => enableMut.mutate(!server.enabled)}
          >
            {server.enabled ? '禁用' : '启用'}
          </Button>
          <Button
            size="sm"
            variant="ghost"
            disabled={deleteMut.isPending}
            onClick={confirmDelete}
          >
            删除
          </Button>
        </div>
      </div>

      {expanded && <EditPane server={server} onError={onError} />}
    </li>
  )
}

function EditPane({
  server,
  onError,
}: {
  server: McpServer
  onError: (msg: string | null) => void
}) {
  const token = useAuthStore((s) => s.token)
  const qc = useQueryClient()
  const [name, setName] = useState(server.name)
  const [description, setDescription] = useState(server.description)
  const [url, setUrl] = useState(server.url)
  const [authType, setAuthType] = useState<'none' | 'bearer'>(
    (server.auth_type as 'none' | 'bearer') || 'none',
  )
  const [authToken, setAuthToken] = useState('')
  const [headersText, setHeadersText] = useState(
    JSON.stringify(server.headers ?? {}, null, 2),
  )
  const [testResult, setTestResult] = useState<string | null>(null)

  useEffect(() => {
    setName(server.name)
    setDescription(server.description)
    setUrl(server.url)
    setAuthType((server.auth_type as 'none' | 'bearer') || 'none')
    setAuthToken('')
    setHeadersText(JSON.stringify(server.headers ?? {}, null, 2))
  }, [server.id, server.updated_at])

  function parseHeaders(): Record<string, string> | undefined {
    const txt = headersText.trim()
    if (!txt) return {}
    try {
      const obj = JSON.parse(txt)
      if (obj && typeof obj === 'object' && !Array.isArray(obj)) {
        return obj as Record<string, string>
      }
      throw new Error('headers must be a JSON object')
    } catch (e) {
      onError('headers JSON 解析失败: ' + (e as Error).message)
      return undefined
    }
  }

  const updateMut = useMutation({
    mutationFn: (body: UpdateMcpServerRequest) =>
      api<McpServer>(`/admin/mcp-servers/${server.id}`, {
        method: 'PUT',
        token,
        body: JSON.stringify(body),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['mcp-servers'] })
      setAuthToken('')
      onError(null)
    },
    onError: (e) => onError(humanError(e)),
  })

  const testMut = useMutation({
    mutationFn: () => {
      const body: TestMcpConnectionRequest = {
        url,
        auth_type: authType,
        headers: parseHeaders() ?? {},
      }
      if (authType === 'bearer' && authToken.trim()) {
        body.auth_token = authToken.trim()
      }
      return api<{ ok: boolean; error?: string }>(
        `/admin/mcp-servers/${server.id}/test`,
        { method: 'POST', token, body: JSON.stringify(body) },
      )
    },
    onSuccess: (r) => {
      setTestResult(r.ok ? '连接成功' : `失败：${r.error ?? ''}`)
    },
    onError: (e) => setTestResult('失败：' + humanError(e)),
  })

  function submitSave() {
    const headers = parseHeaders()
    if (headers === undefined) return
    const body: UpdateMcpServerRequest = {
      name,
      description,
      url,
      auth_type: authType,
      headers,
    }
    if (authToken.trim()) {
      body.auth_token = authToken.trim()
    }
    updateMut.mutate(body)
  }

  return (
    <div className="grid grid-cols-1 gap-3 border-t p-3 lg:grid-cols-[1fr_320px]">
      <div className="flex flex-col gap-3">
        <div className="flex flex-col gap-1">
          <Label>name</Label>
          <Input value={name} onChange={(e) => setName(e.target.value)} />
        </div>
        <div className="flex flex-col gap-1">
          <Label>description</Label>
          <textarea
            className="min-h-[60px] rounded-md border bg-background p-2 text-sm"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
          />
        </div>
        <div className="flex flex-col gap-1">
          <Label>url</Label>
          <Input value={url} onChange={(e) => setUrl(e.target.value)} />
        </div>
        <div className="flex flex-wrap items-end gap-3">
          <div className="flex flex-col gap-1">
            <Label>auth_type</Label>
            <select
              className="h-9 rounded-md border bg-background px-2 text-sm"
              value={authType}
              onChange={(e) =>
                setAuthType(e.target.value as 'none' | 'bearer')
              }
            >
              <option value="none">none</option>
              <option value="bearer">bearer</option>
            </select>
          </div>
          {authType === 'bearer' && (
            <div className="flex flex-1 flex-col gap-1 min-w-[200px]">
              <Label>auth_token（留空保留原值）</Label>
              <Input
                type="password"
                value={authToken}
                onChange={(e) => setAuthToken(e.target.value)}
                placeholder="*** (current secret hidden)"
              />
            </div>
          )}
        </div>
        <div className="flex flex-col gap-1">
          <Label>headers (JSON object)</Label>
          <textarea
            className="min-h-[80px] rounded-md border bg-background p-2 font-mono text-xs"
            value={headersText}
            onChange={(e) => setHeadersText(e.target.value)}
            placeholder='{"X-Org": "acme"}'
          />
        </div>
        <div className="flex gap-2">
          <Button
            size="sm"
            disabled={updateMut.isPending}
            onClick={submitSave}
          >
            {updateMut.isPending ? '保存中…' : '保存'}
          </Button>
          <Button
            size="sm"
            variant="secondary"
            disabled={testMut.isPending}
            onClick={() => testMut.mutate()}
          >
            {testMut.isPending ? '测试中…' : '测试连接'}
          </Button>
          {testResult && (
            <span className="self-center text-xs text-muted-foreground">
              {testResult}
            </span>
          )}
        </div>
      </div>
      <div className="flex flex-col gap-2 rounded-md border p-3">
        <Label className="font-semibold">已发现工具</Label>
        {server.tools_cache.length === 0 ? (
          <p className="text-xs text-muted-foreground">
            还未刷新或服务器未返回工具。点击「刷新工具」拉取。
          </p>
        ) : (
          <ul className="flex flex-col gap-2">
            {server.tools_cache.map((t) => (
              <li
                key={t.name}
                className="rounded border bg-muted/30 p-2 text-xs"
              >
                <div className="font-mono">
                  mcp.{server.slug}.{t.name}
                </div>
                {t.description && (
                  <div className="mt-1 text-muted-foreground">
                    {t.description}
                  </div>
                )}
                <details className="mt-1">
                  <summary className="cursor-pointer text-muted-foreground">
                    schema
                  </summary>
                  <pre className="mt-1 max-h-40 overflow-auto rounded bg-background p-1 font-mono text-[10px]">
                    {JSON.stringify(t.inputSchema, null, 2)}
                  </pre>
                </details>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  )
}

function truncate(s: string, n: number): string {
  if (s.length <= n) return s
  return s.slice(0, n) + '…'
}

function humanError(e: unknown): string {
  if (e instanceof ApiError) {
    try {
      const j = JSON.parse(e.body) as { error?: string; detail?: string }
      return j.error
        ? `${j.error}${j.detail ? ': ' + j.detail : ''}`
        : e.message
    } catch {
      return e.body || e.message
    }
  }
  return e instanceof Error ? e.message : String(e)
}
