import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { HttpResponse, http } from 'msw'
import type React from 'react'
import { MemoryRouter, Route, Routes, useParams } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import { useAuthStore } from '@/stores/auth'
import { server } from '@/test/mswServer'
import type { Session } from '@/types/api'

import { SessionList } from './SessionList'

function makeSession(over: Partial<Session>): Session {
  return {
    id: 's-default',
    tenant_id: 't1',
    owner_user_id: 'u1',
    title: 'Untitled',
    model: 'default-mock:gpt-4o',
    profile: 'coding',
    status: 'active',
    created_at: '2026-05-20T10:00:00Z',
    updated_at: '2026-05-20T10:00:00Z',
    ...over,
  }
}

function ChatProbe() {
  const { id } = useParams<{ id: string }>()
  return <div data-testid="chat-route">chat:{id}</div>
}

function ShellRoute({ children }: { children: React.ReactNode }) {
  return (
    <div>
      <SessionList />
      {children}
    </div>
  )
}

function renderAt(path: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/" element={<ShellRoute>{null}</ShellRoute>} />
          <Route
            path="/sessions/:id"
            element={
              <ShellRoute>
                <ChatProbe />
              </ShellRoute>
            }
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

const seedSessions: Session[] = [
  makeSession({ id: 's1', title: 'First' }),
  makeSession({ id: 's2', title: 'Second' }),
  makeSession({ id: 's3', title: '' }),
]

describe('<SessionList />', () => {
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

  it('renders all sessions from GET /sessions', async () => {
    server.use(http.get('/sessions', () => HttpResponse.json({ sessions: seedSessions })))
    renderAt('/')
    expect(await screen.findByText('First')).toBeInTheDocument()
    expect(screen.getByText('Second')).toBeInTheDocument()
    // Untitled rendered for the row with empty title.
    expect(screen.getByText(/untitled|未命名/i)).toBeInTheDocument()
  })

  it('marks the active route', async () => {
    server.use(http.get('/sessions', () => HttpResponse.json({ sessions: seedSessions })))
    renderAt('/sessions/s2')
    const row = await screen.findByText('Second')
    const link = row.closest('a')!
    expect(link).toHaveAttribute('aria-current', 'page')
  })

  it('creates and navigates to a new session', async () => {
    server.use(
      http.get('/sessions', () => HttpResponse.json({ sessions: seedSessions })),
      http.post('/sessions', async () =>
        HttpResponse.json(makeSession({ id: 's-new', title: '' }), { status: 201 }),
      ),
    )
    const user = userEvent.setup()
    renderAt('/')
    await screen.findByText('First')
    await user.click(screen.getByRole('button', { name: /新建|new/i }))
    await screen.findByText('chat:s-new')
  })

  it('deletes a session and removes it from the list', async () => {
    let listCallCount = 0
    server.use(
      http.get('/sessions', () => {
        listCallCount += 1
        if (listCallCount === 1) {
          return HttpResponse.json({ sessions: seedSessions })
        }
        return HttpResponse.json({ sessions: [seedSessions[1], seedSessions[2]] })
      }),
      http.delete('/sessions/:id', ({ params }) => {
        expect(params.id).toBe('s1')
        return new HttpResponse(null, { status: 204 })
      }),
    )
    const user = userEvent.setup()
    renderAt('/')
    const firstRow = (await screen.findByText('First')).closest('li')!
    const deleteBtn = within(firstRow).getByRole('button', { name: /删除|delete/i })
    await user.click(deleteBtn)
    await waitFor(() => {
      expect(screen.queryByText('First')).toBeNull()
    })
    expect(screen.getByText('Second')).toBeInTheDocument()
  })
})
