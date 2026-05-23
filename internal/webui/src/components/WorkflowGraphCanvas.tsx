import {
  Background,
  Controls,
  Handle,
  MiniMap,
  Position,
  ReactFlow,
  type Edge,
  type Node,
  type NodeProps,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { useMemo } from 'react'

import {
  layoutWorkflowGraph,
  workflowNodeKindClass,
  type WorkflowGraphDTO,
} from '@/lib/workflowGraph'

type WorkflowNodeData = {
  label: string
  detail?: string
  kind: string
}

type WorkflowFlowNode = Node<WorkflowNodeData, 'workflow'>

function WorkflowGraphNode({ data }: NodeProps<WorkflowFlowNode>) {
  const cls =
    workflowNodeKindClass[data.kind] ?? 'border-border bg-background'
  return (
    <div
      className={`min-w-[120px] max-w-[200px] rounded-md border px-2 py-1 text-xs shadow-sm ${cls}`}
    >
      <Handle type="target" position={Position.Top} className="!bg-muted-foreground" />
      <div className="truncate font-medium" title={data.label}>
        {data.label}
      </div>
      {data.detail ? (
        <div
          className="truncate text-[10px] text-muted-foreground"
          title={data.detail}
        >
          {data.detail}
        </div>
      ) : null}
      <Handle type="source" position={Position.Bottom} className="!bg-muted-foreground" />
    </div>
  )
}

const nodeTypes = { workflow: WorkflowGraphNode }

export interface WorkflowGraphCanvasProps {
  graph?: WorkflowGraphDTO | null
  loading?: boolean
  error?: boolean
  emptyMessage?: string
  height?: number
  compact?: boolean
}

export function WorkflowGraphCanvas({
  graph,
  loading,
  error,
  emptyMessage = '暂无流程图',
  height = 420,
  compact = false,
}: WorkflowGraphCanvasProps) {
  const { nodes, edges } = useMemo(() => {
    if (!graph) {
      return { nodes: [] as Node[], edges: [] as Edge[] }
    }
    return layoutWorkflowGraph(graph)
  }, [graph])

  if (loading) {
    return (
      <p className="text-sm text-muted-foreground">
        {compact ? '加载流程图…' : '生成流程图…'}
      </p>
    )
  }
  if (error) {
    return (
      <p className="text-sm text-destructive">无法将 DSL 解析为流程图</p>
    )
  }
  if (nodes.length === 0) {
    return <p className="text-sm text-muted-foreground">{emptyMessage}</p>
  }

  return (
    <div
      className="overflow-hidden rounded-md border bg-background"
      style={{ height }}
    >
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        fitView
        nodesDraggable={false}
        nodesConnectable={false}
        elementsSelectable={false}
        proOptions={{ hideAttribution: true }}
      >
        <Background gap={compact ? 12 : 16} />
        {!compact && <Controls showInteractive={false} />}
        {!compact && <MiniMap zoomable pannable className="!bg-background" />}
      </ReactFlow>
    </div>
  )
}
