import { describe, expect, it } from 'vitest'

import { parseTriggerSummariesFromDSL, triggerSummaryLabel } from './workflowTriggers'

describe('parseTriggerSummariesFromDSL', () => {
  it('parses cron and webhook entries', () => {
    const dsl = `
id: x
name: X
triggers:
  - id: schedule
    cron: "0 9 * * *"
  - id: inbound
    webhook:
      enabled: true
steps:
  - id: a
    wait: 1ms
`
    const rows = parseTriggerSummariesFromDSL(dsl)
    expect(rows).toHaveLength(2)
    expect(rows[0]).toMatchObject({ id: 'schedule', kind: 'cron', detail: '0 9 * * *' })
    expect(rows[1]).toMatchObject({ id: 'inbound', kind: 'webhook' })
    expect(triggerSummaryLabel(rows[0])).toContain('0 9')
  })
})
