import { afterEach, describe, expect, it } from 'vitest'

import {
  readSessionSidebarOpen,
  writeSessionSidebarOpen,
} from '@/lib/sessionSidebarStorage'

describe('sessionSidebarStorage', () => {
  afterEach(() => {
    localStorage.removeItem('pca.ui.sessionSidebarOpen')
  })

  it('defaults to open', () => {
    expect(readSessionSidebarOpen()).toBe(true)
  })

  it('persists collapsed state', () => {
    writeSessionSidebarOpen(false)
    expect(readSessionSidebarOpen()).toBe(false)
    writeSessionSidebarOpen(true)
    expect(readSessionSidebarOpen()).toBe(true)
  })
})
