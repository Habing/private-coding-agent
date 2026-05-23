import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'

import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { api } from '@/lib/api'
import { memoryTypeLabel } from '@/lib/uiLabels'
import { useAuthStore } from '@/stores/auth'
import type {
  CreateMemoryRequest,
  Memory,
  MemoryListResponse,
  MemoryType,
  UpdateMemoryRequest,
} from '@/types/api'

const TYPES: MemoryType[] = ['preference', 'knowledge', 'lesson', 'profile']

export function Memories() {
  const token = useAuthStore((s) => s.token)
  const qc = useQueryClient()
  const [draft, setDraft] = useState<CreateMemoryRequest>({
    type: 'knowledge',
    content: '',
    tags: [],
  })
  const [tagInput, setTagInput] = useState('')

  const { data, isLoading, error } = useQuery({
    queryKey: ['memories'],
    queryFn: () => api<MemoryListResponse>('/memories?limit=50', { token }),
    enabled: !!token,
  })

  const createMut = useMutation({
    mutationFn: (body: CreateMemoryRequest) =>
      api<Memory>('/memories', { method: 'POST', token, body: JSON.stringify(body) }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['memories'] })
      setDraft({ type: 'knowledge', content: '', tags: [] })
      setTagInput('')
    },
  })

  const updateMut = useMutation({
    mutationFn: ({ id, body }: { id: string; body: UpdateMemoryRequest }) =>
      api<Memory>(`/memories/${encodeURIComponent(id)}`, {
        method: 'PUT',
        token,
        body: JSON.stringify(body),
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['memories'] }),
  })

  const deleteMut = useMutation({
    mutationFn: (id: string) =>
      api<void>(`/memories/${encodeURIComponent(id)}`, { method: 'DELETE', token }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['memories'] }),
  })

  function addTag() {
    const t = tagInput.trim()
    if (!t) return
    setDraft((d) => ({ ...d, tags: [...(d.tags ?? []), t] }))
    setTagInput('')
  }

  const memories = data?.memories ?? []

  return (
    <div className="flex h-full flex-col gap-4 overflow-auto p-6">
      <Card>
        <CardHeader>
          <CardTitle>新建记忆</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <div className="flex flex-wrap gap-3">
            <div className="flex flex-col gap-1">
              <Label htmlFor="mem-type">类型</Label>
              <select
                id="mem-type"
                className="h-9 rounded-md border bg-background px-2 text-sm"
                value={draft.type}
                onChange={(e) =>
                  setDraft({ ...draft, type: e.target.value as MemoryType })
                }
              >
                {TYPES.map((t) => (
                  <option key={t} value={t}>
                    {memoryTypeLabel(t)}
                  </option>
                ))}
              </select>
            </div>
            <div className="flex flex-1 flex-col gap-1 min-w-[200px]">
              <Label htmlFor="mem-content">内容</Label>
              <Input
                id="mem-content"
                value={draft.content}
                onChange={(e) => setDraft({ ...draft, content: e.target.value })}
                placeholder="可检索的记忆文本"
              />
            </div>
          </div>
          <div className="flex flex-wrap items-end gap-2">
            <div className="flex flex-col gap-1">
              <Label htmlFor="mem-tag">标签</Label>
              <Input
                id="mem-tag"
                value={tagInput}
                onChange={(e) => setTagInput(e.target.value)}
                onKeyDown={(e) => e.key === 'Enter' && (e.preventDefault(), addTag())}
                className="w-40"
              />
            </div>
            <Button type="button" variant="secondary" size="sm" onClick={addTag}>
              添加标签
            </Button>
            {draft.tags && draft.tags.length > 0 && (
              <span className="text-xs text-muted-foreground">{draft.tags.join(', ')}</span>
            )}
          </div>
          <Button
            size="sm"
            className="w-fit"
            disabled={!draft.content.trim() || createMut.isPending}
            onClick={() => createMut.mutate(draft)}
          >
            保存
          </Button>
        </CardContent>
      </Card>

      <Card className="flex-1">
        <CardHeader>
          <CardTitle>记忆列表</CardTitle>
        </CardHeader>
        <CardContent>
          {isLoading && <p className="text-sm text-muted-foreground">加载中…</p>}
          {error && (
            <p className="text-sm text-destructive">加载失败：{(error as Error).message}</p>
          )}
          {!isLoading && memories.length === 0 && (
            <p className="text-sm text-muted-foreground">暂无记忆</p>
          )}
          <ul className="flex flex-col gap-3">
            {memories.map((m) => (
              <MemoryRow
                key={m.id}
                memory={m}
                onSave={(body) => updateMut.mutate({ id: m.id, body })}
                onDelete={() => deleteMut.mutate(m.id)}
                saving={updateMut.isPending}
              />
            ))}
          </ul>
        </CardContent>
      </Card>
    </div>
  )
}

function MemoryRow({
  memory,
  onSave,
  onDelete,
  saving,
}: {
  memory: Memory
  onSave: (body: UpdateMemoryRequest) => void
  onDelete: () => void
  saving: boolean
}) {
  const [content, setContent] = useState(memory.content)

  return (
    <li className="rounded-md border p-3 text-sm">
      <div className="mb-1 flex items-center justify-between gap-2">
        <span className="font-medium text-muted-foreground">{memoryTypeLabel(memory.type)}</span>
        <div className="flex gap-2">
          <Button
            size="sm"
            variant="secondary"
            disabled={saving || content === memory.content}
            onClick={() => onSave({ content })}
          >
            更新
          </Button>
          <Button size="sm" variant="ghost" onClick={onDelete}>
            删除
          </Button>
        </div>
      </div>
      <textarea
        className="mt-1 w-full min-h-[60px] rounded border bg-background p-2 text-sm"
        value={content}
        onChange={(e) => setContent(e.target.value)}
      />
      {memory.tags?.length > 0 && (
        <p className="mt-1 text-xs text-muted-foreground">标签: {memory.tags.join(', ')}</p>
      )}
    </li>
  )
}
