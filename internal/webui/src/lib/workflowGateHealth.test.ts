import { describe, expect, it } from 'vitest'

import {
  ensureHealthAssignStep,
  hasHealthAssignBeforeGate,
  looksLikeMissingHealthAssign,
  missingHealthAssignMessage,
} from './workflowGateHealth'
import type { WorkflowDesign } from '@/types/api'

const designNoPick: WorkflowDesign = {
  id: 'x',
  name: 'X',
  steps: [
    {
      id: 'status',
      kind: 'tool',
      tool: 'mcp.e2e-mock.fetch_status',
      args: [{ name: 'scenario', value: '${inputs.scenario}', valueKind: 'expr' }],
    },
    {
      id: 'gate',
      kind: 'if',
      condition: {
        left: '${vars.health}',
        op: 'eq',
        right: 'degraded',
        rightKind: 'literal',
      },
      then: [{ id: 'record', kind: 'tool', tool: 'mcp.e2e-mock.record_event', args: [] }],
      else: [{ id: 'ok_msg', kind: 'tool', tool: 'mcp.e2e-mock.echo', args: [] }],
    },
  ],
}

describe('workflowGateHealth', () => {
  it('detects missing pick before gate', () => {
    expect(missingHealthAssignMessage(designNoPick)).toContain('vars.health')
    expect(hasHealthAssignBeforeGate(designNoPick)).toBe(false)
  })

  it('inserts pick after status', () => {
    const next = ensureHealthAssignStep(designNoPick)
    expect(next.steps.map((s) => s.id)).toEqual(['status', 'pick', 'gate'])
    expect(hasHealthAssignBeforeGate(next)).toBe(true)
  })

  it('detects degraded run that only hit ok_msg', () => {
    expect(looksLikeMissingHealthAssign('degraded', ['status', 'gate', 'ok_msg'])).toBe(true)
    expect(looksLikeMissingHealthAssign('ok', ['status', 'gate', 'ok_msg'])).toBe(false)
  })
})
