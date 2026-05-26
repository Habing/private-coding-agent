import { describe, expect, it } from 'vitest'

import { buildExprTree, wrapExpr } from '@/lib/exprPickerTree'
import type { WorkflowDesign } from '@/types/api'

const design: WorkflowDesign = {
  id: 'wf',
  name: 'WF',
  inputs: [{ name: 'scenario', type: 'string', default: 'degraded' }],
  steps: [
    { id: 'status', kind: 'tool', tool: 'mcp.e2e-mock.fetch_status', args: [] },
    {
      id: 'pick',
      kind: 'assign',
      assignments: [{ var: 'health', expr: '${steps.status.output.content.0.text}' }],
    },
  ],
}

describe('exprPickerTree', () => {
  it('buildExprTree includes inputs vars and prior steps', () => {
    const tree = buildExprTree(design, 'pick')
    const inputs = tree.find((n) => n.id === 'inputs')
    expect(inputs?.children?.some((c) => c.path === 'inputs.scenario')).toBe(true)
    const vars = tree.find((n) => n.id === 'vars')
    expect(vars?.children?.some((c) => c.path === 'vars.health')).toBe(true)
    const steps = tree.find((n) => n.id === 'steps')
    expect(steps?.children?.some((c) => c.label === 'status')).toBe(true)
    expect(steps?.children?.some((c) => c.label === 'pick')).toBe(false)
  })

  it('wrapExpr adds braces', () => {
    expect(wrapExpr('inputs.scenario')).toBe('${inputs.scenario}')
    expect(wrapExpr('${already}')).toBe('${already}')
  })

  it('buildExprTree uses friendly MCP output label', () => {
    const tree = buildExprTree(design, 'pick')
    const steps = tree.find((n) => n.id === 'steps')
    const status = steps?.children?.find((c) => c.label === 'status')
    const output = status?.children?.find((c) => c.id === 'steps.status.output')
    const textLeaf = output?.children?.find((c) => c.path === 'steps.status.output.content.0.text')
    expect(textLeaf?.label).toMatch(/状态文本/)
  })
})
