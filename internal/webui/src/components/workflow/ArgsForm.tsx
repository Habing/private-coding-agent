import { useMemo } from 'react'

import { Label } from '@/components/ui/label'
import { ExprField } from '@/components/workflow/ExprField'
import {
  paramDescriptionZh,
  paramLabelZh,
} from '@/lib/workflowDisplayZh'
import {
  parseToolParameters,
  setArgValue,
  syncArgsFromSchema,
  type SchemaField,
} from '@/lib/toolSchemaForm'
import type { ToolSchemaEntry, WorkflowDesign, WorkflowDesignArg } from '@/types/api'

export interface ArgsFormProps {
  design: WorkflowDesign
  currentStepId: string
  tool?: string
  tools: ToolSchemaEntry[]
  args: WorkflowDesignArg[]
  onChange: (args: WorkflowDesignArg[]) => void
}

function SchemaFieldInput({
  field,
  arg,
  design,
  currentStepId,
  hint,
  onValue,
}: {
  field: SchemaField
  arg: WorkflowDesignArg | undefined
  design: WorkflowDesign
  currentStepId: string
  hint?: string
  onValue: (value: string) => void
}) {
  const value = arg?.value ?? ''
  const isExpr = value.includes('${')

  if (field.enum && field.enum.length > 0 && !isExpr) {
    return (
      <select
        className="rounded border bg-background px-2 py-1 text-sm"
        value={value || field.enum[0]}
        onChange={(e) => onValue(e.target.value)}
      >
        {field.enum.map((o) => (
          <option key={o} value={o}>
            {o}
          </option>
        ))}
      </select>
    )
  }

  if (field.type === 'boolean' && !isExpr) {
    return (
      <select
        className="rounded border bg-background px-2 py-1 text-sm"
        value={value === 'true' ? 'true' : 'false'}
        onChange={(e) => onValue(e.target.value)}
      >
        <option value="true">true</option>
        <option value="false">false</option>
      </select>
    )
  }

  if ((field.type === 'number' || field.type === 'integer') && !isExpr) {
    return (
      <input
        type="number"
        className="rounded border bg-background px-2 py-1 font-mono text-xs"
        value={value}
        onChange={(e) => onValue(e.target.value)}
      />
    )
  }

  return (
    <ExprField
      design={design}
      currentStepId={currentStepId}
      value={value}
      onChange={onValue}
      placeholder={hint ?? field.name}
    />
  )
}

export function ArgsForm({
  design,
  currentStepId,
  tool,
  tools,
  args,
  onChange,
}: ArgsFormProps) {
  const schemaEntry = tools.find((t) => t.name === tool)
  const fields = useMemo(
    () => parseToolParameters(schemaEntry?.parameters),
    [schemaEntry?.parameters],
  )

  const displayArgs = useMemo(() => {
    if (fields.length === 0) return args
    return syncArgsFromSchema(fields, args)
  }, [fields, args])

  function updateArg(name: string, value: string) {
    onChange(setArgValue(args, name, value) as WorkflowDesignArg[])
  }

  if (!tool) {
    return <p className="text-xs text-muted-foreground">请先选择工具</p>
  }

  if (fields.length === 0) {
    return (
      <div className="flex flex-col gap-2">
        <p className="text-xs text-muted-foreground">该工具无 schema 属性，可手动添加参数</p>
        {(args.length > 0 ? args : [{ name: '', value: '', valueKind: 'literal' }]).map(
          (a, i) => (
            <div key={a.name || i} className="grid grid-cols-[100px_1fr] gap-2">
              <input
                className="rounded border bg-background px-2 py-1 text-xs"
                placeholder="参数名"
                value={a.name}
                onChange={(e) => {
                  const next = [...args]
                  next[i] = { ...a, name: e.target.value }
                  onChange(next)
                }}
              />
              <ExprField
                design={design}
                currentStepId={currentStepId}
                value={a.value}
                onChange={(v) => updateArg(a.name || `__idx_${i}`, v)}
              />
            </div>
          ),
        )}
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-3">
      {fields.map((field) => {
        const arg = displayArgs.find((a) => a.name === field.name)
        const label = paramLabelZh(tool ?? '', field.name, field.title ?? field.name)
        const desc = paramDescriptionZh(tool ?? '', field.name, field.description)
        return (
          <div key={field.name} className="flex flex-col gap-1">
            <Label className="text-xs">
              {label}
              {field.required && <span className="text-destructive"> *</span>}
            </Label>
            {desc ? (
              <p className="text-[11px] text-muted-foreground">{desc}</p>
            ) : null}
            <SchemaFieldInput
              field={field}
              arg={arg}
              design={design}
              currentStepId={currentStepId}
              hint={desc}
              onValue={(v) => updateArg(field.name, v)}
            />
          </div>
        )
      })}
      {displayArgs
        .filter((a) => !fields.some((f) => f.name === a.name))
        .map((a) => (
          <div key={a.name} className="flex flex-col gap-1 border-t pt-2">
            <Label className="text-xs text-muted-foreground">额外: {a.name}</Label>
            <ExprField
              design={design}
              currentStepId={currentStepId}
              value={a.value}
              onChange={(v) => updateArg(a.name, v)}
            />
          </div>
        ))}
    </div>
  )
}

/** Merge schema defaults when switching tools. */
export function argsForTool(
  tool: string,
  tools: ToolSchemaEntry[],
  prev: WorkflowDesignArg[],
): WorkflowDesignArg[] {
  const entry = tools.find((t) => t.name === tool)
  const fields = parseToolParameters(entry?.parameters)
  return syncArgsFromSchema(fields, prev) as WorkflowDesignArg[]
}
