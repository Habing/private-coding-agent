/** Admin GET /admin/workflows/:slug/triggers row shape. */
export interface WorkflowTriggerRow {
  trigger_id: string
  kind: 'cron' | 'webhook'
  enabled: boolean
  cron_expr?: string
  timezone?: string
  webhook_url?: string
  webhook_token_suffix?: string
  next_run_at?: string | null
  last_run_at?: string | null
  last_status?: string
  last_error?: string
  default_inputs?: Record<string, unknown>
}

export interface WorkflowTriggersResponse {
  triggers: WorkflowTriggerRow[]
  webhook_base_url: string
}

export interface TriggerSummaryLine {
  id: string
  kind: 'cron' | 'webhook'
  detail: string
}

/** Lightweight DSL scan for triggers: (used before publish when API rows are absent). */
export function parseTriggerSummariesFromDSL(dsl: string): TriggerSummaryLine[] {
  const lines = dsl.replace(/\r\n/g, '\n').split('\n')
  const out: TriggerSummaryLine[] = []
  let inTriggers = false
  let indent = 0
  let cur: TriggerSummaryLine | null = null

  const flush = () => {
    if (cur && (cur.kind === 'webhook' || cur.detail)) {
      out.push(cur)
    }
    cur = null
  }

  for (const raw of lines) {
    const line = raw.trimEnd()
    const trimmed = line.trim()
    if (!trimmed || trimmed.startsWith('#')) continue

    if (!inTriggers) {
      if (/^triggers:\s*$/.test(trimmed)) {
        inTriggers = true
      }
      continue
    }

    const leading = line.match(/^(\s*)/)?.[1]?.length ?? 0
    if (trimmed && leading <= indent && !trimmed.startsWith('- ')) {
      if (cur) flush()
      break
    }

    if (trimmed.startsWith('- ')) {
      if (cur) flush()
      indent = leading
      cur = { id: '', kind: 'webhook', detail: '' }
      const idMatch = trimmed.match(/id:\s*([a-z][a-z0-9-]*)/)
      if (idMatch) cur.id = idMatch[1]
      continue
    }

    if (!cur) continue
    const cronMatch = trimmed.match(/^cron:\s*["']?([^"']+)["']?/)
    if (cronMatch) {
      cur.kind = 'cron'
      cur.detail = cronMatch[1].trim()
      continue
    }
    if (/^webhook:\s*/.test(trimmed)) {
      cur.kind = 'webhook'
      cur.detail = 'webhook'
    }
  }
  if (cur) flush()
  return out
}

export function triggerSummaryLabel(row: TriggerSummaryLine): string {
  if (row.kind === 'cron') {
    return `cron · ${row.id} · ${row.detail}`
  }
  return `webhook · ${row.id}`
}

export function triggerRowLabel(row: WorkflowTriggerRow): string {
  if (row.kind === 'cron') {
    const tz = row.timezone && row.timezone !== 'UTC' ? ` (${row.timezone})` : ''
    return `cron · ${row.trigger_id} · ${row.cron_expr ?? ''}${tz}`
  }
  const suffix = row.webhook_token_suffix ? `…${row.webhook_token_suffix}` : ''
  return `webhook · ${row.trigger_id}${suffix ? ` · ${suffix}` : ''}`
}

export async function copyText(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text)
    return true
  } catch {
    return false
  }
}
