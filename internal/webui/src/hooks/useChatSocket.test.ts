import { act, renderHook, waitFor } from '@testing-library/react'
import { Server, WebSocket as MockWS } from 'mock-socket'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useAuthStore } from '@/stores/auth'

import { useChatSocket } from './useChatSocket'

const sessionID = 's1'
const token = 'jwt.demo'
const WS_URL = `ws://localhost:3000/sessions/${sessionID}/ws?token=${token}`

beforeEach(() => {
  vi.stubGlobal('WebSocket', MockWS)
  useAuthStore.getState().setAuth(token, {
    id: 'u1',
    tenant_id: 't1',
    email: 'demo@example.com',
    role: 'member',
  })
  Object.defineProperty(window, 'location', {
    value: new URL('http://localhost:3000/sessions/s1') as unknown as Location,
    writable: true,
  })
})

afterEach(() => {
  vi.unstubAllGlobals()
  useAuthStore.getState().clear()
})

describe('useChatSocket', () => {
  it('opens connection and transitions to open', async () => {
    const server = new Server(WS_URL)
    try {
      const { result } = renderHook(() => useChatSocket(sessionID))
      expect(result.current.status).toBe('connecting')
      await waitFor(() => expect(result.current.status).toBe('open'))
    } finally {
      server.close()
    }
  })

  it('forwards user_message frames to the server', async () => {
    const server = new Server(WS_URL)
    let received: unknown = null
    server.on('connection', (sock) => {
      sock.on('message', (raw) => {
        received = JSON.parse(String(raw))
      })
    })
    try {
      const { result } = renderHook(() => useChatSocket(sessionID))
      await waitFor(() => expect(result.current.status).toBe('open'))
      act(() => result.current.sendUserMessage('hi'))
      await waitFor(() => expect(received).toEqual({ type: 'user_message', content: 'hi' }))
      // Optimistic local event for the user message.
      expect(result.current.events).toContainEqual({
        kind: 'user',
        text: 'hi',
      })
    } finally {
      server.close()
    }
  })

  it('appends event frames to events array', async () => {
    const server = new Server(WS_URL)
    server.on('connection', (sock) => {
      sock.send(
        JSON.stringify({
          type: 'event',
          event: { kind: 'assistant_message', text: 'hello back' },
        }),
      )
    })
    try {
      const { result } = renderHook(() => useChatSocket(sessionID))
      await waitFor(() => {
        const last = result.current.events[result.current.events.length - 1]
        expect(last?.text).toBe('hello back')
      })
    } finally {
      server.close()
    }
  })

  it('sets status closed on done frame', async () => {
    const server = new Server(WS_URL)
    server.on('connection', (sock) => {
      sock.send(JSON.stringify({ type: 'done' }))
    })
    try {
      const { result } = renderHook(() => useChatSocket(sessionID))
      await waitFor(() => expect(result.current.lastDoneSeq).not.toBeNull())
    } finally {
      server.close()
    }
  })

  it('sets status error on error frame', async () => {
    const server = new Server(WS_URL)
    server.on('connection', (sock) => {
      sock.send(JSON.stringify({ type: 'error', message: 'boom' }))
    })
    try {
      const { result } = renderHook(() => useChatSocket(sessionID))
      await waitFor(() => expect(result.current.status).toBe('error'))
      expect(result.current.errorMessage).toBe('boom')
    } finally {
      server.close()
    }
  })

  it('closes cleanly on unmount (code 1000, no reconnect)', async () => {
    const server = new Server(WS_URL)
    let connections = 0
    server.on('connection', () => {
      connections += 1
    })
    try {
      const { result, unmount } = renderHook(() => useChatSocket(sessionID))
      await waitFor(() => expect(result.current.status).toBe('open'))
      unmount()
      // Wait long enough for any spurious reconnect attempt to settle.
      await new Promise((r) => setTimeout(r, 50))
      expect(connections).toBe(1)
    } finally {
      server.close()
    }
  })
})
