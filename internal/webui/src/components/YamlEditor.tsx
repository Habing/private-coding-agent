import { lazy, Suspense } from 'react'

const MonacoEditor = lazy(() =>
  import('@monaco-editor/react').then((m) => ({ default: m.default })),
)

export interface YamlEditorProps {
  value: string
  onChange: (next: string) => void
  height?: string
  readOnly?: boolean
}

export function YamlEditor({ value, onChange, height = '420px', readOnly }: YamlEditorProps) {
  return (
    <div
      data-testid="yaml-editor"
      className="rounded-md border bg-background overflow-hidden"
      style={{ height }}
    >
      <Suspense fallback={<div className="p-3 text-xs text-muted-foreground">Loading editor…</div>}>
        <MonacoEditor
          language="yaml"
          value={value}
          onChange={(v) => onChange(v ?? '')}
          options={{
            readOnly,
            minimap: { enabled: false },
            scrollBeyondLastLine: false,
            fontSize: 13,
            tabSize: 2,
            renderWhitespace: 'selection',
            automaticLayout: true,
          }}
          theme="vs-dark"
        />
      </Suspense>
    </div>
  )
}
