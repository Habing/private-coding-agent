import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect } from 'react'

import { api } from '@/lib/api'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth'
import type { QuotaResponse } from '@/types/api'

function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (n >= 10_000) return `${Math.round(n / 1000)}k`
  return n.toLocaleString('zh-CN')
}

function formatResetsAt(iso: string | undefined): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  return d.toLocaleString('zh-CN', {
    month: 'numeric',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

export function QuotaBar() {
  const token = useAuthStore((s) => s.token)
  const queryClient = useQueryClient()

  const { data } = useQuery({
    queryKey: ['quota'],
    queryFn: () => api<QuotaResponse>('/quota', { token }),
    enabled: !!token,
    refetchInterval: 30_000,
    staleTime: 10_000,
  })

  useEffect(() => {
    const onFocus = () => {
      void queryClient.invalidateQueries({ queryKey: ['quota'] })
    }
    window.addEventListener('focus', onFocus)
    return () => window.removeEventListener('focus', onFocus)
  }, [queryClient])

  const q = data?.llm_tokens
  if (!q?.enabled || q.cap <= 0) return null

  const pct = (q.used / q.cap) * 100
  const barWidth = Math.min(100, pct)
  const over = q.used > q.cap
  const high = !over && pct >= 80

  const barColor = over
    ? 'bg-destructive'
    : high
      ? 'bg-amber-500'
      : 'bg-primary'

  const hint = q.resets_at
    ? `UTC 日配额，约于 ${formatResetsAt(q.resets_at)} 重置`
    : 'UTC 日配额'

  return (
    <div
      className="hidden min-w-[9rem] flex-col gap-0.5 sm:flex md:min-w-[11rem]"
      title={hint}
      aria-label={`LLM 今日用量 ${q.used}，上限 ${q.cap}`}
    >
      <div className="flex items-center justify-between gap-2 text-[10px] leading-none text-muted-foreground">
        <span>LLM 今日</span>
        <span className={cn(over && 'font-medium text-destructive')}>
          {formatTokens(q.used)}/{formatTokens(q.cap)}
          {over ? ` (${Math.round(pct)}%)` : ''}
        </span>
      </div>
      <div
        className="h-1.5 w-full overflow-hidden rounded-full bg-muted"
        role="progressbar"
        aria-valuenow={Math.min(q.used, q.cap)}
        aria-valuemin={0}
        aria-valuemax={q.cap}
      >
        <div
          className={cn('h-full rounded-full transition-[width]', barColor)}
          style={{ width: `${barWidth}%` }}
        />
      </div>
    </div>
  )
}
