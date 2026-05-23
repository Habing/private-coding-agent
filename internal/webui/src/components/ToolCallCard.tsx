import { ChevronDown, ChevronRight } from 'lucide-react'
import { useState } from 'react'

import {
  eventToolError,
  eventToolInput,
  eventToolName,
  eventToolOutput,
  normalizeAgentEvent,
} from '@/lib/agentEvent'
import { cn } from '@/lib/utils'
import type { AgentEvent } from '@/types/api'

function formatPayload(value: unknown): string {
  if (value == null) return ''
  if (typeof value === 'string') return value
  try {
    return JSON.stringify(value, null, 2)
  } catch {
    return String(value)
  }
}

export interface ToolCallCardProps {
  call?: AgentEvent
  result?: AgentEvent
}

export function ToolCallCard({ call, result }: ToolCallCardProps) {
  const [open, setOpen] = useState(false)
  const callEv = call ? normalizeAgentEvent(call) : undefined
  const resultEv = result ? normalizeAgentEvent(result) : undefined

  const name =
    eventToolName(callEv ?? resultEv ?? { kind: 'tool_call' }) ?? 'tool'
  const id = callEv?.tool_call_id ?? resultEv?.tool_call_id
  const input = callEv ? eventToolInput(callEv) : undefined
  const output = resultEv ? eventToolOutput(resultEv) : undefined
  const err = resultEv ? eventToolError(resultEv) : undefined

  let status: 'pending' | 'ok' | 'error' = 'pending'
  if (resultEv) status = err ? 'error' : 'ok'

  const statusColor =
    status === 'ok'
      ? 'text-emerald-600'
      : status === 'error'
        ? 'text-destructive'
        : 'text-amber-600'

  const hasInput = input !== undefined && formatPayload(input) !== ''
  const hasOutput = output !== undefined && formatPayload(output) !== ''
  const hasError = !!err
  const hasDetails = hasInput || hasOutput || hasError

  return (
    <section className="flex pl-6">
      <section className="w-full max-w-[80%] rounded-md border bg-muted/40 text-xs">
        <button
          type="button"
          aria-expanded={open}
          aria-label={`tool ${name}`}
          onClick={() => setOpen((v) => !v)}
          className="flex w-full items-center gap-1 px-2 py-1.5 text-left"
        >
          {open ? (
            <ChevronDown className="h-3 w-3 shrink-0" />
          ) : (
            <ChevronRight className="h-3 w-3 shrink-0" />
          )}
          <span className="font-mono font-medium">{name}</span>
          <span className={cn('ml-1', statusColor)}>· {status}</span>
          {id && (
            <span className="ml-1 truncate opacity-50" title={id}>
              · {id}
            </span>
          )}
        </button>
        {open && (
          <section className="space-y-2 border-t px-2 py-2 font-mono text-[11px]">
            {!hasDetails && (
              <p className="text-muted-foreground">（无 input / output 记录）</p>
            )}
            {hasInput && (
              <section>
                <p className="mb-0.5 text-[10px] uppercase tracking-wide opacity-60">
                  input
                </p>
                <pre className="whitespace-pre-wrap break-words">
                  {formatPayload(input)}
                </pre>
              </section>
            )}
            {hasOutput && (
              <section>
                <p className="mb-0.5 text-[10px] uppercase tracking-wide opacity-60">
                  output
                </p>
                <pre className="whitespace-pre-wrap break-words">
                  {formatPayload(output)}
                </pre>
              </section>
            )}
            {hasError && (
              <section>
                <p className="mb-0.5 text-[10px] uppercase tracking-wide text-destructive">
                  error
                </p>
                <pre className="whitespace-pre-wrap break-words text-destructive">
                  {err}
                </pre>
              </section>
            )}
          </section>
        )}
      </section>
    </section>
  )
}
