import { healthFromStatusAssign } from '@/lib/exprUtil'
import { insertStepAfter } from '@/lib/workflowDesignTree'
import { HEALTH_DEGRADED_CONDITION } from '@/lib/workflowIfCondition'
import type { WorkflowDesign, WorkflowDesignStep } from '@/types/api'

/** gate 条件是否引用 vars.health */
export function gateUsesVarHealth(design: WorkflowDesign | null | undefined): boolean {
  const gate = design?.steps?.find((s) => s.kind === 'if')
  const left = gate?.condition?.left ?? ''
  return left.includes('vars.health')
}

/** 在 gate 之前是否有 assign health */
export function hasHealthAssignBeforeGate(design: WorkflowDesign): boolean {
  const gateIdx = design.steps.findIndex((s) => s.kind === 'if')
  if (gateIdx <= 0) return false
  return design.steps.slice(0, gateIdx).some(
    (s) =>
      s.kind === 'assign' &&
      (s.assignments ?? []).some((a) => a.var === 'health' && a.expr?.trim()),
  )
}

export function missingHealthAssignMessage(design: WorkflowDesign | null | undefined): string | null {
  if (!design || !gateUsesVarHealth(design) || hasHealthAssignBeforeGate(design)) return null
  return (
    '条件分支使用了 vars.health，但前面没有「设置变量」步骤（YAML 里常命名为 pick）把查状态结果写入 health。' +
    'gate 会一直走 else（ok_msg），与 scenario 无关。请点下方按钮自动插入，或从左侧工具箱拖入「设置变量」。'
  )
}

/** 在 fetch_status 与 gate 之间插入 pick（e2e-mock-chain 标准结构） */
export function ensureHealthAssignStep(design: WorkflowDesign): WorkflowDesign {
  if (hasHealthAssignBeforeGate(design)) return design

  const statusStep = design.steps.find(
    (s) => s.kind === 'tool' && (s.tool?.includes('fetch_status') ?? false),
  )
  const statusId = statusStep?.id ?? 'status'

  const pick: WorkflowDesignStep = {
    id: design.steps.some((s) => s.id === 'pick') ? 'pick_assign' : 'pick',
    kind: 'assign',
    assignments: [healthFromStatusAssign(statusId)],
  }

  let next = insertStepAfter(design, statusId, pick)

  const gateIdx = next.steps.findIndex((s) => s.kind === 'if')
  if (gateIdx >= 0) {
    const gate = next.steps[gateIdx]
    if (gate.kind === 'if' && !gate.condition?.left?.includes('vars.health')) {
      const steps = [...next.steps]
      steps[gateIdx] = { ...gate, condition: { ...HEALTH_DEGRADED_CONDITION } }
      next = { ...next, steps }
    }
  }

  return next
}

/** 执行结果是否像「缺 pick」：scenario 与最终分支明显不一致 */
export function looksLikeMissingHealthAssign(
  scenario: unknown,
  liveStepIds: string[],
): boolean {
  const sc = String(scenario ?? '')
  const ran = new Set(liveStepIds)
  if (sc === 'degraded' && ran.has('ok_msg') && !ran.has('record') && !ran.has('alert')) {
    return true
  }
  return false
}
