import { describe, expect, it } from 'vitest'

import { applyWorkflowStepEvent } from './workflowInvokeLive'

describe('applyWorkflowStepEvent', () => {
  it('appends on step_start and updates on step_complete', () => {
    let steps = applyWorkflowStepEvent([], {
      kind: 'step_start',
      step_id: 'status',
      tool: 'mcp.e2e-mock.fetch_status',
    })
    expect(steps).toHaveLength(1)
    expect(steps[0]?.phase).toBe('running')

    steps = applyWorkflowStepEvent(steps, {
      kind: 'step_complete',
      step_id: 'status',
      status: 'ok',
      output: { content: [{ text: 'degraded' }] },
    })
    expect(steps[0]?.phase).toBe('ok')
    expect(steps[0]?.output).toEqual({ content: [{ text: 'degraded' }] })
  })
})
