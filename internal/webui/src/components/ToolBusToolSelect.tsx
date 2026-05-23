import { useQuery } from '@tanstack/react-query'

import { api } from '@/lib/api'
import { filterNotifyTools, groupNotifyToolOptions } from '@/lib/notifyTools'
import { useAuthStore } from '@/stores/auth'
import type { ToolListResponse } from '@/types/api'

export function ToolBusToolSelect({
  value,
  onChange,
  suggested,
}: {
  value: string
  onChange: (v: string) => void
  suggested?: string[]
}) {
  const token = useAuthStore((s) => s.token)
  const toolsQ = useQuery({
    queryKey: ['tools'],
    queryFn: () => api<ToolListResponse>('/tools', { token }),
    enabled: !!token,
  })

  const options = filterNotifyTools((toolsQ.data?.tools ?? []).map((t) => t.name))
  const groups = groupNotifyToolOptions(options)

  return (
    <div className="flex flex-col gap-1">
      <select
        className="h-9 rounded-md border bg-background px-2 text-sm"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        disabled={toolsQ.isLoading}
      >
        {value && !options.includes(value) && (
          <option value={value}>{value}（未在 ToolBus 注册）</option>
        )}
        {!value && <option value="">选择工具…</option>}
        {groups.map((g) => (
          <optgroup key={g.label} label={g.label}>
            {g.options.map((name) => (
              <option key={name} value={name}>
                {name}
              </option>
            ))}
          </optgroup>
        ))}
      </select>
      {suggested && suggested.length > 0 && (
        <p className="text-xs text-muted-foreground">
          推荐：{suggested.join('、')}
          {options.some((o) => suggested.includes(o)) ? '（已安装项可在上方选择）' : ''}
        </p>
      )}
      {toolsQ.error && (
        <p className="text-xs text-destructive">加载工具列表失败</p>
      )}
    </div>
  )
}
