import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { flushSync } from 'react-dom'
import { useLocation } from 'react-router-dom'


import { YamlEditor } from '@/components/YamlEditor'
import { WorkflowGraph } from '@/components/WorkflowGraph'
import { WorkflowDesigner } from '@/components/WorkflowDesigner'
import {
  WorkflowExecuteButton,
  WorkflowInvokeControls,
  WorkflowInvokeResultPanel,
} from '@/components/workflow/WorkflowInvokePanels'
import { useWorkflowInvoke } from '@/hooks/useWorkflowInvoke'
import { WorkflowDesignStepPanel } from '@/components/WorkflowDesignStepPanel'
import { WorkflowNLCreatePanel } from '@/components/WorkflowNLCreatePanel'
import { WorkflowProposalsInbox } from '@/components/WorkflowProposalsInbox'
import { WorkflowStepSummary } from '@/components/WorkflowStepSummary'
import { WorkflowTemplateMarket } from '@/components/WorkflowTemplateMarket'
import { YamlDiffPanel } from '@/components/YamlDiffPanel'
import { TriggersPanel } from '@/components/WorkflowTriggersPanel'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { DesignYamlSyncBanner } from '@/components/workflow/DesignYamlSyncBanner'
import { ApiError, api } from '@/lib/api'
import {
  isDesignerOutOfSync,
  SYNC_YAML_TO_DESIGNER_CONFIRM,
} from '@/lib/designYamlSync'
import {
  consumePendingProposalImport,
  loadProposalForDesigner,
  proposalToImport,
  syncProposalToWorkflowDraft,
  type ProposalDesignerImport,
  type WorkflowsLocationState,
} from '@/lib/proposalDesignerImport'
import { workflowPublishLabel, workflowRunStatusLabel } from '@/lib/uiLabels'
import { useRunStepFocusQueue } from '@/lib/runStepFocusQueue'
import { ensureHealthAssignStep } from '@/lib/workflowGateHealth'
import { hasDiff } from '@/lib/yamlDiff'
import { useAuthStore } from '@/stores/auth'
import type {
  CreateWorkflowRequest,
  ToolSchemasResponse,
  UpdateWorkflowRequest,
  Workflow,
  WorkflowDesign,
  WorkflowListResponse,
  WorkflowProposal,
  WorkflowRun,
  WorkflowRunListResponse,
} from '@/types/api'

const SKELETON_DSL = (slug: string) =>
  `id: ${slug}
name: "${slug}"
description: ""

steps: []

outputs: {}
`

type WorkflowPageTab = 'templates' | 'nl' | 'mine'

const PAGE_TABS: { key: WorkflowPageTab; label: string }[] = [
  { key: 'templates', label: '用模板创建' },
  { key: 'nl', label: '用自然语言创建' },
  { key: 'mine', label: '我的工作流' },
]

export function Workflows() {
  const token = useAuthStore((s) => s.token)
  const qc = useQueryClient()
  const location = useLocation()
  const [tab, setTab] = useState<WorkflowPageTab>('templates')
  const [newSlug, setNewSlug] = useState('')
  const [newName, setNewName] = useState('')
  const [selected, setSelected] = useState<string | null>(null)
  const [designerFocusSlug, setDesignerFocusSlug] = useState<string | null>(null)
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
      setTab('mine')
      setSelected(wf.slug)
      setError(null)
    },
    onError: (e) => setError(humanError(e)),
  })

  const openProposalInDesigner = useCallback(
    async (source: WorkflowProposal | ProposalDesignerImport | { proposalId: string }) => {
      if (!token) return
      try {
        let imp: ProposalDesignerImport
        if ('proposalId' in source && !('id' in source)) {
          imp = await loadProposalForDesigner(token, source)
        } else if ('id' in source) {
          imp = proposalToImport(source as WorkflowProposal)
        } else {
          imp = source as ProposalDesignerImport
        }
        await syncProposalToWorkflowDraft(token, imp)
        await qc.invalidateQueries({ queryKey: ['workflows'] })
        await qc.invalidateQueries({ queryKey: ['workflow', imp.slug] })
        setTab('mine')
        setSelected(imp.slug)
        setDesignerFocusSlug(imp.slug)
        setError(null)
      } catch (e) {
        setError(humanError(e))
      }
    },
    [token, qc],
  )

  useEffect(() => {
    const state = location.state as WorkflowsLocationState | null
    const proposalId = state?.openProposalInDesigner?.proposalId
    if (proposalId && token) {
      void openProposalInDesigner({ proposalId }).finally(() => {
        window.history.replaceState({}, document.title)
      })
      return
    }
    const pending = consumePendingProposalImport()
    if (pending && token) {
      void openProposalInDesigner(pending)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

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
      <div className="flex flex-col gap-2">
        <h1 className="text-lg font-semibold">工作流</h1>
        <p className="text-sm text-muted-foreground">
          推荐用模板或对话生成；YAML 仅在编辑页的「专家模式」中修改。
        </p>
        <div className="flex flex-wrap gap-2">
          {PAGE_TABS.map((t) => (
            <Button
              key={t.key}
              size="sm"
              variant={tab === t.key ? 'default' : 'secondary'}
              onClick={() => setTab(t.key)}
            >
              {t.label}
            </Button>
          ))}
        </div>
      </div>

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

      {tab === 'templates' && (
        <WorkflowTemplateMarket
          onCreated={(slug) => {
            qc.invalidateQueries({ queryKey: ['workflows'] })
            setTab('mine')
            setSelected(slug)
            setError(null)
          }}
          onError={setError}
        />
      )}

      {tab === 'nl' && <WorkflowNLCreatePanel />}

      {tab === 'mine' && (
        <>
          <WorkflowProposalsInbox
            onWorkflowPublished={(slug) => {
              setSelected(slug)
              setError(null)
            }}
            onOpenInDesigner={(imp) => void openProposalInDesigner(imp)}
          />

          <Card className="flex-1">
            <CardHeader>
              <CardTitle>我的工作流</CardTitle>
            </CardHeader>
            <CardContent className="flex flex-col gap-3">
              {listQ.isLoading && (
                <p className="text-sm text-muted-foreground">加载中…</p>
              )}
              {listQ.error && (
                <p className="text-sm text-destructive">
                  加载失败：{(listQ.error as Error).message}
                </p>
              )}
              {!listQ.isLoading && (listQ.data?.workflows.length ?? 0) === 0 && (
                <p className="text-sm text-muted-foreground">
                  还没有工作流。可切换到「用模板创建」或「用自然语言创建」。
                </p>
              )}
              <ul className="flex flex-col gap-3">
                {(listQ.data?.workflows ?? []).map((wf) => (
                  <WorkflowRow
                    key={wf.id}
                    workflow={wf}
                    expanded={selected === wf.slug}
                    focusDesigner={designerFocusSlug === wf.slug}
                    onDesignerFocused={() => setDesignerFocusSlug(null)}
                    onToggle={() =>
                      setSelected(selected === wf.slug ? null : wf.slug)
                    }
                    onError={setError}
                  />
                ))}
              </ul>

              <details className="rounded-md border p-3">
                <summary className="cursor-pointer text-sm font-medium">
                  高级 · 从空白 DSL 创建
                </summary>
                <div className="mt-3 flex flex-wrap items-end gap-3">
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
                  <div className="flex min-w-[200px] flex-1 flex-col gap-1">
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
                </div>
              </details>
            </CardContent>
          </Card>
        </>
      )}
    </div>
  )
}

function WorkflowRow({
  workflow,
  expanded,
  focusDesigner,
  onDesignerFocused,
  onToggle,
  onError,
}: {
  workflow: Workflow
  expanded: boolean
  focusDesigner?: boolean
  onDesignerFocused?: () => void
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
      qc.invalidateQueries({ queryKey: ['workflow-triggers', workflow.slug] })
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
      qc.invalidateQueries({ queryKey: ['workflow-triggers', workflow.slug] })
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
              focusDesigner={focusDesigner}
              onDesignerFocused={onDesignerFocused}
              onSave={(body) => updateMut.mutate(body)}
              saving={updateMut.isPending}
            />
          )}
        </div>
      )}
    </li>
  )
}

type EditViewTab = 'designer' | 'overview' | 'expert'

function EditPane({
  workflow,
  focusDesigner,
  onDesignerFocused,
  onSave,
  saving,
}: {
  workflow: Workflow
  focusDesigner?: boolean
  onDesignerFocused?: () => void
  onSave: (body: UpdateWorkflowRequest) => void
  saving: boolean
}) {
  const [viewTab, setViewTab] = useState<EditViewTab>('designer')
  const [name, setName] = useState(workflow.name)
  const [description, setDescription] = useState(workflow.description)
  const [dsl, setDsl] = useState(workflow.dsl_yaml ?? '')
  const [designerBasisDsl, setDesignerBasisDsl] = useState(workflow.dsl_yaml ?? '')
  const [designerReloadKey, setDesignerReloadKey] = useState(0)
  const [syncBannerDismissed, setSyncBannerDismissed] = useState(false)
  const [invokeDefaults, setInvokeDefaults] = useState<Record<string, unknown>>({})
  const [compileOk, setCompileOk] = useState(true)
  const [compileErr, setCompileErr] = useState<string | null>(null)
  const [liveDesign, setLiveDesign] = useState<WorkflowDesign | null>(null)
  const [selectedStepId, setSelectedStepId] = useState<string | undefined>()
  const [runStepId, setRunStepId] = useState<string | undefined>()
  const [runFocusTick, setRunFocusTick] = useState(0)
  const pushDesignRef = useRef<(d: WorkflowDesign) => void>(() => {})

  const applyRunStepFocus = useCallback((stepId: string) => {
    flushSync(() => {
      setRunStepId(stepId)
      setSelectedStepId(stepId)
      setRunFocusTick((t) => t + 1)
    })
  }, [])

  const runFocusQueue = useRunStepFocusQueue(applyRunStepFocus, 450)

  const token = useAuthStore((s) => s.token)
  const toolSchemasQ = useQuery({
    queryKey: ['workflow-tool-schemas'],
    queryFn: () =>
      api<ToolSchemasResponse>('/admin/workflows/tool-schemas', { token }),
    enabled: !!token,
    staleTime: 60_000,
  })

  const designerOutOfSync = isDesignerOutOfSync(dsl, designerBasisDsl)
  const showSyncBanner = designerOutOfSync && !syncBannerDismissed

  useEffect(() => {
    if (focusDesigner) {
      setViewTab('designer')
      onDesignerFocused?.()
    }
  }, [focusDesigner, onDesignerFocused])

  function syncYamlToDesigner() {
    setDesignerBasisDsl(dsl)
    setDesignerReloadKey((k) => k + 1)
    setSyncBannerDismissed(false)
  }

  function trySetViewTab(tab: EditViewTab) {
    if (tab === 'designer' && designerOutOfSync) {
      if (!window.confirm(SYNC_YAML_TO_DESIGNER_CONFIRM)) return
      syncYamlToDesigner()
    }
    if (tab === 'expert') {
      setSyncBannerDismissed(false)
    }
    setViewTab(tab)
  }

  function onDslFromDesigner(yaml: string) {
    setDsl(yaml)
    setDesignerBasisDsl(yaml)
    setSyncBannerDismissed(false)
  }

  function onDslFromExpert(yaml: string) {
    setDsl(yaml)
    if (yaml.trim() !== designerBasisDsl.trim()) {
      setSyncBannerDismissed(false)
    }
  }

  // Keep local form in sync when react-query refetches (after publish, etc.).
  useEffect(() => {
    const serverDsl = workflow.dsl_yaml ?? ''
    setName(workflow.name)
    setDescription(workflow.description)
    setDsl(serverDsl)
    setDesignerBasisDsl(serverDsl)
    setDesignerReloadKey((k) => k + 1)
    setSyncBannerDismissed(false)
    setCompileOk(true)
    setCompileErr(null)
  }, [workflow.id, workflow.version, workflow.dsl_yaml])

  const dirty =
    name !== workflow.name ||
    description !== workflow.description ||
    dsl !== (workflow.dsl_yaml ?? '')

  const invoke = useWorkflowInvoke({
    slug: workflow.slug,
    inputSchema: liveDesign?.inputs,
    defaultInputs: invokeDefaults,
    resetInputsKey: `${workflow.id}:${workflow.version}`,
    unsaved: dirty,
    onInvokeStart: () => {
      runFocusQueue.reset()
      setRunStepId(undefined)
    },
    onStepProgress: (stepId) => {
      runFocusQueue.enqueue(stepId)
    },
    onInvokeEnd: () => {
      setRunStepId(undefined)
    },
  })

  const saveBlocked = !compileOk || !dsl.trim()
  const saveButton = (
    <div className="flex flex-col gap-1">
      <Button
        size="sm"
        className="w-fit"
        disabled={!dirty || saving || saveBlocked}
        onClick={() => onSave({ name, description, dsl_yaml: dsl })}
      >
        {saving ? '保存中…' : '保存（将重置为未发布）'}
      </Button>
      {compileErr && (
        <p className="text-xs text-destructive">画布未通过校验，无法保存：{compileErr}</p>
      )}
      {!compileErr && saveBlocked && dirty && (
        <p className="text-xs text-muted-foreground">请至少保留一个步骤后再保存。</p>
      )}
    </div>
  )

  const viewTabs = (
    <nav className="flex flex-col gap-1 rounded-md border bg-muted/30 p-2">
      <p className="px-1 text-xs font-medium text-muted-foreground">视图</p>
      <Button
        size="sm"
        className="w-full justify-start"
        variant={viewTab === 'designer' ? 'default' : 'ghost'}
        onClick={() => trySetViewTab('designer')}
      >
        设计器
        {designerOutOfSync && viewTab !== 'designer' ? ' •' : ''}
      </Button>
      <Button
        size="sm"
        className="w-full justify-start"
        variant={viewTab === 'overview' ? 'default' : 'ghost'}
        onClick={() => trySetViewTab('overview')}
      >
        概览
      </Button>
      <Button
        size="sm"
        className="w-full justify-start"
        variant={viewTab === 'expert' ? 'default' : 'ghost'}
        onClick={() => trySetViewTab('expert')}
      >
        高级（YAML）
        {designerOutOfSync && viewTab !== 'expert' ? ' •' : ''}
      </Button>
    </nav>
  )

  const sidebar = (
    <aside className="flex w-[380px] shrink-0 flex-col gap-3">
      {viewTabs}

      {viewTab === 'designer' && liveDesign ? (
        <div className="flex flex-col gap-3 rounded-md border p-3">
          <div className="flex flex-col gap-1">
            <Label htmlFor="wf-name-sidebar">名称</Label>
            <Input
              id="wf-name-sidebar"
              value={liveDesign.name}
              onChange={(e) => {
                const nextName = e.target.value
                setName(nextName)
                pushDesignRef.current({ ...liveDesign, name: nextName })
              }}
            />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="wf-desc-sidebar">描述</Label>
            <textarea
              id="wf-desc-sidebar"
              className="min-h-[48px] rounded-md border bg-background p-2 text-sm"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
            />
          </div>
          <div className="flex flex-col gap-1">
            <Label className="text-xs text-muted-foreground">步骤详情</Label>
            <WorkflowDesignStepPanel
              design={liveDesign}
              selectedStepId={selectedStepId}
              tools={toolSchemasQ.data?.tools ?? []}
              toolsLoading={toolSchemasQ.isLoading}
              onStepChange={(d) => pushDesignRef.current(d)}
            />
          </div>
        </div>
      ) : null}

      {saveButton}
      <WorkflowInvokeControls
        invoke={invoke}
        unsaved={dirty}
        liveDesign={liveDesign}
        onFixHealthChain={
          liveDesign
            ? () => {
                pushDesignRef.current(ensureHealthAssignStep(liveDesign))
              }
            : undefined
        }
      />
      <TriggersPanel
        slug={workflow.slug}
        published={workflow.published}
        dsl={dsl}
      />
      <RunsPanel slug={workflow.slug} />
    </aside>
  )

  return (
    <div className="flex items-start gap-3">
      <div className="flex min-w-0 flex-1 flex-col gap-3">
        {viewTab === 'designer' && (
          <>
            {showSyncBanner ? (
              <DesignYamlSyncBanner
                mode="designer"
                onSyncToDesigner={() => {
                  syncYamlToDesigner()
                  setViewTab('designer')
                }}
                onDismiss={() => setSyncBannerDismissed(true)}
              />
            ) : null}
            <WorkflowDesigner
              key={`${workflow.id}-${workflow.version}-${designerReloadKey}`}
              embedCanvasOnly
              workflowId={workflow.id}
              workflowVersion={workflow.version}
              workflowSlug={workflow.slug}
              workflowName={name}
              description={description}
              dsl={dsl}
              onDslChange={onDslFromDesigner}
              onNameChange={setName}
              onDescriptionChange={setDescription}
              onInvokeDefaultsChange={setInvokeDefaults}
              onCompileOkChange={setCompileOk}
              onCompileError={setCompileErr}
              onDesignSnapshot={setLiveDesign}
              onRegisterDesignMutator={(fn) => {
                pushDesignRef.current = fn
              }}
              selectedStepId={selectedStepId}
              onSelectedStepIdChange={setSelectedStepId}
              focusStepId={runStepId ?? null}
              runFocusTick={runFocusTick}
              canvasToolbarEnd={<WorkflowExecuteButton invoke={invoke} />}
            />
            <WorkflowInvokeResultPanel
              invoke={invoke}
              liveDesign={liveDesign}
              highlightStepId={runStepId}
            />
          </>
        )}

        {viewTab === 'overview' && (
          <div className="flex flex-col gap-4">
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
            <div className="flex flex-col gap-2">
              <Label>步骤说明</Label>
              <WorkflowStepSummary dsl={dsl} />
            </div>
            <div className="flex flex-col gap-2">
              <Label>流程图</Label>
              <div className="min-h-[320px] rounded-md border p-2">
                <WorkflowGraph dsl={dsl} />
              </div>
            </div>
          </div>
        )}

        {viewTab === 'expert' && (
          <div className="flex flex-col gap-2">
            {showSyncBanner ? (
              <DesignYamlSyncBanner
                mode="expert"
                onSyncToDesigner={() => {
                  syncYamlToDesigner()
                  setViewTab('designer')
                }}
                onDismiss={() => setSyncBannerDismissed(true)}
              />
            ) : null}
            <p className="text-xs text-muted-foreground">
              修改 YAML 后请保存，并点击「同步到设计器」或在切换视图时确认同步。
            </p>
            <Label htmlFor="dsl">DSL（YAML）</Label>
            <YamlEditor value={dsl} onChange={onDslFromExpert} />
            {hasDiff(workflow.dsl_yaml ?? '', dsl) ? (
              <YamlDiffPanel before={workflow.dsl_yaml ?? ''} after={dsl} />
            ) : null}
            <div className="flex flex-col gap-2">
              <Label>流程图预览</Label>
              <div className="min-h-[240px] rounded-md border p-2">
                <WorkflowGraph dsl={dsl} />
              </div>
            </div>
          </div>
        )}
      </div>

      {sidebar}
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
