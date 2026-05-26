import { describe, expect, it } from 'vitest'

import {
  paramDescriptionZh,
  paramLabelZh,
  toolDescriptionZh,
} from '@/lib/workflowDisplayZh'

describe('workflowDisplayZh', () => {
  it('returns Chinese tool description', () => {
    expect(
      toolDescriptionZh(
        'mcp.e2e-mock.fetch_status',
        'Return fixed JSON system status for workflow branching.',
      ),
    ).toMatch(/mock 系统状态/)
  })

  it('returns Chinese param label and hides English-only API desc', () => {
    expect(paramLabelZh('mcp.e2e-mock.fetch_status', 'scenario')).toBe('巡检场景')
    expect(
      paramDescriptionZh(
        'mcp.e2e-mock.fetch_status',
        'scenario',
        'ok | degraded (default degraded)',
      ),
    ).toMatch(/degraded/)
    expect(
      paramDescriptionZh('mcp.unknown.tool', 'foo', 'English only'),
    ).toBe('')
  })
})
