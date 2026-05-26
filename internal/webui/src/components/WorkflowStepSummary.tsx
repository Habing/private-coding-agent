import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'

import { api } from '@/lib/api'
import type { WorkflowGraphDTO } from '@/lib/workflowGraph'
import {
  formatWorkflowStep,
  orderedWorkflowStepNodes,
} from '@/lib/workflowStepSummary'
import { useAuthStore } from '@/stores/auth'

function useDebounced<T>(value: T, ms: number): T {
  const [debounced, setDebounced] = useState(value)
  useEffect(() => {
    const id: ReturnType<typeof setTimeout> = setTimeout(() => setDebounced(value), ms)
    return () => clearTimeout(id)
  }, [value, ms])
  return debounced
}

export function WorkflowStepSummary({ dsl }: { dsl: string }) {
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
      <p className="text-sm text-muted-foreground">保存 DSL 后将显示步骤说明。</p>
    )
  }

  if (q.isLoading) {
    return <p className="text-sm text-muted-foreground">解析步骤…</p>
  }

  if (q.isError) {
    return (
      <p className="text-sm text-destructive">DSL 解析失败，无法在概览中展示步骤。</p>
    )
  }

  const steps = q.data ? orderedWorkflowStepNodes(q.data) : []
  if (steps.length === 0) {
    return <p className="text-sm text-muted-foreground">暂无步骤。</p>
  }

  return (
    <ol className="flex list-decimal flex-col gap-1.5 pl-5 text-sm">
      {steps.map((n, i) => (
        <li key={n.id} className="leading-snug">
          {formatWorkflowStep(n, i)}
        </li>
      ))}
    </ol>
  )
}
