import { describe, expect, it } from 'vitest'

import {
  formatWorkflowStep,
  orderedWorkflowStepNodes,
} from '@/lib/workflowStepSummary'
import type { WorkflowGraphDTO } from '@/lib/workflowGraph'

describe('orderedWorkflowStepNodes', () => {
  it('orders steps along sequential edges and skips start/end', () => {
    const graph: WorkflowGraphDTO = {
      meta: { id: 'demo' },
      nodes: [
        { id: '__start__', kind: 'start', label: '开始' },
        { id: 'a', kind: 'tool', label: 'http.fetch', detail: 'url=…' },
        { id: 'b', kind: 'assign', label: 'assign', detail: 'x' },
        { id: '__end__', kind: 'end', label: '结束' },
      ],
      edges: [
        { from: '__start__', to: 'a', type: 'sequential' },
        { from: 'a', to: 'b', type: 'sequential' },
        { from: 'b', to: '__end__', type: 'sequential' },
      ],
    }
    const steps = orderedWorkflowStepNodes(graph)
    expect(steps.map((s) => s.id)).toEqual(['a', 'b'])
  })
})

describe('formatWorkflowStep', () => {
  it('renders Chinese kind label with detail', () => {
    const line = formatWorkflowStep(
      { id: 'x', kind: 'tool', label: 'shell.run', detail: 'cmd=echo' },
      0,
    )
    expect(line).toBe('1. [调用工具] shell.run — cmd=echo')
  })
})
