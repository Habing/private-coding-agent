import { describe, expect, it } from 'vitest'

import {
  buildSwdToolbox,
  dedupeToolsByName,
  toolboxStepLabel,
} from '@/lib/swdToolboxDynamic'
import { PCA_SWD_TOOLBOX } from '@/lib/swdToolbox'
import type { ToolSchemaEntry } from '@/types/api'

const tools: ToolSchemaEntry[] = [
  {
    name: 'mcp.e2e-mock.echo',
    description: 'echo',
    parameters: { type: 'object', properties: {} },
    mutating: false,
  },
  {
    name: 'mcp.e2e-mock.fetch_status',
    description: 'status',
    parameters: { type: 'object', properties: {} },
    mutating: false,
  },
]

describe('dedupeToolsByName', () => {
  it('keeps first occurrence per name', () => {
    const dup = [...tools, tools[0]!]
    expect(dedupeToolsByName(dup)).toHaveLength(2)
  })
})

describe('toolboxStepLabel', () => {
  it('uses slug.tool for mcp tools', () => {
    expect(toolboxStepLabel('mcp.e2e-mock.echo')).toBe('e2e-mock.echo')
  })
})

describe('buildSwdToolbox', () => {
  it('falls back to static toolbox when tools empty', () => {
    expect(buildSwdToolbox([])).toEqual(PCA_SWD_TOOLBOX)
    expect(buildSwdToolbox(undefined)).toEqual(PCA_SWD_TOOLBOX)
  })

  it('generates MCP tool templates', () => {
    const tb = buildSwdToolbox(tools)
    const allSteps = tb.groups.flatMap((g) => g.steps)
    const echo = allSteps.find(
      (s) => s.componentType === 'task' && s.properties.tool === 'mcp.e2e-mock.echo',
    )
    expect(echo).toBeDefined()
    expect(echo?.name).toBe('e2e-mock.echo')
    expect(tb.groups.some((g) => g.name === 'MCP 工具')).toBe(true)
  })

  it('dedupes duplicate API entries', () => {
    const dup = [
      ...tools,
      { ...tools[0]! },
      { ...tools[1]! },
      { ...tools[0]! },
    ]
    const tb = buildSwdToolbox(dup)
    const mcpGroup = tb.groups.find((g) => g.name === 'MCP 工具')
    expect(mcpGroup?.steps).toHaveLength(2)
  })
})
