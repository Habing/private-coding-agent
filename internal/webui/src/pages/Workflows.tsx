import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useMemo, useState } from 'react'

import { YamlEditor } from '@/components/YamlEditor'
import { WorkflowGraph } from '@/components/WorkflowGraph'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ApiError, api } from '@/lib/api'
import { workflowPublishLabel, workflowRunStatusLabel } from '@/lib/uiLabels'
import { useAuthStore } from '@/stores/auth'
import type {
  CreateWorkflowRequest,
  InvokeWorkflowRequest,
  UpdateWorkflowRequest,
  Workflow,
  WorkflowInvokeResult,
  WorkflowListResponse,
  WorkflowRun,
  WorkflowRunListResponse,
} from '@/types/api'

const SKELETON_DSL = (slug: string) =>
  `id: ${slug}
name: "${slug}"
description: ""

inputs:
  message:
    type: string
    default: "hello"

steps:
  - id: echo
    assign:
      reply: \${inputs.message}

outputs:
  reply: \${vars.reply}
`

export function Workflows() {
  const token = useAuthStore((s) => s.token)
  const qc = useQueryClient()
  const [newSlug, setNewSlug] = useState('')
  const [newName, setNewName] = useState('')
  const [selected, setSelected] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  const listQ = useQuery({
    queryKey: ['workflows'],
    queryFn: () => api<WorkflowListResponse>('/admin/workflows', { token }),
    enabled: !!token,
  })

  const createMut = useMutation({
    mutationFn: (body: CreateWorkflowRequest) =>
      api<Workflow>('/admin/workflows', {
        method: 'POST',
        token,
        body: JSON.stringify(body),
      }),
    onSuccess: (wf) => {
      qc.invalidateQueries({ queryKey: ['workflows'] })
      setNewSlug('')
      setNewName('')
      setSelected(wf.slug)
      setError(null)
    },
    onError: (e) => setError(humanError(e)),
  })

  function submitCreate() {
    const slug = newSlug.trim()
    const name = newName.trim() || slug
    if (!slug) return
    createMut.mutate({
      slug,
      name,
      dsl_yaml: SKELETON_DSL(slug),
    })
  }

  return (
    <div className="flex h-full flex-col gap-4 overflow-auto p-6">
      {error && (
        <Card className="border-destructive">
          <CardContent className="flex items-center justify-between py-3 text-sm text-destructive">
            <span>{error}</span>
            <Button size="sm" variant="ghost" onClick={() => setError(null)}>
              关闭
            </Button>
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader>
          <CardTitle>新建工作流</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-wrap items-end gap-3">
          <div className="flex flex-col gap-1">
            <Label htmlFor="wf-slug">标识 (slug)</Label>
            <Input
              id="wf-slug"
              placeholder="kebab-case，如 my-flow"
              value={newSlug}
              onChange={(e) => setNewSlug(e.target.value)}
              className="w-64"
            />
          </div>
          <div className="flex flex-1 flex-col gap-1 min-w-[200px]">
            <Label htmlFor="wf-name">名称（可选，默认同标识）</Label>
            <Input
              id="wf-name"
              value={newName}
              onChange={(e) => setNewName(e.target.value)}
            />
          </div>
          <Button
            size="sm"
            disabled={!newSlug.trim() || createMut.isPending}
            onClick={submitCreate}
          >
            创建
          </Button>
        </CardContent>
      </Card>

      <Card className="flex-1">
        <CardHeader>
          <CardTitle>工作流列表</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          {listQ.isLoading && <p className="text-sm text-muted-foreground">加载中…</p>}
          {listQ.error && (
            <p className="text-sm text-destructive">加载失败：{(listQ.error as Error).message}</p>
          )}
          {!listQ.isLoading && (listQ.data?.workflows.length ?? 0) === 0 && (
            <p className="text-sm text-muted-foreground">还没有工作流，用上方表单创建一个。</p>
          )}
          <ul className="flex flex-col gap-3">
            {(listQ.data?.workflows ?? []).map((wf) => (
              <WorkflowRow
                key={wf.id}
                workflow={wf}
                expanded={selected === wf.slug}
                onToggle={() => setSelected(selected === wf.slug ? null : wf.slug)}
                onError={setError}
              />
            ))}
          </ul>
        </CardContent>
      </Card>
    </div>
  )
}

function WorkflowRow({
  workflow,
  expanded,
  onToggle,
  onError,
}: {
  workflow: Workflow
  expanded: boolean
  onToggle: () => void
  onError: (msg: string | null) => void
}) {
  const token = useAuthStore((s) => s.token)
  const qc = useQueryClient()

  const detailQ = useQuery({
    queryKey: ['workflow', workflow.slug],
    queryFn: () => api<Workflow>(`/admin/workflows/${workflow.slug}`, { token }),
    enabled: !!token && expanded,
  })

  const updateMut = useMutation({
    mutationFn: (body: UpdateWorkflowRequest) =>
      api<Workflow>(`/admin/workflows/${workflow.slug}`, {
        method: 'PUT',
        token,
        body: JSON.stringify(body),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['workflows'] })
      qc.invalidateQueries({ queryKey: ['workflow', workflow.slug] })
      onError(null)
    },
    onError: (e) => onError(humanError(e)),
  })

  const publishMut = useMutation({
    mutationFn: () =>
      api<void>(`/admin/workflows/${workflow.slug}/publish`, {
        method: 'POST',
        token,
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['workflows'] })
      qc.invalidateQueries({ queryKey: ['workflow', workflow.slug] })
      onError(null)
    },
    onError: (e) => onError(humanError(e)),
  })

  const unpublishMut = useMutation({
    mutationFn: () =>
      api<void>(`/admin/workflows/${workflow.slug}/unpublish`, {
        method: 'POST',
        token,
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['workflows'] })
      qc.invalidateQueries({ queryKey: ['workflow', workflow.slug] })
      onError(null)
    },
    onError: (e) => onError(humanError(e)),
  })

  const deleteMut = useMutation({
    mutationFn: () =>
      api<void>(`/admin/workflows/${workflow.slug}`, {
        method: 'DELETE',
        token,
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['workflows'] })
      onError(null)
    },
    onError: (e) => onError(humanError(e)),
  })

  function confirmDelete() {
    if (window.confirm(`确认删除工作流「${workflow.slug}」？此操作不可恢复。`)) {
      deleteMut.mutate()
    }
  }

  return (
    <li className="rounded-md border">
      <div className="flex flex-wrap items-center justify-between gap-2 p-3">
        <div className="flex flex-col">
          <span className="font-mono text-sm">{workflow.slug}</span>
          <span className="text-xs text-muted-foreground">
            {workflow.name} · v{workflow.version} ·{' '}
            {workflow.published ? (
              <span className="text-green-600">{workflowPublishLabel(true)}</span>
            ) : (
              <span>{workflowPublishLabel(false)}</span>
            )}{' '}
            · {new Date(workflow.updated_at).toLocaleString()}
          </span>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button size="sm" variant="secondary" onClick={onToggle}>
            {expanded ? '收起' : '编辑'}
          </Button>
          {workflow.published ? (
            <Button
              size="sm"
              variant="ghost"
              disabled={unpublishMut.isPending}
              onClick={() => unpublishMut.mutate()}
            >
              取消发布
            </Button>
          ) : (
            <Button
              size="sm"
              disabled={publishMut.isPending}
              onClick={() => publishMut.mutate()}
            >
              发布
            </Button>
          )}
          <Button
            size="sm"
            variant="ghost"
            disabled={deleteMut.isPending}
            onClick={confirmDelete}
          >
            删除
          </Button>
        </div>
      </div>

      {expanded && (
        <div className="border-t p-3">
          {detailQ.isLoading && (
            <p className="text-sm text-muted-foreground">加载详情…</p>
          )}
          {detailQ.data && (
            <EditPane
              workflow={detailQ.data}
              onSave={(body) => updateMut.mutate(body)}
              saving={updateMut.isPending}
            />
          )}
        </div>
      )}
    </li>
  )
}

function EditPane({
  workflow,
  onSave,
  saving,
}: {
  workflow: Workflow
  onSave: (body: UpdateWorkflowRequest) => void
  saving: boolean
}) {
  const [name, setName] = useState(workflow.name)
  const [description, setDescription] = useState(workflow.description)
  const [dsl, setDsl] = useState(workflow.dsl_yaml ?? '')

  // Keep local form in sync when react-query refetches (after publish, etc.).
  useEffect(() => {
    setName(workflow.name)
    setDescription(workflow.description)
    setDsl(workflow.dsl_yaml ?? '')
  }, [workflow.id, workflow.version, workflow.dsl_yaml])

  const dirty =
    name !== workflow.name ||
    description !== workflow.description ||
    dsl !== (workflow.dsl_yaml ?? '')

  return (
    <div className="grid grid-cols-1 gap-3 xl:grid-cols-[1fr_1fr_320px]">
      <div className="flex flex-col gap-2 xl:col-span-1">
        <Label htmlFor="dsl">DSL（YAML）</Label>
        <YamlEditor value={dsl} onChange={setDsl} />
        <Button
          size="sm"
          className="w-fit"
          disabled={!dirty || saving}
          onClick={() => onSave({ name, description, dsl_yaml: dsl })}
        >
          {saving ? '保存中…' : '保存（将重置为未发布）'}
        </Button>
      </div>
      <div className="flex flex-col gap-2 xl:col-span-1">
        <Label>流程图（只读预览）</Label>
        <WorkflowGraph dsl={dsl} />
      </div>
      <div className="flex flex-col gap-3">
        <div className="flex flex-col gap-1">
          <Label htmlFor="wf-name-edit">名称</Label>
          <Input
            id="wf-name-edit"
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </div>
        <div className="flex flex-col gap-1">
          <Label htmlFor="wf-desc-edit">描述</Label>
          <textarea
            id="wf-desc-edit"
            className="min-h-[60px] rounded-md border bg-background p-2 text-sm"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
          />
        </div>
        <InvokePanel slug={workflow.slug} />
        <RunsPanel slug={workflow.slug} />
      </div>
    </div>
  )
}

function InvokePanel({ slug }: { slug: string }) {
  const token = useAuthStore((s) => s.token)
  const qc = useQueryClient()
  const [inputsText, setInputsText] = useState('{}')
  const [dryRun, setDryRun] = useState(false)
  const [result, setResult] = useState<WorkflowInvokeResult | null>(null)
  const [err, setErr] = useState<string | null>(null)

  const invokeMut = useMutation({
    mutationFn: async () => {
      let inputs: Record<string, unknown> = {}
      try {
        const parsed = JSON.parse(inputsText)
        if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
          inputs = parsed as Record<string, unknown>
        } else {
          throw new Error('inputs must be a JSON object')
        }
      } catch (e) {
        throw new Error('inputs JSON 解析失败: ' + (e as Error).message)
      }
      const body: InvokeWorkflowRequest = { inputs, dry_run: dryRun }
      return api<WorkflowInvokeResult>(`/admin/workflows/${slug}/invoke`, {
        method: 'POST',
        token,
        body: JSON.stringify(body),
      })
    },
    onSuccess: (r) => {
      setResult(r)
      setErr(null)
      qc.invalidateQueries({ queryKey: ['workflow-runs', slug] })
    },
    onError: (e) => {
      setResult(null)
      setErr(humanError(e))
    },
  })

  return (
    <div className="flex flex-col gap-2 rounded-md border p-3">
      <Label className="font-semibold">试运行</Label>
      <textarea
        className="min-h-[80px] rounded-md border bg-background p-2 font-mono text-xs"
        value={inputsText}
        onChange={(e) => setInputsText(e.target.value)}
        placeholder='{"sandbox_id": "..."}'
      />
      <label className="flex items-center gap-2 text-xs">
        <input
          type="checkbox"
          checked={dryRun}
          onChange={(e) => setDryRun(e.target.checked)}
        />
        Dry-Run（可变更工具不真正执行）
      </label>
      <Button
        size="sm"
        className="w-fit"
        disabled={invokeMut.isPending}
        onClick={() => invokeMut.mutate()}
      >
        {invokeMut.isPending ? '执行中…' : '执行'}
      </Button>
      {err && <p className="text-xs text-destructive">{err}</p>}
      {result && (
        <pre className="max-h-60 overflow-auto rounded bg-muted p-2 font-mono text-[11px]">
          {JSON.stringify(result, null, 2)}
        </pre>
      )}
    </div>
  )
}

function RunsPanel({ slug }: { slug: string }) {
  const token = useAuthStore((s) => s.token)
  const [open, setOpen] = useState(false)
  const runsQ = useQuery({
    queryKey: ['workflow-runs', slug],
    queryFn: () =>
      api<WorkflowRunListResponse>(`/admin/workflows/${slug}/runs?limit=20`, { token }),
    enabled: !!token && open,
  })
  const runs = runsQ.data?.runs ?? []
  return (
    <div className="flex flex-col gap-2 rounded-md border p-3">
      <button
        type="button"
        className="flex items-center justify-between text-left"
        onClick={() => setOpen((v) => !v)}
      >
        <span className="font-semibold text-sm">最近运行</span>
        <span className="text-xs text-muted-foreground">{open ? '收起' : '展开'}</span>
      </button>
      {open && (
        <div className="flex flex-col gap-1">
          {runsQ.isLoading && <p className="text-xs text-muted-foreground">加载中…</p>}
          {!runsQ.isLoading && runs.length === 0 && (
            <p className="text-xs text-muted-foreground">暂无运行记录。</p>
          )}
          {runs.map((r) => (
            <RunRow key={r.id} run={r} />
          ))}
        </div>
      )}
    </div>
  )
}

function RunRow({ run }: { run: WorkflowRun }) {
  const [open, setOpen] = useState(false)
  const outputs = useMemo(() => decodeJSONb64(run.outputs_json), [run.outputs_json])
  const inputs = useMemo(() => decodeJSONb64(run.inputs_json), [run.inputs_json])
  const color =
    run.status === 'ok' ? 'text-green-600' : run.status === 'failed' ? 'text-destructive' : ''
  return (
    <div className="rounded border p-2 text-[11px]">
      <button
        type="button"
        className="flex w-full items-center justify-between text-left"
        onClick={() => setOpen((v) => !v)}
      >
        <span className="font-mono">
          {new Date(run.started_at).toLocaleString()} ·{' '}
          <span className={color}>{workflowRunStatusLabel(run.status)}</span>
          {run.dry_run && <span className="ml-1 text-muted-foreground">[试运行]</span>}
        </span>
        <span className="text-muted-foreground">{run.duration_ms}ms</span>
      </button>
      {open && (
        <div className="mt-1 flex flex-col gap-1">
          {run.error_text && <p className="text-destructive">错误：{run.error_text}</p>}
          <details>
            <summary className="cursor-pointer text-muted-foreground">输入</summary>
            <pre className="overflow-auto rounded bg-muted p-1 font-mono">
              {JSON.stringify(inputs, null, 2)}
            </pre>
          </details>
          <details>
            <summary className="cursor-pointer text-muted-foreground">输出</summary>
            <pre className="overflow-auto rounded bg-muted p-1 font-mono">
              {JSON.stringify(outputs, null, 2)}
            </pre>
          </details>
        </div>
      )}
    </div>
  )
}

function decodeJSONb64(b64?: string): unknown {
  if (!b64) return null
  try {
    return JSON.parse(atob(b64))
  } catch {
    return b64
  }
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
