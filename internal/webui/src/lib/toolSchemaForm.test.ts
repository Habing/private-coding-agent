import { describe, expect, it } from 'vitest'

import {
  parseToolParameters,
  setArgValue,
  syncArgsFromSchema,
} from '@/lib/toolSchemaForm'

describe('toolSchemaForm', () => {
  it('parseToolParameters reads object properties', () => {
    const fields = parseToolParameters({
      type: 'object',
      properties: {
        scenario: { type: 'string', description: 'ok | degraded' },
        count: { type: 'integer' },
      },
      required: ['scenario'],
    })
    expect(fields).toHaveLength(2)
    expect(fields.find((f) => f.name === 'scenario')?.required).toBe(true)
  })

  it('syncArgsFromSchema preserves expr args and adds defaults', () => {
    const fields = parseToolParameters({
      type: 'object',
      properties: {
        scenario: { type: 'string', enum: ['ok', 'degraded'] },
        text: { type: 'string' },
      },
    })
    const synced = syncArgsFromSchema(fields, [
      { name: 'scenario', value: '${inputs.scenario}', valueKind: 'expr' },
    ])
    expect(synced.find((a) => a.name === 'scenario')?.value).toBe('${inputs.scenario}')
    expect(synced.find((a) => a.name === 'text')?.value).toBe('')
  })

  it('setArgValue detects expr kind', () => {
    const next = setArgValue([], 'x', '${vars.health}')
    expect(next[0].valueKind).toBe('expr')
  })
})
