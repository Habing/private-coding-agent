import { describe, expect, it } from 'vitest'

import { profileDescription, profileLabel } from './profileLabels'

describe('profileLabels', () => {
  it('maps known profiles to Chinese', () => {
    expect(profileLabel('coding')).toBe('编码助手')
    expect(profileLabel('review')).toBe('代码评审')
    expect(profileLabel('workflow-authoring')).toBe('工作流编写')
  })

  it('falls back to API name and description for unknown profiles', () => {
    expect(profileLabel('custom')).toBe('custom')
    expect(profileDescription('custom', 'from api')).toBe('from api')
  })
})
