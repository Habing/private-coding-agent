import { describe, expect, it } from 'vitest'

import { toolDescriptionZh, toolTitleZh } from './toolLabels'

describe('toolTitleZh', () => {
  it('maps built-in tools', () => {
    expect(toolTitleZh('fs.read')).toBe('读取文件')
    expect(toolTitleZh('memory.search')).toBe('搜索记忆')
  })

  it('formats workflow and MCP names', () => {
    expect(toolTitleZh('workflow.deploy-check')).toBe('工作流：deploy-check')
    expect(toolTitleZh('mcp.github.create_issue')).toBe('MCP · github · create_issue')
  })
})

describe('toolDescriptionZh', () => {
  it('uses built-in Chinese descriptions', () => {
    expect(toolDescriptionZh('grep', 'Search files with regex')).toContain('正则')
  })

  it('handles published workflow tools', () => {
    expect(
      toolDescriptionZh('workflow.ci', 'Published workflow: ci pipeline'),
    ).toContain('已发布的工作流')
  })

  it('handles MCP tools with custom description', () => {
    expect(
      toolDescriptionZh('mcp.slack.post', 'Post a message to a channel'),
    ).toContain('Post a message')
  })

  it('falls back to API description', () => {
    expect(toolDescriptionZh('custom.tool', '自定义说明')).toBe('自定义说明')
  })
})
