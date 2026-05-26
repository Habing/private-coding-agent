import { describe, expect, it } from 'vitest'

import { designCompileBlockReason } from '@/lib/designCompileGate'
import type { WorkflowDesign } from '@/types/api'

describe('designCompileBlockReason', () => {
  it('blocks empty steps', () => {
    const d: WorkflowDesign = { id: 'w-1', name: 'W', steps: [] }
    expect(designCompileBlockReason(d)).toMatch(/至少需要一个步骤/)
  })

  it('blocks assign without bindings', () => {
    const d: WorkflowDesign = {
      id: 'w-1',
      name: 'W',
      steps: [{ id: 'a', kind: 'assign', assignments: [] }],
    }
    expect(designCompileBlockReason(d)).toMatch(/设置变量/)
  })

  it('blocks if with empty then branch', () => {
    const d: WorkflowDesign = {
      id: 'w-1',
      name: 'W',
      steps: [
        {
          id: 'step_if',
          kind: 'if',
          condition: { left: 'true', op: 'eq', right: 'true', rightKind: 'literal' },
          then: [],
          else: [],
        },
      ],
    }
    expect(designCompileBlockReason(d)).toMatch(/then 分支/)
  })

  it('allows valid tool step', () => {
    const d: WorkflowDesign = {
      id: 'w-1',
      name: 'W',
      steps: [{ id: 'a', kind: 'tool', tool: 'mcp.e2e-mock.echo', args: [] }],
    }
    expect(designCompileBlockReason(d)).toBeNull()
  })
})
