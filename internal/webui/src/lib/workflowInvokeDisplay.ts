import { flattenSteps } from '@/lib/workflowDesignTree'
import {
  assignStepDescription,
  designStepCanvasLabel,
  stepKindLabelZh,
} from '@/lib/workflowStepKindZh'
import { toolDescriptionZh } from '@/lib/workflowDisplayZh'
import type {
  WorkflowDesign,
  WorkflowDesignStep,
  WorkflowInvokeLiveStep,
} from '@/types/api'

export type InvokeTimelinePhase = 'pending' | 'running' | 'ok' | 'error' | 'skipped'

export interface InvokeTimelineRow {
  stepId: string
  order: number
  phase: InvokeTimelinePhase
  /** 主标题，如「status · fetch_status」 */
  title: string
  /** 步骤类型 + 说明 */
  kindLabel: string
  detail?: string
  outputText?: string
  error?: string
}

const INPUT_LABEL_ZH: Record<string, string> = {
  scenario: '巡检场景',
}

const OUTPUT_LABEL_ZH: Record<string, string> = {
  health: '健康状态',
  branch: '分支',
}

export function inputLabelZh(name: string): string {
  return INPUT_LABEL_ZH[name] ?? name
}

export function outputLabelZh(name: string): string {
  return OUTPUT_LABEL_ZH[name] ?? name
}

export function timelinePhaseLabel(phase: InvokeTimelinePhase): string {
  switch (phase) {
    case 'pending':
      return '等待'
    case 'running':
      return '执行中'
    case 'ok':
      return '完成'
    case 'error':
      return '失败'
    case 'skipped':
      return '未执行'
    default:
      return phase
  }
}

function shortToolName(tool: string): string {
  const parts = tool.split('.')
  return parts[parts.length - 1] ?? tool
}

export function liveStepTitle(
  step: WorkflowDesignStep | undefined,
  live: WorkflowInvokeLiveStep,
): string {
  if (step) {
    return `${step.id} · ${designStepCanvasLabel(step)}`
  }
  const id = live.stepId
  if (live.tool) {
    const zh = toolDescriptionZh(live.tool)
    const short = shortToolName(live.tool)
    return zh ? `${id} · ${short}` : `${id} · ${live.tool}`
  }
  const kind = live.stepKind ?? ''
  if (kind === 'assign') return `${id} · 设置变量`
  if (kind === 'if') return `${id} · 条件分支`
  return id
}

export function liveStepDetail(
  step: WorkflowDesignStep | undefined,
  live?: WorkflowInvokeLiveStep,
): string | undefined {
  if (step?.kind === 'assign') {
    const d = assignStepDescription(step)
    return d !== step.id ? d : stepKindLabelZh('assign')
  }
  if (step?.kind === 'tool' && step.tool) {
    return toolDescriptionZh(step.tool) || undefined
  }
  if (live?.tool) return toolDescriptionZh(live.tool) || undefined
  if (step?.kind) return stepKindLabelZh(step.kind)
  if (live?.stepKind) return stepKindLabelZh(live.stepKind)
  return undefined
}

/** 将 MCP / 工具输出格式化为用户可读中文摘要。 */
export function formatStepOutputHuman(output: unknown): string {
  if (output === undefined || output === null) return ''
  if (typeof output === 'string') {
    return output.length > 320 ? output.slice(0, 320) + '…' : output
  }
  if (typeof output === 'object' && !Array.isArray(output)) {
    const o = output as Record<string, unknown>
    if (o.dry_run === true) {
      return 'Dry-Run：已模拟执行（未产生真实副作用）'
    }
    const content = o.content
    if (Array.isArray(content) && content.length > 0) {
      const first = content[0]
      if (first && typeof first === 'object' && 'text' in first) {
        const text = String((first as { text?: unknown }).text ?? '')
        return text ? `结果：${text}` : ''
      }
    }
    if (typeof o.recorded === 'boolean' && o.event_id) {
      return `已记录事件 ${o.event_id}${o.detail ? `（${o.detail}）` : ''}`
    }
  }
  try {
    const text = JSON.stringify(output, null, 2)
    return text.length > 360 ? text.slice(0, 360) + '…' : text
  } catch {
    return String(output)
  }
}

function resolvePhase(
  live: WorkflowInvokeLiveStep | undefined,
  executing: boolean,
): InvokeTimelinePhase {
  if (!live) return executing ? 'pending' : 'skipped'
  if (live.phase === 'running') return 'running'
  if (live.phase === 'error') return 'error'
  if (live.phase === 'ok') return 'ok'
  return executing ? 'pending' : 'skipped'
}

export function buildInvokeTimeline(
  design: WorkflowDesign | null | undefined,
  liveSteps: WorkflowInvokeLiveStep[],
  executing: boolean,
): InvokeTimelineRow[] {
  const liveMap = new Map(liveSteps.map((s) => [s.stepId, s]))

  if (design?.steps?.length) {
    const flat = flattenSteps(design.steps)
    return flat.map(({ step }, i) => {
      const live = liveMap.get(step.id)
      const phase = resolvePhase(live, executing)
      return {
        stepId: step.id,
        order: i + 1,
        phase,
        title: live ? liveStepTitle(step, live) : `${step.id} · ${designStepCanvasLabel(step)}`,
        kindLabel: stepKindLabelZh(step.kind),
        detail: liveStepDetail(step, live),
        outputText:
          live && live.phase !== 'running'
            ? formatStepOutputHuman(live.output)
            : undefined,
        error: live?.error,
      }
    })
  }

  return liveSteps.map((live, i) => ({
    stepId: live.stepId,
    order: i + 1,
    phase: resolvePhase(live, executing),
    title: liveStepTitle(undefined, live),
    kindLabel: live.stepKind ? stepKindLabelZh(live.stepKind) : '步骤',
    detail: liveStepDetail(undefined, live),
    outputText:
      live.phase !== 'running' ? formatStepOutputHuman(live.output) : undefined,
    error: live.error,
  }))
}

export function formatOutputsHuman(outputs: Record<string, unknown> | undefined): string {
  if (!outputs || Object.keys(outputs).length === 0) return ''
  return Object.entries(outputs)
    .map(([k, v]) => `${outputLabelZh(k)}：${typeof v === 'string' ? v : JSON.stringify(v)}`)
    .join('\n')
}
