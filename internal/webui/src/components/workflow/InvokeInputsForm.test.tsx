import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { canUseInvokeInputsForm, InvokeInputsForm } from './InvokeInputsForm'

describe('InvokeInputsForm', () => {
  it('canUseInvokeInputsForm allows small schemas', () => {
    expect(canUseInvokeInputsForm([])).toBe(false)
    expect(canUseInvokeInputsForm([{ name: 'a', type: 'string' }])).toBe(true)
    expect(canUseInvokeInputsForm(Array.from({ length: 9 }, (_, i) => ({ name: `p${i}`, type: 'string' })))).toBe(
      false,
    )
  })

  it('renders select from schema options', async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()
    render(
      <InvokeInputsForm
        schema={[
          {
            name: 'scenario',
            type: 'string',
            label: '巡检场景',
            widget: 'select',
            options: ['ok', 'degraded'],
          },
        ]}
        values={{ scenario: 'degraded' }}
        onChange={onChange}
      />,
    )
    const sel = screen.getByRole('combobox')
    expect(sel).toHaveValue('degraded')
    await user.selectOptions(sel, 'ok')
    expect(onChange).toHaveBeenCalledWith({ scenario: 'ok' })
  })
})
