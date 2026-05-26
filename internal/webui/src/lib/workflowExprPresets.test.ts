import { describe, expect, it } from 'vitest'

import { healthFromStatusAssign, mcpTextOutputExpr } from '@/lib/exprUtil'
import {
  buildAssignExprPresets,
  preferredStatusSourceStep,
  priorToolSteps,
} from '@/lib/workflowExprPresets'
import type { WorkflowDesign } from '@/types/api'

const design: WorkflowDesign = {
  id: 'e2e',
  name: 'E2E',
  steps: [
    { id: 'status', kind: 'tool', tool: 'mcp.e2e-mock.fetch_status', args: [] },
    { id: 'pick', kind: 'assign', assignments: [] },
    { id: 'gate', kind: 'if', condition: { left: '', op: 'eq', right: '' }, then: [], else: [] },
  ],
}

describe('workflowExprPresets', () => {
  it('priorToolSteps returns only upstream tools', () => {
    expect(priorToolSteps(design, 'pick').map((s) => s.id)).toEqual(['status'])
    expect(priorToolSteps(design, 'status')).toEqual([])
  })

  it('preferredStatusSourceStep prefers fetch_status', () => {
    expect(preferredStatusSourceStep(design, 'pick')?.id).toBe('status')
  })

  it('buildAssignExprPresets builds health expr from status', () => {
    const presets = buildAssignExprPresets(design, 'pick')
    expect(presets).toHaveLength(1)
    expect(presets[0].varName).toBe('health')
    expect(presets[0].expr).toBe('${steps.status.output.content.0.text}')
  })

  it('healthFromStatusAssign matches golden yaml', () => {
    expect(healthFromStatusAssign('status')).toEqual({
      var: 'health',
      expr: mcpTextOutputExpr('status'),
    })
  })
})
