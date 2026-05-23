import { useCallback, useEffect, useRef, useState } from 'react'

import { wsURL } from '@/lib/ws'
import { useAuthStore } from '@/stores/auth'
import type { AgentEvent, ServerFrame } from '@/types/api'

export type ChatSocketStatus = 'connecting' | 'open' | 'closed' | 'error'

export interface UseChatSocketResult {
  status: ChatSocketStatus
  events: AgentEvent[]
  errorMessage: string | null
  lastDoneSeq: number | null
  /** True from send until server emits done or error for that turn. */
  awaitingReply: boolean
  sendUserMessage: (content: string) => void
  reset: () => void
}

export function useChatSocket(sessionID: string | undefined): UseChatSocketResult {
  const token = useAuthStore((s) => s.token)
  const [status, setStatus] = useState<ChatSocketStatus>('connecting')
  const [events, setEvents] = useState<AgentEvent[]>([])
  const [errorMessage, setErrorMessage] = useState<string | null>(null)
  const [lastDoneSeq, setLastDoneSeq] = useState<number | null>(null)
  const [awaitingReply, setAwaitingReply] = useState(false)

  const wsRef = useRef<WebSocket | null>(null)

  useEffect(() => {
    if (!sessionID || !token) return
    let cancelled = false
    setStatus('connecting')
    setErrorMessage(null)

    const ws = new WebSocket(wsURL(sessionID, token))
    wsRef.current = ws

    ws.onopen = () => {
      if (!cancelled) setStatus('open')
    }
    ws.onerror = () => {
      if (!cancelled) setStatus('error')
    }
    ws.onclose = (ev) => {
      if (cancelled) return
      // Code 1000 = normal close (e.g. unmount); anything else surfaces as error.
      if (ev.code === 1000) {
        setStatus('closed')
      } else {
        setStatus('error')
        setErrorMessage((prev) => prev ?? (ev.reason || `ws closed (${ev.code})`))
        setAwaitingReply(false)
      }
    }
    ws.onmessage = (ev) => {
      if (cancelled) return
      let frame: ServerFrame
      try {
        frame = JSON.parse(String(ev.data)) as ServerFrame
      } catch {
        return
      }
      if (frame.type === 'event') {
        setEvents((es) => [...es, frame.event])
      } else if (frame.type === 'done') {
        setLastDoneSeq(frame.seq ?? -1)
        setAwaitingReply(false)
      } else if (frame.type === 'error') {
        setStatus('error')
        setErrorMessage(frame.message)
        setAwaitingReply(false)
      }
      // 'pong' frames are no-ops; future heartbeat could observe them.
    }

    return () => {
      cancelled = true
      if (
        ws.readyState === WebSocket.OPEN ||
        ws.readyState === WebSocket.CONNECTING
      ) {
        ws.close(1000, 'unmount')
      }
      wsRef.current = null
    }
  }, [sessionID, token])

  const sendUserMessage = useCallback((content: string) => {
    const ws = wsRef.current
    if (!ws || ws.readyState !== WebSocket.OPEN) return
    ws.send(JSON.stringify({ type: 'user_message', content }))
    setEvents((es) => [...es, { kind: 'user', text: content }])
    setAwaitingReply(true)
  }, [])

  const reset = useCallback(() => {
    setEvents([])
    setLastDoneSeq(null)
    setErrorMessage(null)
    setAwaitingReply(false)
  }, [])

  return {
    status,
    events,
    errorMessage,
    lastDoneSeq,
    awaitingReply,
    sendUserMessage,
    reset,
  }
}
