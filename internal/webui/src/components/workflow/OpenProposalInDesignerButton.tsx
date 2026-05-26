import { useState } from 'react'
import { useNavigate } from 'react-router-dom'

import { Button } from '@/components/ui/button'
import {
  loadProposalForDesigner,
  proposalToImport,
  stashPendingProposalImport,
  type ProposalDesignerImport,
  type WorkflowsLocationState,
} from '@/lib/proposalDesignerImport'
import { useAuthStore } from '@/stores/auth'
import type { WorkflowProposal } from '@/types/api'

export interface OpenProposalInDesignerButtonProps {
  proposalId: string
  /** When already on Workflows page, pass handler to avoid navigation. */
  onOpen?: (imp: ProposalDesignerImport) => void | Promise<void>
  disabled?: boolean
  size?: 'sm'
  variant?: 'default' | 'outline' | 'secondary' | 'ghost'
  className?: string
}

export function OpenProposalInDesignerButton({
  proposalId,
  onOpen,
  disabled,
  size = 'sm',
  variant = 'outline',
  className,
}: OpenProposalInDesignerButtonProps) {
  const token = useAuthStore((s) => s.token)
  const navigate = useNavigate()
  const [busy, setBusy] = useState(false)

  async function handleClick() {
    if (!token || !proposalId) return
    setBusy(true)
    try {
      const imp = await loadProposalForDesigner(token, { proposalId })
      if (onOpen) {
        await onOpen(imp)
        return
      }
      stashPendingProposalImport(imp)
      navigate('/workflows', {
        state: { openProposalInDesigner: { proposalId } } satisfies WorkflowsLocationState,
      })
    } finally {
      setBusy(false)
    }
  }

  return (
    <Button
      type="button"
      size={size}
      variant={variant}
      className={className}
      disabled={disabled || busy || !proposalId}
      onClick={() => void handleClick()}
    >
      {busy ? '打开中…' : '在设计器中打开'}
    </Button>
  )
}

/** Inline open when proposal row already has dsl_yaml. */
export function OpenProposalRowInDesignerButton({
  proposal,
  onOpen,
  disabled,
}: {
  proposal: WorkflowProposal
  onOpen: (imp: ProposalDesignerImport) => void | Promise<void>
  disabled?: boolean
}) {
  const [busy, setBusy] = useState(false)

  return (
    <Button
      type="button"
      size="sm"
      variant="outline"
      disabled={disabled || busy || !proposal.dry_run_ok}
      onClick={() => {
        setBusy(true)
        void Promise.resolve(onOpen(proposalToImport(proposal))).finally(() =>
          setBusy(false),
        )
      }}
    >
      {busy ? '打开中…' : '在设计器中打开'}
    </Button>
  )
}
