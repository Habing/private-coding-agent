import { describe, expect, it } from 'vitest'

import {
  flattenSteps,
  getStepAtPath,
  insertStepAfter,
  moveStep,
  removeStepAtPath,
  reorderSiblings,
  updateStepAtPath,
} from '@/lib/workflowDesignTree'
import type { WorkflowDesign, WorkflowDesignStep } from '@/types/api'

const design: WorkflowDesign = {
  id: 'wf',
  name: 'WF',
  steps: [
    { id: 'status', kind: 'tool', tool: 'mcp.e2e-mock.fetch_status', args: [] },
    {
      id: 'gate',
      kind: 'if',
      condition: { left: '${vars.h}', op: 'eq', right: 'degraded' },
      then: [{ id: 'alert', kind: 'tool', tool: 'mcp.e2e-mock.echo', args: [] }],
      else: [{ id: 'ok_msg', kind: 'tool', tool: 'mcp.e2e-mock.echo', args: [] }],
    },
  ],
}

describe('workflowDesignTree', () => {
  it('flatten includes nested steps', () => {
    const flat = flattenSteps(design.steps)
    const ids = flat.map((f) => f.path)
    expect(ids).toContain('status')
    expect(ids).toContain('gate/then/alert')
    expect(ids).toContain('gate/else/ok_msg')
  })

  it('getStepAtPath nested', () => {
    const s = getStepAtPath(design, 'gate/then/alert')
    expect(s?.id).toBe('alert')
  })

  it('updateStepAtPath nested', () => {
    const updated: WorkflowDesignStep = {
      id: 'alert',
      kind: 'tool',
      tool: 'mcp.e2e-mock.echo',
      args: [{ name: 'text', value: 'ALERT', valueKind: 'literal' }],
    }
    const next = updateStepAtPath(design, 'gate/then/alert', updated)
    const s = getStepAtPath(next, 'gate/then/alert')
    expect(s?.args?.[0].value).toBe('ALERT')
  })

  it('removeStepAtPath nested', () => {
    const next = removeStepAtPath(design, 'gate/else/ok_msg')
    expect(getStepAtPath(next, 'gate/else/ok_msg')).toBeNull()
    expect(getStepAtPath(next, 'gate/then/alert')).not.toBeNull()
  })

  it('insertStepAfter top-level and branch', () => {
    const tool = {
      id: 'mid',
      kind: 'tool' as const,
      tool: 'mcp.e2e-mock.echo',
      args: [],
    }
    const top = insertStepAfter(design, 'status', tool)
    expect(top.steps.map((s) => s.id)).toEqual(['status', 'mid', 'gate'])
    const branch = insertStepAfter(top, 'gate/then/alert', {
      ...tool,
      id: 'between',
    })
    expect(getStepAtPath(branch, 'gate/then/between')).not.toBeNull()
    expect(getStepAtPath(branch, 'gate/then/alert')).not.toBeNull()
  })

  it('insertStepAfter __start__ prepends', () => {
    const step = {
      id: 'first',
      kind: 'tool' as const,
      tool: 'mcp.e2e-mock.echo',
      args: [],
    }
    const next = insertStepAfter(design, '__start__', step)
    expect(next.steps[0].id).toBe('first')
  })

  it('moveStep swaps siblings', () => {
    const next = moveStep(design, 'gate', 'up')
    expect(next?.steps.map((s) => s.id)).toEqual(['gate', 'status'])
  })

  it('reorderSiblings applies order', () => {
    const next = reorderSiblings(design, '', ['gate', 'status'])
    expect(next?.steps.map((s) => s.id)).toEqual(['gate', 'status'])
  })
})
