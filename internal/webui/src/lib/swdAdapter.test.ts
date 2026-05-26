import type { BranchedStep } from 'sequential-workflow-designer'
import { describe, expect, it } from 'vitest'

import {
  buildSwdToPcaStepIdMap,
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
