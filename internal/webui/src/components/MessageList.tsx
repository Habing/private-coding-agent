import { useEffect, useRef } from 'react'

import { ToolCallCard } from '@/components/ToolCallCard'
import { WorkflowProposalCard } from '@/components/WorkflowProposalCard'
import { TypingIndicator } from '@/components/TypingIndicator'
import { shouldShowWaitingIndicator } from '@/lib/chatWaiting'
import {
  normalizeAgentEvent,
  eventToolName,
  parseJSONString,
} from '@/lib/agentEvent'
import { parseWorkflowProposalFromResult } from '@/lib/workflowProposal'
import { cn } from '@/lib/utils'
import type { AgentEvent, Message } from '@/types/api'

export interface MessageListProps {
  history: Message[]
  events: AgentEvent[]
  awaitingReply?: boolean
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

function AssistantBubble({ text, streaming }: { text: string; streaming?: boolean }) {
  return (
    <div className="flex justify-start">
      <div
        className={cn(
          'max-w-[80%] whitespace-pre-wrap rounded-2xl border bg-card px-3 py-2 text-sm',
          streaming && 'border-primary/30 shadow-sm',
        )}
      >
        {text}
        {streaming && (
          <span
            className="ml-0.5 inline-block h-[1em] w-0.5 animate-pulse bg-primary align-[-0.15em]"
            aria-hidden="true"
          />
        )}
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

interface RenderItem {
  key: string
  node: JSX.Element
  /** True while assistant text is still streaming (assistant_delta). */
  streaming?: boolean
  streamText?: string
}

function appendAssistantText(items: RenderItem[], key: string, chunk: string) {
  const last = items[items.length - 1]
  if (last?.streaming) {
    last.streamText = (last.streamText ?? '') + chunk
    last.node = <AssistantBubble text={last.streamText} streaming />
    return
  }
  items.push({
    key,
    streaming: true,
    streamText: chunk,
    node: <AssistantBubble text={chunk} streaming />,
  })
}

function finalizeStreamingAssistant(items: RenderItem[]) {
  const last = items[items.length - 1]
  if (last?.streaming) {
    last.streaming = false
  }
}

function buildHistoryItems(history: Message[]): RenderItem[] {
  const items: RenderItem[] = []
  const pendingCalls = new Map<
    string,
    { tool: string; input: unknown; toolCallId: string }
  >()

  for (const msg of history) {
    if (msg.role === 'user') {
      items.push({ key: msg.id, node: <UserBubble text={msg.content} /> })
      continue
    }
    if (msg.role === 'system') {
      items.push({ key: msg.id, node: <SystemBubble text={msg.content} /> })
      continue
    }
    if (msg.role === 'assistant') {
      if (msg.content.trim()) {
        items.push({ key: msg.id, node: <AssistantBubble text={msg.content} /> })
      }
      for (const tc of msg.tool_calls ?? []) {
        pendingCalls.set(tc.id, {
          tool: tc.function.name,
          input: parseJSONString(tc.function.arguments),
          toolCallId: tc.id,
        })
      }
      continue
    }
    if (msg.role === 'tool') {
      const pending = msg.tool_call_id
        ? pendingCalls.get(msg.tool_call_id)
        : undefined
      const meta = msg.metadata ?? {}
      const toolName =
        pending?.tool ??
        (typeof meta.tool_name === 'string' ? meta.tool_name : undefined) ??
        'tool'
      const toolError =
        typeof meta.tool_error === 'string' && meta.tool_error
          ? meta.tool_error
          : undefined

      const resultEv = normalizeAgentEvent({
        kind: 'tool_result',
        tool_call_id: msg.tool_call_id,
        tool_name: toolName,
        tool_output: toolError ? undefined : msg.content,
        tool_error: toolError,
      })

      if (toolName === 'workflow.propose') {
        const proposal = parseWorkflowProposalFromResult(resultEv)
        if (proposal) {
          items.push({
            key: msg.id,
            node: <WorkflowProposalCard payload={proposal} />,
          })
          if (msg.tool_call_id) pendingCalls.delete(msg.tool_call_id)
          continue
        }
      }

      const callEv = pending
        ? normalizeAgentEvent({
            kind: 'tool_call',
            tool_call_id: pending.toolCallId,
            tool_name: pending.tool,
            tool_input: pending.input,
          })
        : undefined

      items.push({
        key: msg.id,
        node: <ToolCallCard call={callEv} result={resultEv} />,
      })
      if (msg.tool_call_id) pendingCalls.delete(msg.tool_call_id)
    }
  }

  return items
}

function buildEventItems(events: AgentEvent[]): RenderItem[] {
  const normalized = events.map(normalizeAgentEvent)
  const items: RenderItem[] = []
  const callIndex = new Map<string, number>()

  normalized.forEach((ev, i) => {
    if (ev.kind === 'tool_call') {
      const id = ev.tool_call_id ?? `idx-${i}`
      const at = items.length
      callIndex.set(id, at)
      if (eventToolName(ev) === 'workflow.propose') {
        return
      }
      items.push({
        key: `ev-${i}`,
        node: <ToolCallCard call={ev} />,
      })
      return
    }
    if (ev.kind === 'tool_result') {
      const id = ev.tool_call_id ?? ''
      const proposal = parseWorkflowProposalFromResult(ev)
      if (proposal) {
        items.push({
          key: `ev-${i}-proposal`,
          node: <WorkflowProposalCard payload={proposal} />,
        })
        return
      }
      const at = id ? callIndex.get(id) : undefined
      if (at !== undefined) {
        const prev = items[at]
        items[at] = {
          key: prev.key,
          node: (
            <ToolCallCard
              call={extractCall(normalized, id)}
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
    if (ev.kind === 'assistant_delta') {
      const delta = ev.text ?? ''
      if (!delta) return
      appendAssistantText(items, `ev-${i}`, delta)
      return
    }
    if (ev.kind === 'final') {
      finalizeStreamingAssistant(items)
      return
    }
    if (ev.kind === 'assistant_message') {
      const text = ev.text ?? ''
      if (!text) {
        finalizeStreamingAssistant(items)
        return
      }
      const last = items[items.length - 1]
      if (last?.streaming) {
        last.streamText = text
        last.node = <AssistantBubble text={text} />
        last.streaming = false
        return
      }
      items.push({
        key: `ev-${i}`,
        node: <AssistantBubble text={text} />,
      })
      return
    }
    if (ev.kind === 'error') {
      items.push({
        key: `ev-${i}`,
        node: <ErrorBanner text={ev.error ?? ev.text ?? 'error'} />,
      })
    }
  })

  return items
}

function extractCall(events: AgentEvent[], id: string): AgentEvent | undefined {
  return events.find((e) => e.kind === 'tool_call' && e.tool_call_id === id)
}

export function MessageList({
  history,
  events,
  awaitingReply = false,
}: MessageListProps) {
  const scrollRef = useRef<HTMLDivElement | null>(null)

  const historyItems = buildHistoryItems(history)
  const eventItems = buildEventItems(events)
  const showWaiting = shouldShowWaitingIndicator(events, awaitingReply)
  const empty =
    historyItems.length === 0 && eventItems.length === 0 && !showWaiting

  useEffect(() => {
    const el = scrollRef.current
    if (!el) return
    el.scrollTop = el.scrollHeight
  }, [history, events, showWaiting])

  return (
    <div ref={scrollRef} className="flex-1 space-y-3 overflow-y-auto px-4 py-3">
      {historyItems.map((it) => (
        <div key={it.key}>{it.node}</div>
      ))}
      {eventItems.map((it) => (
        <div key={it.key}>{it.node}</div>
      ))}
      {showWaiting && <TypingIndicator label="正在思考…" />}
      {empty && (
        <div className="flex h-full items-center justify-center text-xs text-muted-foreground">
          还没有消息，发一条试试
        </div>
      )}
    </div>
  )
}
