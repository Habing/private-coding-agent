import { describe, expect, it } from 'vitest'

import { layoutWorkflowGraph, type WorkflowGraphDTO } from './workflowGraph'

describe('layoutWorkflowGraph', () => {
  it('assigns positions to nodes and wires edges', () => {
    const graph: WorkflowGraphDTO = {
      meta: { id: 'demo' },
      nodes: [
        { id: '__start__', kind: 'start', label: '开始' },
        { id: 'a', kind: 'tool', label: 'shell.run' },
        { id: '__end__', kind: 'end', label: '结束' },
      ],
      edges: [
        { from: '__start__', to: 'a', type: 'sequential' },
        { from: 'a', to: '__end__', type: 'sequential' },
      ],
    }
    const { nodes, edges } = layoutWorkflowGraph(graph)
    expect(nodes).toHaveLength(3)
    expect(edges).toHaveLength(2)
    for (const n of nodes) {
      expect(Number.isFinite(n.position.x)).toBe(true)
      expect(Number.isFinite(n.position.y)).toBe(true)
    }
    expect(nodes.find((n) => n.id === 'a')?.type).toBe('workflow')
  })
})
