import { useMemo } from 'react'

import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import type { WorkflowInvokeState } from '@/hooks/useWorkflowInvoke'
import { fetchStatusUsesInvokeScenario } from '@/lib/workflowInvokeInputs'
import { looksLikeMissingHealthAssign, missingHealthAssignMessage } from '@/lib/workflowGateHealth'
import {
  buildInvokeTimeline,
  formatOutputsHuman,
  inputLabelZh,
  timelinePhaseLabel,
  type InvokeTimelinePhase,
  type InvokeTimelineRow,
} from '@/lib/workflowInvokeDisplay'
import { workflowRunStatusLabel } from '@/lib/uiLabels'
import type { WorkflowDesign } from '@/types/api'
import type { WorkflowInvokeResult } from '@/types/api'

import { InvokeInputsForm } from './InvokeInputsForm'

export function WorkflowExecuteButton({ invoke }: { invoke: WorkflowInvokeState }) {
  const { executing, tryExecute } = invoke
  return (
    <Button size="sm" className="shrink-0" disabled={executing} onClick={tryExecute}>
      {executing ? '执行中…' : '执行'}
    </Button>
  )
}

export function WorkflowInvokeControls({
  invoke,
  unsaved,
  liveDesign,
  onFixHealthChain,
}: {
  invoke: WorkflowInvokeState
  unsaved?: boolean
  liveDesign?: WorkflowDesign | null
  onFixHealthChain?: () => void
}) {
  const {
    schema,
    useForm,
    invokeValues,
    applyInvokeValues,
    inputsText,
    setInputsText,
    showJson,
    setShowJson,
    dryRun,
    setDryRun,
  } = invoke

  const scenarioIgnored =
    (liveDesign?.inputs ?? []).some((i) => i.name === 'scenario') &&
    !fetchStatusUsesInvokeScenario(liveDesign)

  const healthMissing = missingHealthAssignMessage(liveDesign)

  return (
    <div className="flex flex-col gap-2 rounded-md border p-3">
      <Label className="font-semibold">执行流程</Label>
      {healthMissing ? (
        <div className="rounded border border-amber-500/50 bg-amber-500/10 p-2 text-xs text-amber-900 dark:text-amber-200">
          <p>{healthMissing}</p>
          {onFixHealthChain ? (
            <Button type="button" size="sm" variant="secondary" className="mt-2" onClick={onFixHealthChain}>
              自动插入「设置变量」步骤（pick）
            </Button>
          ) : null}
        </div>
      ) : null}
      {scenarioIgnored ? (
        <p className="text-xs text-amber-700 dark:text-amber-400">
          第一步「查状态」未使用 <code className="font-mono">${'${inputs.scenario}'}</code>
          ，修改下方 scenario 不会改变执行结果。请在步骤详情中绑定试运行参数后保存。
        </p>
      ) : null}
      {unsaved ? (
        <p className="text-xs text-amber-700 dark:text-amber-400">
          有未保存修改：执行的是已保存版本，画布上多出的步骤不会运行。请先点「保存」。
        </p>
      ) : null}
      {useForm ? (
        <>
          <InvokeInputsForm
            schema={schema}
            values={invokeValues}
            onChange={applyInvokeValues}
          />
          <p className="text-[11px] text-muted-foreground">
            以下参数仅用于本次执行。修改工作流默认值请在「高级（YAML）」的 inputs 段编辑后保存。
          </p>
          <button
            type="button"
            className="text-left text-xs text-muted-foreground underline-offset-2 hover:underline"
            onClick={() => setShowJson((v) => !v)}
          >
            {showJson ? '隐藏 JSON' : '高级：JSON 编辑'}
          </button>
        </>
      ) : null}
      {(!useForm || showJson) && (
        <textarea
          className="min-h-[80px] rounded-md border bg-background p-2 font-mono text-xs"
          value={inputsText}
          onChange={(e) => setInputsText(e.target.value)}
          placeholder='{"scenario": "degraded"}'
        />
      )}
      <label className="flex items-center gap-2 text-xs">
        <input
          type="checkbox"
          checked={dryRun}
          onChange={(e) => setDryRun(e.target.checked)}
        />
        Dry-Run（可变更工具不真正执行）
      </label>
    </div>
  )
}

export function WorkflowInvokeResultPanel({
  invoke,
  liveDesign,
  highlightStepId,
}: {
  invoke: WorkflowInvokeState
  liveDesign?: WorkflowDesign | null
  /** 与画布高亮同步：当前执行或聚焦的步骤 id */
  highlightStepId?: string
}) {
  const { result, liveSteps, lastInvokedInputs, err, executing, streaming } = invoke

  const timeline = useMemo(
    () => buildInvokeTimeline(liveDesign, liveSteps, executing),
    [liveDesign, liveSteps, executing],
  )

  const hasActivity = timeline.length > 0 || executing || streaming
  const runMismatch =
    lastInvokedInputs?.scenario !== undefined &&
    looksLikeMissingHealthAssign(
      lastInvokedInputs.scenario,
      liveSteps.map((s) => s.stepId),
    )

  if (!result && !err && !hasActivity) return null

  const inputsSummary =
    lastInvokedInputs && Object.keys(lastInvokedInputs).length > 0
      ? Object.entries(lastInvokedInputs)
          .map(([k, v]) => `${inputLabelZh(k)}=${JSON.stringify(v)}`)
          .join('，')
      : null

  return (
    <div className="flex flex-col gap-2 rounded-md border p-3">
      <div className="flex items-center justify-between gap-2">
        <Label className="text-sm font-semibold">执行结果</Label>
        {executing ? (
          <span className="text-xs text-primary animate-pulse">流式更新中…</span>
        ) : null}
      </div>

      {inputsSummary ? (
        <p className="text-[11px] text-muted-foreground">本次参数：{inputsSummary}</p>
      ) : null}

      {runMismatch ? (
        <p className="text-xs text-amber-700 dark:text-amber-400">
          巡检场景为 degraded 却走了正常分支：通常缺少「设置变量」步骤。请插入 pick 并保存后重试。
        </p>
      ) : null}

      {timeline.length > 0 ? (
        <ol className="flex max-h-[min(420px,50vh)] flex-col gap-2 overflow-auto rounded-md border bg-muted/30 p-2">
          {timeline.map((row) => (
            <TimelineStepRow
              key={row.stepId}
              row={row}
              highlighted={highlightStepId === row.stepId}
            />
          ))}
        </ol>
      ) : executing ? (
        <p className="text-xs text-muted-foreground">等待首个步骤…</p>
      ) : null}

      {err ? <p className="text-xs text-destructive">执行失败：{err}</p> : null}
      {result ? <InvokeSummary result={result} /> : null}
    </div>
  )
}

function phaseStyles(phase: InvokeTimelinePhase, highlighted: boolean): string {
  const base = 'rounded-md border px-2.5 py-2 transition-colors'
  const ring = highlighted ? ' ring-2 ring-primary ring-offset-1' : ''
  switch (phase) {
    case 'running':
      return `${base} border-amber-500/60 bg-amber-500/10${ring}`
    case 'ok':
      return `${base} border-emerald-500/40 bg-emerald-500/5${ring}`
    case 'error':
      return `${base} border-destructive/50 bg-destructive/5${ring}`
    case 'skipped':
      return `${base} border-border/40 bg-background/50 opacity-60${ring}`
    default:
      return `${base} border-border/50 bg-background/80${ring}`
  }
}

function phaseTextClass(phase: InvokeTimelinePhase): string {
  switch (phase) {
    case 'running':
      return 'text-amber-700 dark:text-amber-400'
    case 'ok':
      return 'text-emerald-700 dark:text-emerald-400'
    case 'error':
      return 'text-destructive'
    case 'skipped':
      return 'text-muted-foreground'
    default:
      return 'text-muted-foreground'
  }
}

function TimelineStepRow({
  row,
  highlighted,
}: {
  row: InvokeTimelineRow
  highlighted: boolean
}) {
  return (
    <li className={phaseStyles(row.phase, highlighted)}>
      <div className="flex flex-wrap items-start justify-between gap-x-2 gap-y-0.5">
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
            <span className="text-[10px] font-medium text-muted-foreground">#{row.order}</span>
            <span className="text-sm font-medium leading-snug">{row.title}</span>
          </div>
          <p className="mt-0.5 text-[11px] text-muted-foreground">
            [{row.kindLabel}]
            {row.detail ? ` · ${row.detail}` : null}
          </p>
        </div>
        <span className={`shrink-0 text-xs font-medium ${phaseTextClass(row.phase)}`}>
          {timelinePhaseLabel(row.phase)}
        </span>
      </div>
      {row.outputText ? (
        <p className="mt-1.5 text-xs leading-relaxed text-foreground/90">{row.outputText}</p>
      ) : null}
      {row.error ? (
        <p className="mt-1 text-xs text-destructive">{row.error}</p>
      ) : null}
    </li>
  )
}

function InvokeSummary({ result }: { result: WorkflowInvokeResult }) {
  const outputsHuman = formatOutputsHuman(result.outputs)

  return (
    <div className="flex flex-col gap-2 border-t pt-2">
      <p className="text-xs font-medium">
        运行结束 · {workflowRunStatusLabel(result.status)}
        {result.dry_run ? (
          <span className="ml-1 font-normal text-muted-foreground">（Dry-Run）</span>
        ) : null}
        <span className="ml-2 font-normal text-muted-foreground">
          耗时 {result.duration_ms}ms · 共 {result.steps} 步
        </span>
      </p>
      {outputsHuman ? (
        <div className="rounded-md bg-muted/50 p-2 text-xs leading-relaxed whitespace-pre-wrap">
          <span className="font-medium text-muted-foreground">工作流输出</span>
          <div className="mt-1">{outputsHuman}</div>
        </div>
      ) : null}
      {result.error ? (
        <p className="text-xs text-destructive">{result.error}</p>
      ) : null}
      <details className="text-xs">
        <summary className="cursor-pointer text-muted-foreground hover:text-foreground">
          技术详情（JSON）
        </summary>
        <pre className="mt-1 max-h-48 overflow-auto rounded bg-muted p-2 font-mono text-[10px]">
          {JSON.stringify(result, null, 2)}
        </pre>
      </details>
    </div>
  )
}
