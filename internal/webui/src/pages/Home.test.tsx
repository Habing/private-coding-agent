import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { HttpResponse, http } from 'msw'
import { MemoryRouter, Route, Routes, useParams } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import { GuideProvider } from '@/components/GuideProvider'
import { markWelcomeGuideSeen } from '@/lib/guideStorage'
import { useAuthStore } from '@/stores/auth'
import { server } from '@/test/mswServer'
import type { ProfileListResponse, Session } from '@/types/api'

import { Home } from './Home'

const mockProfiles: ProfileListResponse = {
  profiles: [
    { name: 'coding', description: 'coding agent' },
    { name: 'review', description: 'review agent' },
  ],
}

function ChatProbe() {
  const { id } = useParams<{ id: string }>()
  return <div data-testid="chat-route">chat:{id}</div>
}

function LoginProbe() {
  return <div data-testid="login-route">login</div>
}

function renderHome(initialPath = '/') {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <GuideProvider>
        <MemoryRouter initialEntries={[initialPath]}>
          <Routes>
            <Route path="/" element={<Home />} />
            <Route path="/sessions/:id" element={<ChatProbe />} />
            <Route path="/login" element={<LoginProbe />} />
          </Routes>
        </MemoryRouter>
      </GuideProvider>
    </QueryClientProvider>,
  )
}

function makeSession(id: string): Session {
  return {
    id,
    tenant_id: 't1',
    owner_user_id: 'u1',
    title: `S-${id}`,
    model: 'dashscope:qwen3.6-plus',
    profile: 'coding',
    status: 'active',
    created_at: '2026-05-20T10:00:00Z',
    updated_at: '2026-05-20T10:00:00Z',
  }
}

describe('<Home />', () => {
  beforeEach(() => {
    window.localStorage.clear()
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

  it('shows welcome guide for first-time users', async () => {
    server.use(
      http.get('/sessions', () => HttpResponse.json({ sessions: [] })),
      http.get('/agent/profiles', () => HttpResponse.json(mockProfiles)),
    )
    renderHome()
    expect(await screen.findByText(/欢迎使用 Private Coding Agent/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '查看完整指引' })).toBeInTheDocument()
  })

  it('creates and navigates when there are zero sessions', async () => {
    const user = userEvent.setup()
    markWelcomeGuideSeen()
    server.use(
      http.get('/sessions', () => HttpResponse.json({ sessions: [] })),
      http.get('/agent/profiles', () => HttpResponse.json(mockProfiles)),
      http.post('/sessions', () =>
        HttpResponse.json(makeSession('s-new'), { status: 201 }),
      ),
    )
    renderHome()
    await user.click(await screen.findByRole('button', { name: '新建会话' }))
    expect(await screen.findByTestId('chat-route')).toHaveTextContent('chat:s-new')
  })

  it('renders placeholder when sessions exist', async () => {
    markWelcomeGuideSeen()
    server.use(
      http.get('/sessions', () =>
        HttpResponse.json({ sessions: [makeSession('s1')] }),
      ),
      http.get('/agent/profiles', () => HttpResponse.json(mockProfiles)),
    )
    renderHome()
    expect(await screen.findByText(/选择.*会话|请选择/i)).toBeInTheDocument()
  })

  it('redirects to /login on 401', async () => {
    markWelcomeGuideSeen()
    server.use(
      http.get('/sessions', () =>
        HttpResponse.json({ error: 'unauthorized' }, { status: 401 }),
      ),
      http.get('/agent/profiles', () => HttpResponse.json(mockProfiles)),
    )
    renderHome()
    expect(await screen.findByTestId('login-route')).toBeInTheDocument()
    expect(useAuthStore.getState().token).toBeNull()
  })
})
