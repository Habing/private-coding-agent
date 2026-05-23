/** 管理端 UI 中文标签。API 枚举值保持英文，仅展示层汉化。 */

import type { MemoryType } from '@/types/api'

export const MEMORY_TYPE_LABELS: Record<MemoryType, string> = {
  preference: '偏好',
  knowledge: '知识',
  lesson: '经验',
  profile: '画像',
}

export function memoryTypeLabel(type: MemoryType | string): string {
  return MEMORY_TYPE_LABELS[type as MemoryType] ?? type
}

export function workflowPublishLabel(published: boolean): string {
  return published ? '已发布' : '草稿'
}

export function workflowRunStatusLabel(status: string): string {
  switch (status) {
    case 'ok':
      return '成功'
    case 'failed':
      return '失败'
    default:
      return status
  }
}

export function mcpEnabledLabel(enabled: boolean): string {
  return enabled ? '已启用' : '已禁用'
}

export function authTypeLabel(type: 'none' | 'bearer'): string {
  return type === 'bearer' ? 'Bearer 令牌' : '无'
}
