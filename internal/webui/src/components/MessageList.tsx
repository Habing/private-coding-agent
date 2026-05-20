import { useEffect, useRef } from 'react'

import { ToolCallCard } from '@/components/ToolCallCard'
import { cn } from '@/lib/utils'
import type { AgentEvent, Message } from '@/types/api'

export interface MessageListProps {
  history: Message[]
  events: AgentEvent[]
}

function UserBubble({ text }: { text: string }) {
  return (
    <div className="flex justify-end">
      <div className="max-w-[80%] whitespace-pre-wrap rounded-2xl bg-primary px-3 py-2 text-sm text-primary-foreground">
        {text}
      </div>
    </div>
  )
}

function AssistantBubble({ text }: { text: string }) {
  return (
    <div className="flex justify-start">
      <div
        className={cn(
          'max-w-[80%] whitespace-pre-wrap rounded-2xl border bg-card px-3 py-2 text-sm',
        )}
      >
        {text}
      </div>
    </div>
  )
}

function SystemBubble({ text }: { text: string }) {
  return (
    <div className="flex justify-center">
      <div className="rounded-full bg-muted px-3 py-1 text-xs text-muted-foreground">
        {text}
      </div>
    </div>
  )
}

function ErrorBanner({ text }: { text: string }) {
  return (
    <div
      role="alert"
      className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-xs text-destructive"
    >
      {text}
    </div>
  )
}

function HistoryBubble({ msg }: { msg: Message }) {
  if (msg.role === 'user') return <UserBubble text={msg.content} />
  if (msg.role === 'system') return <SystemBubble text={msg.content} />
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
  return <AssistantBubble text={msg.content} />
}

interface RenderItem {
  key: string
  node: JSX.Element
}

function buildEventItems(events: AgentEvent[]): RenderItem[] {
  const items: RenderItem[] = []
  const callIndex = new Map<string, number>()

  events.forEach((ev, i) => {
    if (ev.kind === 'tool_call') {
      const id = ev.tool_call_id ?? `idx-${i}`
      const at = items.length
      callIndex.set(id, at)
      items.push({
        key: `ev-${i}`,
        node: <ToolCallCard call={ev} />,
      })
      return
    }
    if (ev.kind === 'tool_result') {
      const id = ev.tool_call_id ?? ''
      const at = id ? callIndex.get(id) : undefined
      if (at !== undefined) {
        const prev = items[at]
        // Re-render the same card with both call + result so the existing
        // item updates in place.
        items[at] = {
          key: prev.key,
          node: (
            <ToolCallCard
              call={extractCall(events, id)}
              result={ev}
            />
          ),
        }
      } else {
        items.push({ key: `ev-${i}`, node: <ToolCallCard result={ev} /> })
      }
      return
    }
    if (ev.kind === 'user') {
      items.push({
        key: `ev-${i}`,
        node: <UserBubble text={ev.text ?? ''} />,
      })
      return
    }
    if (ev.kind === 'assistant_message' || ev.kind === 'final') {
      const text = ev.text ?? ''
      if (!text) return
      items.push({
        key: `ev-${i}`,
        node: <AssistantBubble text={text} />,
      })
      return
    }
    if (ev.kind === 'error') {
      items.push({
        key: `ev-${i}`,
        node: <ErrorBanner text={ev.error ?? 'error'} />,
      })
    }
  })

  return items
}

function extractCall(events: AgentEvent[], id: string): AgentEvent | undefined {
  return events.find((e) => e.kind === 'tool_call' && e.tool_call_id === id)
}

export function MessageList({ history, events }: MessageListProps) {
  const scrollRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    const el = scrollRef.current
    if (!el) return
    el.scrollTop = el.scrollHeight
  }, [history, events])

  const eventItems = buildEventItems(events)
  const empty = history.length === 0 && eventItems.length === 0

  return (
    <div ref={scrollRef} className="flex-1 space-y-3 overflow-y-auto px-4 py-3">
      {history.map((m) => (
        <HistoryBubble key={m.id} msg={m} />
      ))}
      {eventItems.map((it) => (
        <div key={it.key}>{it.node}</div>
      ))}
      {empty && (
        <div className="flex h-full items-center justify-center text-xs text-muted-foreground">
          还没有消息，发一条试试
        </div>
      )}
    </div>
  )
}
