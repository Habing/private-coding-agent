import { useQuery } from '@tanstack/react-query'
import { useParams } from 'react-router-dom'

import { Composer } from '@/components/Composer'
import { MessageList } from '@/components/MessageList'
import { useChatSocket } from '@/hooks/useChatSocket'
import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth'
import type { MessageListResponse } from '@/types/api'

export function Chat() {
  const { id } = useParams<{ id: string }>()
  const token = useAuthStore((s) => s.token)

  const { data, isLoading } = useQuery({
    queryKey: ['messages', id],
    queryFn: () =>
      api<MessageListResponse>(`/sessions/${encodeURIComponent(id!)}/messages`, { token }),
    enabled: !!id && !!token,
  })

  const { status, events, errorMessage, sendUserMessage } = useChatSocket(id)

  const history = data?.messages ?? []
  const composerDisabled = status !== 'open'

  return (
    <div className="flex h-full flex-col">
      <div className="border-b px-4 py-1.5 text-[11px] text-muted-foreground">
        <StatusLabel status={status} errorMessage={errorMessage} />
      </div>
      {isLoading ? (
        <div className="flex-1 px-4 py-3 text-xs text-muted-foreground">加载中…</div>
      ) : (
        <MessageList history={history} events={events} />
      )}
      <Composer
        onSend={(content) => sendUserMessage(content)}
        disabled={composerDisabled}
      />
    </div>
  )
}

function StatusLabel({
  status,
  errorMessage,
}: {
  status: ReturnType<typeof useChatSocket>['status']
  errorMessage: string | null
}) {
  if (status === 'open') return <span className="text-emerald-600">已连接</span>
  if (status === 'connecting') return <span>连接中…</span>
  if (status === 'closed') return <span>已关闭</span>
  return <span className="text-destructive">连接错误{errorMessage ? `：${errorMessage}` : ''}</span>
}
