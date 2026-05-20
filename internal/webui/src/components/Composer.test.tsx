import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { Composer } from './Composer'

describe('<Composer />', () => {
  it('sends on Enter and clears the textarea', async () => {
    const onSend = vi.fn()
    const user = userEvent.setup()
    render(<Composer onSend={onSend} disabled={false} />)

    const textarea = screen.getByRole('textbox')
    await user.type(textarea, 'hello{Enter}')
    expect(onSend).toHaveBeenCalledWith('hello')
    expect(textarea).toHaveValue('')
  })

  it('inserts newline on Shift+Enter without sending', async () => {
    const onSend = vi.fn()
    const user = userEvent.setup()
    render(<Composer onSend={onSend} disabled={false} />)

    const textarea = screen.getByRole('textbox')
    await user.type(textarea, 'line1{Shift>}{Enter}{/Shift}line2')
    expect(onSend).not.toHaveBeenCalled()
    expect(textarea).toHaveValue('line1\nline2')
  })

  it('does not send when input is blank', async () => {
    const onSend = vi.fn()
    const user = userEvent.setup()
    render(<Composer onSend={onSend} disabled={false} />)

    const textarea = screen.getByRole('textbox')
    await user.type(textarea, '   {Enter}')
    expect(onSend).not.toHaveBeenCalled()
  })

  it('disables textarea and button when disabled', () => {
    const onSend = vi.fn()
    render(<Composer onSend={onSend} disabled={true} />)
    expect(screen.getByRole('textbox')).toBeDisabled()
    expect(screen.getByRole('button', { name: /发送|send/i })).toBeDisabled()
  })

  it('does not send when disabled and Enter pressed', async () => {
    const onSend = vi.fn()
    const user = userEvent.setup()
    render(<Composer onSend={onSend} disabled={true} />)
    // userEvent.type on a disabled input is a no-op; press Enter explicitly.
    const textarea = screen.getByRole('textbox')
    textarea.focus()
    await user.keyboard('{Enter}')
    expect(onSend).not.toHaveBeenCalled()
  })

  it('sends via button click', async () => {
    const onSend = vi.fn()
    const user = userEvent.setup()
    render(<Composer onSend={onSend} disabled={false} />)
    await user.type(screen.getByRole('textbox'), 'click me')
    await user.click(screen.getByRole('button', { name: /发送|send/i }))
    expect(onSend).toHaveBeenCalledWith('click me')
    expect(screen.getByRole('textbox')).toHaveValue('')
  })

  it('shows pending state while onSend promise is unresolved', async () => {
    let release!: () => void
    const onSend = vi.fn(
      () =>
        new Promise<void>((r) => {
          release = r
        }),
    )
    const user = userEvent.setup()
    render(<Composer onSend={onSend} disabled={false} />)
    await user.type(screen.getByRole('textbox'), 'pending{Enter}')

    const textarea = screen.getByRole('textbox')
    expect(textarea).toBeDisabled()
    release()
    await waitFor(() => expect(textarea).not.toBeDisabled())
  })
})
