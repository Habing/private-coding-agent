import { describe, expect, it } from 'vitest'

import {
  featureGuidesForUser,
  featureHintByPath,
} from './featureGuide'

describe('featureGuide', () => {
  it('filters admin-only items for members', () => {
    const member = featureGuidesForUser(false)
    expect(member.some((f) => f.id === 'memories')).toBe(true)
    expect(member.some((f) => f.id === 'workflows')).toBe(false)
  })

  it('includes admin items for admins', () => {
    const admin = featureGuidesForUser(true)
    expect(admin.some((f) => f.id === 'workflows')).toBe(true)
  })

  it('returns hint for known nav paths', () => {
    expect(featureHintByPath('/toolbox', false)).toMatch(/工具/)
  })
})
