import { describe, expect, it } from 'vitest'

import {
  isSuspiciousLiteralIfCondition,
  normalizeConditionExpr,
  normalizeIfCondition,
} from './workflowIfCondition'

describe('workflowIfCondition', () => {
  it('detects ok vs degraded literal mistake', () => {
    expect(
      isSuspiciousLiteralIfCondition({
        left: 'ok',
        op: 'eq',
        right: 'degraded',
        rightKind: 'literal',
      }),
    ).toBe(true)
    expect(
      isSuspiciousLiteralIfCondition({
        left: '${vars.health}',
        op: 'eq',
        right: 'degraded',
        rightKind: 'literal',
      }),
    ).toBe(false)
  })

  it('wraps bare vars path', () => {
    expect(normalizeConditionExpr('vars.health')).toBe('${vars.health}')
  })

  it('normalizes if condition', () => {
    const c = normalizeIfCondition({
      left: 'vars.health',
      op: 'eq',
      right: 'degraded',
      rightKind: 'literal',
    })
    expect(c.left).toBe('${vars.health}')
  })
})
