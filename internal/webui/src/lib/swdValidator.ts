import type { Step, ValidatorConfiguration } from 'sequential-workflow-designer'

import type { ToolSchemaEntry } from '@/types/api'

const TOOL_STEP_TYPES = new Set(['tool', 'pca-tool'])

/** Registered tool names from GET /admin/workflows/tool-schemas. */
export function buildAllowedToolSet(tools: ToolSchemaEntry[] | undefined): Set<string> {
  const set = new Set<string>()
  if (!tools?.length) return set
  for (const t of tools) {
    const name = t.name?.trim()
    if (name) set.add(name)
  }
  return set
}

export function isToolSwdStep(step: Step): boolean {
  return TOOL_STEP_TYPES.has(step.type)
}

/**
 * SWD step validator: tool steps must reference a registered tool name.
 * When allowlist is empty (schemas still loading), skip tool checks to avoid false reds.
 */
export function isRegisteredToolName(toolName: string, allowedTools: Set<string>): boolean {
  const tool = toolName.trim()
  if (!tool) return false
  if (allowedTools.size === 0) return true
  return allowedTools.has(tool)
}

export function isSwdToolStepValid(step: Step, allowedTools: Set<string>): boolean {
  if (!isToolSwdStep(step)) return true
  return isRegisteredToolName(String(step.properties?.tool ?? ''), allowedTools)
}

export function buildSwdValidatorConfiguration(allowedTools: Set<string>): ValidatorConfiguration {
  return {
    step: (step) => isSwdToolStepValid(step, allowedTools),
  }
}
