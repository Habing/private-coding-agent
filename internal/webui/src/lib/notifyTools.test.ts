import { describe, expect, it } from 'vitest'

import { filterNotifyTools, groupNotifyToolOptions } from './notifyTools'

describe('filterNotifyTools', () => {
  it('keeps mcp, llm.chat, http.fetch', () => {
    const names = [
      'fs.read',
      'mcp.slack.post',
      'llm.chat',
      'http.fetch',
      'shell.exec',
    ]
    expect(filterNotifyTools(names)).toEqual([
      'mcp.slack.post',
      'http.fetch',
      'llm.chat',
    ])
  })
})

describe('groupNotifyToolOptions', () => {
  it('groups mcp vs builtin', () => {
    const groups = groupNotifyToolOptions(['mcp.a.x', 'llm.chat'])
    expect(groups).toHaveLength(2)
    expect(groups[0]?.label).toBe('MCP 连接器')
    expect(groups[1]?.options).toContain('llm.chat')
  })
})
