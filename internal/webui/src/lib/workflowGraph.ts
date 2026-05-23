import dagre from '@dagrejs/dagre'
import { MarkerType, type Edge, type Node } from '@xyflow/react'

export interface WorkflowGraphDTO {
  meta: { id?: string; name?: string; description?: string }
  inputs?: { name: string; type?: string }[]
  outputs?: { name: string; expr?: string }[]
  nodes: WorkflowGraphNodeDTO[]
  edges: WorkflowGraphEdgeDTO[]
}

export interface WorkflowGraphNodeDTO {
  id: string
  kind: string
  label: string
  detail?: string
  region?: string
}

export interface WorkflowGraphEdgeDTO {
  from: string
  to: string
  type: string
  label?: string
}

const NODE_WIDTH = 190
const NODE_HEIGHT = 64

/** layoutWorkflowGraph runs dagre top-to-bottom layout for React Flow. */
export function layoutWorkflowGraph(graph: WorkflowGraphDTO): {
  nodes: Node[]
  edges: Edge[]
} {
  const g = new dagre.graphlib.Graph()
  g.setDefaultEdgeLabel(() => ({}))
  g.setGraph({ rankdir: 'TB', nodesep: 48, ranksep: 72, marginx: 16, marginy: 16 })

  for (const n of graph.nodes) {
    g.setNode(n.id, { width: NODE_WIDTH, height: NODE_HEIGHT })
  }
  for (const e of graph.edges) {
    g.setEdge(e.from, e.to)
  }
  dagre.layout(g)

  const nodes: Node[] = graph.nodes.map((n) => {
    const pos = g.node(n.id)
    return {
      id: n.id,
      type: 'workflow',
      position: {
        x: pos.x - NODE_WIDTH / 2,
        y: pos.y - NODE_HEIGHT / 2,
      },
      data: { label: n.label, detail: n.detail, kind: n.kind },
      draggable: false,
      selectable: false,
    }
  })

  const edges: Edge[] = graph.edges.map((e, i) => ({
    id: `${e.from}-${e.to}-${i}`,
    source: e.from,
    target: e.to,
    label: e.label || undefined,
    animated: e.type === 'parallel',
    markerEnd: { type: MarkerType.ArrowClosed, width: 16, height: 16 },
    style:
      e.type === 'branch'
        ? { stroke: '#d97706' }
        : e.type === 'parallel'
          ? { stroke: '#ea580c' }
          : undefined,
  }))

  return { nodes, edges }
}

export const workflowNodeKindClass: Record<string, string> = {
  start: 'border-green-600 bg-green-50 dark:bg-green-950/30',
  end: 'border-slate-500 bg-slate-50 dark:bg-slate-900/40',
  tool: 'border-blue-600 bg-blue-50 dark:bg-blue-950/30',
  assign: 'border-violet-600 bg-violet-50 dark:bg-violet-950/30',
  if: 'border-amber-600 bg-amber-50 dark:bg-amber-950/30',
  foreach: 'border-purple-600 bg-purple-50 dark:bg-purple-950/30',
  parallel: 'border-orange-600 bg-orange-50 dark:bg-orange-950/30',
  wait: 'border-slate-600 bg-slate-50 dark:bg-slate-900/40',
}
