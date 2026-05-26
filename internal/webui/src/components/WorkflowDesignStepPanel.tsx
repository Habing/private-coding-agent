import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ScrollArea } from '@/components/ui/scroll-area'
import { ArgsForm, argsForTool } from '@/components/workflow/ArgsForm'
import { ExprField } from '@/components/workflow/ExprField'
import { ToolPicker } from '@/components/workflow/ToolPicker'
import {
  getStepAtPath,
  stepPathFromStepId,
  updateStepAtPath,
  type StepPath,
} from '@/lib/workflowDesignTree'
import { healthFromStatusAssign } from '@/lib/exprUtil'
import { WORKFLOW_STEP_KIND_ZH } from '@/lib/workflowStepKindZh'
import {
  buildAssignExprPresets,
  preferredStatusSourceStep,
} from '@/lib/workflowExprPresets'
import {
  HEALTH_DEGRADED_CONDITION,
  HEALTH_OK_CONDITION,
  isSuspiciousLiteralIfCondition,
  normalizeIfCondition,
} from '@/lib/workflowIfCondition'
import type {
  ToolSchemaEntry,
  WorkflowDesign,
  WorkflowDesignAssign,
  WorkflowDesignCondition,
  WorkflowDesignStep,
} from '@/types/api'

const STEP_KIND_LABELS = WORKFLOW_STEP_KIND_ZH

const CONDITION_OPS: { value: string; label: string }[] = [
  { value: 'eq', label: '等于 (==)' },
  { value: 'ne', label: '不等于 (!=)' },
  { value: 'lt', label: '小于 (<)' },
  { value: 'le', label: '小于等于 (<=)' },
  { value: 'gt', label: '大于 (>)' },
  { value: 'ge', label: '大于等于 (>=)' },
]

export interface WorkflowDesignStepPanelProps {
  design: WorkflowDesign
  selectedStepId?: string
  tools: ToolSchemaEntry[]
  toolsLoading?: boolean
  onStepChange: (design: WorkflowDesign) => void
}

export function WorkflowDesignStepPanel({
  design,
  selectedStepId,
  tools,
  toolsLoading,
  onStepChange,
}: WorkflowDesignStepPanelProps) {
  if (!selectedStepId) {
    return (
      <div className="flex h-full min-h-[200px] flex-col items-center justify-center rounded-md border border-dashed p-4 text-center">
        <p className="text-sm text-muted-foreground">在左侧画布中选中一步</p>
        <p className="mt-1 text-xs text-muted-foreground">
          工具参数、设置变量与条件在此编辑；分支内子步骤请用画布拖入与排序。
        </p>
      </div>
    )
  }

  const stepPath = stepPathFromStepId(design, selectedStepId)
  if (!stepPath) {
    return (
      <p className="p-3 text-sm text-muted-foreground">
        未找到步骤「{selectedStepId}」，可能已被删除。
      </p>
    )
  }

  const step = getStepAtPath(design, stepPath)
  if (!step) {
    return <p className="p-3 text-sm text-muted-foreground">无法加载步骤详情。</p>
  }

  const pathForUpdate: StepPath = stepPath
  const commit = (updated: WorkflowDesignStep) => {
    onStepChange(updateStepAtPath(design, pathForUpdate, updated))
  }

  return (
    <ScrollArea className="h-full max-h-[min(360px,50vh)] rounded-md border">
      <div className="flex flex-col gap-3 p-3">
        <div>
          <p className="font-mono text-xs text-muted-foreground">{selectedStepId}</p>
          <p className="text-sm font-medium">{STEP_KIND_LABELS[step.kind] ?? step.kind}</p>
        </div>

        {step.kind === 'tool' ? (
          <ToolStepEditor
            design={design}
            step={step}
            tools={tools}
            toolsLoading={toolsLoading}
            onCommit={commit}
          />
        ) : null}

        {step.kind === 'assign' ? (
          <AssignStepEditor design={design} step={step} onCommit={commit} />
        ) : null}

        {step.kind === 'if' ? (
          <IfStepEditor design={design} step={step} onCommit={commit} />
        ) : null}

        {step.kind !== 'tool' && step.kind !== 'assign' && step.kind !== 'if' ? (
          <p className="text-xs text-muted-foreground">暂不支持此步骤类型的可视化编辑。</p>
        ) : null}
      </div>
    </ScrollArea>
  )
}

function ToolStepEditor({
  design,
  step,
  tools,
  toolsLoading,
  onCommit,
}: {
  design: WorkflowDesign
  step: WorkflowDesignStep
  tools: ToolSchemaEntry[]
  toolsLoading?: boolean
  onCommit: (s: WorkflowDesignStep) => void
}) {
  const tool = step.tool ?? ''

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-col gap-1">
        <Label className="text-xs">工具</Label>
        {toolsLoading ? (
          <p className="text-xs text-muted-foreground">加载工具列表…</p>
        ) : (
          <ToolPicker
            tools={tools}
            value={tool}
            onChange={(nextTool) => {
              onCommit({
                ...step,
                tool: nextTool,
                args: argsForTool(nextTool, tools, step.args ?? []),
              })
            }}
          />
        )}
      </div>
      <div className="flex flex-col gap-1">
        <Label className="text-xs">参数</Label>
        <ArgsForm
          design={design}
          currentStepId={step.id}
          tool={tool}
          tools={tools}
          args={step.args ?? []}
          onChange={(args) => onCommit({ ...step, args })}
        />
      </div>
    </div>
  )
}

function AssignStepEditor({
  design,
  step,
  onCommit,
}: {
  design: WorkflowDesign
  step: WorkflowDesignStep
  onCommit: (s: WorkflowDesignStep) => void
}) {
  const rows = step.assignments ?? []
  const presets = buildAssignExprPresets(design, step.id)
  const statusSource = preferredStatusSourceStep(design, step.id)

  function setRows(assignments: WorkflowDesignAssign[]) {
    onCommit({ ...step, assignments })
  }

  function applyPreset(preset: { varName: string; expr: string }, rowIndex?: number) {
    const target =
      rowIndex !== undefined && rowIndex >= 0 && rowIndex < rows.length ? rowIndex : rows.length - 1
    if (target >= 0 && target < rows.length) {
      const next = [...rows]
      next[target] = {
        var: preset.varName || rows[target].var,
        expr: preset.expr,
      }
      setRows(next)
      return
    }
    setRows([
      ...rows,
      { var: preset.varName || 'health', expr: preset.expr },
    ])
  }

  function applyHealthFromStatus() {
    if (!statusSource) return
    const row = healthFromStatusAssign(statusSource.id)
    const existing = rows.findIndex((r) => r.var === 'health')
    if (existing >= 0) {
      const next = [...rows]
      next[existing] = row
      setRows(next)
      return
    }
    if (rows.length === 1 && !rows[0].var?.trim() && !rows[0].expr?.trim()) {
      setRows([row])
      return
    }
    setRows([...rows, row])
  }

  return (
    <div className="flex flex-col gap-2">
      <Label className="text-xs">变量列表</Label>
      <p className="text-[11px] leading-snug text-muted-foreground">
        无需手写 <span className="font-mono">content.0.text</span>：用下方快捷项，或表达式旁的「插入变量」。
      </p>
      {statusSource ? (
        <Button
          type="button"
          size="sm"
          variant="secondary"
          className="h-8 justify-start text-xs"
          onClick={applyHealthFromStatus}
        >
          一键：health ← 「{statusSource.id}」状态文本
        </Button>
      ) : (
        <p className="text-[11px] text-muted-foreground">
          请在本步骤之前添加「调用工具」（如 fetch_status），即可出现一键设置。
        </p>
      )}
      {presets.length > 0 ? (
        <div className="flex flex-wrap gap-1">
          {presets.map((p) => (
            <Button
              key={p.id}
              type="button"
              size="sm"
              variant="outline"
              className="h-7 max-w-full truncate text-[11px]"
              title={p.expr}
              onClick={() => applyPreset({ varName: p.varName, expr: p.expr })}
            >
              {p.label}
            </Button>
          ))}
        </div>
      ) : null}
      {rows.length === 0 ? (
        <p className="text-xs text-muted-foreground">暂无变量行，点击下方添加。</p>
      ) : null}
      {rows.map((row, i) => (
        <div key={i} className="flex flex-col gap-1 rounded border border-border/60 p-2">
          <div className="grid grid-cols-[88px_1fr_auto] items-center gap-2">
            <Input
              className="font-mono text-xs h-8"
              placeholder="变量名"
              value={row.var}
              onChange={(e) => {
                const next = [...rows]
                next[i] = { ...row, var: e.target.value }
                setRows(next)
              }}
            />
            <ExprField
              design={design}
              currentStepId={step.id}
              value={row.expr}
              onChange={(expr) => {
                const next = [...rows]
                next[i] = { ...row, expr }
                setRows(next)
              }}
            />
            <Button
              type="button"
              size="sm"
              variant="ghost"
              className="shrink-0 px-2"
              onClick={() => setRows(rows.filter((_, j) => j !== i))}
            >
              删
            </Button>
          </div>
          {presets.length > 0 ? (
            <div className="flex flex-wrap gap-1 pl-0.5">
              {presets.map((p) => (
                <Button
                  key={`${i}-${p.id}`}
                  type="button"
                  size="sm"
                  variant="ghost"
                  className="h-6 px-2 text-[10px] text-muted-foreground"
                  title={p.expr}
                  onClick={() => applyPreset({ varName: p.varName || row.var, expr: p.expr }, i)}
                >
                  填入：{p.sourceStepId}
                </Button>
              ))}
            </div>
          ) : null}
        </div>
      ))}
      <Button
        type="button"
        size="sm"
        variant="secondary"
        onClick={() => setRows([...rows, { var: '', expr: '' }])}
      >
        添加变量
      </Button>
    </div>
  )
}

function IfStepEditor({
  design,
  step,
  onCommit,
}: {
  design: WorkflowDesign
  step: WorkflowDesignStep
  onCommit: (s: WorkflowDesignStep) => void
}) {
  const cond: WorkflowDesignCondition = step.condition ?? {
    left: '',
    op: 'eq',
    right: '',
    rightKind: 'literal',
  }
  const suspicious = isSuspiciousLiteralIfCondition(cond)

  function patch(partial: Partial<WorkflowDesignCondition>) {
    const next = normalizeIfCondition({ ...cond, ...partial })
    onCommit({ ...step, condition: next })
  }

  return (
    <div className="flex flex-col gap-3">
      <Label className="text-xs">条件</Label>
      <p className="text-[11px] leading-snug text-muted-foreground">
        巡检分支推荐用变量 <span className="font-mono">vars.health</span>（需前面有「设置变量」步骤）。
      </p>
      <div className="flex flex-wrap gap-1">
        <Button
          type="button"
          size="sm"
          variant="secondary"
          className="h-7 text-[11px]"
          onClick={() => onCommit({ ...step, condition: { ...HEALTH_DEGRADED_CONDITION } })}
        >
          当 health 为 degraded
        </Button>
        <Button
          type="button"
          size="sm"
          variant="outline"
          className="h-7 text-[11px]"
          onClick={() => onCommit({ ...step, condition: { ...HEALTH_OK_CONDITION } })}
        >
          当 health 为 ok
        </Button>
      </div>
      <div className="flex flex-col gap-1">
        <span className="text-[11px] text-muted-foreground">左值</span>
        <ExprField
          design={design}
          currentStepId={step.id}
          value={cond.left}
          onChange={(left) => patch({ left })}
        />
      </div>
      <div className="flex flex-col gap-1">
        <span className="text-[11px] text-muted-foreground">运算符</span>
        <select
          className="rounded border bg-background px-2 py-1 text-sm"
          value={cond.op}
          onChange={(e) => patch({ op: e.target.value })}
        >
          {CONDITION_OPS.map((o) => (
            <option key={o.value} value={o.value}>
              {o.label}
            </option>
          ))}
        </select>
      </div>
      <div className="flex flex-col gap-1">
        <span className="text-[11px] text-muted-foreground">右值</span>
        <ExprField
          design={design}
          currentStepId={step.id}
          value={cond.right}
          onChange={(right) =>
            patch({
              right,
              rightKind:
                right.includes('${') || /^(inputs|vars|steps)\./.test(right.trim())
                  ? 'expr'
                  : 'literal',
            })
          }
        />
      </div>
      {suspicious ? (
        <div className="rounded border border-amber-500/50 bg-amber-500/10 p-2 text-xs text-amber-900 dark:text-amber-200">
          <p>
            当前条件比较的是两个固定字符串（如 ok 与 degraded），结果不会随「巡检场景」变化，通常会一直走
            else 分支。左值应使用变量，例如 <code className="font-mono">${'${vars.health}'}</code>。
          </p>
          <Button
            type="button"
            size="sm"
            variant="secondary"
            className="mt-2"
            onClick={() => onCommit({ ...step, condition: { ...HEALTH_DEGRADED_CONDITION } })}
          >
            修复 gate 条件（需已有「设置变量」步骤）
          </Button>
        </div>
      ) : null}
      <p className="text-xs text-muted-foreground">
        then / else 分支内的步骤请在画布中拖入与调整顺序。
      </p>
    </div>
  )
}
