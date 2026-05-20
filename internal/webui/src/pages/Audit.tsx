import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'

import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth'
import type { AuditListResponse } from '@/types/api'

const PAGE_SIZE = 25

interface Filters {
  action: string
  userID: string
}

function buildQuery(f: Filters, offset: number): string {
  const p = new URLSearchParams()
  if (f.action) p.set('action', f.action)
  if (f.userID) p.set('user_id', f.userID)
  p.set('limit', String(PAGE_SIZE))
  p.set('offset', String(offset))
  return `/audit?${p.toString()}`
}

export function Audit() {
  const token = useAuthStore((s) => s.token)
  const [filters, setFilters] = useState<Filters>({ action: '', userID: '' })
  const [pending, setPending] = useState<Filters>({ action: '', userID: '' })
  const [offset, setOffset] = useState(0)

  const { data, error, isLoading } = useQuery({
    queryKey: ['audit', filters, offset],
    queryFn: () => api<AuditListResponse>(buildQuery(filters, offset), { token }),
    enabled: !!token,
  })

  function applyFilters() {
    setOffset(0)
    setFilters(pending)
  }

  function resetFilters() {
    const empty = { action: '', userID: '' }
    setPending(empty)
    setFilters(empty)
    setOffset(0)
  }

  const total = data?.total ?? 0
  const entries = data?.entries ?? []
  const hasNext = offset + PAGE_SIZE < total
  const hasPrev = offset > 0

  return (
    <div className="flex h-full flex-col gap-4 overflow-auto p-6">
      <Card>
        <CardHeader>
          <CardTitle>审计日志</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-wrap items-end gap-3">
          <div className="flex flex-col gap-1">
            <Label htmlFor="audit-action">Action 前缀</Label>
            <Input
              id="audit-action"
              placeholder="如 auth.login 或 sandbox."
              value={pending.action}
              onChange={(e) => setPending({ ...pending, action: e.target.value })}
              className="w-56"
            />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="audit-uid">User ID</Label>
            <Input
              id="audit-uid"
              placeholder="UUID"
              value={pending.userID}
              onChange={(e) => setPending({ ...pending, userID: e.target.value })}
              className="w-72"
            />
          </div>
          <Button onClick={applyFilters} size="sm">
            筛选
          </Button>
          <Button variant="ghost" onClick={resetFilters} size="sm">
            重置
          </Button>
        </CardContent>
      </Card>

      <Card className="flex-1">
        <CardHeader className="flex flex-row items-center justify-between">
          <CardTitle className="text-sm">
            共 {total} 条 · 第 {Math.floor(offset / PAGE_SIZE) + 1} 页
          </CardTitle>
          <div className="flex gap-2">
            <Button
              size="sm"
              variant="outline"
              disabled={!hasPrev}
              onClick={() => setOffset(Math.max(0, offset - PAGE_SIZE))}
            >
              上一页
            </Button>
            <Button
              size="sm"
              variant="outline"
              disabled={!hasNext}
              onClick={() => setOffset(offset + PAGE_SIZE)}
            >
              下一页
            </Button>
          </div>
        </CardHeader>
        <CardContent>
          {isLoading && <p className="text-sm text-muted-foreground">加载中…</p>}
          {error && (
            <p className="text-sm text-destructive">
              加载失败：{error instanceof Error ? error.message : String(error)}
            </p>
          )}
          {!isLoading && !error && entries.length === 0 && (
            <p className="text-sm text-muted-foreground">无记录</p>
          )}
          {entries.length > 0 && (
            <div className="overflow-auto">
              <table className="w-full text-left text-sm">
                <thead className="border-b text-xs uppercase text-muted-foreground">
                  <tr>
                    <th className="py-2 pr-3">时间</th>
                    <th className="py-2 pr-3">Action</th>
                    <th className="py-2 pr-3">Target</th>
                    <th className="py-2 pr-3">User</th>
                    <th className="py-2 pr-3">Status</th>
                    <th className="py-2 pr-3">Duration (ms)</th>
                  </tr>
                </thead>
                <tbody>
                  {entries.map((e, i) => (
                    <tr key={`${e.occurred_at}-${i}`} className="border-b last:border-b-0">
                      <td className="py-2 pr-3 font-mono text-xs">
                        {new Date(e.occurred_at).toLocaleString()}
                      </td>
                      <td className="py-2 pr-3 font-mono text-xs">{e.action}</td>
                      <td className="py-2 pr-3 font-mono text-xs">{e.target}</td>
                      <td className="py-2 pr-3 font-mono text-xs">
                        {e.user_id ? e.user_id.slice(0, 8) : '-'}
                      </td>
                      <td className="py-2 pr-3">{e.status || '-'}</td>
                      <td className="py-2 pr-3">{e.duration_ms || '-'}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
