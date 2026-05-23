import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState, type ReactNode } from 'react'
import { Link } from 'react-router-dom'

import { Button } from '@/components/ui/button'
import { WorkflowGraphMini } from '@/components/WorkflowGraph'
import { ApiError, api } from '@/lib/api'
import { isAdmin } from '@/lib/roles'
import type { WorkflowProposalPayload } from '@/lib/workflowProposal'
import {
  proposalDryRunLabel,
  proposalSourceBadge,
  proposalStatusLabel,
} from '@/lib/workflowProposalLabels'
import { useAuthStore } from '@/stores/auth'

export interface WorkflowProposalCardProps {
  payload: WorkflowProposalPayload
}

interface ConfirmResponse {
  proposal: { id: string; status: string; slug: string }
  summary: string
}

export function WorkflowProposalCard({ payload }: WorkflowProposalCardProps) {
  const token = useAuthStore((s) => s.token)
  const user = useAuthStore((s) => s.user)
  const qc = useQueryClient()
  const [toast, setToast] = useState<string | null>(null)
  const [err, setErr] = useState<string | null>(null)

  const admin = isAdmin(user)
  const canAct =
    payload.ok &&
    payload.dry_run_ok &&
    payload.proposal_id &&
    payload.status === 'draft'

  const confirmMut = useMutation({
    mutationFn: () =>
      api<ConfirmResponse>(
        `/agent/workflow/proposals/${encodeURIComponent(payload.proposal_id!)}/confirm`,
        { method: 'POST', token },
      ),
    onSuccess: (res) => {
      qc.invalidateQueries({ queryKey: ['workflows'] })
      setErr(null)
      if (res.proposal.status === 'published') {
        setToast(`已发布 workflow.${res.proposal.slug}`)
      } else {
        setToast('已提交审批，等待管理员发布')
      }
    },
    onError: (e) => setErr(humanError(e)),
  })

  if (!payload.ok) {
    return (
      <div className="flex pl-6">
        <CardShell className="border-destructive/40">
          <div className="font-medium text-destructive">工作流草案创建失败</div>
          <p className="mt-1 text-xs text-destructive">
            {payload.error ?? payload.detail ?? '未知错误'}
          </p>
        </CardShell>
      </div>
    )
  }

  const source = proposalSourceBadge(payload.source, payload.template_id)

  return (
    <div className="flex pl-6">
      <CardShell className="border-primary/30 bg-card">
        <div className="flex flex-wrap items-center gap-2">
          <span className="font-medium">工作流草案：{payload.name ?? payload.slug}</span>
          <span className="rounded-full bg-muted px-2 py-0.5 text-[10px] text-muted-foreground">
            {source}
          </span>
        </div>
        {payload.summary && (
          <p className="mt-1 text-xs text-muted-foreground">{payload.summary}</p>
        )}
        <p
          className={
            payload.dry_run_ok ? 'mt-2 text-xs text-emerald-600' : 'mt-2 text-xs text-destructive'
          }
        >
          {proposalDryRunLabel(!!payload.dry_run_ok, payload.dry_run_error)}
        </p>
        <p className="mt-1 text-[10px] text-muted-foreground">
          状态：{proposalStatusLabel(payload.status)}
        </p>

        {payload.proposal_id && payload.dry_run_ok && (
          <div className="mt-3">
            <WorkflowGraphMini proposalId={payload.proposal_id} />
          </div>
        )}

        <div className="mt-3 flex flex-wrap gap-2">
          {canAct && (
            <Button
              size="sm"
              disabled={confirmMut.isPending}
              onClick={() => confirmMut.mutate()}
            >
              {confirmMut.isPending ? '处理中…' : admin ? '确认发布' : '提交审批'}
            </Button>
          )}
          {admin && payload.slug && (
            <Button size="sm" variant="outline" asChild>
              <Link to="/workflows">在工作流页编辑</Link>
            </Button>
          )}
        </div>

        {toast && (
          <p className="mt-2 text-xs text-emerald-600" role="status">
            {toast}
          </p>
        )}
        {err && (
          <p className="mt-2 text-xs text-destructive" role="alert">
            {err}
          </p>
        )}
      </CardShell>
    </div>
  )
}

function CardShell({ children, className = '' }: { children: ReactNode; className?: string }) {
  return (
    <div className={`w-full max-w-[80%] rounded-md border p-3 text-xs ${className}`}>
      {children}
    </div>
  )
}

function humanError(e: unknown): string {
  if (e instanceof ApiError) {
    try {
      const j = JSON.parse(e.body) as { error?: string; detail?: string }
      if (j.detail) return j.detail
      if (j.error) return j.error
    } catch {
      /* ignore */
    }
    return e.body || e.message
  }
  return e instanceof Error ? e.message : String(e)
}
