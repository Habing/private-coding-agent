import type { BranchedStep } from 'sequential-workflow-designer'
import { describe, expect, it } from 'vitest'

import {
  buildSwdToPcaStepIdMap,
  designSyncFingerprint,
  designToSwdDefinition,
  findSwdCanvasIdForPcaStepId,
  mapSwdSelectionToPcaStepId,
  normalizeWorkflowDesign,
  swdDefinitionToDesign,
} from '@/lib/swdAdapter'
import type { WorkflowDesign } from '@/types/api'

const mockChainDesign: WorkflowDesign = {
  id: 'e2e-mock-chain',
  name: 'Mock 状态巡检',
  inputs: [
    {
      name: 'scenario',
      type: 'string',
      default: 'degraded',
    },
  ],
  steps: [
    {
      id: 'status',
      kind: 'tool',
      tool: 'mcp.e2e-mock.fetch_status',
      args: [{ name: 'scenario', value: '${inputs.scenario}', valueKind: 'expr' }],
    },
    {
      id: 'pick',
      kind: 'assign',
      assignments: [{ var: 'health', expr: '${steps.status.output.content.0.text}' }],
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
      then: [
        {
          id: 'record',
          kind: 'tool',
          tool: 'mcp.e2e-mock.record_event',
          args: [],
        },
        {
          id: 'alert',
          kind: 'tool',
          tool: 'mcp.e2e-mock.echo',
          args: [{ name: 'text', value: 'ALERT: system degraded', valueKind: 'literal' }],
        },
      ],
      else: [
        {
          id: 'ok_msg',
          kind: 'tool',
          tool: 'mcp.e2e-mock.echo',
          args: [{ name: 'text', value: 'OK: system healthy', valueKind: 'literal' }],
        },
      ],
    },
  ],
  outputs: [
    { name: 'health', expr: '${vars.health}' },
    { name: 'branch', expr: '${vars.health}' },
  ],
}

describe('swdAdapter round-trip', () => {
  it('preserves e2e-mock-chain step tree', () => {
    const def = designToSwdDefinition(mockChainDesign)
    expect(def.sequence).toHaveLength(3)
    expect(def.sequence[2]?.componentType).toBe('switch')

    const back = swdDefinitionToDesign(def, mockChainDesign)
    expect(back.steps).toHaveLength(3)
    expect(back.steps[0]?.id).toBe('status')
    expect(back.steps[1]?.kind).toBe('assign')
    expect(back.steps[2]?.kind).toBe('if')
    const gate = back.steps[2]
    if (gate?.kind === 'if') {
      expect(gate.condition).toEqual(mockChainDesign.steps[2]?.condition)
      expect(gate.then?.map((s) => s.id)).toEqual(['record', 'alert'])
      expect(gate.else?.map((s) => s.id)).toEqual(['ok_msg'])
    }
    expect(back.inputs).toEqual(mockChainDesign.inputs)
    expect(back.outputs).toEqual(mockChainDesign.outputs)
  })

  it('round-trips assign-only step', () => {
    const d: WorkflowDesign = {
      id: 'assign-only',
      name: 'Assign',
      steps: [
        {
          id: 'pick',
          kind: 'assign',
          assignments: [{ var: 'x', expr: '${inputs.a}' }],
        },
      ],
    }
    const back = swdDefinitionToDesign(designToSwdDefinition(d), d)
    expect(back.steps[0]?.kind).toBe('assign')
    expect(back.steps[0]?.assignments).toEqual(d.steps[0]?.assignments)
  })

  it('round-trips after adding a tool step to empty sequence', () => {
    const empty: WorkflowDesign = {
      id: 'demo',
      name: 'Demo',
      steps: [],
    }
    const def = designToSwdDefinition(empty)
    def.sequence.push({
      id: 's1',
      componentType: 'task',
      type: 'tool',
      name: 'e2e-mock.echo',
      properties: {
        stepId: 's1',
        tool: 'mcp.e2e-mock.echo',
        argsJson: '[]',
      },
    })
    const back = swdDefinitionToDesign(def, empty)
    expect(back.steps).toHaveLength(1)
    expect(back.steps[0]?.kind).toBe('tool')
    expect(back.steps[0]?.tool).toBe('mcp.e2e-mock.echo')
  })

  it('maps SWD hex selection id to PCA step id for detail panel', () => {
    const empty: WorkflowDesign = { id: 'w-1', name: 'W', steps: [] }
    const def = designToSwdDefinition(empty)
    const hex = '5d6bbe591450ad77b168bcd942bafee6'
    def.sequence.push({
      id: hex,
      componentType: 'task',
      type: 'tool',
      name: 'fetch_status',
      properties: {
        stepId: 'status',
        tool: 'mcp.e2e-mock.fetch_status',
        argsJson: JSON.stringify([{ name: 'scenario', value: '${inputs.scenario}', valueKind: 'expr' }]),
      },
    })
    const pca = mapSwdSelectionToPcaStepId(def, empty, hex)
    expect(pca).toBe('status')
    const map = buildSwdToPcaStepIdMap(def, empty)
    expect(map.get(hex)).toBe('status')
  })

  it('maps SWD hex canvas id to toolbox stepId property', () => {
    const empty: WorkflowDesign = { id: 'w-1', name: 'W', steps: [] }
    const def = designToSwdDefinition(empty)
    def.sequence.push({
      id: '5d6bbe591450ad77b168bcd942bafee6',
      componentType: 'task',
      type: 'tool',
      name: 'echo',
      properties: {
        stepId: 'step_tool',
        tool: 'mcp.e2e-mock.echo',
        argsJson: '[]',
      },
    })
    const back = swdDefinitionToDesign(def, empty)
    expect(back.steps[0]?.id).toBe('step_tool')
  })

  it('renames invalid SWD-only hex id to step_* fallback', () => {
    const empty: WorkflowDesign = { id: 'w-1', name: 'W', steps: [] }
    const def = designToSwdDefinition(empty)
    def.sequence.push({
      id: '5d6bbe591450ad77b168bcd942bafee6',
      componentType: 'task',
      type: 'tool',
      name: 'echo',
      properties: {
        tool: 'mcp.e2e-mock.echo',
        argsJson: '[]',
      },
    })
    const back = swdDefinitionToDesign(def, empty)
    expect(back.steps[0]?.id).toBe('step_mcp_e2e_mock_echo')
  })

  it('normalizeWorkflowDesign dedupes duplicate top-level step ids', () => {
    const d: WorkflowDesign = {
      id: 'wrong',
      name: 'W',
      steps: [
        { id: 'step_tool', kind: 'tool', tool: 'mcp.e2e-mock.echo', args: [] },
        { id: 'step_tool', kind: 'tool', tool: 'mcp.e2e-mock.echo', args: [] },
      ],
    }
    const n = normalizeWorkflowDesign(d, 'w-1')
    expect(n.id).toBe('w-1')
    expect(n.steps.map((s) => s.id)).toEqual(['step_tool', 'step_mcp_e2e_mock_echo'])
  })

  it('finds nested PCA step id on SWD canvas (hex id + properties.stepId)', () => {
    const base: WorkflowDesign = {
      id: 'w-1',
      name: 'W',
      steps: [
        {
          id: 'gate',
          kind: 'if',
          condition: { left: '1', op: 'eq', right: '1' },
          then: [{ id: 'inner', kind: 'assign', assignments: [] }],
          else: [],
        },
      ],
    }
    const def = designToSwdDefinition(base)
    const gate = def.sequence[0] as BranchedStep
    const hex = 'a1b2c3d4e5f6789012345678abcdef01'
    gate.branches.then[0] = {
      ...gate.branches.then[0]!,
      id: hex,
      properties: { ...gate.branches.then[0]!.properties, stepId: 'inner' },
    }
    expect(findSwdCanvasIdForPcaStepId(def, 'inner')).toBe(hex)
  })

  it('round-trips empty workflow', () => {
    const empty: WorkflowDesign = {
      id: 'demo',
      name: 'Demo',
      steps: [],
    }
    const back = swdDefinitionToDesign(designToSwdDefinition(empty), empty)
    expect(back.steps).toEqual([])
  })
})

/** Deep if nesting, orphan SWD steps, malformed JSON — regression for 21e-b / A2. */
describe('swdAdapter edge cases', () => {
  const deepNestedIfDesign: WorkflowDesign = {
    id: 'deep-if',
    name: 'Deep if',
    steps: [
      {
        id: 'outer',
        kind: 'if',
        condition: { left: '${vars.a}', op: 'eq', right: '1', rightKind: 'literal' },
        then: [
          {
            id: 'inner',
            kind: 'if',
            condition: { left: '${vars.b}', op: 'ne', right: '0', rightKind: 'literal' },
            then: [
              {
                id: 'leaf_then',
                kind: 'tool',
                tool: 'mcp.e2e-mock.echo',
                args: [{ name: 'text', value: 'nested-then', valueKind: 'literal' }],
              },
            ],
            else: [
              {
                id: 'leaf_else_inner',
                kind: 'assign',
                assignments: [{ var: 'flag', expr: '${inputs.x}' }],
              },
            ],
          },
        ],
        else: [
          {
            id: 'outer_else',
            kind: 'tool',
            tool: 'mcp.e2e-mock.record_event',
            args: [],
          },
        ],
      },
    ],
  }

  it('round-trips three-level nested if branches', () => {
    const def = designToSwdDefinition(deepNestedIfDesign)
    const outer = def.sequence[0] as BranchedStep
    expect(outer.componentType).toBe('switch')
    expect(outer.branches.then).toHaveLength(1)
    const inner = outer.branches.then[0] as BranchedStep
    expect(inner.componentType).toBe('switch')
    expect(inner.branches.then).toHaveLength(1)
    expect(inner.branches.else).toHaveLength(1)

    const back = swdDefinitionToDesign(def, deepNestedIfDesign)
    expect(back.steps).toHaveLength(1)
    const root = back.steps[0]
    expect(root?.kind).toBe('if')
    if (root?.kind !== 'if') return

    expect(root.condition).toEqual(deepNestedIfDesign.steps[0]?.condition)
    expect(root.then?.[0]?.id).toBe('inner')
    const innerBack = root.then?.[0]
    expect(innerBack?.kind).toBe('if')
    if (innerBack?.kind !== 'if') return

    expect(innerBack.then?.[0]?.id).toBe('leaf_then')
    expect(innerBack.then?.[0]?.tool).toBe('mcp.e2e-mock.echo')
    expect(innerBack.else?.[0]?.id).toBe('leaf_else_inner')
    expect(innerBack.else?.[0]?.assignments).toEqual([
      { var: 'flag', expr: '${inputs.x}' },
    ])
    expect(root.else?.[0]?.id).toBe('outer_else')
  })

  it('maps PCA ids through deeply nested branches', () => {
    const def = designToSwdDefinition(deepNestedIfDesign)
    const leafCanvasId = findSwdCanvasIdForPcaStepId(def, 'leaf_then')
    expect(leafCanvasId).toBeTruthy()
    expect(mapSwdSelectionToPcaStepId(def, deepNestedIfDesign, leafCanvasId)).toBe('leaf_then')

    const map = buildSwdToPcaStepIdMap(def, deepNestedIfDesign)
    expect([...map.values()]).toContain('leaf_then')
    expect([...map.values()]).toContain('leaf_else_inner')
    expect([...map.values()]).toContain('outer_else')
  })

  it('drops orphan SWD steps with unknown type from top-level sequence', () => {
    const base: WorkflowDesign = {
      id: 'orphan-top',
      name: 'Orphan',
      steps: [{ id: 'ok', kind: 'tool', tool: 'mcp.e2e-mock.echo', args: [] }],
    }
    const def = designToSwdDefinition(base)
    def.sequence.splice(1, 0, {
      id: 'orphan-hex',
      componentType: 'task',
      type: 'legacy-custom',
      name: 'Unknown',
      properties: { stepId: 'should_drop' },
    })
    const back = swdDefinitionToDesign(def, base)
    expect(back.steps).toHaveLength(1)
    expect(back.steps[0]?.id).toBe('ok')
    expect(mapSwdSelectionToPcaStepId(def, base, 'orphan-hex')).toBeNull()
  })

  it('drops orphan steps inside if branches but keeps valid siblings', () => {
    const base: WorkflowDesign = {
      id: 'orphan-branch',
      name: 'Orphan branch',
      steps: [
        {
          id: 'gate',
          kind: 'if',
          condition: { left: '1', op: 'eq', right: '1' },
          then: [{ id: 'good', kind: 'tool', tool: 'mcp.e2e-mock.echo', args: [] }],
          else: [],
        },
      ],
    }
    const def = designToSwdDefinition(base)
    const gate = def.sequence[0] as BranchedStep
    gate.branches.then.push({
      id: 'bad',
      componentType: 'task',
      type: 'not-a-pca-step',
      name: 'junk',
      properties: {},
    })
    const back = swdDefinitionToDesign(def, base)
    const gateBack = back.steps[0]
    expect(gateBack?.kind).toBe('if')
    if (gateBack?.kind !== 'if') return
    expect(gateBack.then?.map((s) => s.id)).toEqual(['good'])
  })

  it('tolerates malformed JSON in tool args and if condition', () => {
    const base: WorkflowDesign = { id: 'bad-json', name: 'Bad', steps: [] }
    const def = designToSwdDefinition(base)
    def.sequence.push({
      id: 't1',
      componentType: 'task',
      type: 'tool',
      name: 'tool',
      properties: { stepId: 't1', tool: 'mcp.e2e-mock.echo', argsJson: '{not-json' },
    })
    def.sequence.push({
      id: 'g1',
      componentType: 'switch',
      type: 'if',
      name: 'if',
      properties: { stepId: 'g1', conditionJson: '[]' },
      branches: { then: [], else: [] },
    })
    const back = swdDefinitionToDesign(def, base)
    expect(back.steps[0]?.kind).toBe('tool')
    if (back.steps[0]?.kind === 'tool') {
      expect(back.steps[0].args).toEqual([])
    }
    expect(back.steps[1]?.kind).toBe('if')
    if (back.steps[1]?.kind === 'if') {
      expect(back.steps[1].condition).toBeUndefined()
    }
  })

  it('round-trips empty then/else on if step', () => {
    const d: WorkflowDesign = {
      id: 'empty-branches',
      name: 'Empty branches',
      steps: [
        {
          id: 'gate',
          kind: 'if',
          condition: { left: 'true', op: 'eq', right: 'true', rightKind: 'literal' },
          then: [],
          else: [],
        },
      ],
    }
    const back = swdDefinitionToDesign(designToSwdDefinition(d), d)
    const gate = back.steps[0]
    expect(gate?.kind).toBe('if')
    if (gate?.kind === 'if') {
      expect(gate.then).toEqual([])
      expect(gate.else).toEqual([])
    }
  })

  it('designSyncFingerprint ignores arg edits but reacts to structure', () => {
    const a: WorkflowDesign = {
      id: 'fp',
      name: 'Fp',
      steps: [
        {
          id: 's1',
          kind: 'tool',
          tool: 'mcp.e2e-mock.echo',
          args: [{ name: 'text', value: 'v1', valueKind: 'literal' }],
        },
      ],
    }
    const b: WorkflowDesign = {
      ...a,
      steps: [
        {
          ...a.steps[0]!,
          args: [{ name: 'text', value: 'v2', valueKind: 'literal' }],
        },
      ],
    }
    const c: WorkflowDesign = {
      id: 'fp',
      name: 'Fp',
      steps: [
        {
          id: 's2',
          kind: 'tool',
          tool: 'mcp.e2e-mock.echo',
          args: [],
        },
      ],
    }
    expect(designSyncFingerprint(a)).toBe(designSyncFingerprint(b))
    expect(designSyncFingerprint(a)).not.toBe(designSyncFingerprint(c))
  })

  it('round-trips legacy pca-tool / pca-if SWD types', () => {
    const base: WorkflowDesign = { id: 'legacy', name: 'Legacy', steps: [] }
    const def = designToSwdDefinition(base)
    def.sequence.push({
      id: 't',
      componentType: 'task',
      type: 'pca-tool',
      name: 'tool',
      properties: { stepId: 'legacy_tool', tool: 'mcp.e2e-mock.echo', argsJson: '[]' },
    })
    def.sequence.push({
      id: 'i',
      componentType: 'switch',
      type: 'pca-if',
      name: 'if',
      properties: {
        stepId: 'legacy_if',
        conditionJson: JSON.stringify({ left: '1', op: 'eq', right: '2' }),
      },
      branches: {
        then: [
          {
            id: 'inner',
            componentType: 'task',
            type: 'pca-assign',
            name: 'assign',
            properties: {
              stepId: 'legacy_assign',
              assignmentsJson: JSON.stringify([{ var: 'v', expr: '1' }]),
            },
          },
        ],
        else: [],
      },
    })
    const back = swdDefinitionToDesign(def, base)
    expect(back.steps[0]?.id).toBe('legacy_tool')
    expect(back.steps[1]?.kind).toBe('if')
    if (back.steps[1]?.kind === 'if') {
      expect(back.steps[1].then?.[0]?.kind).toBe('assign')
    }
  })
})
