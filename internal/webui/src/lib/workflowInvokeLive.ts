import type {
  WorkflowInvokeLiveStep,
  WorkflowInvokeStepEvent,
} from '@/types/api'

export function applyWorkflowStepEvent(
  steps: WorkflowInvokeLiveStep[],
  ev: WorkflowInvokeStepEvent,
): WorkflowInvokeLiveStep[] {
  const stepId = ev.step_id
  if (!stepId) return steps

  const idx = steps.findIndex((s) => s.stepId === stepId)

  if (ev.kind === 'step_start') {
    const row: WorkflowInvokeLiveStep = {
      stepId,
      stepKind: ev.step_kind,
      tool: ev.tool,
      phase: 'running',
    }
    if (idx >= 0) {
      const next = [...steps]
      next[idx] = { ...next[idx], ...row, phase: 'running' }
      return next
    }
    return [...steps, row]
  }

  if (ev.kind === 'step_complete') {
    const phase = ev.status === 'error' ? 'error' : 'ok'
    const row: WorkflowInvokeLiveStep = {
      stepId,
      stepKind: ev.step_kind,
      tool: ev.tool,
      phase,
      error: ev.error,
      output: ev.output,
    }
    if (idx >= 0) {
      const next = [...steps]
      next[idx] = { ...next[idx], ...row }
      return next
    }
    return [...steps, row]
  }

  return steps
}

export function formatStepOutputPreview(output: unknown, maxLen = 240): string {
  if (output === undefined || output === null) return ''
  let text: string
  try {
    text = typeof output === 'string' ? output : JSON.stringify(output)
  } catch {
    text = String(output)
  }
  if (text.length <= maxLen) return text
  return text.slice(0, maxLen) + '…'
}
