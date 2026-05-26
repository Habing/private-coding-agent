/**
 * 21e-b+：sequential-workflow-editor 与 PCA WorkflowDesign 使用不同模型
 *（editor-model vs design compile）。当前产品路径为右侧 ArgsForm + ExprPicker，
 * 不嵌入 SWD 内置 schema 表单，避免双轨编辑。
 *
 * 若未来接入，应在此定义 tool schema → editor definition 的转换，并与
 * POST /admin/workflows/design/compile 共用校验。
 */
import type { ToolSchemaEntry } from '@/types/api'

export type SwdEditorIntegrationStatus = 'deferred'

export function swdEditorIntegrationStatus(): SwdEditorIntegrationStatus {
  return 'deferred'
}

/** 占位：列出已支持 schema 驱动表单的工具（右栏 ArgsForm 已覆盖）。 */
export function toolsWithRightPanelForms(tools: ToolSchemaEntry[]): string[] {
  return tools.map((t) => t.name).filter(Boolean)
}
