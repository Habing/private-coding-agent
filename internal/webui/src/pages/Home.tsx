import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { CircleHelp } from 'lucide-react'
import { useEffect, useState } from 'react'
import { Navigate, useNavigate } from 'react-router-dom'

import { useGuide } from '@/hooks/useGuide'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { ApiError, api } from '@/lib/api'
import { QUICK_START_STEPS } from '@/lib/featureGuide'
import { hasSeenWelcomeGuide, markWelcomeGuideSeen } from '@/lib/guideStorage'
import { formatQuotaErrorMessage } from '@/lib/quotaError'
import { profileDescription, profileLabel } from '@/lib/profileLabels'
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
  const { openGuide } = useGuide()
  const token = useAuthStore((s) => s.token)
  const clearAuth = useAuthStore((s) => s.clear)
  const queryClient = useQueryClient()
  const [profile, setProfile] = useState(DEFAULT_PROFILE)
  const [showWelcome, setShowWelcome] = useState(() => !hasSeenWelcomeGuide())

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
      markWelcomeGuideSeen()
      setShowWelcome(false)
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

  function dismissWelcome() {
    markWelcomeGuideSeen()
    setShowWelcome(false)
  }

  function openGuideAndDismissWelcome() {
    dismissWelcome()
    openGuide()
  }

  return (
    <div className="flex h-full flex-col items-center justify-center gap-4 overflow-y-auto p-6 text-sm">
      {showWelcome && (
        <div className="w-full max-w-lg rounded-lg border border-primary/25 bg-primary/5 p-4">
          <h2 className="text-base font-semibold">欢迎使用 Private Coding Agent</h2>
          <p className="mt-1 text-xs text-muted-foreground">
            这是一个可调用工具、记忆与工作流的私有编程助手。按下面三步即可开始：
          </p>
          <ol className="mt-3 space-y-2">
            {QUICK_START_STEPS.map((step, i) => (
              <li key={step.title} className="flex gap-2 text-xs">
                <span
                  className="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-primary text-[10px] font-medium text-primary-foreground"
                  aria-hidden="true"
                >
                  {i + 1}
                </span>
                <span>
                  <span className="font-medium text-foreground">{step.title}</span>
                  {' — '}
                  <span className="text-muted-foreground">{step.description}</span>
                </span>
              </li>
            ))}
          </ol>
          <div className="mt-4 flex flex-wrap gap-2">
            <Button type="button" size="sm" onClick={openGuideAndDismissWelcome}>
              <CircleHelp className="mr-1 h-3.5 w-3.5" aria-hidden="true" />
              查看完整指引
            </Button>
            <Button type="button" size="sm" variant="outline" onClick={dismissWelcome}>
              知道了
            </Button>
          </div>
        </div>
      )}
      <div className="text-muted-foreground">
        {hasSessions
          ? '从左侧选择会话，或新建一个会话'
          : '欢迎，新建你的第一个会话'}
      </div>
      <div className="flex min-w-[280px] flex-col gap-3 rounded-md border bg-card p-4">
        <div className="flex flex-col gap-1">
          <Label htmlFor="home-profile">智能体类型</Label>
          <select
            id="home-profile"
            className="h-9 rounded-md border bg-background px-2 text-sm"
            value={profile}
            onChange={(e) => setProfile(e.target.value)}
            disabled={profilesQ.isLoading || profiles.length === 0}
          >
            {profiles.map((p) => (
              <option key={p.name} value={p.name}>
                {profileLabel(p.name)}
              </option>
            ))}
          </select>
          {selected && profileDescription(selected.name, selected.description) && (
            <p className="text-xs text-muted-foreground">
              {profileDescription(selected.name, selected.description)}
            </p>
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
        {!showWelcome && (
          <Button type="button" variant="link" size="sm" className="h-auto p-0 text-xs" onClick={openGuide}>
            不确定各功能做什么？查看使用指引
          </Button>
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
      if (j.error) {
        const raw = j.detail ? `${j.error}: ${j.detail}` : j.error
        return formatQuotaErrorMessage(raw)
      }
      return e.body || e.message
    } catch {
      return e.body || e.message
    }
  }
  const raw = e instanceof Error ? e.message : String(e)
  return formatQuotaErrorMessage(raw)
}
