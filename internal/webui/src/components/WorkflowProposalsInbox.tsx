import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'

import { WorkflowGraphMini } from '@/components/WorkflowGraph'
import { OpenProposalRowInDesignerButton } from '@/components/workflow/OpenProposalInDesignerButton'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { ApiError, api } from '@/lib/api'
import {
  proposalDryRunLabel,
  proposalSourceBadge,
  proposalStatusLabel,
} from '@/lib/workflowProposalLabels'
import { useAuthStore } from '@/stores/auth'
import type { ProposalDesignerImport } from '@/lib/proposalDesignerImport'
import type { WorkflowProposal, WorkflowProposalListResponse } from '@/types/api'

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

function useProposals(status: string) {
  const token = useAuthStore((s) => s.token)
  return useQuery({
    queryKey: ['workflow-proposals', status],
    queryFn: () =>
      api<WorkflowProposalListResponse>(
        `/admin/workflow/proposals?status=${encodeURIComponent(status)}&limit=50`,
        { token },
      ),
    enabled: !!token,
  })
}

export function WorkflowProposalsInbox({
  onWorkflowPublished,
  onOpenInDesigner,
}: {
  onWorkflowPublished?: (slug: string) => void
  onOpenInDesigner?: (imp: ProposalDesignerImport) => void | Promise<void>
}) {
  const token = useAuthStore((s) => s.token)
  const qc = useQueryClient()
  const [error, setError] = useState<string | null>(null)

  const draftQ = useProposals('draft')
  const pendingQ = useProposals('pending_approval')

  const approveMut = useMutation({
    mutationFn: (id: string) =>
      api<{ proposal: WorkflowProposal }>(
        `/admin/workflow/proposals/${id}/approve`,
        { method: 'POST', token },
      ),
    onSuccess: (res) => {
      qc.invalidateQueries({ queryKey: ['workflow-proposals'] })
      qc.invalidateQueries({ queryKey: ['workflows'] })
      setError(null)
      onWorkflowPublished?.(res.proposal.slug)
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

  const drafts = draftQ.data?.proposals ?? []
  const pending = pendingQ.data?.proposals ?? []
  const total = drafts.length + pending.length

  if (total === 0 && !draftQ.isLoading && !pendingQ.isLoading) {
    return null
  }

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between gap-2 space-y-0">
        <CardTitle className="text-base">草案与待审批</CardTitle>
        <Button size="sm" variant="ghost" asChild>
          <Link to="/admin/workflow-proposals">全部提议</Link>
        </Button>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {error && <p className="text-sm text-destructive">{error}</p>}
        {(draftQ.isLoading || pendingQ.isLoading) && (
          <p className="text-sm text-muted-foreground">加载草案…</p>
        )}
        {pending.length > 0 && (
          <section className="flex flex-col gap-2">
            <h3 className="text-sm font-medium">待审批 ({pending.length})</h3>
            <ul className="flex flex-col gap-2">
              {pending.map((p) => (
                <ProposalRow
                  key={p.id}
                  proposal={p}
                  actions={
                    <div className="flex gap-2">
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
                    </div>
                  }
                />
              ))}
            </ul>
          </section>
        )}
        {drafts.length > 0 && (
          <section className="flex flex-col gap-2">
            <h3 className="text-sm font-medium">草案 ({drafts.length})</h3>
            <p className="text-xs text-muted-foreground">
              在对话中确认发布，或点击「在设计器中打开」精修步骤与参数。
            </p>
            <ul className="flex flex-col gap-2">
              {drafts.map((p) => (
                <ProposalRow
                  key={p.id}
                  proposal={p}
                  actions={
                    onOpenInDesigner ? (
                      <OpenProposalRowInDesignerButton
                        proposal={p}
                        onOpen={onOpenInDesigner}
                      />
                    ) : undefined
                  }
                />
              ))}
            </ul>
          </section>
        )}
      </CardContent>
    </Card>
  )
}

function ProposalRow({
  proposal,
  actions,
}: {
  proposal: WorkflowProposal
  actions?: ReactNode
}) {
  return (
    <li className="rounded-md border p-3">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div className="flex flex-col gap-1">
          <span className="font-mono text-sm">{proposal.slug}</span>
          <span className="text-sm">{proposal.name}</span>
          <span className="text-xs text-muted-foreground">
            {proposalSourceBadge(proposal.source, proposal.template_id)} ·{' '}
            {proposalStatusLabel(proposal.status)} ·{' '}
            {proposalDryRunLabel(proposal.dry_run_ok, proposal.dry_run_error)}
          </span>
        </div>
        {actions}
      </div>
      {proposal.dry_run_ok && (
        <div className="mt-2">
          <WorkflowGraphMini proposalId={proposal.id} />
        </div>
      )}
    </li>
  )
}
