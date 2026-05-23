import type { AgentEvent } from '@/types/api'

import {
  eventToolName,
  eventToolOutput,
  normalizeAgentEvent,
} from './agentEvent'

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
  const ev = normalizeAgentEvent(result)
  if (ev.kind !== 'tool_result') return null
  if (eventToolName(ev) !== 'workflow.propose') return null
  if (ev.error) {
    return { ok: false, error: ev.error }
  }
  return parseOutput(eventToolOutput(ev))
}
