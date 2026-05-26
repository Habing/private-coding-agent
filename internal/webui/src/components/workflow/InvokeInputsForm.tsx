import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import type { WorkflowDesignInput } from '@/types/api'

export function InvokeInputsForm({
  schema,
  values,
  onChange,
}: {
  schema: WorkflowDesignInput[]
  values: Record<string, unknown>
  onChange: (next: Record<string, unknown>) => void
}) {
  if (schema.length === 0) return null

  return (
    <div className="flex flex-col gap-2">
      {schema.map((inp) => (
        <div key={inp.name} className="flex flex-col gap-1">
          <Label className="text-xs">{inp.label ?? inp.name}</Label>
          {inp.description ? (
            <p className="text-[11px] text-muted-foreground">{inp.description}</p>
          ) : null}
          {inp.widget === 'select' && inp.options && inp.options.length > 0 ? (
            <select
              className="rounded border bg-background px-2 py-1 text-sm"
              value={String(values[inp.name] ?? inp.default ?? inp.options[0])}
              onChange={(e) => onChange({ ...values, [inp.name]: e.target.value })}
            >
              {inp.options.map((o) => (
                <option key={o} value={o}>
                  {o}
                </option>
              ))}
            </select>
          ) : (
            <Input
              className="font-mono text-xs"
              value={String(values[inp.name] ?? inp.default ?? '')}
              onChange={(e) => onChange({ ...values, [inp.name]: e.target.value })}
            />
          )}
        </div>
      ))}
    </div>
  )
}

/** True when we can render a compact invoke form instead of raw JSON. */
export function canUseInvokeInputsForm(schema: WorkflowDesignInput[] | undefined): boolean {
  const s = schema ?? []
  return s.length > 0 && s.length <= 8
}
