import { QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { HttpResponse, http } from 'msw'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import { queryClient } from '@/queryClient'
import { useAuthStore } from '@/stores/auth'
import { server } from '@/test/mswServer'

import { Login } from './Login'

function renderLogin() {
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={['/login']}>
        <Routes>
          <Route path="/login" element={<Login />} />
          <Route path="/" element={<div data-testid="home">home</div>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('<Login />', () => {
  beforeEach(() => {
    useAuthStore.getState().clear()
    queryClient.clear()
    window.localStorage.clear()
  })

  afterEach(() => {
    useAuthStore.getState().clear()
  })

  it('submits credentials, stores auth, navigates home', async () => {
    server.use(
      http.post('/auth/login', async ({ request }) => {
        const body = (await request.json()) as Record<string, string>
        expect(body).toEqual({
          tenant: 'default',
          email: 'demo@example.com',
          password: 'demo123',
        })
        return HttpResponse.json({ token: 'jwt.demo' })
      }),
      http.get('/me', () =>
        HttpResponse.json({
          user_id: 'u1',
          tenant_id: 't1',
          role: 'member',
        }),
      ),
    )

    const user = userEvent.setup()
    renderLogin()

    await user.clear(screen.getByLabelText(/租户|tenant/i))
    await user.type(screen.getByLabelText(/租户|tenant/i), 'default')
    await user.type(screen.getByLabelText(/邮箱|email/i), 'demo@example.com')
    await user.type(screen.getByLabelText(/密码|password/i), 'demo123')
    await user.click(screen.getByRole('button', { name: /登录|login/i }))

    await waitFor(() => {
      expect(useAuthStore.getState().token).toBe('jwt.demo')
    })
    expect(useAuthStore.getState().user).toEqual({
      id: 'u1',
      tenant_id: 't1',
      email: 'demo@example.com',
      role: 'member',
    })
    await screen.findByTestId('home')
  })

  it('shows error message on 401', async () => {
    server.use(
      http.post('/auth/login', () =>
        HttpResponse.json({ error: 'invalid_credentials' }, { status: 401 }),
      ),
    )

    const user = userEvent.setup()
    renderLogin()

    await user.clear(screen.getByLabelText(/租户|tenant/i))
    await user.type(screen.getByLabelText(/租户|tenant/i), 'default')
    await user.type(screen.getByLabelText(/邮箱|email/i), 'bad@example.com')
    await user.type(screen.getByLabelText(/密码|password/i), 'wrong')
    await user.click(screen.getByRole('button', { name: /登录|login/i }))

    expect(await screen.findByText(/登录失败|login failed/i)).toBeInTheDocument()
    expect(useAuthStore.getState().token).toBeNull()
    expect(screen.queryByTestId('home')).toBeNull()
  })

  it('disables submit while pending', async () => {
    let release!: () => void
    const gate = new Promise<void>((r) => {
      release = r
    })
    server.use(
      http.post('/auth/login', async () => {
        await gate
        return HttpResponse.json({ token: 'jwt.late' })
      }),
      http.get('/me', () =>
        HttpResponse.json({ user_id: 'u1', tenant_id: 't1', role: 'member' }),
      ),
    )

    const user = userEvent.setup()
    renderLogin()
    await user.clear(screen.getByLabelText(/租户|tenant/i))
    await user.type(screen.getByLabelText(/租户|tenant/i), 'default')
    await user.type(screen.getByLabelText(/邮箱|email/i), 'demo@example.com')
    await user.type(screen.getByLabelText(/密码|password/i), 'demo123')
    const submit = screen.getByRole('button', { name: /登录|login/i })
    await user.click(submit)

    expect(submit).toBeDisabled()
    release()
    await waitFor(() => {
      expect(useAuthStore.getState().token).toBe('jwt.late')
    })
  })
})
