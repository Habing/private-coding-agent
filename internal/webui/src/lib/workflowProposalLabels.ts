/** Chinese labels for workflow proposal UI (Slice 19b). */

export function proposalSourceBadge(source?: string, templateId?: string): string {
  if (templateId) return `模板 · ${templateId}`
  if (source?.startsWith('template:')) return `模板 · ${source.slice('template:'.length)}`
  return '自由生成'
}

export function proposalDryRunLabel(ok: boolean, error?: string): string {
  if (ok) return '✓ 模拟通过'
  return error ? `✗ 失败：${error}` : '✗ 模拟未通过'
}

export function proposalStatusLabel(status?: string): string {
  switch (status) {
    case 'draft':
      return '草案'
    case 'pending_approval':
      return '待审批'
    case 'published':
      return '已发布'
    case 'rejected':
      return '已拒绝'
    case 'confirmed':
      return '已确认'
    default:
      return status ?? '—'
  }
}
