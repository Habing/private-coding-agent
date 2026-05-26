import { describe, expect, it } from 'vitest'

import {
  formatQuotaErrorMessage,
  parseLLMQuotaExceeded,
} from './quotaError'

describe('quotaError', () => {
  it('parses llm.tokens exceeded', () => {
    const p = parseLLMQuotaExceeded(
      'quota exceeded: llm.tokens tenant=abc used=221150 cap=200000',
    )
    expect(p).toEqual({ used: 221150, cap: 200000 })
  })

  it('formats friendly llm message', () => {
    const msg = formatQuotaErrorMessage(
      'quota exceeded: llm.tokens tenant=x used=221150 cap=200000',
    )
    expect(msg).toContain('今日 LLM 用量已达上限')
    expect(msg).toContain('221,150')
    expect(msg).toContain('200,000')
  })
})
