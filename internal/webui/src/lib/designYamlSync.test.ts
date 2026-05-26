import { describe, expect, it } from 'vitest'

import { isDesignerOutOfSync } from '@/lib/designYamlSync'

describe('designYamlSync', () => {
  it('detects yaml drift from designer basis', () => {
    expect(isDesignerOutOfSync('a: 1\n', 'a: 1\n')).toBe(false)
    expect(isDesignerOutOfSync('a: 2\n', 'a: 1\n')).toBe(true)
    expect(isDesignerOutOfSync('  a: 1  ', 'a: 1')).toBe(false)
  })
})
