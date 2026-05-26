import type { StepDefinition, ToolboxConfiguration } from 'sequential-workflow-designer'

import { groupTools } from '@/components/workflow/ToolPicker'
import { PCA_SWD_IF_TEMPLATE, PCA_SWD_TOOLBOX, taskStepDef } from '@/lib/swdToolbox'
import type { ToolSchemaEntry } from '@/types/api'

/** One entry per tool name (API / multi-server refresh may repeat names). */
export function dedupeToolsByName(tools: ToolSchemaEntry[]): ToolSchemaEntry[] {
  const seen = new Set<string>()
  const out: ToolSchemaEntry[] = []
  for (const t of tools) {
    if (seen.has(t.name)) continue
    seen.add(t.name)
    out.push(t)
  }
  return out
}

/** Unique toolbox label: mcp.e2e-mock.echo → e2e-mock.echo (not bare echo). */
export function toolboxStepLabel(toolName: string): string {
  const parts = toolName.split('.')
  if (parts.length >= 3 && parts[0] === 'mcp') {
    return `${parts[parts.length - 2]}.${parts[parts.length - 1]}`
  }
  if (parts.length >= 2) {
    return `${parts[parts.length - 2]}.${parts[parts.length - 1]}`
  }
  return toolName
}

function taskToolTemplate(tool: ToolSchemaEntry): StepDefinition {
  const label = toolboxStepLabel(tool.name)
  const stepId = `step_${tool.name.replace(/\W+/g, '_')}`
  return taskStepDef('tool', label, {
    stepId,
    tool: tool.name,
    argsJson: '[]',
  })
}

const ASSIGN_TEMPLATE = taskStepDef('assign', '设置变量（如 pick）', {
  stepId: 'step_assign',
  assignmentsJson: JSON.stringify([
    { var: 'health', expr: '${steps.status.output.content.0.text}' },
  ]),
})

/** Build SWD toolbox from registered tools; falls back to static POC toolbox when empty. */
export function buildSwdToolbox(tools: ToolSchemaEntry[] | undefined): ToolboxConfiguration {
  if (!tools?.length) {
    return PCA_SWD_TOOLBOX
  }

  const unique = dedupeToolsByName(tools)
  const groups = groupTools(unique)
  const mcpTools = groups.find((g) => g.label === 'MCP')?.tools ?? []

  const mcpSteps = mcpTools.slice(0, 40).map(taskToolTemplate)
  const fallbackTools =
    mcpTools.length === 0 ? unique.slice(0, 20).map(taskToolTemplate) : []

  if (mcpSteps.length === 0 && fallbackTools.length === 0) {
    return PCA_SWD_TOOLBOX
  }

  const toolboxGroups: ToolboxConfiguration['groups'] = []

  if (mcpSteps.length > 0) {
    toolboxGroups.push({ name: 'MCP 工具', steps: mcpSteps })
  }

  toolboxGroups.push({
    name: '步骤',
    steps: [...fallbackTools, ASSIGN_TEMPLATE, PCA_SWD_IF_TEMPLATE],
  })

  return { groups: toolboxGroups }
}
