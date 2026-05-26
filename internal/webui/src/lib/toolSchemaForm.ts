/** Parsed JSON Schema field for workflow tool argument forms. */
export interface SchemaField {
  name: string
  type: 'string' | 'number' | 'integer' | 'boolean'
  title?: string
  description?: string
  required: boolean
  enum?: string[]
  default?: string | number | boolean
}

function propType(prop: Record<string, unknown>): SchemaField['type'] {
  const t = prop.type
  if (t === 'number' || t === 'integer' || t === 'boolean') return t
  return 'string'
}

function parseProperty(name: string, prop: Record<string, unknown>, required: boolean): SchemaField {
  const field: SchemaField = {
    name,
    type: propType(prop),
    title: typeof prop.title === 'string' ? prop.title : undefined,
    description: typeof prop.description === 'string' ? prop.description : undefined,
    required,
  }
  if (Array.isArray(prop.enum)) {
    field.enum = prop.enum.map(String)
    field.type = 'string'
  }
  if (prop.default !== undefined && prop.default !== null) {
    if (typeof prop.default === 'boolean' || typeof prop.default === 'number') {
      field.default = prop.default
    } else {
      field.default = String(prop.default)
    }
  }
  return field
}

/** Parse OpenAI-style tool `parameters` JSON Schema into form fields. */
export function parseToolParameters(parameters: unknown): SchemaField[] {
  if (!parameters || typeof parameters !== 'object') return []
  const root = parameters as Record<string, unknown>
  const props = root.properties as Record<string, Record<string, unknown>> | undefined
  if (!props || typeof props !== 'object') return []

  const required = new Set(
    Array.isArray(root.required) ? root.required.map(String) : [],
  )
  return Object.keys(props)
    .sort()
    .map((name) => parseProperty(name, props[name] ?? {}, required.has(name)))
}

export function syncArgsFromSchema(
  fields: SchemaField[],
  existing: { name: string; value: string; valueKind: string }[],
): { name: string; value: string; valueKind: 'literal' | 'expr' }[] {
  const byName = new Map(existing.map((a) => [a.name, a]))
  const out: { name: string; value: string; valueKind: 'literal' | 'expr' }[] = []
  for (const f of fields) {
    const prev = byName.get(f.name)
    if (prev) {
      out.push({
        name: f.name,
        value: prev.value,
        valueKind: prev.valueKind === 'expr' ? 'expr' : 'literal',
      })
      continue
    }
    const def =
      f.default !== undefined
        ? String(f.default)
        : f.enum?.[0] ?? (f.type === 'boolean' ? 'false' : '')
    out.push({ name: f.name, value: def, valueKind: 'literal' })
  }
  for (const a of existing) {
    if (!out.some((x) => x.name === a.name)) {
      out.push({
        name: a.name,
        value: a.value,
        valueKind: a.valueKind === 'expr' ? 'expr' : 'literal',
      })
    }
  }
  return out
}

export function setArgValue(
  args: { name: string; value: string; valueKind: string }[],
  name: string,
  value: string,
): { name: string; value: string; valueKind: 'literal' | 'expr' }[] {
  const valueKind: 'literal' | 'expr' = value.includes('${') ? 'expr' : 'literal'
  const idx = args.findIndex((a) => a.name === name)
  if (idx < 0) return [...args, { name, value, valueKind }] as { name: string; value: string; valueKind: 'literal' | 'expr' }[]
  return args.map((a, i) =>
    i === idx ? { ...a, value, valueKind } : { ...a, valueKind: a.valueKind === 'expr' ? 'expr' as const : 'literal' as const },
  )
}
