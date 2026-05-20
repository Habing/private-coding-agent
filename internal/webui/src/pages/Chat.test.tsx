import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import { useAuthStore } from '@/stores/auth'
import { server } from '@/test/mswServer'
import type { Message } from '@/types/api'

import { Chat } from './Chat'

function renderChat(sid: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[`/sessions/${sid}`]}>
        <Routes>
          <Route path="/sessions/:id" element={<Chat />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

const sessionID = 's1'
const history: Message[] = [
  {
    id: 'm1',
    session_id: sessionID,
    seq: 1,
    role: 'user',
    content: 'hi',
    created_at: '2026-05-20T10:00:00Z',
  },
  {
    id: 'm2',
    session_id: sessionID,
    seq: 2,
    role: 'assistant',
    content: 'hello back',
    created_at: '2026-05-20T10:00:01Z',
  },
  {
    id: 'm3',
    session_id: sessionID,
    seq: 3,
    role: 'tool',
    content: '{"ok":true}',
    tool_call_id: 'tc1',
    created_at: '2026-05-20T10:00:02Z',
  },
]

describe('<Chat />', () => {
  beforeEach(() => {
    useAuthStore.getState().setAuth('jwt.demo', {
      id: 'u1',
      tenant_id: 't1',
      email: 'demo@example.com',
      role: 'member',
    })
  })
  afterEach(() => {
    useAuthStore.getState().clear()
  })

  it('renders history bubbles', async () => {
    server.use(
      http.get(`/sessions/${sessionID}/messages`, () =>
        HttpResponse.json({ messages: history }),
      ),
    )
    renderChat(sessionID)
    expect(await screen.findByText('hi')).toBeInTheDocument()
    expect(screen.getByText('hello back')).toBeInTheDocument()
    expect(screen.getByText(/ok/)).toBeInTheDocument()
  })

  it('renders the Composer disabled until WS hook is wired (Task 7)', async () => {
    server.use(
      http.get(`/sessions/${sessionID}/messages`, () =>
        HttpResponse.json({ messages: [] }),
      ),
    )
    renderChat(sessionID)
    const textarea = await screen.findByRole('textbox')
    expect(textarea).toBeInTheDocument()
  })
})
