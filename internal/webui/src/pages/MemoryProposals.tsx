import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'

import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ApiError, api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth'
import type {
  ApproveMemoryProposalRequest,
  MemoryProposal,
  MemoryProposalListResponse,
  MemoryProposalStatus,
  MemoryType,
  RejectMemoryProposalRequest,
} from '@/types/api'

const TABS: { key: MemoryProposalStatus; label: string }[] = [
  { key: 'pending', label: '待审' },
  { key: 'auto_approved', label: '自动通过' },
  { key: 'approved', label: '已通过' },
  { key: 'rejected', label: '已驳回' },
]

const TYPES: MemoryType[] = ['profile', 'preference', 'knowledge', 'lesson']

export function MemoryProposals() {
  const token = useAuthStore((s) => s.token)
  const qc = useQueryClient()
  const [status, setStatus] = useState<MemoryProposalStatus>('pending')
  const [error, setError] = useState<string | null>(null)
  const [editing, setEditing] = useState<MemoryProposal | null>(null)
  const [editDraft, setEditDraft] = useState<ApproveMemoryProposalRequest>({})

  const { data, isLoading, error: listErr } = useQuery({
    queryKey: ['memory-proposals', status],
    queryFn: () =>
      api<MemoryProposalListResponse>(
        `/admin/memory-proposals?status=${status}&limit=100`,
        { token },
      ),
    enabled: !!token,
  })

  const approveMut = useMutation({
    mutationFn: ({
      id,
      body,
    }: {
      id: string
      body: ApproveMemoryProposalRequest
    }) =>
      api<MemoryProposal>(`/admin/memory-proposals/${id}/approve`, {
        method: 'POST',
        token,
        body: JSON.stringify(body),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['memory-proposals'] })
      setEditing(null)
      setEditDraft({})
      setError(null)
    },
    onError: (e) => setError(humanError(e)),
  })

  const rejectMut = useMutation({
    mutationFn: ({
      id,
      body,
    }: {
      id: string
      body: RejectMemoryProposalRequest
    }) =>
      api<MemoryProposal>(`/admin/memory-proposals/${id}/reject`, {
        method: 'POST',
        token,
        body: JSON.stringify(body),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['memory-proposals'] })
      setError(null)
    },
    onError: (e) => setError(humanError(e)),
  })

  const proposals = data?.proposals ?? []

  function openEdit(p: MemoryProposal) {
    setEditing(p)
    setEditDraft({ type: p.type, content: p.content, tags: p.tags })
  }

  function submitApprove() {
    if (!editing) return
    const body: ApproveMemoryProposalRequest = {}
    if (editDraft.type !== editing.type) body.type = editDraft.type
    if (editDraft.content !== editing.content) body.content = editDraft.content
    if (
      JSON.stringify(editDraft.tags ?? []) !== JSON.stringify(editing.tags ?? [])
    ) {
      body.tags = editDraft.tags ?? []
    }
    approveMut.mutate({ id: editing.id, body })
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

      <div className="flex items-center gap-2">
        {TABS.map((t) => (
          <Button
            key={t.key}
            size="sm"
            variant={status === t.key ? 'default' : 'secondary'}
            onClick={() => setStatus(t.key)}
          >
            {t.label}
          </Button>
        ))}
      </div>

      <Card className="flex-1">
        <CardHeader>
          <CardTitle>记忆提议</CardTitle>
        </CardHeader>
        <CardContent>
          {isLoading && <p className="text-sm text-muted-foreground">加载中…</p>}
          {listErr && (
            <p className="text-sm text-destructive">
              加载失败：{(listErr as Error).message}
            </p>
          )}
          {!isLoading && proposals.length === 0 && (
            <p className="text-sm text-muted-foreground">
              当前 tab 下没有提议
            </p>
          )}
          <ul className="flex flex-col gap-3">
            {proposals.map((p) => (
              <li key={p.id} className="rounded-md border p-3 text-sm">
                <div className="mb-1 flex items-center justify-between gap-2">
                  <div className="flex flex-col">
                    <span className="font-mono text-xs text-muted-foreground">
                      {p.type} · conf {p.confidence.toFixed(2)} ·{' '}
                      {new Date(p.created_at).toLocaleString()}
                    </span>
                    {p.tags.length > 0 && (
                      <span className="text-xs text-muted-foreground">
                        tags: {p.tags.join(', ')}
                      </span>
                    )}
                  </div>
                  {p.status === 'pending' && (
                    <div className="flex gap-2">
                      <Button
                        size="sm"
                        disabled={approveMut.isPending}
                        onClick={() => openEdit(p)}
                      >
                        通过
                      </Button>
                      <Button
                        size="sm"
                        variant="secondary"
                        disabled={rejectMut.isPending}
                        onClick={() => rejectMut.mutate({ id: p.id, body: {} })}
                      >
                        驳回
                      </Button>
                    </div>
                  )}
                </div>
                <p className="whitespace-pre-wrap text-sm">{p.content}</p>
                {p.memory_id && (
                  <p className="mt-1 font-mono text-xs text-muted-foreground">
                    memory_id: {p.memory_id}
                  </p>
                )}
              </li>
            ))}
          </ul>
        </CardContent>
      </Card>

      {editing && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4">
          <Card className="w-full max-w-2xl">
            <CardHeader>
              <CardTitle>通过提议（可编辑后入库）</CardTitle>
            </CardHeader>
            <CardContent className="flex flex-col gap-3">
              <div className="flex flex-col gap-1">
                <Label htmlFor="edit-type">类型</Label>
                <select
                  id="edit-type"
                  className="h-9 rounded-md border bg-background px-2 text-sm"
                  value={editDraft.type ?? editing.type}
                  onChange={(e) =>
                    setEditDraft({
                      ...editDraft,
                      type: e.target.value as MemoryType,
                    })
                  }
                >
                  {TYPES.map((t) => (
                    <option key={t} value={t}>
                      {t}
                    </option>
                  ))}
                </select>
              </div>
              <div className="flex flex-col gap-1">
                <Label htmlFor="edit-content">内容</Label>
                <textarea
                  id="edit-content"
                  className="min-h-[100px] rounded-md border bg-background p-2 font-mono text-xs"
                  value={editDraft.content ?? ''}
                  onChange={(e) =>
                    setEditDraft({ ...editDraft, content: e.target.value })
                  }
                />
              </div>
              <div className="flex flex-col gap-1">
                <Label htmlFor="edit-tags">标签（逗号分隔）</Label>
                <Input
                  id="edit-tags"
                  value={(editDraft.tags ?? []).join(', ')}
                  onChange={(e) =>
                    setEditDraft({
                      ...editDraft,
                      tags: e.target.value
                        .split(',')
                        .map((s) => s.trim())
                        .filter((s) => s.length > 0),
                    })
                  }
                />
              </div>
              <div className="flex justify-end gap-2">
                <Button
                  variant="secondary"
                  onClick={() => {
                    setEditing(null)
                    setEditDraft({})
                  }}
                >
                  取消
                </Button>
                <Button
                  disabled={approveMut.isPending}
                  onClick={submitApprove}
                >
                  通过并写入记忆
                </Button>
              </div>
            </CardContent>
          </Card>
        </div>
      )}
    </div>
  )
}

function humanError(e: unknown): string {
  if (e instanceof ApiError) {
    return e.message
  }
  if (e instanceof Error) return e.message
  return String(e)
}
