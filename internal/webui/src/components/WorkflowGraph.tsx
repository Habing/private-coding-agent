import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'

import { WorkflowGraphPreview } from '@/components/WorkflowGraphPreview'
import { api } from '@/lib/api'
import type { WorkflowGraphDTO } from '@/lib/workflowGraph'
import { useAuthStore } from '@/stores/auth'
import type { WorkflowDesign } from '@/types/api'

function useDebounced<T>(value: T, ms: number): T {
  const [debounced, setDebounced] = useState(value)
  useEffect(() => {
    const id = window.setTimeout(() => setDebounced(value), ms)
    return () => window.clearTimeout(id)
  }, [value, ms])
  return debounced
}

export function WorkflowGraph({
  dsl,
  onSelectStep,
  selectedStepId,
  height,
}: {
  dsl: string
  onSelectStep?: (stepId: string) => void
  editable?: boolean
  selectedStepId?: string
  design?: WorkflowDesign | null
  height?: number
}) {
  const token = useAuthStore((s) => s.token)
  const debounced = useDebounced(dsl, 400)

  const q = useQuery({
    queryKey: ['workflow-graph-preview', debounced],
    queryFn: () =>
      api<WorkflowGraphDTO>('/admin/workflows/graph-preview', {
        method: 'POST',
        token,
        body: JSON.stringify({ dsl_yaml: debounced }),
      }),
    enabled: debounced.trim().length > 0,
    retry: false,
    staleTime: 5000,
  })

  if (debounced.trim().length === 0) {
    return (
      <p className="text-sm text-muted-foreground">输入 DSL 后显示流程图预览</p>
    )
  }

  return (
    <WorkflowGraphPreview
      graph={q.data}
      loading={q.isLoading}
      error={q.isError}
      emptyMessage="暂无节点"
      height={height ?? 280}
      selectedStepId={selectedStepId}
      onSelectStep={onSelectStep}
    />
  )
}

export function WorkflowGraphMini({ proposalId }: { proposalId: string }) {
  const token = useAuthStore((s) => s.token)

  const q = useQuery({
    queryKey: ['workflow-proposal-graph', proposalId],
    queryFn: () =>
      api<WorkflowGraphDTO>(
        `/agent/workflow/proposals/${encodeURIComponent(proposalId)}/graph`,
        { token },
      ),
    enabled: proposalId.length > 0,
    retry: false,
    staleTime: 30_000,
  })

  return (
    <WorkflowGraphPreview
      graph={q.data}
      loading={q.isLoading}
      error={q.isError}
      emptyMessage="暂无流程图"
      height={220}
      compact
    />
  )
}
