import { ApiError, api } from '@/lib/api'
import type { Workflow, WorkflowProposal } from '@/types/api'

export interface ProposalDesignerImport {
  proposalId: string
  slug: string
  name: string
  description: string
  dsl_yaml: string
}

export const PENDING_PROPOSAL_DESIGNER_KEY = 'workflow:pending-proposal-designer'

export type WorkflowsLocationState = {
  openProposalInDesigner?: { proposalId: string }
}

export function proposalToImport(p: WorkflowProposal): ProposalDesignerImport {
  return {
    proposalId: p.id,
    slug: p.slug,
    name: p.name,
    description: p.description ?? '',
    dsl_yaml: p.dsl_yaml,
  }
}

export function stashPendingProposalImport(imp: ProposalDesignerImport): void {
  sessionStorage.setItem(PENDING_PROPOSAL_DESIGNER_KEY, JSON.stringify(imp))
}

export function consumePendingProposalImport(): ProposalDesignerImport | null {
  const raw = sessionStorage.getItem(PENDING_PROPOSAL_DESIGNER_KEY)
  if (!raw) return null
  sessionStorage.removeItem(PENDING_PROPOSAL_DESIGNER_KEY)
  try {
    const o = JSON.parse(raw) as ProposalDesignerImport
    if (o?.slug && o?.dsl_yaml) return o
  } catch {
    /* ignore */
  }
  return null
}

export async function fetchWorkflowProposal(
  token: string,
  proposalId: string,
): Promise<WorkflowProposal> {
  const res = await api<{ proposal: WorkflowProposal }>(
    `/agent/workflow/proposals/${encodeURIComponent(proposalId)}`,
    { token },
  )
  return res.proposal
}

/** Resolve full import payload from list row or proposal id only. */
export async function loadProposalForDesigner(
  token: string,
  source: WorkflowProposal | { proposalId: string },
): Promise<ProposalDesignerImport> {
  if ('id' in source) {
    return proposalToImport(source)
  }
  const p = await fetchWorkflowProposal(token, source.proposalId)
  return proposalToImport(p)
}

/** Upsert unpublished workflow draft with proposal DSL (propose 通常已写入，此处再同步一次). */
export async function syncProposalToWorkflowDraft(
  token: string,
  imp: ProposalDesignerImport,
): Promise<Workflow> {
  try {
    return await api<Workflow>(`/admin/workflows/${encodeURIComponent(imp.slug)}`, {
      method: 'PUT',
      token,
      body: JSON.stringify({
        name: imp.name,
        description: imp.description,
        dsl_yaml: imp.dsl_yaml,
      }),
    })
  } catch (e) {
    if (e instanceof ApiError && e.status === 404) {
      return await api<Workflow>('/admin/workflows', {
        method: 'POST',
        token,
        body: JSON.stringify({
          slug: imp.slug,
          name: imp.name,
          description: imp.description,
          dsl_yaml: imp.dsl_yaml,
        }),
      })
    }
    throw e
  }
}
