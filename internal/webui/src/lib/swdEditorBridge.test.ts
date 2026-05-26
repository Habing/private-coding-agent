import { describe, expect, it } from 'vitest'

import { swdEditorIntegrationStatus, toolsWithRightPanelForms } from '@/lib/swdEditorBridge'

describe('swdEditorBridge', () => {
  it('defers sequential-workflow-editor (right panel forms are canonical)', () => {
    expect(swdEditorIntegrationStatus()).toBe('deferred')
  })

  it('lists tools covered by ArgsForm', () => {
    expect(
      toolsWithRightPanelForms([
        { name: 'mcp.e2e-mock.echo', description: '', parameters: { type: 'object' }, mutating: false },
      ]),
    ).toEqual(['mcp.e2e-mock.echo'])
  })
})
