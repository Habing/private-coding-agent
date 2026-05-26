import { useMemo } from 'react'

import { Label } from '@/components/ui/label'
import { toolDescriptionZh } from '@/lib/workflowDisplayZh'
import type { ToolSchemaEntry } from '@/types/api'

export interface ToolGroup {
  label: string
  tools: ToolSchemaEntry[]
}

export function groupTools(tools: ToolSchemaEntry[]): ToolGroup[] {
  const buckets = new Map<string, ToolSchemaEntry[]>()
  for (const t of tools) {
    let key = '其他'
    if (t.name.startsWith('mcp.')) key = 'MCP'
    else if (t.name.startsWith('workflow.')) key = '工作流'
    else if (t.name.includes('.')) key = t.name.split('.')[0]
    const list = buckets.get(key) ?? []
    list.push(t)
    buckets.set(key, list)
  }
  const order = ['MCP', '工作流', 'fs', 'shell', '其他']
  const groups: ToolGroup[] = []
  for (const label of order) {
    const list = buckets.get(label)
    if (list?.length) {
      groups.push({ label, tools: [...list].sort((a, b) => a.name.localeCompare(b.name)) })
      buckets.delete(label)
    }
  }
  for (const [label, list] of [...buckets.entries()].sort((a, b) => a[0].localeCompare(b[0]))) {
    groups.push({ label, tools: [...list].sort((a, b) => a.name.localeCompare(b.name)) })
  }
  return groups
}

export interface ToolPickerProps {
  tools: ToolSchemaEntry[]
  value: string
  onChange: (toolName: string) => void
}

export function ToolPicker({ tools, value, onChange }: ToolPickerProps) {
  const groups = useMemo(() => groupTools(tools), [tools])

  return (
    <div className="flex flex-col gap-1">
      <Label>工具 (use)</Label>
      <select
        className="rounded border bg-background px-2 py-1 font-mono text-xs"
        value={value}
        onChange={(e) => onChange(e.target.value)}
      >
        <option value="">选择…</option>
        {groups.map((g) => (
          <optgroup key={g.label} label={g.label}>
            {g.tools.map((t) => (
              <option key={t.name} value={t.name}>
                {t.name}
              </option>
            ))}
          </optgroup>
        ))}
      </select>
      {value && (
        <p className="text-[11px] text-muted-foreground">
          {toolDescriptionZh(
            value,
            tools.find((t) => t.name === value)?.description,
          )}
        </p>
      )}
    </div>
  )
}
