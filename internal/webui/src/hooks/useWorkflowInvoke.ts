import { useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'

import { canUseInvokeInputsForm } from '@/components/workflow/InvokeInputsForm'
import { ApiError } from '@/lib/api'
import { buildInvokeDefaults } from '@/lib/workflowDesignTree'
import { applyWorkflowStepEvent } from '@/lib/workflowInvokeLive'
import { useAuthStore } from '@/stores/auth'
import type {
  WorkflowDesignInput,
  WorkflowInvokeLiveStep,
  WorkflowInvokeResult,
  WorkflowInvokeStepEvent,
} from '@/types/api'

function humanError(e: unknown): string {
  if (e instanceof ApiError) return e.message
  if (e instanceof Error) return e.message
  return String(e)
}

export interface UseWorkflowInvokeOptions {
  slug: string
  inputSchema?: WorkflowDesignInput[]
  defaultInputs?: Record<string, unknown>
  /** When this changes (e.g. workflow version), trial inputs reset to defaults. */
  resetInputsKey?: string
  unsaved?: boolean
  onInvokeStart?: () => void
  onStepProgress?: (stepId: string) => void
  onInvokeEnd?: () => void
}

export function useWorkflowInvoke({
  slug,
  inputSchema,
  defaultInputs,
  resetInputsKey,
  unsaved,
  onInvokeStart,
  onStepProgress,
  onInvokeEnd,
}: UseWorkflowInvokeOptions) {
  const token = useAuthStore((s) => s.token)
  const qc = useQueryClient()
  const schema = inputSchema ?? []
  const useForm = canUseInvokeInputsForm(schema)
  const [invokeValues, setInvokeValues] = useState<Record<string, unknown>>({})
  const [inputsText, setInputsText] = useState('{}')
  const [showJson, setShowJson] = useState(false)
  const [dryRun, setDryRun] = useState(false)
  const [result, setResult] = useState<WorkflowInvokeResult | null>(null)
  const [liveSteps, setLiveSteps] = useState<WorkflowInvokeLiveStep[]>([])
  const [lastInvokedInputs, setLastInvokedInputs] = useState<Record<string, unknown> | null>(
    null,
  )
  const [inputsTouched, setInputsTouched] = useState(false)
  const [err, setErr] = useState<string | null>(null)
  const [streaming, setStreaming] = useState(false)

  useEffect(() => {
    setInputsTouched(false)
  }, [slug, resetInputsKey])

  useEffect(() => {
    if (inputsTouched) return
    const base =
      defaultInputs && Object.keys(defaultInputs).length > 0
        ? defaultInputs
        : buildInvokeDefaults(inputSchema)
    setInvokeValues(base)
    setInputsText(JSON.stringify(base, null, 2))
    setShowJson(false)
  }, [defaultInputs, inputSchema, inputsTouched, slug, resetInputsKey])

  function markInputsTouched() {
    setInputsTouched(true)
  }

  function applyInvokeValues(next: Record<string, unknown>) {
    markInputsTouched()
    setInvokeValues(next)
    setInputsText(JSON.stringify(next, null, 2))
  }

  function parseInvokeInputs(): Record<string, unknown> {
    if (useForm && !showJson) return invokeValues
    try {
      const parsed = JSON.parse(inputsText)
      if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
        return parsed as Record<string, unknown>
      }
      throw new Error('inputs must be a JSON object')
    } catch (e) {
      throw new Error('inputs JSON 解析失败: ' + (e as Error).message)
    }
  }

  const executing = streaming

  async function streamInvoke() {
    if (!token) return
    onInvokeStart?.()
    setStreaming(true)
    setErr(null)
    setResult(null)
    setLiveSteps([])

    let inputs: Record<string, unknown>
    try {
      inputs = parseInvokeInputs()
      setLastInvokedInputs(inputs)
    } catch (e) {
      setStreaming(false)
      setErr((e as Error).message)
      onInvokeEnd?.()
      return
    }

    const json = JSON.stringify(inputs)
    const b64 = btoa(unescape(encodeURIComponent(json)))
    const url = `/admin/workflows/${slug}/invoke/stream?dry_run=${dryRun ? '1' : '0'}&inputs_b64=${encodeURIComponent(b64)}`
    try {
      const resp = await fetch(url, {
        method: 'GET',
        headers: { Authorization: `Bearer ${token}` },
      })
      if (!resp.ok || !resp.body) {
        const body = await resp.text().catch(() => '')
        throw new ApiError(resp.status, body)
      }

      const reader = resp.body.getReader()
      const decoder = new TextDecoder()
      let buf = ''

      while (true) {
        const { value, done } = await reader.read()
        if (done) break
        buf += decoder.decode(value, { stream: true })
        for (;;) {
          const idx = buf.indexOf('\n\n')
          if (idx < 0) break
          const chunk = buf.slice(0, idx)
          buf = buf.slice(idx + 2)
          const lines = chunk.split('\n').map((l) => l.trimEnd())
          const event = lines.find((l) => l.startsWith('event:'))?.slice('event:'.length).trim()
          const dataLine =
            lines.find((l) => l.startsWith('data:'))?.slice('data:'.length).trim() ?? ''
          if (!event) continue
          if (event === 'step') {
            try {
              const j = JSON.parse(dataLine) as WorkflowInvokeStepEvent
              if (j.step_id) {
                setLiveSteps((prev) => applyWorkflowStepEvent(prev, j))
                if (j.kind === 'step_start') onStepProgress?.(j.step_id)
              }
            } catch {
              // ignore
            }
          }
          if (event === 'done') {
            const j = JSON.parse(dataLine) as WorkflowInvokeResult
            setResult(j)
            qc.invalidateQueries({ queryKey: ['workflow-runs', slug] })
          }
          if (event === 'error') {
            try {
              const j = JSON.parse(dataLine) as { detail?: string; error?: string }
              setErr(j.detail ? `${j.error ?? 'error'}: ${j.detail}` : j.error ?? 'error')
            } catch {
              setErr(dataLine || 'error')
            }
          }
        }
      }
    } catch (e) {
      setErr(humanError(e))
    } finally {
      setStreaming(false)
      onInvokeEnd?.()
    }
  }

  function tryExecute() {
    if (unsaved) {
      const ok = window.confirm(
        '当前画布有未保存的修改。试运行只会执行已保存到服务器的步骤，画布上新增的步骤不会运行。是否继续？',
      )
      if (!ok) return
    }
    void streamInvoke()
  }

  return {
    schema,
    useForm,
    invokeValues,
    setInvokeValues,
    applyInvokeValues,
    markInputsTouched,
    inputsText,
    setInputsText: (text: string) => {
      markInputsTouched()
      setInputsText(text)
    },
    showJson,
    setShowJson,
    dryRun,
    setDryRun,
    result,
    liveSteps,
    lastInvokedInputs,
    err,
    executing,
    streaming,
    tryExecute,
  }
}

export type WorkflowInvokeState = ReturnType<typeof useWorkflowInvoke>
