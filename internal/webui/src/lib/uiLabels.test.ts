import { describe, expect, it } from 'vitest'

import {
  authTypeLabel,
  memoryTypeLabel,
  mcpEnabledLabel,
  workflowPublishLabel,
  workflowRunStatusLabel,
} from './uiLabels'

describe('uiLabels', () => {
  it('maps memory types to Chinese', () => {
    expect(memoryTypeLabel('knowledge')).toBe('知识')
    expect(memoryTypeLabel('unknown')).toBe('unknown')
  })

  it('maps workflow and mcp states', () => {
    expect(workflowPublishLabel(true)).toBe('已发布')
    expect(workflowPublishLabel(false)).toBe('草稿')
    expect(workflowRunStatusLabel('ok')).toBe('成功')
    expect(workflowRunStatusLabel('failed')).toBe('失败')
    expect(mcpEnabledLabel(true)).toBe('已启用')
    expect(authTypeLabel('bearer')).toBe('Bearer 令牌')
  })
})
