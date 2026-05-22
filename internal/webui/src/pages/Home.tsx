import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { Navigate, useNavigate } from 'react-router-dom'

import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { ApiError, api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth'
import type {
  CreateSessionRequest,
  ProfileListResponse,
  Session,
  SessionListResponse,
} from '@/types/api'

// provider:model — dashscope from migration 0012; requires DASHSCOPE_API_KEY in compose .env
const DEFAULT_MODEL = 'dashscope:qwen3.6-plus'
const DEFAULT_PROFILE = 'coding'

export function Home() {
  const navigate = useNavigate()
  const token = useAuthStore((s) => s.token)
  const clearAuth = useAuthStore((s) => s.clear)
  const queryClient = useQueryClient()
  const [profile, setProfile] = useState(DEFAULT_PROFILE)

  const sessionsQ = useQuery({
    queryKey: ['sessions'],
    queryFn: () => api<SessionListResponse>('/sessions', { token }),
    enabled: !!token,
  })

  const profilesQ = useQuery({
    queryKey: ['profiles'],
    queryFn: () => api<ProfileListResponse>('/agent/profiles', { token }),
    enabled: !!token,
    staleTime: 5 * 60 * 1000,
  })

  const [createErr, setCreateErr] = useState<string | null>(null)
  const createMut = useMutation({
    mutationFn: () => {
      setCreateErr(null)
      const body: CreateSessionRequest = { model: DEFAULT_MODEL, profile }
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
    onError: (e) => setCreateErr(humanError(e)),
  })

  const isUnauthorized =
    sessionsQ.error instanceof ApiError && sessionsQ.error.status === 401
  useEffect(() => {
    if (isUnauthorized) clearAuth()
  }, [isUnauthorized, clearAuth])

  if (isUnauthorized) {
    return <Navigate to="/login" replace />
  }
  if (sessionsQ.isLoading || sessionsQ.data === undefined) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
        加载中…
      </div>
    )
  }

  const profiles = profilesQ.data?.profiles ?? []
  const hasSessions = sessionsQ.data.sessions.length > 0
  const selected = profiles.find((p) => p.name === profile)
  const canCreate = !createMut.isPending && profiles.length > 0

  return (
    <div className="flex h-full flex-col items-center justify-center gap-4 p-6 text-sm">
      <div className="text-muted-foreground">
        {hasSessions
          ? '从左侧选择会话，或新建一个会话'
          : '欢迎，新建你的第一个会话'}
      </div>
      <div className="flex min-w-[280px] flex-col gap-3 rounded-md border bg-card p-4">
        <div className="flex flex-col gap-1">
          <Label htmlFor="home-profile">Profile</Label>
          <select
            id="home-profile"
            className="h-9 rounded-md border bg-background px-2 text-sm"
            value={profile}
            onChange={(e) => setProfile(e.target.value)}
            disabled={profilesQ.isLoading || profiles.length === 0}
          >
            {profiles.map((p) => (
              <option key={p.name} value={p.name}>
                {p.name}
              </option>
            ))}
          </select>
          {selected?.description && (
            <p className="text-xs text-muted-foreground">{selected.description}</p>
          )}
        </div>
        <Button onClick={() => createMut.mutate()} disabled={!canCreate}>
          {createMut.isPending ? '创建中…' : '新建会话'}
        </Button>
        {createErr && (
          <div
            role="alert"
            className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-xs text-destructive"
          >
            {createErr}
          </div>
        )}
      </div>
    </div>
  )
}

function humanError(e: unknown): string {
  if (e instanceof ApiError) {
    try {
      const j = JSON.parse(e.body) as {
        error?: string
        kind?: string
        detail?: string
      }
      if (j.error === 'quota_exceeded' && j.kind === 'sandbox.active') {
        return '沙箱配额已满：每个租户活跃沙箱上限已达。请先归档闲置会话，或调大 PCA_QUOTA_SANDBOX_MAX_ACTIVE。'
      }
      if (j.error) return j.detail ? `${j.error}: ${j.detail}` : j.error
      return e.body || e.message
    } catch {
      return e.body || e.message
    }
  }
  return e instanceof Error ? e.message : String(e)
}
