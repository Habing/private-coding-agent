import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import type { AgentEvent, Message } from '@/types/api'

import { MessageList } from './MessageList'

const history: Message[] = [
  {
    id: 'm1',
    session_id: 's1',
    seq: 1,
    role: 'user',
    content: 'hi',
    created_at: '2026-05-20T10:00:00Z',
  },
  {
    id: 'm2',
    session_id: 's1',
    seq: 2,
    role: 'assistant',
    content: 'hello',
    created_at: '2026-05-20T10:00:01Z',
  },
]

describe('<MessageList />', () => {
  it('renders persisted history bubbles', () => {
    render(<MessageList history={history} events={[]} />)
    expect(screen.getByText('hi')).toBeInTheDocument()
    expect(screen.getByText('hello')).toBeInTheDocument()
  })

  it('appends streamed user echo and assistant message', () => {
    const events: AgentEvent[] = [
      { kind: 'user', text: 'follow up' },
      { kind: 'assistant_message', text: 'sure thing', step: 1 },
    ]
    render(<MessageList history={history} events={events} />)
    expect(screen.getByText('follow up')).toBeInTheDocument()
    expect(screen.getByText('sure thing')).toBeInTheDocument()
  })

  it('pairs tool_call with tool_result by tool_call_id and shows ok status', async () => {
    const events: AgentEvent[] = [
      {
        kind: 'tool_call',
        step: 1,
        tool: 'bash',
        tool_call_id: 'tc1',
        input: { command: 'ls' },
      },
      {
        kind: 'tool_result',
        step: 1,
        tool: 'bash',
        tool_call_id: 'tc1',
        output: 'README.md\n',
      },
    ]
    render(<MessageList history={[]} events={events} />)
    const card = screen.getByLabelText('tool bash')
    expect(card).toBeInTheDocument()
    // collapsed header shows status badge
    expect(screen.getByText(/ok/)).toBeInTheDocument()
    // expand to see input/output
    await userEvent.click(card)
    expect(screen.getByText('input')).toBeInTheDocument()
    expect(screen.getByText('output')).toBeInTheDocument()
    expect(screen.getByText(/README.md/)).toBeInTheDocument()
  })

  it('shows error status when tool_result has an error', () => {
    const events: AgentEvent[] = [
      {
        kind: 'tool_call',
        step: 1,
        tool: 'bash',
        tool_call_id: 'tc1',
        input: { command: 'bad' },
      },
      {
        kind: 'tool_result',
        step: 1,
        tool: 'bash',
        tool_call_id: 'tc1',
        error: 'exit 1',
      },
    ]
    render(<MessageList history={[]} events={events} />)
    const card = screen.getByLabelText('tool bash')
    expect(within(card).getByText(/error/)).toBeInTheDocument()
  })

  it('renders error events as a banner', () => {
    const events: AgentEvent[] = [{ kind: 'error', error: 'boom' }]
    render(<MessageList history={[]} events={events} />)
    expect(screen.getByRole('alert')).toHaveTextContent('boom')
  })

  it('shows empty placeholder when nothing to render', () => {
    render(<MessageList history={[]} events={[]} />)
    expect(screen.getByText(/还没有消息/)).toBeInTheDocument()
  })
})
