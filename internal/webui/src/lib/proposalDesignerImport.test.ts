import { describe, expect, it } from 'vitest'

import {
  consumePendingProposalImport,
  proposalToImport,
  stashPendingProposalImport,
} from '@/lib/proposalDesignerImport'
import type { WorkflowProposal } from '@/types/api'

const sample: WorkflowProposal = {
  id: 'p1',
  tenant_id: 't1',
  slug: 'e2e-mock-chain',
  name: 'Mock 巡检',
  description: 'desc',
  dsl_yaml: 'id: e2e-mock-chain\nsteps: []\n',
  source: 'freeform',
  dry_run_ok: true,
  status: 'draft',
  created_at: '',
  updated_at: '',
}

describe('proposalDesignerImport', () => {
  it('proposalToImport maps fields', () => {
    const imp = proposalToImport(sample)
    expect(imp.slug).toBe('e2e-mock-chain')
    expect(imp.dsl_yaml).toContain('e2e-mock-chain')
  })

  it('stash and consume pending import', () => {
    stashPendingProposalImport(proposalToImport(sample))
    const got = consumePendingProposalImport()
    expect(got?.slug).toBe('e2e-mock-chain')
    expect(consumePendingProposalImport()).toBeNull()
  })
})
