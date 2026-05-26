import type { BranchedStep, Definition, Sequence, Step } from 'sequential-workflow-designer'

import { stepPathFromStepId } from '@/lib/workflowDesignTree'
import { designStepCanvasLabel } from '@/lib/workflowStepKindZh'
import type {
  WorkflowDesign,
  WorkflowDesignArg,
  WorkflowDesignAssign,
  WorkflowDesignCondition,
  WorkflowDesignStep,
} from '@/types/api'

export interface PcaSwdProperties {
  workflowId: string
  workflowName: string
}

export type PcaSwdDefinition = Definition & {
  properties: PcaSwdProperties
}

const BRANCH_THEN = 'then'
const BRANCH_ELSE = 'else'

/** Must match Go workflow.stepIDPattern — SWD often uses hex ids that start with a digit. */
export const PCA_STEP_ID_PATTERN = /^[a-zA-Z_][a-zA-Z0-9_.-]*$/

function isPcaStepId(id: string): boolean {
  return PCA_STEP_ID_PATTERN.test(id.trim())
}

/** Prefer toolbox stepId property, then SWD id when it already matches PCA rules. */
export function resolvePcaStepId(step: Step): string {
  const fromProps = String(step.properties?.stepId ?? '').trim()
  if (fromProps && isPcaStepId(fromProps)) return fromProps
  const fromSwd = step.id?.trim() ?? ''
  if (fromSwd && isPcaStepId(fromSwd)) return fromSwd
  return ''
}

function fallbackStepId(step: WorkflowDesignStep, index: number): string {
  if (step.kind === 'tool' && step.tool) {
    const base = `step_${step.tool.replace(/\W+/g, '_')}`
    if (isPcaStepId(base)) return base
  }
  if (step.kind === 'assign') return 'step_assign'
  if (step.kind === 'if') return 'step_if'
  return `step_${index + 1}`
}

function parseArgs(json: string): WorkflowDesignArg[] {
  try {
    const v = JSON.parse(json) as unknown
    return Array.isArray(v) ? (v as WorkflowDesignArg[]) : []
  } catch {
    return []
  }
}

function parseAssignments(json: string): WorkflowDesignAssign[] {
  try {
    const v = JSON.parse(json) as unknown
    return Array.isArray(v) ? (v as WorkflowDesignAssign[]) : []
  } catch {
    return []
  }
}

function parseCondition(json: string): WorkflowDesignCondition | undefined {
  try {
    const v = JSON.parse(json) as WorkflowDesignCondition
    return v && typeof v === 'object' ? v : undefined
  } catch {
    return undefined
  }
}

function isBranchedStep(step: Step): step is BranchedStep {
  return 'branches' in step && typeof (step as BranchedStep).branches === 'object'
}

function stepLabel(step: WorkflowDesignStep): string {
  return designStepCanvasLabel(step)
}

function stepToSwd(step: WorkflowDesignStep): Step {
  if (step.kind === 'if') {
    const branched: BranchedStep = {
      id: step.id,
      componentType: 'switch',
      type: 'if',
      name: stepLabel(step),
      properties: {
        stepId: step.id,
        conditionJson: JSON.stringify(step.condition ?? {}, null, 0),
      },
      branches: {
        [BRANCH_THEN]: (step.then ?? []).map(stepToSwd),
        [BRANCH_ELSE]: (step.else ?? []).map(stepToSwd),
      },
    }
    return branched
  }
  if (step.kind === 'assign') {
    return {
      id: step.id,
      componentType: 'task',
      type: 'assign',
      name: stepLabel(step),
      properties: {
        stepId: step.id,
        assignmentsJson: JSON.stringify(step.assignments ?? [], null, 0),
      },
    }
  }
  return {
    id: step.id,
    componentType: 'task',
    type: 'tool',
    name: stepLabel(step),
    properties: {
      stepId: step.id,
      tool: step.tool ?? '',
      argsJson: JSON.stringify(step.args ?? [], null, 0),
    },
  }
}

function swdStepToDesign(step: Step): WorkflowDesignStep | null {
  const id = resolvePcaStepId(step)
  if (isBranchedStep(step) && (step.type === 'if' || step.type === 'pca-if')) {
    const branches = step.branches
    return {
      id,
      kind: 'if',
      condition: parseCondition(String(step.properties.conditionJson ?? '{}')),
      then: (branches[BRANCH_THEN] ?? []).map(swdStepToDesign).filter((s): s is WorkflowDesignStep => !!s),
      else: (branches[BRANCH_ELSE] ?? []).map(swdStepToDesign).filter((s): s is WorkflowDesignStep => !!s),
    }
  }
  if (step.type === 'assign' || step.type === 'pca-assign') {
    return {
      id,
      kind: 'assign',
      assignments: parseAssignments(String(step.properties.assignmentsJson ?? '[]')),
    }
  }
  if (step.type === 'tool' || step.type === 'pca-tool') {
    return {
      id,
      kind: 'tool',
      tool: String(step.properties.tool ?? ''),
      args: parseArgs(String(step.properties.argsJson ?? '[]')),
    }
  }
  return null
}

function uniquifyStep(
  s: WorkflowDesignStep,
  seen: Set<string>,
  index: number,
): WorkflowDesignStep {
  let id = s.id?.trim() || ''
  if (!id || !isPcaStepId(id) || seen.has(id)) {
    id = fallbackStepId({ ...s, id }, index)
    let n = 2
    while (seen.has(id)) {
      id = `${fallbackStepId({ ...s, id }, index)}_${n}`
      n += 1
    }
  }
  seen.add(id)
  let out: WorkflowDesignStep = id === s.id ? s : { ...s, id }
  if (out.kind === 'if') {
    out = {
      ...out,
      then: ensureUniqueStepIds(out.then ?? [], seen, 0),
      else: ensureUniqueStepIds(out.else ?? [], seen, 0),
    }
  }
  return out
}

function ensureUniqueStepIds(
  steps: WorkflowDesignStep[],
  seen = new Set<string>(),
  startIndex = 0,
): WorkflowDesignStep[] {
  return steps.map((s, i) => uniquifyStep(s, seen, startIndex + i))
}

/** Pin workflow slug as design id and dedupe step ids (including if branches). */
export function normalizeWorkflowDesign(
  design: WorkflowDesign,
  workflowSlug: string,
): WorkflowDesign {
  return {
    ...design,
    id: workflowSlug,
    steps: ensureUniqueStepIds(design.steps ?? []),
  }
}

function sequenceToDesignSteps(sequence: Sequence): WorkflowDesignStep[] {
  const steps: WorkflowDesignStep[] = []
  for (const step of sequence) {
    const mapped = swdStepToDesign(step)
    if (mapped) steps.push(mapped)
  }
  return ensureUniqueStepIds(steps)
}

/** PCA WorkflowDesign → Sequential Workflow Designer definition. */
export function designToSwdDefinition(design: WorkflowDesign): PcaSwdDefinition {
  return {
    properties: {
      workflowId: design.id,
      workflowName: design.name,
    },
    sequence: design.steps.map(stepToSwd),
  }
}

/** Map SWD canvas step ids (often hex) → PCA step ids used in WorkflowDesign. */
export function buildSwdToPcaStepIdMap(
  definition: PcaSwdDefinition,
  base: WorkflowDesign,
): Map<string, string> {
  const design = normalizeWorkflowDesign(swdDefinitionToDesign(definition, base), base.id)
  const map = new Map<string, string>()

  function walk(swdSeq: Sequence, pcaSteps: WorkflowDesignStep[]) {
    for (let i = 0; i < swdSeq.length; i++) {
      const swd = swdSeq[i]
      const pca = pcaSteps[i]
      if (!swd || !pca) continue
      const pcaId = resolvePcaStepId(swd) || pca.id
      map.set(swd.id, pcaId)
      if (isBranchedStep(swd) && pca.kind === 'if') {
        walk(swd.branches[BRANCH_THEN] ?? [], pca.then ?? [])
        walk(swd.branches[BRANCH_ELSE] ?? [], pca.else ?? [])
      }
    }
  }

  walk(definition.sequence, design.steps)
  return map
}

/** Resolve SWD selection id for the right-side step detail panel. */
export function mapSwdSelectionToPcaStepId(
  definition: PcaSwdDefinition,
  base: WorkflowDesign,
  swdCanvasStepId: string | null,
): string | null {
  if (!swdCanvasStepId) return null
  const design = normalizeWorkflowDesign(swdDefinitionToDesign(definition, base), base.id)
  const mapped = buildSwdToPcaStepIdMap(definition, base).get(swdCanvasStepId)
  if (mapped) return mapped
  if (stepPathFromStepId(design, swdCanvasStepId)) return swdCanvasStepId
  return null
}

function walkSequenceForPcaStepId(seq: Sequence, pcaStepId: string): string | null {
  for (const step of seq) {
    const pid = resolvePcaStepId(step)
    if (pid === pcaStepId || step.id === pcaStepId) return step.id
    if (isBranchedStep(step)) {
      for (const branchSeq of Object.values(step.branches)) {
        const found = walkSequenceForPcaStepId(branchSeq ?? [], pcaStepId)
        if (found) return found
      }
    }
  }
  return null
}

/** Resolve PCA step id (from engine/SSE) → SWD canvas step id (often hex). */
export function findSwdCanvasIdForPcaStepId(
  definition: PcaSwdDefinition,
  pcaStepId: string,
): string | null {
  return walkSequenceForPcaStepId(definition.sequence, pcaStepId)
}

export function mapPcaStepIdToSwdCanvasId(
  definition: PcaSwdDefinition,
  base: WorkflowDesign,
  pcaStepId: string | null | undefined,
): string | null {
  if (!pcaStepId) return null
  const direct = findSwdCanvasIdForPcaStepId(definition, pcaStepId)
  if (direct) return direct
  const map = buildSwdToPcaStepIdMap(definition, base)
  for (const [swdID, pcaID] of map.entries()) {
    if (pcaID === pcaStepId) return swdID
  }
  // If the id already exists in SWD (rare), allow it.
  if (definition.sequence.some((s) => s.id === pcaStepId)) return pcaStepId
  return null
}

/** SWD definition → PCA WorkflowDesign (keeps inputs/outputs from base). */
export function swdDefinitionToDesign(
  definition: PcaSwdDefinition,
  base: WorkflowDesign,
): WorkflowDesign {
  return {
    ...base,
    id: definition.properties.workflowId || base.id,
    name: definition.properties.workflowName || base.name,
    steps: sequenceToDesignSteps(definition.sequence),
  }
}

function stepStructureFingerprint(step: WorkflowDesignStep): string {
  if (step.kind === 'if') {
    const then = (step.then ?? []).map(stepStructureFingerprint).join(',')
    const els = (step.else ?? []).map(stepStructureFingerprint).join(',')
    return `if:${step.id}:t[${then}]e[${els}]`
  }
  if (step.kind === 'assign') {
    return `assign:${step.id}`
  }
  return `tool:${step.id}:${step.tool ?? ''}`
}

/** Step tree shape only — remount SWD when steps are added/removed/reordered/tool id changes. */
export function designSyncFingerprint(design: WorkflowDesign): string {
  return `${design.id}:${design.steps.map(stepStructureFingerprint).join(';')}`
}
