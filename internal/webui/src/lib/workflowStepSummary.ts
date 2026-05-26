import type { WorkflowGraphDTO, WorkflowGraphNodeDTO } from '@/lib/workflowGraph'

import { WORKFLOW_STEP_KIND_ZH } from '@/lib/workflowStepKindZh'

const KIND_LABELS = WORKFLOW_STEP_KIND_ZH

/** orderedWorkflowStepNodes walks sequential edges from __start__ for display order. */
export function orderedWorkflowStepNodes(
  graph: WorkflowGraphDTO,
): WorkflowGraphNodeDTO[] {
  const nodeMap = new Map(graph.nodes.map((n) => [n.id, n]))
  const adj = new Map<string, string[]>()
  for (const e of graph.edges) {
    const list = adj.get(e.from) ?? []
    list.push(e.to)
    adj.set(e.from, list)
  }

  const result: WorkflowGraphNodeDTO[] = []
  const seen = new Set<string>()
  const queue = ['__start__']

  while (queue.length > 0) {
    const id = queue.shift()!
    if (seen.has(id)) continue
    seen.add(id)
    const n = nodeMap.get(id)
    if (n && n.kind !== 'start' && n.kind !== 'end') {
      result.push(n)
    }
    for (const next of adj.get(id) ?? []) {
      if (!seen.has(next)) queue.push(next)
    }
  }

  for (const n of graph.nodes) {
    if (!seen.has(n.id) && n.kind !== 'start' && n.kind !== 'end') {
      result.push(n)
    }
  }
  return result
}

export function formatWorkflowStep(node: WorkflowGraphNodeDTO, index: number): string {
  const kindLabel = KIND_LABELS[node.kind] ?? node.kind
  const primary = node.label || node.id
  const detail = node.detail ? ` — ${node.detail}` : ''
  return `${index + 1}. [${kindLabel}] ${primary}${detail}`
}
