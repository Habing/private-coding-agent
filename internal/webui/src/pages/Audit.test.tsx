import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { HttpResponse, http } from 'msw'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import { AdminGuard } from '@/components/AdminGuard'
import { useAuthStore } from '@/stores/auth'
import { server } from '@/test/mswServer'
import type { AuditEntry } from '@/types/api'

import { Audit } from './Audit'

function makeEntry(action: string): AuditEntry {
  return {
    occurred_at: '2026-05-20T10:00:00Z',
    user_id: 'aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee',
    tenant_id: 't1',
    action,
    target: 'demo@example.com',
    method: 'POST',
    path: '/auth/login',
    status: 200,
    duration_ms: 12,
    metadata: { role: 'admin' },
  }
}

function renderAudit(initialPath = '/audit') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[initialPath]}>
        <Routes>
          <Route
            path="/audit"
            element={
              <AdminGuard>
                <Audit />
              </AdminGuard>
            }
          />
          <Route path="/" element={<div data-testid="home-route">home</div>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('<Audit />', () => {
  beforeEach(() => {
    useAuthStore.getState().setAuth('jwt.demo', {
      id: 'u1',
      tenant_id: 't1',
      email: 'admin@example.com',
      role: 'admin',
    })
  })
  afterEach(() => {
    useAuthStore.getState().clear()
  })

  it('renders entries from GET /audit', async () => {
    server.use(
      http.get('/audit', () =>
        HttpResponse.json({
          entries: [makeEntry('auth.login.success'), makeEntry('sandbox.create')],
          total: 2,
          limit: 25,
          offset: 0,
        }),
      ),
    )
    renderAudit()
    expect(await screen.findByText('auth.login.success')).toBeInTheDocument()
    expect(screen.getByText('sandbox.create')).toBeInTheDocument()
    expect(screen.getByText(/共 2 条/)).toBeInTheDocument()
  })

  it('passes action filter to backend on Apply', async () => {
    let lastURL: URL | null = null
    server.use(
      http.get('/audit', ({ request }) => {
        lastURL = new URL(request.url)
        return HttpResponse.json({ entries: [], total: 0, limit: 25, offset: 0 })
      }),
    )
    renderAudit()
    await waitFor(() => expect(lastURL).not.toBeNull())
    const user = userEvent.setup()
    await user.type(screen.getByLabelText(/Action/i), 'auth.login')
    await user.click(screen.getByRole('button', { name: '筛选' }))
    await waitFor(() => {
      expect(lastURL?.searchParams.get('action')).toBe('auth.login')
    })
  })

  it('redirects non-admin to home', () => {
    useAuthStore.getState().setAuth('jwt.member', {
      id: 'u2',
      tenant_id: 't1',
      email: 'member@example.com',
      role: 'member',
    })
    renderAudit()
    expect(screen.getByTestId('home-route')).toBeInTheDocument()
  })
})
