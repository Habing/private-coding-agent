import type { WorkflowDesign, WorkflowDesignInput, WorkflowDesignStep } from '@/types/api'

export type StepPath = string

export interface FlatStep {
  path: StepPath
  depth: number
  step: WorkflowDesignStep
  branchLabel?: string
}

/** First top-level step id — entry point when a run starts. */
export function firstRootStepId(design: WorkflowDesign | null | undefined): string | undefined {
  const id = design?.steps?.[0]?.id?.trim()
  return id || undefined
}

export function flattenSteps(steps: WorkflowDesignStep[], prefix = ''): FlatStep[] {
  const out: FlatStep[] = []
  for (const s of steps) {
    const path = prefix ? `${prefix}/${s.id}` : s.id
    const depth = path.split('/').filter((p) => p !== 'then' && p !== 'else').length
    out.push({ path, depth, step: s })
    if (s.kind === 'if') {
      for (const c of s.then ?? []) {
        out.push(
          ...flattenSteps([c], `${path}/then`).map((x) => ({ ...x, branchLabel: 'then' })),
        )
      }
      for (const c of s.else ?? []) {
        out.push(
          ...flattenSteps([c], `${path}/else`).map((x) => ({ ...x, branchLabel: 'else' })),
        )
      }
    }
  }
  return out
}

function cloneSteps(steps: WorkflowDesignStep[]): WorkflowDesignStep[] {
  return steps.map((s) => ({
    ...s,
    args: s.args ? s.args.map((a) => ({ ...a })) : undefined,
    assignments: s.assignments ? s.assignments.map((a) => ({ ...a })) : undefined,
    condition: s.condition ? { ...s.condition } : undefined,
    then: s.then ? cloneSteps(s.then) : undefined,
    else: s.else ? cloneSteps(s.else) : undefined,
  }))
}

function updateInList(
  steps: WorkflowDesignStep[],
  parts: string[],
  updated: WorkflowDesignStep,
): WorkflowDesignStep[] {
  const head = parts[0]
  if (parts.length === 1) {
    return steps.map((s) => (s.id === head ? updated : s))
  }
  const branch = parts[1]
  if (branch !== 'then' && branch !== 'else') return steps
  const rest = parts.slice(2)
  return steps.map((s) => {
    if (s.id !== head || s.kind !== 'if') return s
    const list = branch === 'then' ? (s.then ?? []) : (s.else ?? [])
    const next = updateInList(list, rest, updated)
    return branch === 'then' ? { ...s, then: next } : { ...s, else: next }
  })
}

function removeInList(steps: WorkflowDesignStep[], parts: string[]): WorkflowDesignStep[] {
  const head = parts[0]
  if (parts.length === 1) {
    return steps.filter((s) => s.id !== head)
  }
  const branch = parts[1]
  if (branch !== 'then' && branch !== 'else') return steps
  const rest = parts.slice(2)
  return steps.map((s) => {
    if (s.id !== head || s.kind !== 'if') return s
    const list = branch === 'then' ? (s.then ?? []) : (s.else ?? [])
    const next = removeInList(list, rest)
    return branch === 'then' ? { ...s, then: next } : { ...s, else: next }
  })
}

export function getStepAtPath(
  design: WorkflowDesign,
  path: StepPath,
): WorkflowDesignStep | null {
  const parts = path.split('/')
  let steps = design.steps
  for (let i = 0; i < parts.length; ) {
    const stepId = parts[i]
    const found = steps.find((s) => s.id === stepId)
    if (!found) return null
    if (i === parts.length - 1) return found
    const branch = parts[i + 1]
    if (branch !== 'then' && branch !== 'else') return null
    steps = branch === 'then' ? (found.then ?? []) : (found.else ?? [])
    i += 2
  }
  return null
}

export function updateStepAtPath(
  design: WorkflowDesign,
  path: StepPath,
  updated: WorkflowDesignStep,
): WorkflowDesign {
  const parts = path.split('/')
  return { ...design, steps: updateInList(cloneSteps(design.steps), parts, updated) }
}

export function removeStepAtPath(
  design: WorkflowDesign,
  path: StepPath,
): WorkflowDesign {
  const parts = path.split('/')
  return { ...design, steps: removeInList(cloneSteps(design.steps), parts) }
}

export function addStepToBranch(
  design: WorkflowDesign,
  ifPath: StepPath,
  branch: 'then' | 'else',
  step: WorkflowDesignStep,
): WorkflowDesign {
  const parts = ifPath.split('/')
  const rootId = parts[0]
  return {
    ...design,
    steps: design.steps.map((s) => {
      if (s.id !== rootId || s.kind !== 'if') return s
      if (branch === 'then') return { ...s, then: [...(s.then ?? []), step] }
      return { ...s, else: [...(s.else ?? []), step] }
    }),
  }
}

export function addTopLevelStep(
  design: WorkflowDesign,
  step: WorkflowDesignStep,
): WorkflowDesign {
  return { ...design, steps: [...design.steps, step] }
}

export function buildInvokeDefaults(
  inputs: WorkflowDesignInput[] | undefined,
): Record<string, unknown> {
  const out: Record<string, unknown> = {}
  for (const inp of inputs ?? []) {
    if (inp.default !== undefined) out[inp.name] = inp.default
  }
  return out
}

export function isIfBranchPath(path: StepPath): boolean {
  return path.includes('/then/') || path.includes('/else/')
}

export function parentIfPath(path: StepPath): StepPath | null {
  const m = path.match(/^(.+)\/(then|else)\/[^/]+$/)
  return m ? m[1] : null
}

/** Parent list path: '' = top-level steps; `gate/then` = branch list. */
export function parentListPath(path: StepPath): StepPath | '' {
  const m = path.match(/^(.+)\/(then|else)\/[^/]+$/)
  return m ? (`${m[1]}/${m[2]}` as StepPath) : ''
}

export function stepPathFromStepId(
  design: WorkflowDesign,
  stepId: string,
): StepPath | null {
  const hit = flattenSteps(design.steps).find((f) => f.step.id === stepId)
  return hit?.path ?? null
}

export function isGraphStepNodeId(nodeId: string): boolean {
  return (
    nodeId !== '__start__' &&
    nodeId !== '__end__' &&
    !nodeId.startsWith('trigger:')
  )
}

/** Insert after `afterPath`, or at top-level start when `afterPath` is `__start__`. */
export function insertStepAfter(
  design: WorkflowDesign,
  afterPath: StepPath | '__start__',
  step: WorkflowDesignStep,
): WorkflowDesign {
  if (afterPath === '__start__') {
    return { ...design, steps: [step, ...design.steps] }
  }
  const parts = afterPath.split('/')
  return { ...design, steps: insertAfterInList(cloneSteps(design.steps), parts, step) }
}

function insertAfterInList(
  steps: WorkflowDesignStep[],
  parts: string[],
  newStep: WorkflowDesignStep,
): WorkflowDesignStep[] {
  if (parts.length === 1) {
    const idx = steps.findIndex((s) => s.id === parts[0])
    if (idx < 0) return steps
    const next = [...steps]
    next.splice(idx + 1, 0, newStep)
    return next
  }
  const head = parts[0]
  const branch = parts[1]
  if (branch !== 'then' && branch !== 'else') return steps
  const rest = parts.slice(2)
  return steps.map((s) => {
    if (s.id !== head || s.kind !== 'if') return s
    const list = branch === 'then' ? (s.then ?? []) : (s.else ?? [])
    if (rest.length === 1) {
      const idx = list.findIndex((x) => x.id === rest[0])
      if (idx < 0) return s
      const childList = [...list]
      childList.splice(idx + 1, 0, newStep)
      return branch === 'then' ? { ...s, then: childList } : { ...s, else: childList }
    }
    const childList = insertAfterInList(list, rest, newStep)
    return branch === 'then' ? { ...s, then: childList } : { ...s, else: childList }
  })
}

/** Swap step with previous/next sibling in the same list. Returns null if no move. */
export function moveStep(
  design: WorkflowDesign,
  path: StepPath,
  direction: 'up' | 'down',
): WorkflowDesign | null {
  const parts = path.split('/')
  const delta = direction === 'up' ? -1 : 1
  const nextSteps = moveInList(cloneSteps(design.steps), parts, delta)
  if (!nextSteps) return null
  return { ...design, steps: nextSteps }
}

function moveInList(
  steps: WorkflowDesignStep[],
  parts: string[],
  delta: number,
): WorkflowDesignStep[] | null {
  if (parts.length === 1) {
    const idx = steps.findIndex((s) => s.id === parts[0])
    const newIdx = idx + delta
    if (idx < 0 || newIdx < 0 || newIdx >= steps.length) return null
    const next = [...steps]
    ;[next[idx], next[newIdx]] = [next[newIdx], next[idx]]
    return next
  }
  const head = parts[0]
  const branch = parts[1]
  if (branch !== 'then' && branch !== 'else') return null
  const rest = parts.slice(2)
  let changed = false
  const mapped = steps.map((s) => {
    if (s.id !== head || s.kind !== 'if') return s
    const list = branch === 'then' ? (s.then ?? []) : (s.else ?? [])
    const moved = moveInList(list, rest, delta)
    if (!moved) return s
    changed = true
    return branch === 'then' ? { ...s, then: moved } : { ...s, else: moved }
  })
  return changed ? mapped : null
}

/** Reorder siblings in one container to match `orderedIds` (must be same set). */
export function reorderSiblings(
  design: WorkflowDesign,
  containerPath: StepPath | '',
  orderedIds: string[],
): WorkflowDesign | null {
  const list = getStepListAtContainer(design, containerPath)
  if (!list || list.length !== orderedIds.length) return null
  const byId = new Map(list.map((s) => [s.id, s]))
  if (!orderedIds.every((id) => byId.has(id))) return null
  const reordered = orderedIds.map((id) => byId.get(id)!)
  return setStepListAtContainer(design, containerPath, reordered)
}

function getStepListAtContainer(
  design: WorkflowDesign,
  containerPath: StepPath | '',
): WorkflowDesignStep[] | null {
  if (containerPath === '') return design.steps
  const parts = containerPath.split('/')
  if (parts.length !== 3 || (parts[1] !== 'then' && parts[1] !== 'else')) return null
  const gate = design.steps.find((s) => s.id === parts[0] && s.kind === 'if')
  if (!gate) return null
  return parts[1] === 'then' ? (gate.then ?? []) : (gate.else ?? [])
}

function setStepListAtContainer(
  design: WorkflowDesign,
  containerPath: StepPath | '',
  list: WorkflowDesignStep[],
): WorkflowDesign {
  if (containerPath === '') return { ...design, steps: list }
  const parts = containerPath.split('/')
  const branch = parts[1] as 'then' | 'else'
  return {
    ...design,
    steps: design.steps.map((s) => {
      if (s.id !== parts[0] || s.kind !== 'if') return s
      return branch === 'then' ? { ...s, then: list } : { ...s, else: list }
    }),
  }
}
