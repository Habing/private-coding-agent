import {
  Background,
  Controls,
  ReactFlow,
  ReactFlowProvider,
  type Node,
  type NodeMouseHandler,
  type NodeProps,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { memo, useMemo } from 'react'

import {
  layoutWorkflowGraph,
  workflowNodeKindClass,
  type WorkflowGraphDTO,
} from '@/lib/workflowGraph'

type PreviewNodeData = {
  label: string
  detail?: string
  kind: string
  selected?: boolean
}

const PreviewNode = memo(function PreviewNode({
  data,
}: NodeProps<Node<PreviewNodeData>>) {
  const kindClass =
    workflowNodeKindClass[data.kind] ?? 'border-border bg-card'
  return (
    <div
      className={`max-w-[220px] min-w-[160px] rounded-md border px-2 py-1.5 text-xs shadow-sm ${kindClass} ${
        data.selected ? 'ring-2 ring-primary' : ''
      }`}
    >
      <div className="truncate font-medium leading-snug">{data.label}</div>
      {data.detail ? (
        <div className="mt-0.5 truncate text-[10px] text-muted-foreground">
          {data.detail}
        </div>
      ) : null}
    </div>
  )
})

const nodeTypes = {
  workflow: PreviewNode,
  workflowEditable: PreviewNode,
}

export interface WorkflowGraphPreviewProps {
  graph?: WorkflowGraphDTO | null
  loading?: boolean
  error?: boolean
  emptyMessage?: string
  height?: number
  compact?: boolean
  selectedStepId?: string
  onSelectStep?: (stepId: string) => void
}

function WorkflowGraphPreviewInner({
  graph,
  loading,
  error,
  emptyMessage = '暂无节点',
  height = 280,
  selectedStepId,
  onSelectStep,
}: WorkflowGraphPreviewProps) {
  const { nodes, edges } = useMemo(() => {
    if (!graph?.nodes?.length) return { nodes: [], edges: [] }
    return layoutWorkflowGraph(graph, { selectedStepId, editable: !!onSelectStep })
  }, [graph, onSelectStep, selectedStepId])

  const onNodeClick: NodeMouseHandler = (_, node) => {
    if (!onSelectStep) return
    if (node.id === '__start__' || node.id === '__end__') return
    onSelectStep(node.id)
  }

  if (loading) {
    return (
      <p className="text-sm text-muted-foreground" style={{ minHeight: height }}>
        加载流程图…
      </p>
    )
  }
  if (error) {
    return (
      <p className="text-sm text-destructive" style={{ minHeight: height }}>
        无法生成流程图（请检查 DSL）
      </p>
    )
  }
  if (!graph || nodes.length === 0) {
    return (
      <p className="text-sm text-muted-foreground" style={{ minHeight: height }}>
        {emptyMessage}
      </p>
    )
  }

  return (
    <div
      className="workflow-graph-preview overflow-hidden rounded-md border bg-background"
      style={{ height, minHeight: height }}
    >
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        nodesDraggable={false}
        nodesConnectable={false}
        elementsSelectable={!!onSelectStep}
        fitView
        fitViewOptions={{ padding: 0.2 }}
        proOptions={{ hideAttribution: true }}
        onNodeClick={onSelectStep ? onNodeClick : undefined}
      >
        <Background gap={16} size={1} />
        <Controls showInteractive={false} />
      </ReactFlow>
    </div>
  )
}

export function WorkflowGraphPreview(props: WorkflowGraphPreviewProps) {
  return (
    <ReactFlowProvider>
      <WorkflowGraphPreviewInner {...props} />
    </ReactFlowProvider>
  )
}
