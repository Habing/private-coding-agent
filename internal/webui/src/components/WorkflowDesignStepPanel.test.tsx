import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { WorkflowDesignStepPanel } from '@/components/WorkflowDesignStepPanel'
import type { ToolSchemaEntry, WorkflowDesign } from '@/types/api'

const tools: ToolSchemaEntry[] = [
  {
    name: 'mcp.e2e-mock.echo',
    description: 'echo',
    parameters: {
      type: 'object',
      properties: { text: { type: 'string' } },
      required: ['text'],
    },
    mutating: false,
  },
]

const design: WorkflowDesign = {
  id: 'e2e-mock-chain',
  name: 'Mock',
  steps: [
    {
      id: 'status',
      kind: 'tool',
      tool: 'mcp.e2e-mock.fetch_status',
      args: [{ name: 'scenario', value: '${inputs.scenario}', valueKind: 'expr' }],
    },
  ],
}

describe('WorkflowDesignStepPanel', () => {
  it('shows placeholder when no step selected', () => {
    render(
      <WorkflowDesignStepPanel
        design={design}
        tools={tools}
        onStepChange={() => {}}
      />,
    )
    expect(screen.getByText(/在左侧画布中选中一步/)).toBeInTheDocument()
  })

  it('renders ArgsForm for selected tool step', () => {
    render(
      <WorkflowDesignStepPanel
        design={design}
        selectedStepId="status"
        tools={tools}
        onStepChange={() => {}}
      />,
    )
    expect(screen.getByText('调用工具')).toBeInTheDocument()
    expect(screen.getByText('工具')).toBeInTheDocument()
  })

  it('shows assign quick preset after fetch_status step', () => {
    const withPick: WorkflowDesign = {
      ...design,
      steps: [
        ...design.steps,
        { id: 'pick', kind: 'assign', assignments: [{ var: '', expr: '' }] },
      ],
    }
    render(
      <WorkflowDesignStepPanel
        design={withPick}
        selectedStepId="pick"
        tools={tools}
        onStepChange={() => {}}
      />,
    )
    expect(screen.getByText('变量列表')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /一键：health ← 「status」状态文本/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /状态文本 ← status/ })).toBeInTheDocument()
  })

  it('shows if condition presets on gate step', () => {
    const withGate: WorkflowDesign = {
      ...design,
      steps: [
        ...design.steps,
        {
          id: 'gate',
          kind: 'if',
          condition: { left: '', op: 'eq', right: '' },
          then: [],
          else: [],
        },
      ],
    }
    render(
      <WorkflowDesignStepPanel
        design={withGate}
        selectedStepId="gate"
        tools={tools}
        onStepChange={() => {}}
      />,
    )
    expect(screen.getByRole('button', { name: /当 health 为 degraded/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /当 health 为 ok/ })).toBeInTheDocument()
  })
})
