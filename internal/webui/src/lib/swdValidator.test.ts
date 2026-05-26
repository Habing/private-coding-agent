import type { Step } from 'sequential-workflow-designer'
import { describe, expect, it } from 'vitest'

import {
  buildAllowedToolSet,
  buildSwdValidatorConfiguration,
  isSwdToolStepValid,
  isToolSwdStep,
} from '@/lib/swdValidator'

function toolStep(tool: string): Step {
  return {
    id: 's1',
    componentType: 'task',
    type: 'tool',
    name: '调用工具',
    properties: { stepId: 's1', tool, argsJson: '[]' },
  }
}

describe('swdValidator', () => {
  it('buildAllowedToolSet dedupes by name', () => {
    const set = buildAllowedToolSet([
      { name: 'mcp.a.x', description: '', parameters: {}, mutating: false },
      { name: 'mcp.a.x', description: '', parameters: {}, mutating: false },
      { name: 'llm.chat', description: '', parameters: {}, mutating: false },
    ])
    expect([...set].sort()).toEqual(['llm.chat', 'mcp.a.x'])
  })

  it('isToolSwdStep matches tool types only', () => {
    expect(isToolSwdStep(toolStep('mcp.a.x'))).toBe(true)
    expect(
      isToolSwdStep({
        id: 'a',
        componentType: 'task',
        type: 'assign',
        name: '设置变量',
        properties: {},
      }),
    ).toBe(false)
  })

  it('flags unknown tool when allowlist loaded', () => {
    const allowed = new Set(['mcp.e2e-mock.echo'])
    expect(isSwdToolStepValid(toolStep('mcp.e2e-mock.echo'), allowed)).toBe(true)
    expect(isSwdToolStepValid(toolStep('mcp.unknown.tool'), allowed)).toBe(false)
  })

  it('skips allowlist check while schemas empty', () => {
    const allowed = new Set<string>()
    expect(isSwdToolStepValid(toolStep('mcp.unknown.tool'), allowed)).toBe(true)
  })

  it('empty tool name is invalid', () => {
    const allowed = new Set(['mcp.a.x'])
    expect(isSwdToolStepValid(toolStep(''), allowed)).toBe(false)
    expect(isSwdToolStepValid(toolStep('   '), allowed)).toBe(false)
  })

  it('buildSwdValidatorConfiguration wires step validator', () => {
    const cfg = buildSwdValidatorConfiguration(new Set(['mcp.ok']))
    expect(cfg.step?.(toolStep('mcp.ok'), [] as never, {} as never)).toBe(true)
    expect(cfg.step?.(toolStep('mcp.bad'), [] as never, {} as never)).toBe(false)
  })
})
