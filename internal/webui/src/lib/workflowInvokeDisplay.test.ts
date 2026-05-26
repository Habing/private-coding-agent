import { describe, expect, it } from 'vitest'

import {
  buildInvokeTimeline,
  formatOutputsHuman,
  formatStepOutputHuman,
  liveStepTitle,
} from '@/lib/workflowInvokeDisplay'
import type { WorkflowDesign, WorkflowInvokeLiveStep } from '@/types/api'

const design: WorkflowDesign = {
  id: 'e2e',
  name: 'E2E',
  steps: [
    { id: 'status', kind: 'tool', tool: 'mcp.e2e-mock.fetch_status', args: [] },
    {
      id: 'pick',
      kind: 'assign',
      assignments: [{ var: 'health', expr: '${steps.status.output.content.0.text}' }],
    },
  ],
}

describe('workflowInvokeDisplay', () => {
  it('formatStepOutputHuman extracts MCP text', () => {
    expect(
      formatStepOutputHuman({ content: [{ type: 'text', text: 'degraded' }] }),
    ).toBe('结果：degraded')
  })

  it('buildInvokeTimeline includes pending design steps', () => {
    const live: WorkflowInvokeLiveStep[] = [
      { stepId: 'status', phase: 'ok', tool: 'mcp.e2e-mock.fetch_status', output: { content: [{ text: 'ok' }] } },
    ]
    const rows = buildInvokeTimeline(design, live, true)
    expect(rows).toHaveLength(2)
    expect(rows[0]?.phase).toBe('ok')
    expect(rows[0]?.title).toContain('status')
    expect(rows[1]?.phase).toBe('pending')
    expect(rows[1]?.kindLabel).toBe('设置变量')
  })

  it('liveStepTitle uses design label', () => {
    expect(
      liveStepTitle(design.steps[1], { stepId: 'pick', phase: 'running', stepKind: 'assign' }),
    ).toContain('设置变量')
  })

  it('formatOutputsHuman labels keys', () => {
    expect(formatOutputsHuman({ health: 'degraded', branch: 'degraded' })).toContain('健康状态')
  })
})
