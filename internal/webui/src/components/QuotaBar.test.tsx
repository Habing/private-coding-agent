import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import { useAuthStore } from '@/stores/auth'
import { server } from '@/test/mswServer'

import { QuotaBar } from './QuotaBar'

function renderBar() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <QuotaBar />
    </QueryClientProvider>,
  )
}

describe('<QuotaBar />', () => {
  beforeEach(() => {
    useAuthStore.getState().setAuth('jwt', {
      id: 'u1',
      tenant_id: 't1',
      email: 'u@example.com',
      role: 'member',
    })
  })
  afterEach(() => {
    useAuthStore.getState().clear()
  })

  it('shows progress when quota is enabled', async () => {
    server.use(
      http.get('/quota', () =>
        HttpResponse.json({
          llm_tokens: {
            used: 150_000,
            cap: 200_000,
            enabled: true,
            resets_at: '2026-05-25T00:00:00Z',
          },
        }),
      ),
    )
    renderBar()
    expect(await screen.findByText('LLM 今日')).toBeInTheDocument()
    expect(screen.getByText('150k/200k')).toBeInTheDocument()
    expect(screen.getByRole('progressbar')).toBeInTheDocument()
  })

  it('hides when quota is disabled', async () => {
    server.use(
      http.get('/quota', () =>
        HttpResponse.json({
          llm_tokens: { used: 0, cap: 0, enabled: false },
        }),
      ),
    )
    renderBar()
    await new Promise((r) => setTimeout(r, 50))
    expect(screen.queryByText('LLM 今日')).not.toBeInTheDocument()
  })
})
