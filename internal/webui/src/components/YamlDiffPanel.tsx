import { diffLines } from '@/lib/yamlDiff'

export function YamlDiffPanel({ before, after }: { before: string; after: string }) {
  const lines = diffLines(before, after)
  return (
    <div className="flex flex-col gap-1 rounded-md border bg-muted/30 p-2">
      <p className="text-xs font-semibold text-muted-foreground">版本变更（相对已保存）</p>
      <div className="max-h-48 overflow-auto font-mono text-[11px] leading-relaxed">
        {lines.map((line, i) => (
          <div
            key={i}
            className={
              line.kind === 'add'
                ? 'bg-green-500/15 text-green-800 dark:text-green-300'
                : line.kind === 'remove'
                  ? 'bg-red-500/15 text-red-800 dark:text-red-300'
                  : 'text-muted-foreground'
            }
          >
            {line.kind === 'add' ? '+ ' : line.kind === 'remove' ? '- ' : '  '}
            {line.text || '\u00a0'}
          </div>
        ))}
      </div>
    </div>
  )
}
