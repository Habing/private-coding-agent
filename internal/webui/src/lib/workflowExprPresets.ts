import { mcpTextOutputExpr } from '@/lib/exprUtil'
import { flattenSteps } from '@/lib/workflowDesignTree'
import type { WorkflowDesign, WorkflowDesignStep } from '@/types/api'

/** Tool steps that run before `currentStepId` in flattened execution order. */
export function priorToolSteps(
  design: WorkflowDesign,
  currentStepId: string,
): WorkflowDesignStep[] {
  const flat = flattenSteps(design.steps)
  const idx = flat.findIndex((f) => f.step.id === currentStepId)
  if (idx < 0) return []
  return flat
    .slice(0, idx)
    .filter((f) => f.step.kind === 'tool')
    .map((f) => f.step)
}

/** Prefer nearest upstream fetch_status; else last prior tool step. */
export function preferredStatusSourceStep(
  design: WorkflowDesign,
  currentStepId: string,
): WorkflowDesignStep | undefined {
  const prior = priorToolSteps(design, currentStepId)
  return (
    [...prior].reverse().find((s) => s.tool?.includes('fetch_status')) ??
    prior[prior.length - 1]
  )
}

export interface AssignExprPreset {
  id: string
  label: string
  varName: string
  expr: string
  sourceStepId: string
}

export function buildAssignExprPresets(
  design: WorkflowDesign,
  currentStepId: string,
): AssignExprPreset[] {
  return priorToolSteps(design, currentStepId).map((s) => ({
    id: `from-${s.id}`,
    label: `状态文本 ← ${s.id}`,
    varName: s.tool?.includes('fetch_status') ? 'health' : '',
    expr: mcpTextOutputExpr(s.id),
    sourceStepId: s.id,
  }))
}
