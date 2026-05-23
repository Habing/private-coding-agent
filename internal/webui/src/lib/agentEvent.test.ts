import { describe, expect, it } from 'vitest'

import {
  eventToolName,
  eventToolOutput,
  normalizeAgentEvent,
} from './agentEvent'

describe('normalizeAgentEvent', () => {
  it('maps backend snake_case tool fields', () => {
    const ev = normalizeAgentEvent({
      kind: 'tool_call',
      tool_call_id: 'call_1',
      tool_name: 'shell.exec',
      tool_input: { command: 'ls' },
    })
    expect(ev.tool).toBe('shell.exec')
    expect(ev.input).toEqual({ command: 'ls' })
  })

  it('maps tool_result output and error', () => {
    const ok = normalizeAgentEvent({
      kind: 'tool_result',
      tool_name: 'fs.read',
      tool_output: { content: 'hi' },
    })
    expect(ok.output).toEqual({ content: 'hi' })

    const fail = normalizeAgentEvent({
      kind: 'tool_result',
      tool_name: 'fs.read',
      tool_error: 'not found',
    })
    expect(fail.error).toBe('not found')
  })

  it('maps error kind text to error field', () => {
    const ev = normalizeAgentEvent({ kind: 'error', text: 'boom' })
    expect(ev.error).toBe('boom')
  })

  it('prefers canonical fields when both are present', () => {
    expect(
      eventToolName({
        kind: 'tool_call',
        tool: 'grep',
        tool_name: 'fs.read',
      }),
    ).toBe('grep')
    expect(
      eventToolOutput({
        kind: 'tool_result',
        output: 'a',
        tool_output: 'b',
      }),
    ).toBe('a')
  })
})
