import type { WorkflowDesignAssign, WorkflowDesignCondition, WorkflowDesignStep } from '@/types/api'

import { toolDescriptionZh } from '@/lib/workflowDisplayZh'

/** 步骤类型中文（设计器右栏、执行摘要等）。 */
export const WORKFLOW_STEP_KIND_ZH: Record<string, string> = {
  tool: '调用工具',
  assign: '设置变量',
  if: '条件分支',
  foreach: '循环',
  parallel: '并行',
  wait: '等待',
  'trigger-cron': '定时触发',
  'trigger-webhook': 'Webhook 触发',
  trigger: '触发器',
}

export function stepKindLabelZh(kind: string): string {
  return WORKFLOW_STEP_KIND_ZH[kind] ?? kind
}

/** SWD / 画布节点主标题。 */
export function assignStepDisplayLabel(step: {
  assignments?: WorkflowDesignAssign[]
}): string {
  const vars = (step.assignments ?? [])
    .map((a) => a.var?.trim())
    .filter((v): v is string => Boolean(v))
  const base = WORKFLOW_STEP_KIND_ZH.assign
  if (vars.length === 0) return base
  if (vars.length === 1) return `${base} · ${vars[0]}`
  if (vars.length === 2) return `${base} · ${vars.join(', ')}`
  return `${base} · ${vars[0]} 等${vars.length}项`
}

const STEPS_OUTPUT_RE = /^\$\{steps\.([^.}]+)\.output/

/** 画布节点副标题：如 health ← status */
export function assignStepDescription(step: WorkflowDesignStep): string {
  const rows = step.assignments ?? []
  if (rows.length === 0) return step.id
  const parts = rows.map((a) => {
    const v = a.var?.trim()
    if (!v) return ''
    const m = a.expr?.trim().match(STEPS_OUTPUT_RE)
    if (m) return `${v} ← ${m[1]}`
    return v
  })
  return parts.filter(Boolean).join('；') || step.id
}

export function ifStepDisplayLabel(condition?: WorkflowDesignCondition): string {
  const base = WORKFLOW_STEP_KIND_ZH.if
  if (!condition?.left?.trim()) return base
  const left = condition.left.replace(/^\$\{|\}$/g, '')
  const right = condition.right?.replace(/^"|"$/g, '') ?? ''
  if (left.includes('vars.health') && right === 'degraded') return `${base} · health 为 degraded`
  if (left.includes('vars.health') && right === 'ok') return `${base} · health 为 ok`
  if (left.startsWith('vars.')) {
    const varName = left.replace(/^vars\./, '')
    return right ? `${base} · ${varName} ${condition.op ?? 'eq'} ${right}` : `${base} · ${varName}`
  }
  return base
}

/** tool 步骤画布标题：优先 stepId + 工具短名/中文说明。 */
export function toolStepDisplayLabel(step: WorkflowDesignStep): string {
  const id = step.id?.trim() || 'step'
  const tool = step.tool?.trim() ?? ''
  if (!tool) return id
  const short = tool.split('.').pop() ?? tool
  const zh = toolDescriptionZh(tool)
  if (zh) return `${id} · ${short}`
  return `${id} · ${tool}`
}

export function designStepCanvasLabel(step: WorkflowDesignStep): string {
  if (step.kind === 'assign') return assignStepDisplayLabel(step)
  if (step.kind === 'if') return ifStepDisplayLabel(step.condition)
  if (step.kind === 'tool') return toolStepDisplayLabel(step)
  return step.id
}

export function designStepCanvasDescription(step: WorkflowDesignStep): string {
  if (step.kind === 'assign') return assignStepDescription(step)
  if (step.kind === 'tool' && step.tool) {
    const zh = toolDescriptionZh(step.tool)
    return zh || step.tool
  }
  if (step.kind === 'if' && step.condition?.left) {
    return step.condition.left
  }
  return step.id
}
