import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { groupTools, ToolPicker } from '@/components/workflow/ToolPicker'
import type { ToolSchemaEntry } from '@/types/api'

const tools: ToolSchemaEntry[] = [
  { name: 'mcp.e2e-mock.echo', description: 'echo', parameters: {}, mutating: false },
  { name: 'workflow.publish', description: 'pub', parameters: {}, mutating: true },
  { name: 'fs.read', description: 'read', parameters: {}, mutating: false },
]

describe('ToolPicker', () => {
  it('groupTools buckets by prefix', () => {
    const groups = groupTools(tools)
    expect(groups.find((g) => g.label === 'MCP')?.tools).toHaveLength(1)
    expect(groups.find((g) => g.label === '工作流')?.tools).toHaveLength(1)
  })

  it('renders grouped select', () => {
    render(<ToolPicker tools={tools} value="mcp.e2e-mock.echo" onChange={() => {}} />)
    expect(screen.getByRole('combobox')).toHaveValue('mcp.e2e-mock.echo')
  })
})
