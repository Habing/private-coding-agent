/** Wrap a path as a workflow expression, e.g. inputs.x → ${inputs.x} */
export function wrapExpr(path: string): string {
  return path.includes('${') ? path : `\${${path}}`
}

/** MCP tools return text in content[0].text — common workflow assign target. */
export const MCP_TEXT_OUTPUT_SUFFIX = 'content.0.text'

export const MCP_TEXT_OUTPUT_LABEL = '状态文本（MCP 第一段）'

export function mcpTextOutputPath(stepId: string): string {
  return `steps.${stepId}.output.${MCP_TEXT_OUTPUT_SUFFIX}`
}

export function mcpTextOutputExpr(stepId: string): string {
  return wrapExpr(mcpTextOutputPath(stepId))
}

/** Standard pick row for e2e-mock-chain style flows. */
export function healthFromStatusAssign(statusStepId: string): { var: string; expr: string } {
  return { var: 'health', expr: mcpTextOutputExpr(statusStepId) }
}
