import { useQuery } from '@tanstack/react-query'
import { useMemo, useState } from 'react'

import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { api } from '@/lib/api'
import { toolDescriptionZh, toolTitleZh } from '@/lib/toolLabels'
import { useAuthStore } from '@/stores/auth'
import type { ToolDef, ToolListResponse } from '@/types/api'

export function Toolbox() {
  const token = useAuthStore((s) => s.token)
  const [filter, setFilter] = useState('')

  const { data, isLoading, error } = useQuery({
    queryKey: ['tools'],
    queryFn: () => api<ToolListResponse>('/tools', { token }),
    enabled: !!token,
  })

  const tools = useMemo(() => {
    const all = (data?.tools ?? []).slice().sort((a, b) => a.name.localeCompare(b.name))
    if (!filter.trim()) return all
    const q = filter.toLowerCase()
    return all.filter((t) => {
      const title = toolTitleZh(t.name)
      const desc = toolDescriptionZh(t.name, t.description)
      return (
        t.name.toLowerCase().includes(q) ||
        title.toLowerCase().includes(q) ||
        desc.toLowerCase().includes(q) ||
        t.description.toLowerCase().includes(q)
      )
    })
  }, [data, filter])

  const mutatingCount = useMemo(
    () => (data?.tools ?? []).filter((t) => t.mutating).length,
    [data],
  )

  return (
    <div className="flex h-full flex-col gap-4 overflow-auto p-6">
      <Card>
        <CardHeader>
          <CardTitle>工具箱</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-2">
          <p className="text-xs text-muted-foreground">
            列出后端 ToolBus 中已注册的所有工具。带红色「可变更」徽标的工具会修改沙箱、记忆或派生子 Agent；
            工作流的 Dry-Run 会拦截这些工具不真正执行。要试跑请用 <code>POST /tools/invoke</code>{' '}
            或在「工作流」页面中编排。
          </p>
          <Input
            placeholder="按名称或描述过滤…"
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            className="max-w-md"
          />
          {data && (
            <p className="text-xs text-muted-foreground">
              共 {data.tools.length} 个工具，其中 {mutatingCount} 个为可变更工具。
            </p>
          )}
        </CardContent>
      </Card>

      {isLoading && <p className="text-sm text-muted-foreground">加载中…</p>}
      {error && (
        <p className="text-sm text-destructive">加载失败：{(error as Error).message}</p>
      )}
      {!isLoading && tools.length === 0 && data && (
        <p className="text-sm text-muted-foreground">没有匹配的工具。</p>
      )}

      <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">
        {tools.map((t) => (
          <ToolCard key={t.name} tool={t} />
        ))}
      </div>
    </div>
  )
}

function ToolCard({ tool }: { tool: ToolDef }) {
  const [open, setOpen] = useState(false)
  return (
    <Card>
      <CardHeader className="flex flex-row items-start justify-between gap-2 space-y-0 pb-2">
        <div className="flex flex-col gap-0.5 min-w-0">
          <CardTitle className="text-sm break-all">{toolTitleZh(tool.name)}</CardTitle>
          <span className="font-mono text-[11px] text-muted-foreground break-all">{tool.name}</span>
        </div>
        {tool.mutating && (
          <span className="rounded-md bg-destructive/15 px-2 py-0.5 text-xs font-medium text-destructive whitespace-nowrap">
            可变更
          </span>
        )}
      </CardHeader>
      <CardContent className="flex flex-col gap-2 text-xs">
        <p className="text-muted-foreground">{toolDescriptionZh(tool.name, tool.description)}</p>
        <button
          type="button"
          className="self-start text-xs underline text-muted-foreground hover:text-foreground"
          onClick={() => setOpen((v) => !v)}
        >
          {open ? '收起参数' : '查看参数'}
        </button>
        {open && (
          <pre className="max-h-80 overflow-auto rounded bg-muted p-2 font-mono text-[11px] leading-tight">
            {JSON.stringify(tool.parameters, null, 2)}
          </pre>
        )}
      </CardContent>
    </Card>
  )
}
