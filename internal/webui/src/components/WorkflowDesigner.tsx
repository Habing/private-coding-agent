import { useMutation, useQuery } from '@tanstack/react-query'
import { useCallback, useEffect, useRef, useState } from 'react'

import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { SequentialWorkflowDesignerPane } from '@/components/SequentialWorkflowDesignerPane'
import { WorkflowDesignStepPanel } from '@/components/WorkflowDesignStepPanel'
import { ApiError, api } from '@/lib/api'
import { designCompileBlockReason } from '@/lib/designCompileGate'
import { normalizeWorkflowDesign } from '@/lib/swdAdapter'
import { buildInvokeDefaults, stepPathFromStepId } from '@/lib/workflowDesignTree'
import { useAuthStore } from '@/stores/auth'
import type {
  DesignCompileResponse,
  DesignDecompileResponse,
  ToolSchemasResponse,
  WorkflowDesign,
} from '@/types/api'

const EMPTY_DESIGN = (id: string, name: string): WorkflowDesign => ({
  id,
  name,
  inputs: [],
  steps: [],
  outputs: [],
})

/** Debounce YAML compile while typing in the right panel (assignments, args, inputs). */
const DESIGN_COMPILE_DEBOUNCE_MS = 450

export interface WorkflowDesignerProps {
  workflowId: string
  workflowVersion: number
  workflowSlug: string
  workflowName: string
  description: string
  dsl: string
  onDslChange: (yaml: string) => void
  onNameChange?: (name: string) => void
  onDescriptionChange?: (desc: string) => void
  onInvokeDefaultsChange?: (defaults: Record<string, unknown>) => void
  /** False when compile failed — parent should block save. */
  onCompileOkChange?: (ok: boolean) => void
  onCompileError?: (message: string | null) => void
  /** Canvas only; step panel and meta fields live in parent sidebar. */
  embedCanvasOnly?: boolean
  /** Current design model (for external step detail panel). */
  onDesignSnapshot?: (design: WorkflowDesign) => void
  /** Register pushDesign for sidebar step / inputs editors. */
  onRegisterDesignMutator?: (push: (design: WorkflowDesign) => void) => void
  selectedStepId?: string
  onSelectedStepIdChange?: (stepId: string | undefined) => void
  focusStepId?: string | null
  /** Bumped per streamed step so SWD re-applies viewport focus. */
  runFocusTick?: number
  /** Shown on the canvas toolbar row (right-aligned), e.g. workflow execute. */
  canvasToolbarEnd?: React.ReactNode
}

export function WorkflowDesigner({
  workflowId,
  workflowVersion,
  workflowSlug,
  workflowName,
  description,
  dsl,
  onDslChange,
  onNameChange,
  onDescriptionChange,
  onInvokeDefaultsChange,
  onCompileOkChange,
  onCompileError,
  embedCanvasOnly = false,
  onDesignSnapshot,
  onRegisterDesignMutator,
  selectedStepId: selectedStepIdProp,
  onSelectedStepIdChange,
  focusStepId,
  runFocusTick,
  canvasToolbarEnd,
}: WorkflowDesignerProps) {
  const token = useAuthStore((s) => s.token)
  const [selectedStepIdInternal, setSelectedStepIdInternal] = useState<string | undefined>()
  const selectedStepId = selectedStepIdProp ?? selectedStepIdInternal
  const setSelectedStepId = useCallback(
    (id: string | undefined) => {
      if (onSelectedStepIdChange) onSelectedStepIdChange(id)
      else setSelectedStepIdInternal(id)
    },
    [onSelectedStepIdChange],
  )
  const [design, setDesign] = useState<WorkflowDesign | null>(null)
  const [loadErr, setLoadErr] = useState<string | null>(null)
  const [decompilePending, setDecompilePending] = useState(false)
  const skipDecompileRef = useRef(false)
  const lastLoadKeyRef = useRef('')
  const decompileSeqRef = useRef(0)
  const compileTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const onInvokeDefaultsChangeRef = useRef(onInvokeDefaultsChange)
  onInvokeDefaultsChangeRef.current = onInvokeDefaultsChange
  const workflowVersionRef = useRef(workflowVersion)
  workflowVersionRef.current = workflowVersion

  const toolSchemasQ = useQuery({
    queryKey: ['workflow-tool-schemas'],
    queryFn: () =>
      api<ToolSchemasResponse>('/admin/workflows/tool-schemas', { token }),
    enabled: !!token,
    staleTime: 60_000,
  })

  const compileMut = useMutation({
    mutationFn: (d: WorkflowDesign) =>
      api<DesignCompileResponse>('/admin/workflows/design/compile', {
        method: 'POST',
        token,
        body: JSON.stringify({ design: d, slug: workflowSlug }),
      }),
    onSuccess: (res) => {
      skipDecompileRef.current = true
      lastLoadKeyRef.current = `${workflowId}:${workflowVersionRef.current}:${res.dsl_yaml}`
      onCompileOkChange?.(true)
      onCompileError?.(null)
      onDslChange(res.dsl_yaml)
    },
    onError: (e) => {
      onCompileOkChange?.(false)
      onCompileError?.(humanCompileError(e))
    },
  })

  useEffect(() => {
    if (skipDecompileRef.current) {
      skipDecompileRef.current = false
      return
    }

    const loadKey = `${workflowId}:${workflowVersion}:${dsl}`

    if (!dsl.trim()) {
      lastLoadKeyRef.current = loadKey
      setDecompilePending(false)
      setLoadErr(null)
      const empty = EMPTY_DESIGN(workflowSlug, workflowName)
      setDesign(empty)
      onInvokeDefaultsChangeRef.current?.({})
      return
    }

    if (loadKey === lastLoadKeyRef.current) {
      return
    }

    const seq = ++decompileSeqRef.current
    const timer = window.setTimeout(() => {
      setDecompilePending(true)
      setLoadErr(null)
      void api<DesignDecompileResponse>('/admin/workflows/design/decompile', {
        method: 'POST',
        token,
        body: JSON.stringify({ dsl_yaml: dsl }),
      })
        .then((res) => {
          if (seq !== decompileSeqRef.current) return
          lastLoadKeyRef.current = loadKey
          setDesign(res.design)
          onInvokeDefaultsChangeRef.current?.(buildInvokeDefaults(res.design.inputs))
        })
        .catch((e) => {
          if (seq !== decompileSeqRef.current) return
          setLoadErr(humanError(e))
        })
        .finally(() => {
          if (seq === decompileSeqRef.current) {
            setDecompilePending(false)
          }
        })
    }, 300)

    return () => {
      window.clearTimeout(timer)
      decompileSeqRef.current += 1
    }
  }, [workflowId, workflowVersion, workflowSlug, dsl, workflowName, token])

  const pushDesign = useCallback(
    (next: WorkflowDesign) => {
      const normalized = normalizeWorkflowDesign(next, workflowSlug)
      setDesign(normalized)
      onDesignSnapshot?.(normalized)
      onNameChange?.(normalized.name)
      onInvokeDefaultsChange?.(buildInvokeDefaults(normalized.inputs))
      const block = designCompileBlockReason(normalized)
      if (block) {
        if (compileTimerRef.current) {
          clearTimeout(compileTimerRef.current)
          compileTimerRef.current = null
        }
        onCompileOkChange?.(false)
        onCompileError?.(block)
        return
      }
      if (compileTimerRef.current) clearTimeout(compileTimerRef.current)
      compileTimerRef.current = setTimeout(() => {
        compileTimerRef.current = null
        compileMut.mutate(normalized)
      }, DESIGN_COMPILE_DEBOUNCE_MS)
    },
    [
      compileMut,
      onCompileError,
      onCompileOkChange,
      onDesignSnapshot,
      onInvokeDefaultsChange,
      onNameChange,
      workflowSlug,
    ],
  )

  useEffect(() => {
    return () => {
      if (compileTimerRef.current) clearTimeout(compileTimerRef.current)
    }
  }, [])

  useEffect(() => {
    onRegisterDesignMutator?.(pushDesign)
  }, [onRegisterDesignMutator, pushDesign])

  useEffect(() => {
    if (design) onDesignSnapshot?.(design)
  }, [design, onDesignSnapshot])

  useEffect(() => {
    if (!design || !selectedStepId) return
    if (!stepPathFromStepId(design, selectedStepId)) {
      // During invoke streaming, `focusStepId` should keep canvas selection stable.
      // Otherwise React may clear `selectedStepId` before SWD can map it to a canvas node.
      if (focusStepId) return
      setSelectedStepId(undefined)
    }
  }, [design, selectedStepId, focusStepId])

  if (loadErr) {
    return (
      <p className="text-sm text-destructive">
        无法解析为设计器模型：{loadErr}。请使用「高级（YAML）」编辑。
      </p>
    )
  }

  if (!design || decompilePending) {
    return <p className="text-sm text-muted-foreground">加载设计器…</p>
  }

  return (
    <div className="flex flex-col gap-3">
      {!embedCanvasOnly ? (
        <div className="grid gap-2 sm:grid-cols-2">
          <div className="flex flex-col gap-1">
            <Label>名称</Label>
            <Input
              value={design.name}
              onChange={(e) => {
                const name = e.target.value
                pushDesign({ ...design, name })
              }}
            />
          </div>
          <div className="flex flex-col gap-1 sm:col-span-2">
            <Label>描述</Label>
            <textarea
              className="min-h-[48px] rounded-md border bg-background p-2 text-sm"
              value={description}
              onChange={(e) => onDescriptionChange?.(e.target.value)}
            />
          </div>
        </div>
      ) : null}

      <div>
        <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
          <Label className="mb-0">流程画布</Label>
          {embedCanvasOnly && canvasToolbarEnd ? (
            <div className="ml-auto flex shrink-0 items-center">{canvasToolbarEnd}</div>
          ) : null}
        </div>
        <p className="mb-2 text-xs text-muted-foreground">
          从左侧工具箱拖入步骤；选中节点后在侧栏「步骤详情」编辑参数。条件分支在 switch 节点展开
          then/else。执行时画布会跟随高亮当前步骤。
        </p>
        <div
          className={
            embedCanvasOnly
              ? 'h-[720px] min-h-[720px]'
              : 'flex h-[720px] min-h-[720px] gap-3'
          }
        >
          <div className="flex h-full min-h-0 min-w-0 flex-1 flex-col">
            <SequentialWorkflowDesignerPane
              design={design}
              height={720}
              tools={toolSchemasQ.data?.tools}
              selectedStepId={selectedStepId ?? null}
              focusStepId={focusStepId}
              runFocusTick={runFocusTick}
              onSelectedStepIdChange={(id) => setSelectedStepId(id ?? undefined)}
              onDesignChange={(next) => pushDesign(next)}
            />
          </div>
          {!embedCanvasOnly ? (
            <div className="flex w-[360px] shrink-0 flex-col gap-1">
              <Label className="text-xs text-muted-foreground">步骤详情</Label>
              <WorkflowDesignStepPanel
                design={design}
                selectedStepId={selectedStepId}
                tools={toolSchemasQ.data?.tools ?? []}
                toolsLoading={toolSchemasQ.isLoading}
                onStepChange={pushDesign}
              />
            </div>
          ) : null}
        </div>
      </div>

      {compileMut.isError && (
        <p className="text-sm text-destructive">{humanError(compileMut.error)}</p>
      )}
    </div>
  )
}

function humanCompileError(e: unknown): string {
  if (e instanceof ApiError) {
    try {
      const j = JSON.parse(e.body) as { error?: string; detail?: string }
      if (j.detail) return j.detail
      if (j.error) return j.error
    } catch {
      /* fall through */
    }
    return e.body || e.message
  }
  return e instanceof Error ? e.message : String(e)
}

function humanError(e: unknown): string {
  if (e instanceof ApiError) return e.message
  if (e instanceof Error) return e.message
  return String(e)
}
