import { beforeEach, describe, expect, it } from 'vitest'

import type { User } from '@/types/api'

import { useAuthStore } from './auth'

const demoUser: User = {
  id: 'u1',
  tenant_id: 't1',
  email: 'demo@example.com',
  name: 'demo',
  role: 'member',
}

describe('useAuthStore', () => {
  beforeEach(() => {
    useAuthStore.getState().clear()
    window.localStorage.clear()
  })

  it('starts empty', () => {
    expect(useAuthStore.getState().token).toBeNull()
    expect(useAuthStore.getState().user).toBeNull()
  })

  it('setAuth stores token + user', () => {
    useAuthStore.getState().setAuth('jwt.demo', demoUser)
    expect(useAuthStore.getState().token).toBe('jwt.demo')
    expect(useAuthStore.getState().user).toEqual(demoUser)
  })

  it('clear wipes token + user', () => {
    useAuthStore.getState().setAuth('jwt.demo', demoUser)
    useAuthStore.getState().clear()
    expect(useAuthStore.getState().token).toBeNull()
    expect(useAuthStore.getState().user).toBeNull()
  })

  it('persists to localStorage under pca-auth', () => {
    useAuthStore.getState().setAuth('jwt.demo', demoUser)
    const raw = window.localStorage.getItem('pca-auth')
    expect(raw).not.toBeNull()
    const parsed = JSON.parse(raw!)
    expect(parsed.state.token).toBe('jwt.demo')
    expect(parsed.state.user.email).toBe('demo@example.com')
  })
})
