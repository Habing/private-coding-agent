import type { AgentEvent } from '@/types/api'

/** Wire format from agent.Engine (snake_case JSON fields). */
export type WireAgentEvent = AgentEvent & {
  tool_name?: string
  tool_input?: unknown
  tool_output?: unknown
  tool_error?: string
}

/** Normalize backend SSE/WS events to the shape ToolCallCard expects. */
export function normalizeAgentEvent(ev: AgentEvent): AgentEvent {
  const raw = ev as WireAgentEvent
  const tool = raw.tool ?? raw.tool_name
  const input = raw.input ?? coercePayload(raw.tool_input)
  const output = raw.output ?? coercePayload(raw.tool_output)
  const error =
    raw.error ??
    raw.tool_error ??
    (ev.kind === 'error' ? raw.text : undefined)

  return {
    ...ev,
    tool,
    input,
    output,
    error,
  }
}

export function eventToolName(ev: AgentEvent): string | undefined {
  return normalizeAgentEvent(ev).tool
}

export function eventToolInput(ev: AgentEvent): unknown {
  return normalizeAgentEvent(ev).input
}

export function eventToolOutput(ev: AgentEvent): unknown {
  return normalizeAgentEvent(ev).output
}

export function eventToolError(ev: AgentEvent): string | undefined {
  return normalizeAgentEvent(ev).error
}

export function parseJSONString(raw: string): unknown {
  try {
    return JSON.parse(raw) as unknown
  } catch {
    return raw
  }
}

function coercePayload(value: unknown): unknown {
  if (value == null) return undefined
  if (typeof value === 'string') {
    const trimmed = value.trim()
    if (trimmed === '') return undefined
    if (trimmed.startsWith('{') || trimmed.startsWith('[')) {
      return parseJSONString(trimmed)
    }
    return value
  }
  return value
}
