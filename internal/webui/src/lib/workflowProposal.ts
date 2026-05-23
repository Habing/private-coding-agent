import type { AgentEvent } from '@/types/api'

/** Parsed workflow.propose tool output envelope. */
export interface WorkflowProposalPayload {
  ok: boolean
  proposal_id?: string
  slug?: string
  name?: string
  source?: string
  template_id?: string
  dry_run_ok?: boolean
  dry_run_error?: string
  status?: string
  summary?: string
  error?: string
  detail?: string
}

function eventToolName(ev: AgentEvent): string | undefined {
  const anyEv = ev as AgentEvent & { tool_name?: string }
  return anyEv.tool ?? anyEv.tool_name
}

function eventOutput(ev: AgentEvent): unknown {
  const anyEv = ev as AgentEvent & { tool_output?: unknown }
  return anyEv.output ?? anyEv.tool_output
}

function parseOutput(raw: unknown): WorkflowProposalPayload | null {
  if (raw == null) return null
  let obj: unknown = raw
  if (typeof raw === 'string') {
    try {
      obj = JSON.parse(raw) as unknown
    } catch {
      return null
    }
  }
  if (typeof obj !== 'object' || obj === null) return null
  const p = obj as WorkflowProposalPayload
  if (typeof p.ok !== 'boolean') return null
  return p
}

/** Extract workflow.propose result from a paired tool_result event. */
export function parseWorkflowProposalFromResult(
  result: AgentEvent,
): WorkflowProposalPayload | null {
  if (result.kind !== 'tool_result') return null
  if (eventToolName(result) !== 'workflow.propose') return null
  if (result.error) {
    return { ok: false, error: result.error }
  }
  return parseOutput(eventOutput(result))
}
