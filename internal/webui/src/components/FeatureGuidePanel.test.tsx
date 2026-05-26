import type { ComponentProps } from 'react'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it } from 'vitest'

import { useAuthStore } from '@/stores/auth'

import { FeatureGuidePanel } from './FeatureGuidePanel'

function renderPanel(props: ComponentProps<typeof FeatureGuidePanel>) {
  return render(
    <MemoryRouter>
      <FeatureGuidePanel {...props} />
    </MemoryRouter>,
  )
}

describe('<FeatureGuidePanel />', () => {
  it('renders quick start and member-visible features', () => {
    useAuthStore.getState().setAuth('jwt', {
      id: 'u1',
      tenant_id: 't1',
      email: 'm@example.com',
      role: 'member',
    })
    renderPanel({ open: true, onOpenChange: () => {} })
    expect(screen.getByRole('dialog', { name: /使用指引/ })).toBeInTheDocument()
    expect(screen.getByText('快速上手')).toBeInTheDocument()
    expect(screen.getByText('记忆')).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: /前往 工作流/ })).not.toBeInTheDocument()
    expect(
      screen.getByText(/部分管理功能/),
    ).toBeInTheDocument()
  })

  it('shows admin features for admin users', () => {
    useAuthStore.getState().setAuth('jwt', {
      id: 'u1',
      tenant_id: 't1',
      email: 'a@example.com',
      role: 'admin',
    })
    renderPanel({ open: true, onOpenChange: () => {} })
    expect(screen.getByRole('link', { name: '前往 工作流 →' })).toBeInTheDocument()
    expect(screen.queryByText(/部分管理功能/)).not.toBeInTheDocument()
  })

  it('calls onOpenChange when backdrop is clicked', async () => {
    useAuthStore.getState().setAuth('jwt', {
      id: 'u1',
      tenant_id: 't1',
      email: 'm@example.com',
      role: 'member',
    })
    const user = userEvent.setup()
    let open = true
    const onOpenChange = (v: boolean) => {
      open = v
    }
    const { rerender } = renderPanel({
      open,
      onOpenChange,
    })
    await user.click(screen.getByRole('presentation'))
    expect(open).toBe(false)
    rerender(
      <MemoryRouter>
        <FeatureGuidePanel open={open} onOpenChange={onOpenChange} />
      </MemoryRouter>,
    )
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })
})
