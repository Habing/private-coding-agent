import { useQuery } from '@tanstack/react-query'
import { useParams } from 'react-router-dom'

import { Composer } from '@/components/Composer'
import { MessageList } from '@/components/MessageList'
import { SandboxFiles } from '@/components/SandboxFiles'
import { useChatSocket } from '@/hooks/useChatSocket'
import { api } from '@/lib/api'
import { profileLabel } from '@/lib/profileLabels'
import { useAuthStore } from '@/stores/auth'
import type { MessageListResponse, Session } from '@/types/api'

export function Chat() {
  const { id } = useParams<{ id: string }>()
  const token = useAuthStore((s) => s.token)

  const { data: session } = useQuery({
    queryKey: ['session', id],
    queryFn: () => api<Session>(`/sessions/${encodeURIComponent(id!)}`, { token }),
    enabled: !!id && !!token,
  })

  const { data, isLoading } = useQuery({
    queryKey: ['messages', id],
    queryFn: () =>
      api<MessageListResponse>(`/sessions/${encodeURIComponent(id!)}/messages`, { token }),
    enabled: !!id && !!token,
  })

  const { status, events, errorMessage, awaitingReply, sendUserMessage } =
    useChatSocket(id)

  const history = data?.messages ?? []
  const composerDisabled = status !== 'open' || awaitingReply

  return (
    <div className="flex h-full">
      <div className="flex min-w-0 flex-1 flex-col">
        <div className="border-b px-4 py-1.5 text-[11px] text-muted-foreground">
          <StatusLabel status={status} errorMessage={errorMessage} />
        </div>
        {isLoading ? (
          <div className="flex-1 px-4 py-3 text-xs text-muted-foreground">加载中…</div>
        ) : (
          <MessageList
            history={history}
            events={events}
            awaitingReply={awaitingReply}
          />
        )}
        <Composer
          onSend={(content) => sendUserMessage(content)}
          disabled={composerDisabled}
          placeholder={awaitingReply ? '等待回复中…' : '输入消息…'}
        />
      </div>
      <aside className="flex w-64 shrink-0 flex-col border-l bg-muted/20">
        <div className="border-b px-3 py-2 text-xs">
          <div className="font-medium text-foreground">会话设置</div>
          <dl className="mt-1 space-y-0.5 text-muted-foreground">
            <div>
              <dt className="inline">模型 </dt>
              <dd className="inline font-mono text-[10px]">{session?.model ?? '—'}</dd>
            </div>
            <div>
              <dt className="inline">智能体 </dt>
              <dd
                className="inline text-[10px]"
                title={session?.profile}
              >
                {session?.profile ? profileLabel(session.profile) : '—'}
              </dd>
            </div>
            {session?.sandbox_id && (
              <div>
                <dt className="inline">沙箱 </dt>
                <dd className="inline truncate font-mono text-[10px]" title={session.sandbox_id}>
                  {session.sandbox_id.slice(0, 8)}…
                </dd>
              </div>
            )}
          </dl>
        </div>
        {session?.sandbox_id ? (
          <SandboxFiles sandboxId={session.sandbox_id} />
        ) : (
          <p className="p-3 text-xs text-muted-foreground">无绑定沙箱</p>
        )}
      </aside>
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
