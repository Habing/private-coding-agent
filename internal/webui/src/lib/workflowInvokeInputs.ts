import type { WorkflowDesign } from '@/types/api'

/** True when fetch_status step passes ${inputs.scenario} (trial run can affect outcome). */
export function fetchStatusUsesInvokeScenario(design: WorkflowDesign | null | undefined): boolean {
  if (!design?.steps?.length) return true
  const step = design.steps.find(
    (s) => s.kind === 'tool' && (s.tool?.includes('fetch_status') ?? false),
  )
  if (!step) return true
  const arg = step.args?.find((a) => a.name === 'scenario')
  if (!arg?.value) return false
  return arg.value.includes('inputs.scenario')
}
