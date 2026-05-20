import { ChevronDown, ChevronRight } from 'lucide-react'
import { useState } from 'react'

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
  const name = call?.tool ?? result?.tool ?? 'tool'
  const id = call?.tool_call_id ?? result?.tool_call_id

  let status: 'pending' | 'ok' | 'error' = 'pending'
  if (result) status = result.error ? 'error' : 'ok'

  const statusColor =
    status === 'ok'
      ? 'text-emerald-600'
      : status === 'error'
        ? 'text-destructive'
        : 'text-amber-600'

  return (
    <div className="flex pl-6">
      <div className="w-full max-w-[80%] rounded-md border bg-muted/40 text-xs">
        <button
          type="button"
          aria-expanded={open}
          aria-label={`tool ${name}`}
          onClick={() => setOpen((v) => !v)}
          className="flex w-full items-center gap-1 px-2 py-1.5 text-left"
        >
          {open ? (
            <ChevronDown className="h-3 w-3" />
          ) : (
            <ChevronRight className="h-3 w-3" />
          )}
          <span className="font-mono font-medium">{name}</span>
          <span className={cn('ml-1', statusColor)}>· {status}</span>
          {id && <span className="ml-1 opacity-50">· {id}</span>}
        </button>
        {open && (
          <div className="space-y-2 border-t px-2 py-2 font-mono text-[11px]">
            {call?.input !== undefined && (
              <div>
                <div className="mb-0.5 text-[10px] uppercase tracking-wide opacity-60">
                  input
                </div>
                <pre className="whitespace-pre-wrap break-words">
                  {formatPayload(call.input)}
                </pre>
              </div>
            )}
            {result?.output !== undefined && (
              <div>
                <div className="mb-0.5 text-[10px] uppercase tracking-wide opacity-60">
                  output
                </div>
                <pre className="whitespace-pre-wrap break-words">
                  {formatPayload(result.output)}
                </pre>
              </div>
            )}
            {result?.error && (
              <div>
                <div className="mb-0.5 text-[10px] uppercase tracking-wide text-destructive">
                  error
                </div>
                <pre className="whitespace-pre-wrap break-words text-destructive">
                  {result.error}
                </pre>
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  )
}
