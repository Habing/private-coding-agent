import { flattenSteps } from '@/lib/workflowDesignTree'
import type { WorkflowDesign } from '@/types/api'

import { MCP_TEXT_OUTPUT_LABEL, MCP_TEXT_OUTPUT_SUFFIX } from '@/lib/exprUtil'

export interface ExprTreeNode {
  id: string
  label: string
  /** Expression path without ${} wrapper, e.g. inputs.scenario */
  path?: string
  /** Tooltip: technical path or full expression */
  hint?: string
  children?: ExprTreeNode[]
}

/** MCP tool step output paths commonly used in workflows. */
const MCP_OUTPUT_CHILDREN: ExprTreeNode[] = [
  {
    id: 'out-text',
    label: MCP_TEXT_OUTPUT_LABEL,
    path: MCP_TEXT_OUTPUT_SUFFIX,
    hint: `output.${MCP_TEXT_OUTPUT_SUFFIX}`,
  },
  { id: 'out-content', label: '完整 content 数组', path: 'content', hint: 'output.content' },
]

export function buildExprTree(
  design: WorkflowDesign,
  currentStepId?: string,
): ExprTreeNode[] {
  const roots: ExprTreeNode[] = []

  if ((design.inputs ?? []).length > 0) {
    roots.push({
      id: 'inputs',
      label: 'inputs',
      children: (design.inputs ?? []).map((inp) => ({
        id: `inputs.${inp.name}`,
        label: inp.label ?? inp.name,
        path: `inputs.${inp.name}`,
      })),
    })
  }

  const vars = new Set<string>()
  for (const { step } of flattenSteps(design.steps)) {
    if (step.kind === 'assign') {
      for (const a of step.assignments ?? []) {
        if (a.var) vars.add(a.var)
      }
    }
  }
  if (vars.size > 0) {
    roots.push({
      id: 'vars',
      label: 'vars',
      children: [...vars].sort().map((v) => ({
        id: `vars.${v}`,
        label: v,
        path: `vars.${v}`,
      })),
    })
  }

  const stepNodes: ExprTreeNode[] = []
  for (const { step } of flattenSteps(design.steps)) {
    if (step.id === currentStepId || step.kind !== 'tool') continue
    stepNodes.push({
      id: `steps.${step.id}`,
      label: step.id,
      children: [
        {
          id: `steps.${step.id}.output`,
          label: 'output（工具返回值）',
          children: MCP_OUTPUT_CHILDREN.map((c) => ({
            ...c,
            id: `steps.${step.id}.output.${c.id}`,
            path: `steps.${step.id}.output.${c.path}`,
            hint: c.hint ? `steps.${step.id}.${c.hint}` : `steps.${step.id}.output.${c.path}`,
          })),
        },
      ],
    })
  }
  if (stepNodes.length > 0) {
    roots.push({ id: 'steps', label: 'steps', children: stepNodes })
  }

  return roots
}

export { wrapExpr } from '@/lib/exprUtil'
