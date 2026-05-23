import { type ComponentProps } from 'react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it } from 'vitest'

import type { AgentEvent, Message } from '@/types/api'

import { MessageList } from './MessageList'

function renderList(props: ComponentProps<typeof MessageList>) {
  const qc = new QueryClient()
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <MessageList {...props} />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

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
    renderList({ history, events: [] })
    expect(screen.getByText('hi')).toBeInTheDocument()
    expect(screen.getByText('hello')).toBeInTheDocument()
  })

  it('appends streamed user echo and assistant message', () => {
    const events: AgentEvent[] = [
      { kind: 'user', text: 'follow up' },
      { kind: 'assistant_message', text: 'sure thing', step: 1 },
    ]
    renderList({ history, events })
    expect(screen.getByText('follow up')).toBeInTheDocument()
    expect(screen.getByText('sure thing')).toBeInTheDocument()
  })

  it('merges assistant_delta chunks into one bubble', () => {
    const events: AgentEvent[] = [
      { kind: 'assistant_delta', text: 'hel', step: 1 },
      { kind: 'assistant_delta', text: 'lo', step: 1 },
      { kind: 'assistant_message', text: 'hello', step: 1 },
    ]
    renderList({ history: [], events })
    expect(screen.getByText('hello')).toBeInTheDocument()
    expect(screen.queryAllByText('hel')).toHaveLength(0)
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
    renderList({ history: [], events })
    const card = screen.getByLabelText('tool bash')
    expect(card).toBeInTheDocument()
    expect(screen.getByText(/ok/)).toBeInTheDocument()
    await userEvent.click(card)
    expect(screen.getByText('input')).toBeInTheDocument()
    expect(screen.getByText('output')).toBeInTheDocument()
    expect(screen.getByText(/README.md/)).toBeInTheDocument()
  })

  it('maps backend tool_name/tool_input/tool_output wire fields', async () => {
    const events: AgentEvent[] = [
      {
        kind: 'tool_call',
        step: 1,
        tool_call_id: 'call_abc',
        tool_name: 'shell.exec',
        tool_input: { command: 'python -V' },
      },
      {
        kind: 'tool_result',
        step: 1,
        tool_call_id: 'call_abc',
        tool_name: 'shell.exec',
        tool_output: 'Python 3.12\n',
      },
    ]
    renderList({ history: [], events })
    const card = screen.getByLabelText('tool shell.exec')
    await userEvent.click(card)
    expect(screen.getByText(/python -V/)).toBeInTheDocument()
    expect(screen.getByText(/Python 3.12/)).toBeInTheDocument()
  })

  it('renders persisted tool messages with assistant tool_calls', async () => {
    const persisted: Message[] = [
      {
        id: 'm1',
        session_id: 's1',
        seq: 1,
        role: 'user',
        content: 'run ls',
        created_at: '2026-05-20T10:00:00Z',
      },
      {
        id: 'm2',
        session_id: 's1',
        seq: 2,
        role: 'assistant',
        content: '',
        tool_calls: [
          {
            id: 'call_hist',
            type: 'function',
            function: { name: 'shell.exec', arguments: '{"command":"ls"}' },
          },
        ],
        created_at: '2026-05-20T10:00:01Z',
      },
      {
        id: 'm3',
        session_id: 's1',
        seq: 3,
        role: 'tool',
        content: 'README.md\n',
        tool_call_id: 'call_hist',
        metadata: { tool_name: 'shell.exec' },
        created_at: '2026-05-20T10:00:02Z',
      },
    ]
    renderList({ history: persisted, events: [] })
    const card = screen.getByLabelText('tool shell.exec')
    await userEvent.click(card)
    expect(screen.getByText(/README.md/)).toBeInTheDocument()
    expect(screen.getByText(/"command": "ls"/)).toBeInTheDocument()
  })

  it('renders workflow.propose result as confirmation card', () => {
    const events: AgentEvent[] = [
      {
        kind: 'tool_call',
        step: 1,
        tool: 'workflow.propose',
        tool_call_id: 'tc-wf',
        input: { slug: 'weekly-summary' },
      },
      {
        kind: 'tool_result',
        step: 1,
        tool: 'workflow.propose',
        tool_call_id: 'tc-wf',
        output: {
          ok: true,
          proposal_id: 'p1',
          slug: 'weekly-summary',
          name: '每周摘要',
          template_id: 'llm-summarize-notify',
          dry_run_ok: true,
          status: 'draft',
          summary: 'dry ok',
        },
      },
    ]
    renderList({ history: [], events })
    expect(screen.getByText(/工作流草案：每周摘要/)).toBeInTheDocument()
    expect(screen.getByText(/模板 · llm-summarize-notify/)).toBeInTheDocument()
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
    renderList({ history: [], events })
    const card = screen.getByLabelText('tool bash')
    expect(within(card).getByText(/error/)).toBeInTheDocument()
  })

  it('renders error events as a banner', () => {
    const events: AgentEvent[] = [{ kind: 'error', error: 'boom' }]
    renderList({ history: [], events })
    expect(screen.getByRole('alert')).toHaveTextContent('boom')
  })

  it('shows empty placeholder when nothing to render', () => {
    renderList({ history: [], events: [] })
    expect(screen.getByText(/还没有消息/)).toBeInTheDocument()
  })

  it('shows typing indicator while awaiting reply', () => {
    const events: AgentEvent[] = [{ kind: 'user', text: 'question' }]
    renderList({ history: [], events, awaitingReply: true })
    expect(screen.getByRole('status')).toBeInTheDocument()
    expect(screen.getByText('正在思考…')).toBeInTheDocument()
  })

  it('hides typing indicator once assistant streams text', () => {
    const events: AgentEvent[] = [
      { kind: 'user', text: 'question' },
      { kind: 'assistant_delta', text: 'ans' },
    ]
    renderList({ history: [], events, awaitingReply: true })
    expect(screen.queryByRole('status')).not.toBeInTheDocument()
    expect(screen.getByText('ans')).toBeInTheDocument()
  })
})
