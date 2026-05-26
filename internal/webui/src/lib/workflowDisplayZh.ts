/** 工作流设计器：工具与参数的中文展示（优先于 API 英文 description）。 */

const TOOL_ZH: Record<string, string> = {
  'mcp.e2e-mock.echo': '回显输入文本（模拟通知）',
  'mcp.e2e-mock.fetch_status': '查询 mock 系统状态，供工作流 if 分支使用',
  'mcp.e2e-mock.record_event': '模拟写入审计/事件记录（有副作用，试运行可 Dry-Run）',
}

const PARAM_LABEL_ZH: Record<string, Record<string, string>> = {
  'mcp.e2e-mock.echo': { text: '文本内容' },
  'mcp.e2e-mock.fetch_status': { scenario: '巡检场景' },
  'mcp.e2e-mock.record_event': { kind: '事件类型', detail: '详情' },
}

const PARAM_DESC_ZH: Record<string, Record<string, string>> = {
  'mcp.e2e-mock.fetch_status': {
    scenario: 'ok（正常）或 degraded（异常），默认 degraded',
  },
  'mcp.e2e-mock.echo': {
    text: '要回显的字符串',
  },
  'mcp.e2e-mock.record_event': {
    kind: '事件分类，如 inspect',
    detail: '事件详情，可引用 ${vars.*}',
  },
}

/** 通用参数名中文标签（无工具专属映射时） */
const COMMON_PARAM_LABEL: Record<string, string> = {
  scenario: '巡检场景',
  text: '文本',
  kind: '类型',
  detail: '详情',
  url: 'URL',
  body: '请求体',
}

export function toolDescriptionZh(toolName: string, fallback?: string): string {
  if (TOOL_ZH[toolName]) return TOOL_ZH[toolName]
  return fallback ?? ''
}

export function paramLabelZh(
  toolName: string,
  paramName: string,
  fallback?: string,
): string {
  return (
    PARAM_LABEL_ZH[toolName]?.[paramName] ??
    COMMON_PARAM_LABEL[paramName] ??
    fallback ??
    paramName
  )
}

export function paramDescriptionZh(
  toolName: string,
  paramName: string,
  fallback?: string,
): string {
  if (PARAM_DESC_ZH[toolName]?.[paramName]) return PARAM_DESC_ZH[toolName][paramName]
  // 已是中文则保留 API 描述
  if (fallback && /[\u4e00-\u9fff]/.test(fallback)) return fallback
  return fallback ? '' : ''
}
