import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect } from 'react'
import { Navigate, useNavigate } from 'react-router-dom'

import { ApiError, api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth'
import type { CreateSessionRequest, Session, SessionListResponse } from '@/types/api'

// provider:model — dashscope from migration 0012; requires DASHSCOPE_API_KEY in compose .env
const DEFAULT_MODEL = 'dashscope:qwen3.6-plus'

export function Home() {
  const navigate = useNavigate()
  const token = useAuthStore((s) => s.token)
  const clearAuth = useAuthStore((s) => s.clear)
  const queryClient = useQueryClient()

  const { data, error, isLoading } = useQuery({
    queryKey: ['sessions'],
    queryFn: () => api<SessionListResponse>('/sessions', { token }),
    enabled: !!token,
  })

  const createMut = useMutation({
    mutationFn: () => {
      const body: CreateSessionRequest = { model: DEFAULT_MODEL, profile: 'coding' }
      return api<Session>('/sessions', {
        token,
        method: 'POST',
        body: JSON.stringify(body),
      })
    },
    onSuccess: (sess) => {
      queryClient.invalidateQueries({ queryKey: ['sessions'] })
      navigate(`/sessions/${sess.id}`, { replace: true })
    },
  })

  const sessionCount = data?.sessions.length
  const createPending = createMut.isPending
  const createTrigger = createMut.mutate
  useEffect(() => {
    if (sessionCount === 0 && !createPending) {
      createTrigger()
    }
  }, [sessionCount, createPending, createTrigger])

  const isUnauthorized = error instanceof ApiError && error.status === 401
  useEffect(() => {
    if (isUnauthorized) clearAuth()
  }, [isUnauthorized, clearAuth])

  if (isUnauthorized) {
    return <Navigate to="/login" replace />
  }

  if (isLoading || sessionCount === undefined || sessionCount === 0) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
        {sessionCount === 0 ? '正在创建首个会话…' : '加载中…'}
      </div>
    )
  }

  return (
    <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
      请选择一个会话开始对话
    </div>
  )
}
