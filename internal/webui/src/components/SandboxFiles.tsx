import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'

import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth'
import type { SandboxFileListResponse, SandboxFileReadResponse } from '@/types/api'

const MAX_DEPTH = 3

interface Props {
  sandboxId: string
}

export function SandboxFiles({ sandboxId }: Props) {
  const token = useAuthStore((s) => s.token)
  const [stack, setStack] = useState<string[]>(['.'])
  const [previewPath, setPreviewPath] = useState<string | null>(null)

  const cwd = stack[stack.length - 1] ?? '.'
  const depth = stack.length

  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ['sandbox-files', sandboxId, cwd],
    queryFn: () =>
      api<SandboxFileListResponse>(
        `/sandbox/sessions/${encodeURIComponent(sandboxId)}/files?path=${encodeURIComponent(cwd)}&list=1`,
        { token },
      ),
    enabled: !!token && !!sandboxId,
  })

  const { data: preview } = useQuery({
    queryKey: ['sandbox-file', sandboxId, previewPath],
    queryFn: () =>
      api<SandboxFileReadResponse>(
        `/sandbox/sessions/${encodeURIComponent(sandboxId)}/files?path=${encodeURIComponent(previewPath!)}`,
        { token },
      ),
    enabled: !!token && !!previewPath,
  })

  function enterDir(name: string) {
    if (depth >= MAX_DEPTH) return
    const next =
      cwd === '.' ? name : `${cwd.replace(/\/$/, '')}/${name}`
    setStack((s) => [...s, next])
    setPreviewPath(null)
  }

  function goUp() {
    if (stack.length <= 1) return
    setStack((s) => s.slice(0, -1))
    setPreviewPath(null)
  }

  let previewText: string | null = null
  if (preview?.content_base64) {
    try {
      previewText = atob(preview.content_base64)
    } catch {
      previewText = '(无法解码预览)'
    }
  }

  const entries = data?.entries ?? []

  return (
    <div className="flex flex-1 flex-col overflow-hidden text-xs">
      <div className="border-b px-2 py-1.5 font-medium">沙箱文件</div>
      <div className="flex items-center gap-1 border-b px-2 py-1 text-[10px] text-muted-foreground">
        <button
          type="button"
          className="hover:text-foreground disabled:opacity-40"
          disabled={stack.length <= 1}
          onClick={goUp}
        >
          ↑
        </button>
        <span className="truncate" title={cwd}>
          {cwd}
        </span>
        <button type="button" className="ml-auto hover:text-foreground" onClick={() => refetch()}>
          刷新
        </button>
      </div>
      <div className="flex-1 overflow-auto">
        {isLoading && <p className="p-2 text-muted-foreground">加载中…</p>}
        {error && (
          <p className="p-2 text-destructive">{(error as Error).message}</p>
        )}
        <ul>
          {entries.map((e) => (
            <li key={e.name}>
              <button
                type="button"
                className="flex w-full items-center gap-1 px-2 py-1 text-left hover:bg-muted/60"
                onClick={() =>
                  e.type === 'dir' ? enterDir(e.name) : setPreviewPath(joinPath(cwd, e.name))
                }
              >
                <span>{e.type === 'dir' ? '📁' : '📄'}</span>
                <span className="truncate">{e.name}</span>
              </button>
            </li>
          ))}
        </ul>
      </div>
      {previewPath && previewText !== null && (
        <div className="max-h-32 shrink-0 overflow-auto border-t p-2">
          <div className="mb-1 truncate font-medium text-muted-foreground">{previewPath}</div>
          <pre className="whitespace-pre-wrap break-all text-[10px]">{previewText}</pre>
        </div>
      )}
    </div>
  )
}

function joinPath(base: string, name: string): string {
  if (base === '.' || base === '') return name
  return `${base.replace(/\/$/, '')}/${name}`
}
