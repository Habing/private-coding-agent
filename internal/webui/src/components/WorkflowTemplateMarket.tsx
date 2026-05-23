import { useMutation, useQuery } from '@tanstack/react-query'
import { useEffect, useMemo, useState } from 'react'

import { WorkflowGraph } from '@/components/WorkflowGraph'
import { ToolBusToolSelect } from '@/components/ToolBusToolSelect'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ApiError, api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth'
import type {
  CreateWorkflowRequest,
  Workflow,
  WorkflowTemplate,
  WorkflowTemplateListResponse,
  WorkflowTemplatePreviewResponse,
} from '@/types/api'

export function WorkflowTemplateMarket({
  onCreated,
  onError,
}: {
  onCreated: (slug: string) => void
  onError: (msg: string | null) => void
}) {
  const token = useAuthStore((s) => s.token)
  const [expanded, setExpanded] = useState<string | null>(null)

  const listQ = useQuery({
    queryKey: ['workflow-templates'],
    queryFn: () =>
      api<WorkflowTemplateListResponse>('/agent/workflow/templates', { token }),
    enabled: !!token,
  })

  return (
    <Card>
      <CardHeader>
        <CardTitle>模板市场</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {listQ.isLoading && (
          <p className="text-sm text-muted-foreground">加载模板…</p>
        )}
        {listQ.error && (
          <p className="text-sm text-destructive">
            加载失败：{(listQ.error as Error).message}
          </p>
        )}
        <ul className="grid gap-3 md:grid-cols-2">
          {(listQ.data?.templates ?? []).map((tpl) => (
            <li key={tpl.id} className="rounded-md border p-3">
              <div className="flex flex-col gap-1">
                <span className="font-medium">{tpl.name}</span>
                <span className="font-mono text-xs text-muted-foreground">{tpl.id}</span>
                <p className="text-sm text-muted-foreground">{tpl.description}</p>
              </div>
              <Button
                size="sm"
                variant="secondary"
                className="mt-2"
                onClick={() => setExpanded(expanded === tpl.id ? null : tpl.id)}
              >
                {expanded === tpl.id ? '收起' : '使用模板'}
              </Button>
              {expanded === tpl.id && (
                <TemplateInstallForm
                  template={tpl}
                  onCreated={onCreated}
                  onError={onError}
                  onCancel={() => setExpanded(null)}
                />
              )}
            </li>
          ))}
        </ul>
      </CardContent>
    </Card>
  )
}

function TemplateInstallForm({
  template,
  onCreated,
  onError,
  onCancel,
}: {
  template: WorkflowTemplate
  onCreated: (slug: string) => void
  onError: (msg: string | null) => void
  onCancel: () => void
}) {
  const token = useAuthStore((s) => s.token)
  const [slug, setSlug] = useState(`tpl-${template.id}`)
  const [name, setName] = useState(template.name)
  const [slotText, setSlotText] = useState<Record<string, string>>(() =>
    initialSlotText(template),
  )
  const [previewDsl, setPreviewDsl] = useState<string | null>(null)
  const [previewErr, setPreviewErr] = useState<string | null>(null)

  const slotsPayload = useMemo(() => {
    try {
      return parseSlots(template, slotText)
    } catch (e) {
      return null
    }
  }, [template, slotText])

  const slotParseErr = useMemo(() => {
    try {
      parseSlots(template, slotText)
      return null
    } catch (e) {
      return e instanceof Error ? e.message : String(e)
    }
  }, [template, slotText])

  const previewMut = useMutation({
    mutationFn: () => {
      if (!slotsPayload) {
        throw new Error(slotParseErr ?? 'invalid slots')
      }
      return api<WorkflowTemplatePreviewResponse>(
        `/agent/workflow/templates/${encodeURIComponent(template.id)}/preview`,
        {
          method: 'POST',
          token,
          body: JSON.stringify({
            slug: slug.trim(),
            name: name.trim() || slug.trim(),
            slots: slotsPayload,
          }),
        },
      )
    },
    onSuccess: (res) => {
      setPreviewDsl(res.dsl_yaml)
      setPreviewErr(null)
    },
    onError: (e) => {
      setPreviewDsl(null)
      setPreviewErr(humanError(e))
    },
  })

  const createMut = useMutation({
    mutationFn: (body: CreateWorkflowRequest) =>
      api<Workflow>('/admin/workflows', {
        method: 'POST',
        token,
        body: JSON.stringify(body),
      }),
    onSuccess: (wf) => {
      onError(null)
      onCreated(wf.slug)
      onCancel()
    },
    onError: (e) => onError(humanError(e)),
  })

  useEffect(() => {
    if (!slug.trim() || slotsPayload == null) {
      setPreviewDsl(null)
      setPreviewErr(slotParseErr)
      return
    }
    const t = window.setTimeout(() => previewMut.mutate(), 400)
    return () => window.clearTimeout(t)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [slug, name, slotsPayload, template.id])

  function submit() {
    if (!previewDsl) {
      onError(previewErr ?? slotParseErr ?? '请先修正模板参数以生成预览')
      return
    }
    createMut.mutate({
      slug: slug.trim(),
      name: name.trim() || slug.trim(),
      description: template.description,
      dsl_yaml: previewDsl,
    })
  }

  return (
    <div className="mt-3 flex flex-col gap-3 border-t pt-3">
      <div className="grid gap-2 sm:grid-cols-2">
        <div className="flex flex-col gap-1">
          <Label>标识 (slug)</Label>
          <Input value={slug} onChange={(e) => setSlug(e.target.value)} />
        </div>
        <div className="flex flex-col gap-1">
          <Label>名称</Label>
          <Input value={name} onChange={(e) => setName(e.target.value)} />
        </div>
      </div>
      {template.slots.map((slot) => (
        <div key={slot.name} className="flex flex-col gap-1">
          <Label>
            {slot.name}
            {slot.required ? ' *' : ''}
            <span className="ml-1 font-normal text-muted-foreground">({slot.type})</span>
          </Label>
          {slot.description && (
            <p className="text-xs text-muted-foreground">{slot.description}</p>
          )}
          {slot.type === 'string' && slot.tool_picker ? (
            <ToolBusToolSelect
              value={slotText[slot.name] ?? ''}
              suggested={slot.suggested_tools}
              onChange={(v) =>
                setSlotText((prev) => ({ ...prev, [slot.name]: v }))
              }
            />
          ) : slot.type === 'string' ? (
            <Input
              value={slotText[slot.name] ?? ''}
              onChange={(e) =>
                setSlotText((prev) => ({ ...prev, [slot.name]: e.target.value }))
              }
            />
          ) : (
            <textarea
              className="min-h-[72px] rounded-md border bg-background p-2 font-mono text-xs"
              value={slotText[slot.name] ?? ''}
              onChange={(e) =>
                setSlotText((prev) => ({ ...prev, [slot.name]: e.target.value }))
              }
            />
          )}
        </div>
      ))}
      {(previewErr || slotParseErr) && (
        <p className="text-xs text-destructive">{previewErr ?? slotParseErr}</p>
      )}
      {previewDsl && (
        <div className="flex flex-col gap-1">
          <Label>流程图预览</Label>
          <WorkflowGraph dsl={previewDsl} />
        </div>
      )}
      <div className="flex flex-wrap gap-2">
        <Button
          size="sm"
          disabled={!slug.trim() || createMut.isPending || !previewDsl}
          onClick={submit}
        >
          {createMut.isPending ? '创建中…' : '从模板创建工作流'}
        </Button>
        <Button size="sm" variant="ghost" onClick={onCancel}>
          取消
        </Button>
      </div>
    </div>
  )
}

function initialSlotText(tpl: WorkflowTemplate): Record<string, string> {
  const out: Record<string, string> = {}
  for (const slot of tpl.slots) {
    if (slot.default != null) {
      out[slot.name] =
        typeof slot.default === 'string'
          ? slot.default
          : JSON.stringify(slot.default, null, 2)
    } else if (slot.type !== 'string') {
      out[slot.name] = slot.type === 'array' ? '[]' : '{}'
    } else {
      out[slot.name] = ''
    }
  }
  return out
}

function parseSlots(
  tpl: WorkflowTemplate,
  text: Record<string, string>,
): Record<string, unknown> {
  const out: Record<string, unknown> = {}
  for (const slot of tpl.slots) {
    const raw = text[slot.name] ?? ''
    if (slot.type === 'string') {
      if (raw !== '') out[slot.name] = raw
      continue
    }
    if (raw.trim() === '') continue
    const parsed: unknown = JSON.parse(raw)
    out[slot.name] = parsed
  }
  return out
}

function humanError(e: unknown): string {
  if (e instanceof ApiError) {
    try {
      const j = JSON.parse(e.body) as { error?: string; detail?: string }
      return j.error ? `${j.error}${j.detail ? ': ' + j.detail : ''}` : e.message
    } catch {
      return e.body || e.message
    }
  }
  return e instanceof Error ? e.message : String(e)
}
