/** Tool names suitable for workflow notify / forward slots (Slice 25b). */
export function filterNotifyTools(names: string[]): string[] {
  const allow = (name: string) =>
    name.startsWith('mcp.') ||
    name === 'llm.chat' ||
    name === 'http.fetch' ||
    name === 'llm.embed'

  return names.filter(allow).sort((a, b) => {
    const rank = (n: string) => {
      if (n.startsWith('mcp.')) return 0
      if (n === 'http.fetch') return 1
      if (n === 'llm.chat') return 2
      return 3
    }
    const dr = rank(a) - rank(b)
    return dr !== 0 ? dr : a.localeCompare(b)
  })
}

export function groupNotifyToolOptions(names: string[]): { label: string; options: string[] }[] {
  const mcp = names.filter((n) => n.startsWith('mcp.'))
  const builtin = names.filter((n) => !n.startsWith('mcp.'))
  const groups: { label: string; options: string[] }[] = []
  if (mcp.length) groups.push({ label: 'MCP 连接器', options: mcp })
  if (builtin.length) groups.push({ label: '内置工具', options: builtin })
  return groups
}
