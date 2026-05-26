import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'

import { WorkflowGraphMini } from '@/components/WorkflowGraph'
import { OpenProposalInDesignerButton } from '@/components/workflow/OpenProposalInDesignerButton'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { ApiError, api } from '@/lib/api'
import {
  proposalDryRunLabel,
  proposalSourceBadge,
  proposalStatusLabel,
} from '@/lib/workflowProposalLabels'
import { useAuthStore } from '@/stores/auth'
import type { WorkflowProposal, WorkflowProposalListResponse } from '@/types/api'

const TABS: { key: string; label: string }[] = [
  { key: 'pending_approval', label: '待审批' },
  { key: 'draft', label: '草案' },
  { key: 'published', label: '已发布' },
  { key: 'rejected', label: '已拒绝' },
]

export function WorkflowProposals() {
  const token = useAuthStore((s) => s.token)
  const qc = useQueryClient()
  const [status, setStatus] = useState('pending_approval')
  const [error, setError] = useState<string | null>(null)

  const { data, isLoading, error: listErr } = useQuery({
    queryKey: ['workflow-proposals', status],
    queryFn: () =>
      api<WorkflowProposalListResponse>(
        `/admin/workflow/proposals?status=${encodeURIComponent(status)}&limit=100`,
        { token },
      ),
    enabled: !!token,
  })

  const approveMut = useMutation({
    mutationFn: (id: string) =>
      api<{ proposal: WorkflowProposal }>(
        `/admin/workflow/proposals/${id}/approve`,
        { method: 'POST', token },
      ),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['workflow-proposals'] })
      qc.invalidateQueries({ queryKey: ['workflows'] })
      setError(null)
    },
    onError: (e) => setError(humanError(e)),
  })

  const rejectMut = useMutation({
    mutationFn: (id: string) =>
      api<void>(`/admin/workflow/proposals/${id}/reject`, {
        method: 'POST',
        token,
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['workflow-proposals'] })
      setError(null)
    },
    onError: (e) => setError(humanError(e)),
  })

  const proposals = data?.proposals ?? []

  return (
    <div className="flex h-full flex-col gap-4 overflow-auto p-6">
      <Card>
        <CardHeader>
          <CardTitle>工作流提议</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <div className="flex flex-wrap gap-2">
            {TABS.map((tab) => (
              <Button
                key={tab.key}
                size="sm"
                variant={status === tab.key ? 'default' : 'secondary'}
                onClick={() => setStatus(tab.key)}
              >
                {tab.label}
              </Button>
            ))}
          </div>
          {error && <p className="text-sm text-destructive">{error}</p>}
          {listErr && (
            <p className="text-sm text-destructive">
              加载失败：{(listErr as Error).message}
            </p>
          )}
          {isLoading && <p className="text-sm text-muted-foreground">加载中…</p>}
          {!isLoading && proposals.length === 0 && (
            <p className="text-sm text-muted-foreground">暂无记录。</p>
          )}
          <ul className="flex flex-col gap-3">
            {proposals.map((p) => (
              <li key={p.id} className="rounded-md border p-3">
                <div className="flex flex-wrap items-start justify-between gap-2">
                  <div className="flex flex-col gap-1">
                    <span className="font-mono text-sm">{p.slug}</span>
                    <span className="text-sm">{p.name}</span>
                    <span className="text-xs text-muted-foreground">
                      {proposalSourceBadge(p.source, p.template_id)} ·{' '}
                      {proposalStatusLabel(p.status)} ·{' '}
                      {proposalDryRunLabel(p.dry_run_ok, p.dry_run_error)}
                    </span>
                  </div>
                  <div className="flex flex-wrap gap-2">
                    {p.dry_run_ok && (
                      <OpenProposalInDesignerButton proposalId={p.id} variant="outline" />
                    )}
                    {status === 'pending_approval' && (
                      <>
                        <Button
                          size="sm"
                          disabled={approveMut.isPending || !p.dry_run_ok}
                          onClick={() => approveMut.mutate(p.id)}
                        >
                          批准发布
                        </Button>
                        <Button
                          size="sm"
                          variant="ghost"
                          disabled={rejectMut.isPending}
                          onClick={() => {
                            if (window.confirm(`驳回提议「${p.slug}」？`)) {
                              rejectMut.mutate(p.id)
                            }
                          }}
                        >
                          驳回
                        </Button>
                      </>
                    )}
                  </div>
                </div>
                {p.dry_run_ok && (
                  <div className="mt-2">
                    <WorkflowGraphMini proposalId={p.id} />
                  </div>
                )}
              </li>
            ))}
          </ul>
        </CardContent>
      </Card>
    </div>
  )
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
