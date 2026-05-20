import { useQuery } from '@tanstack/react-query'
import { useParams } from 'react-router-dom'

import { Composer } from '@/components/Composer'
import { api } from '@/lib/api'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth'
import type { Message, MessageListResponse } from '@/types/api'

function Bubble({ msg }: { msg: Message }) {
  if (msg.role === 'user') {
    return (
      <div className="flex justify-end">
        <div className="max-w-[80%] whitespace-pre-wrap rounded-2xl bg-primary px-3 py-2 text-sm text-primary-foreground">
          {msg.content}
        </div>
      </div>
    )
  }
  if (msg.role === 'tool') {
    return (
      <div className="flex pl-6">
        <div className="max-w-[80%] rounded-md border bg-muted px-3 py-2 text-xs font-mono text-muted-foreground">
          <div className="mb-1 text-[10px] uppercase tracking-wide opacity-70">
            tool result {msg.tool_call_id && `· ${msg.tool_call_id}`}
          </div>
          <pre className="whitespace-pre-wrap break-words">{msg.content}</pre>
        </div>
      </div>
    )
  }
  if (msg.role === 'system') {
    return (
      <div className="flex justify-center">
        <div className="rounded-full bg-muted px-3 py-1 text-xs text-muted-foreground">
          {msg.content}
        </div>
      </div>
    )
  }
  // assistant
  return (
    <div className="flex justify-start">
      <div
        className={cn(
          'max-w-[80%] whitespace-pre-wrap rounded-2xl border bg-card px-3 py-2 text-sm',
        )}
      >
        {msg.content}
      </div>
    </div>
  )
}

export function Chat() {
  const { id } = useParams<{ id: string }>()
  const token = useAuthStore((s) => s.token)

  const { data, isLoading } = useQuery({
    queryKey: ['messages', id],
    queryFn: () =>
      api<MessageListResponse>(`/sessions/${encodeURIComponent(id!)}/messages`, { token }),
    enabled: !!id && !!token,
  })

  function onSend(content: string) {
    // WS wiring lands in Task 7.
    console.warn('chat send (no WS yet):', content)
  }

  return (
    <div className="flex h-full flex-col">
      <div className="flex-1 space-y-3 overflow-y-auto px-4 py-3">
        {isLoading && <div className="text-xs text-muted-foreground">加载中…</div>}
        {data?.messages.map((m) => <Bubble key={m.id} msg={m} />)}
        {data && data.messages.length === 0 && !isLoading && (
          <div className="flex h-full items-center justify-center text-xs text-muted-foreground">
            还没有消息，发一条试试
          </div>
        )}
      </div>
      <Composer onSend={onSend} disabled={false} />
    </div>
  )
}
