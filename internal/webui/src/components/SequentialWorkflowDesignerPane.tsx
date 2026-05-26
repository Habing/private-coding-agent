import { DefinitionChangeType, type StepsConfiguration, type ValidatorConfiguration } from 'sequential-workflow-designer'
import {
  SequentialWorkflowDesigner,
  useSequentialWorkflowDesignerController,
  useStepEditor,
  wrapDefinition,
  type WrappedDefinition,
} from 'sequential-workflow-designer-react'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

import { Label } from '@/components/ui/label'
import { usePrefersColorScheme } from '@/hooks/usePrefersColorScheme'
import {
  designSyncFingerprint,
  designToSwdDefinition,
  mapSwdSelectionToPcaStepId,
  mapPcaStepIdToSwdCanvasId,
  swdDefinitionToDesign,
  type PcaSwdDefinition,
} from '@/lib/swdAdapter'
import { focusPcaStepOnSwd } from '@/lib/swdFocus'
import { buildAllowedToolSet, buildSwdValidatorConfiguration } from '@/lib/swdValidator'
import { buildSwdToolbox } from '@/lib/swdToolboxDynamic'
import type { ToolSchemaEntry, WorkflowDesign } from '@/types/api'

/** Canvas gestures that should round-trip to PCA design + /design/compile. */
const CANVAS_COMPILE_CHANGE_TYPES = new Set<DefinitionChangeType>([
  DefinitionChangeType.stepInserted,
  DefinitionChangeType.stepMoved,
  DefinitionChangeType.stepDeleted,
  DefinitionChangeType.stepChildrenChanged,
])

export interface SequentialWorkflowDesignerPaneProps {
  design: WorkflowDesign
  height?: number
  tools?: ToolSchemaEntry[]
  /** Lift selection to parent for the right panel; do not pass back into SWD (avoids destroy races). */
  onSelectedStepIdChange?: (stepId: string | null) => void
  /** PCA step id for right panel sync; mapped to SWD canvas id for selection highlight. */
  selectedStepId?: string | null
  /** When set, pans viewport to the step (e.g. during invoke). */
  focusStepId?: string | null
  runFocusTick?: number
  onDesignChange: (design: WorkflowDesign) => void
}

function SwdRootEditorHint() {
  return (
    <p className="p-2 text-xs text-muted-foreground">
      工作流名称请在上方表单编辑；步骤参数请在右侧「步骤详情」面板修改。
    </p>
  )
}

function SwdStepEditor() {
  const editor = useStepEditor()
  const { name, setName, isReadonly } = editor

  return (
    <div className="flex flex-col gap-2 p-2 text-sm">
      <div className="flex flex-col gap-1">
        <Label className="text-xs">画布显示名称</Label>
        <input
          className="rounded-md border bg-background px-2 py-1"
          value={name}
          disabled={isReadonly}
          onChange={(e) => setName(e.target.value)}
        />
      </div>
      <p className="text-xs text-muted-foreground">
        步骤 ID: <span className="font-mono">{editor.id}</span>
        <br />
        工具、参数与条件请在右侧「步骤详情」编辑。
      </p>
    </div>
  )
}

export function SequentialWorkflowDesignerPane({
  design,
  height = 720,
  tools,
  onSelectedStepIdChange,
  selectedStepId,
  focusStepId,
  runFocusTick,
  onDesignChange,
}: SequentialWorkflowDesignerPaneProps) {
  const colorScheme = usePrefersColorScheme()
  const baseRef = useRef(design)
  baseRef.current = design

  const structureKey = useMemo(() => designSyncFingerprint(design), [design])

  // Keep the SWD definition prop aligned with the designer's internal state. A stale
  // parent prop after toolbox drop makes the React wrapper destroy/recreate the canvas
  // while mouseUp is still running → "Root component not found".
  const [definition, setDefinition] = useState(() =>
    wrapDefinition(designToSwdDefinition(design)),
  )

  const saveTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const skipReplaceRef = useRef(false)
  const replaceInFlightRef = useRef(false)

  const toolsKey = useMemo(
    () => (tools ?? []).map((t) => t.name).sort().join('|'),
    [tools],
  )
  const controller = useSequentialWorkflowDesignerController()

  const toolboxConfiguration = useMemo(() => buildSwdToolbox(tools), [toolsKey])

  const stepsConfiguration = useMemo<StepsConfiguration>(
    () => ({
      iconUrlProvider: () => null,
    }),
    [],
  )

  const validatorConfiguration = useMemo<ValidatorConfiguration>(
    () => buildSwdValidatorConfiguration(buildAllowedToolSet(tools)),
    [toolsKey],
  )

  const highlightPcaStepId = focusStepId ?? selectedStepId ?? null
  // During invoke streaming, focusPcaStepOnSwd calls selectStepById; skip the prop to avoid
  // racing with setFolderPath / updateRootComponent ("Cannot find single sequence in folder step").
  const swdSelectedStepId = useMemo(() => {
    if (focusStepId) return undefined
    return (
      mapPcaStepIdToSwdCanvasId(definition.value, baseRef.current, highlightPcaStepId) ??
      undefined
    )
  }, [definition, highlightPcaStepId, focusStepId])

  const definitionRef = useRef(definition)
  definitionRef.current = definition

  const handleSwdSelection = useCallback(
    (swdCanvasStepId: string | null) => {
      const pcaStepId = mapSwdSelectionToPcaStepId(
        definitionRef.current.value,
        baseRef.current,
        swdCanvasStepId,
      )
      onSelectedStepIdChange?.(pcaStepId)
    },
    [onSelectedStepIdChange],
  )

  const onDefinitionChange = useCallback(
    (
      state: WrappedDefinition<PcaSwdDefinition>,
      event?: { changeType?: DefinitionChangeType } | undefined,
    ) => {
      setDefinition(state)
      if (saveTimer.current) clearTimeout(saveTimer.current)
      saveTimer.current = setTimeout(() => {
        // Right-panel edits (assignments/args) are compiled via pushDesign; only structural
        // canvas edits should round-trip here. onReady uses event=undefined.
        const changeType = event?.changeType
        if (changeType === undefined || !CANVAS_COMPILE_CHANGE_TYPES.has(changeType)) return
        if (replaceInFlightRef.current) return

        skipReplaceRef.current = true
        onDesignChange(swdDefinitionToDesign(state.value, baseRef.current))
      }, 200)
    },
    [onDesignChange],
  )

  // External edits: only re-mount/replace SWD when step tree shape changes (add/move/delete).
  // Assignment/args edits stay in PCA + right panel; full replace here causes flicker.
  useEffect(() => {
    if (skipReplaceRef.current) {
      skipReplaceRef.current = false
      return
    }
    const wrapped = wrapDefinition(designToSwdDefinition(baseRef.current))
    setDefinition(wrapped)
    if (!controller.isReady()) return
    replaceInFlightRef.current = true
    void controller
      .replaceDefinition(wrapped.value)
      .then(() => {
        // The React wrapper may destroy/recreate the Designer when definition changes.
        // Guard against calling controller methods while it's temporarily not ready.
        if (!controller.isReady()) return
        try {
          controller.updateLayout()
        } catch {
          // ignore
        }
      })
      .finally(() => {
        replaceInFlightRef.current = false
      })
  }, [structureKey, controller])

  useEffect(() => {
    if (!controller.isReady()) return
    const t = window.setTimeout(() => controller.updateLayout(), 50)
    return () => window.clearTimeout(t)
  }, [structureKey, toolsKey, height, controller])

  // Execution streaming: pan/select/folder-open (not on every manual click).
  useEffect(() => {
    if (!focusStepId) return
    if (replaceInFlightRef.current) return

    const apply = () =>
      focusPcaStepOnSwd(
        controller,
        definitionRef.current.value,
        baseRef.current,
        focusStepId,
      )

    if (apply()) return

    const onReady = () => {
      apply()
    }
    controller.onIsReadyChanged.subscribe(onReady)
    const timers = [0, 50, 150, 400].map((ms) =>
      window.setTimeout(() => {
        apply()
      }, ms),
    )
    return () => {
      controller.onIsReadyChanged.unsubscribe(onReady)
      for (const id of timers) window.clearTimeout(id)
    }
  }, [controller, focusStepId, runFocusTick])

  return (
    <div
      data-testid="workflow-swd-canvas"
      className="swd-embed overflow-hidden rounded-md border bg-background"
      style={{ height, minHeight: height }}
    >
      <SequentialWorkflowDesigner
        controller={controller}
        definition={definition}
        onDefinitionChange={onDefinitionChange}
        theme={colorScheme}
        undoStackSize={16}
        stepsConfiguration={stepsConfiguration}
        validatorConfiguration={validatorConfiguration}
        toolboxConfiguration={toolboxConfiguration}
        controlBar
        contextMenu
        rootEditor={<SwdRootEditorHint />}
        stepEditor={<SwdStepEditor />}
        selectedStepId={swdSelectedStepId}
        onSelectedStepIdChanged={handleSwdSelection}
        isEditorCollapsed
        isToolboxCollapsed={false}
      />
    </div>
  )
}
