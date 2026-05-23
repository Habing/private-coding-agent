import { describe, expect, it } from 'vitest'

import type { AgentEvent } from '@/types/api'

import { shouldShowWaitingIndicator } from './chatWaiting'

describe('shouldShowWaitingIndicator', () => {
  it('is false when not awaiting', () => {
    expect(shouldShowWaitingIndicator([{ kind: 'user', text: 'hi' }], false)).toBe(
      false,
    )
  })

  it('is true after user message with no assistant text yet', () => {
    expect(
      shouldShowWaitingIndicator([{ kind: 'user', text: 'hi' }], true),
    ).toBe(true)
  })

  it('is false once assistant_delta has text', () => {
    const events: AgentEvent[] = [
      { kind: 'user', text: 'hi' },
      { kind: 'assistant_delta', text: 'hel' },
    ]
    expect(shouldShowWaitingIndicator(events, true)).toBe(false)
  })

  it('stays true during tool-only stretch before assistant text', () => {
    const events: AgentEvent[] = [
      { kind: 'user', text: 'run ls' },
      { kind: 'tool_call', tool: 'shell.exec', tool_call_id: 't1' },
      { kind: 'tool_result', tool: 'shell.exec', tool_call_id: 't1', output: 'ok' },
    ]
    expect(shouldShowWaitingIndicator(events, true)).toBe(true)
  })

  it('is false on error after user message', () => {
    const events: AgentEvent[] = [
      { kind: 'user', text: 'hi' },
      { kind: 'error', error: 'boom' },
    ]
    expect(shouldShowWaitingIndicator(events, true)).toBe(false)
  })
})
