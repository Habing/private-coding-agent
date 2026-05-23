import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useMemo, useState } from 'react'

import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { ApiError, api } from '@/lib/api'
import {
  copyText,
  parseTriggerSummariesFromDSL,
  triggerRowLabel,
  triggerSummaryLabel,
} from '@/lib/workflowTriggers'
import { useAuthStore } from '@/stores/auth'
import type { WorkflowTriggersResponse } from '@/types/api'

export function TriggersPanel({
  slug,
  published,
  dsl,
}: {
  slug: string
  published: boolean
  dsl: string
}) {
  const token = useAuthStore((s) => s.token)
  const qc = useQueryClient()
  const [toast, setToast] = useState<string | null>(null)
  const [err, setErr] = useState<string | null>(null)

  const triggersQ = useQuery({
    queryKey: ['workflow-triggers', slug],
    queryFn: () =>
      api<WorkflowTriggersResponse>(`/admin/workflows/${slug}/triggers`, { token }),
    enabled: !!token && published,
  })

  const parsed = useMemo(() => parseTriggerSummariesFromDSL(dsl), [dsl])

  const runMut = useMutation({
    mutationFn: (triggerId: string) =>
      api<{ run_id: string; status: string }>(
        `/admin/workflows/${slug}/triggers/${triggerId}/run`,
        { method: 'POST', token },
      ),
    onSuccess: () => {
      setErr(null)
      qc.invalidateQueries({ queryKey: ['workflow-runs', slug] })
      qc.invalidateQueries({ queryKey: ['workflow-triggers', slug] })
    },
    onError: (e) => setErr(humanError(e)),
  })

  const rows = published ? (triggersQ.data?.triggers ?? []) : []

  return (
    <div className="flex flex-col gap-2 rounded-md border p-3">
      <Label className="font-semibold">触发器</Label>
      {!published && parsed.length === 0 && (
        <p className="text-xs text-muted-foreground">
          DSL 无 triggers 段；发布后同步到调度器。
        </p>
      )}
      {published && triggersQ.isLoading && (
        <p className="text-xs text-muted-foreground">加载触发器…</p>
      )}
      {published && triggersQ.error && (
        <p className="text-xs text-destructive">{(triggersQ.error as Error).message}</p>
      )}
      {!published &&
        parsed.map((p) => (
          <p key={p.id} className="font-mono text-xs text-muted-foreground">
            {triggerSummaryLabel(p)}（发布后生效）
          </p>
        ))}
      {published &&
        rows.map((tr) => (
          <div key={tr.trigger_id} className="rounded border p-2 text-[11px]">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <span className="font-mono">{triggerRowLabel(tr)}</span>
              <span className={tr.enabled ? 'text-green-600' : 'text-muted-foreground'}>
                {tr.enabled ? '启用' : '已禁用'}
              </span>
            </div>
            {tr.kind === 'webhook' && tr.webhook_url && (
              <div className="mt-1 flex flex-wrap items-center gap-2">
                <code className="max-w-full truncate rounded bg-muted px-1 py-0.5 text-[10px]">
                  {tr.webhook_url}
                </code>
                <Button
                  size="sm"
                  variant="ghost"
                  className="h-6 px-2 text-[10px]"
                  onClick={async () => {
                    const ok = await copyText(tr.webhook_url!)
                    setToast(ok ? '已复制 webhook URL' : '复制失败')
                  }}
                >
                  复制 URL
                </Button>
              </div>
            )}
            {tr.next_run_at && (
              <p className="mt-1 text-muted-foreground">
                下次 cron：{new Date(tr.next_run_at).toLocaleString()}
              </p>
            )}
            {tr.last_run_at && (
              <p className="text-muted-foreground">
                上次运行：{new Date(tr.last_run_at).toLocaleString()}
                {tr.last_status ? ` · ${tr.last_status}` : ''}
              </p>
            )}
            <Button
              size="sm"
              variant="secondary"
              className="mt-2 h-7 text-[10px]"
              disabled={!tr.enabled || runMut.isPending}
              onClick={() => runMut.mutate(tr.trigger_id)}
            >
              手动触发
            </Button>
          </div>
        ))}
      {toast && <p className="text-xs text-emerald-600">{toast}</p>}
      {err && <p className="text-xs text-destructive">{err}</p>}
    </div>
  )
}

function humanError(e: unknown): string {
  if (e instanceof ApiError) {
    try {
      const j = JSON.parse(e.body) as { error?: string; detail?: string }
      return j.detail || j.error || e.message
    } catch {
      return e.body || e.message
    }
  }
  return e instanceof Error ? e.message : String(e)
}
