import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus, Trash2 } from 'lucide-react'
import { useEffect } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'

import { Button } from '@/components/ui/button'
import { ApiError, api } from '@/lib/api'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth'
import type { CreateSessionRequest, Session, SessionListResponse } from '@/types/api'

// No model picker UI in slice 8, so new sessions default to the mock model
// the rest of the project uses for E2E. Replace once a settings page lands.
const DEFAULT_MODEL = 'default-mock:gpt-4o'

export function SessionList() {
  const navigate = useNavigate()
  const params = useParams<{ id?: string }>()
  const activeID = params.id
  const queryClient = useQueryClient()

  const token = useAuthStore((s) => s.token)
  const clearAuth = useAuthStore((s) => s.clear)

  const { data, error, isLoading } = useQuery({
    queryKey: ['sessions'],
    queryFn: () => api<SessionListResponse>('/sessions', { token }),
    enabled: !!token,
  })

  const isUnauthorized = error instanceof ApiError && error.status === 401
  useEffect(() => {
    if (isUnauthorized) clearAuth()
  }, [isUnauthorized, clearAuth])

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
      navigate(`/sessions/${sess.id}`)
    },
  })

  const deleteMut = useMutation({
    mutationFn: (id: string) =>
      api<void>(`/sessions/${encodeURIComponent(id)}`, { token, method: 'DELETE' }),
    onSuccess: (_, id) => {
      queryClient.invalidateQueries({ queryKey: ['sessions'] })
      if (id === activeID) navigate('/', { replace: true })
    },
  })

  return (
    <div className="flex h-full flex-col">
      <div className="flex items-center justify-between border-b px-3 py-2">
        <div className="text-xs font-medium text-muted-foreground">会话</div>
        <Button
          size="sm"
          variant="outline"
          onClick={() => createMut.mutate()}
          disabled={createMut.isPending}
        >
          <Plus className="mr-1 h-3.5 w-3.5" />
          新建
        </Button>
      </div>
      <ul className="flex-1 overflow-y-auto p-2">
        {isLoading && <li className="px-2 py-1 text-xs text-muted-foreground">加载中…</li>}
        {data?.sessions.map((s) => {
          const active = s.id === activeID
          const title = s.title.trim() || 'Untitled'
          return (
            <li key={s.id} className="group flex items-center">
              <Link
                to={`/sessions/${s.id}`}
                aria-current={active ? 'page' : undefined}
                className={cn(
                  'flex-1 truncate rounded-md px-2 py-1.5 text-sm',
                  active
                    ? 'bg-accent font-medium text-accent-foreground'
                    : 'hover:bg-accent/50',
                )}
              >
                {title}
              </Link>
              <Button
                size="icon"
                variant="ghost"
                aria-label="删除"
                className="ml-1 h-7 w-7 opacity-0 group-hover:opacity-100"
                onClick={() => deleteMut.mutate(s.id)}
                disabled={deleteMut.isPending}
              >
                <Trash2 className="h-3.5 w-3.5" />
              </Button>
            </li>
          )
        })}
        {data && data.sessions.length === 0 && !isLoading && (
          <li className="px-2 py-1 text-xs text-muted-foreground">还没有会话</li>
        )}
      </ul>
    </div>
  )
}
