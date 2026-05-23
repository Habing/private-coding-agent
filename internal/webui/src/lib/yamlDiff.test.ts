import { describe, expect, it } from 'vitest'

import { diffLines, hasDiff } from './yamlDiff'

describe('yamlDiff', () => {
  it('detects added and removed lines', () => {
    const d = diffLines('a\nb', 'a\nc')
    expect(d).toEqual([
      { kind: 'same', text: 'a' },
      { kind: 'remove', text: 'b' },
      { kind: 'add', text: 'c' },
    ])
  })

  it('hasDiff compares full strings', () => {
    expect(hasDiff('x', 'x')).toBe(false)
    expect(hasDiff('x', 'y')).toBe(true)
  })
})
