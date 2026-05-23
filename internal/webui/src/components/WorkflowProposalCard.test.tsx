import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { HttpResponse, http } from 'msw'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it } from 'vitest'

import { useAuthStore } from '@/stores/auth'
import { server } from '@/test/mswServer'

import { WorkflowProposalCard } from './WorkflowProposalCard'

function renderCard(role: 'admin' | 'member') {
  useAuthStore.getState().setAuth('tok', {
    id: 'u1',
    tenant_id: 't1',
    email: role === 'admin' ? 'demo@example.com' : 'member@example.com',
    role,
  })
  const qc = new QueryClient({ defaultOptions: { mutations: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <WorkflowProposalCard
          payload={{
            ok: true,
            proposal_id: 'p1',
            slug: 'weekly-summary',
            name: '每周摘要',
            template_id: 'llm-summarize-notify',
            dry_run_ok: true,
            status: 'draft',
            summary:
              '工作流「每周摘要」(模板 · llm-summarize-notify)，模拟通过，状态=draft',
          }}
        />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('<WorkflowProposalCard />', () => {
  beforeEach(() => {
    useAuthStore.getState().clear()
  })

  it('shows admin confirm publish button', () => {
    renderCard('admin')
    expect(screen.getByRole('button', { name: '确认发布' })).toBeInTheDocument()
    expect(screen.getAllByText(/模板 · llm-summarize-notify/).length).toBeGreaterThan(0)
    expect(screen.getByText('✓ 模拟通过')).toBeInTheDocument()
  })

  it('shows member submit approval button', () => {
    renderCard('member')
    expect(screen.getByRole('button', { name: '提交审批' })).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: '在工作流页编辑' })).not.toBeInTheDocument()
  })

  it('admin confirm calls REST confirm and shows toast', async () => {
    server.use(
      http.post('/agent/workflow/proposals/p1/confirm', () =>
        HttpResponse.json({
          proposal: { id: 'p1', status: 'published', slug: 'weekly-summary' },
          summary: 'published',
        }),
      ),
    )
    renderCard('admin')
    await userEvent.click(screen.getByRole('button', { name: '确认发布' }))
    expect(await screen.findByText(/已发布 workflow.weekly-summary/)).toBeInTheDocument()
  })
})
