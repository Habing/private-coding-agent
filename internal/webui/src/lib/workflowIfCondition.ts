import type { WorkflowDesignCondition } from '@/types/api'

const PATH_PREFIX = /^(inputs|vars|steps)\./

/** True when both sides look like fixed strings (e.g. ok vs degraded), not variables. */
export function isSuspiciousLiteralIfCondition(c: WorkflowDesignCondition | undefined): boolean {
  if (!c?.left?.trim() || !c.right?.trim()) return false
  const leftExpr = normalizeConditionExpr(c.left)
  const rightExpr = normalizeConditionExpr(c.right, c.rightKind)
  const leftLit = isLiteralOperand(leftExpr)
  const rightLit = isLiteralOperand(rightExpr)
  return leftLit && rightLit
}

function isLiteralOperand(expr: string): boolean {
  const s = expr.trim()
  if (PATH_PREFIX.test(s)) return false
  if (s.includes('${')) return false
  return true
}

/** Wrap bare paths as ${vars.x}; leave existing ${...} and quoted literals unchanged. */
export function normalizeConditionExpr(value: string, kind?: string): string {
  const v = value.trim()
  if (!v) return v
  if (v.includes('${')) return v
  if (kind === 'expr' || PATH_PREFIX.test(v)) {
    return `\${${v.replace(/^\$\{|\}$/g, '')}}`
  }
  return v
}

export function normalizeIfCondition(c: WorkflowDesignCondition): WorkflowDesignCondition {
  const rightKind =
    c.rightKind ??
    (c.right.includes('${') || PATH_PREFIX.test(c.right.trim()) ? 'expr' : 'literal')
  return {
    ...c,
    left: normalizeConditionExpr(c.left),
    right: normalizeConditionExpr(c.right, rightKind),
    rightKind,
  }
}

/** Recommended gate for e2e-mock-chain style flows (vars.health from assign). */
export const HEALTH_DEGRADED_CONDITION: WorkflowDesignCondition = {
  left: '${vars.health}',
  op: 'eq',
  right: 'degraded',
  rightKind: 'literal',
}

export const HEALTH_OK_CONDITION: WorkflowDesignCondition = {
  left: '${vars.health}',
  op: 'eq',
  right: 'ok',
  rightKind: 'literal',
}
