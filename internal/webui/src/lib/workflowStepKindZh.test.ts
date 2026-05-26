import { describe, expect, it } from 'vitest'

import {
  assignStepDescription,
  assignStepDisplayLabel,
  designStepCanvasLabel,
  ifStepDisplayLabel,
  toolStepDisplayLabel,
} from '@/lib/workflowStepKindZh'
import type { WorkflowDesignStep } from '@/types/api'

describe('workflowStepKindZh', () => {
  it('assignStepDisplayLabel includes var names', () => {
    expect(
      assignStepDisplayLabel({
        assignments: [{ var: 'health', expr: '${steps.status.output.content.0.text}' }],
      }),
    ).toBe('设置变量 · health')
  })

  it('assignStepDescription shows upstream step id', () => {
    const step: WorkflowDesignStep = {
      id: 'pick',
      kind: 'assign',
      assignments: [{ var: 'health', expr: '${steps.status.output.content.0.text}' }],
    }
    expect(assignStepDescription(step)).toBe('health ← status')
  })

  it('ifStepDisplayLabel recognizes health gate', () => {
    expect(
      ifStepDisplayLabel({
        left: '${vars.health}',
        op: 'eq',
        right: 'degraded',
        rightKind: 'literal',
      }),
    ).toBe('条件分支 · health 为 degraded')
  })

  it('toolStepDisplayLabel uses step id and tool short name', () => {
    const step: WorkflowDesignStep = {
      id: 'status',
      kind: 'tool',
      tool: 'mcp.e2e-mock.fetch_status',
      args: [],
    }
    expect(toolStepDisplayLabel(step)).toBe('status · fetch_status')
    expect(designStepCanvasLabel(step)).toBe('status · fetch_status')
  })
})
