import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { HttpResponse, http } from 'msw'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { useAuthStore } from '@/stores/auth'
import { server } from '@/test/mswServer'
import type { Workflow } from '@/types/api'

// Replace the heavy Monaco-backed editor with a plain textarea so jsdom can
// drive it. The component under test only reads value + onChange.
vi.mock('@/components/YamlEditor', () => ({
  YamlEditor: ({
    value,
    onChange,
  }: {
    value: string
    onChange: (v: string) => void
  }) => (
    <textarea
      data-testid="yaml-editor"
      value={value}
      onChange={(e) => onChange(e.target.value)}
    />
  ),
}))

vi.mock('@/components/WorkflowGraph', () => ({
  WorkflowGraph: () => <div data-testid="workflow-graph">graph</div>,
  WorkflowGraphMini: () => null,
}))

vi.mock('@/components/WorkflowTriggersPanel', () => ({
  TriggersPanel: () => <div data-testid="triggers-panel">triggers</div>,
}))

vi.mock('@/components/WorkflowTemplateMarket', () => ({
  WorkflowTemplateMarket: () => <div data-testid="template-market">market</div>,
}))

import { Workflows } from './Workflows'

function makeWorkflow(over: Partial<Workflow> = {}): Workflow {
  return {
    id: 'w1',
    tenant_id: 't1',
    slug: 'demo',
    name: 'Demo',
    description: 'a demo',
    dsl_yaml: 'id: demo\nname: Demo\nsteps: []\n',
    version: 1,
    published: false,
    created_at: '2026-05-20T10:00:00Z',
    updated_at: '2026-05-20T10:00:00Z',
    ...over,
  }
}

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <Workflows />
    </QueryClientProvider>,
  )
}

describe('<Workflows />', () => {
  beforeEach(() => {
    useAuthStore.getState().setAuth('jwt.admin', {
      id: 'u1',
      tenant_id: 't1',
      email: 'admin@example.com',
      role: 'admin',
    })
  })
  afterEach(() => {
    useAuthStore.getState().clear()
  })

  it('lists workflows and shows draft/published status', async () => {
    server.use(
      http.get('/admin/workflows', () =>
        HttpResponse.json({
          workflows: [
            makeWorkflow({ slug: 'a', published: true }),
            makeWorkflow({ id: 'w2', slug: 'b', published: false }),
          ],
        }),
      ),
    )
    renderPage()
    await screen.findByText('a')
    expect(screen.getByText('b')).toBeInTheDocument()
    expect(screen.getByText('已发布')).toBeInTheDocument()
    expect(screen.getByText('草稿')).toBeInTheDocument()
  })

  it('creates a workflow and refreshes the list', async () => {
    let created = false
    server.use(
      http.get('/admin/workflows', () =>
        HttpResponse.json({ workflows: created ? [makeWorkflow({ slug: 'fresh' })] : [] }),
      ),
      http.post('/admin/workflows', async () => {
        created = true
        return HttpResponse.json(makeWorkflow({ slug: 'fresh' }), { status: 201 })
      }),
    )
    renderPage()
    await screen.findByText(/还没有工作流/)

    await userEvent.type(screen.getByLabelText(/标识 \(slug\)/), 'fresh')
    await userEvent.click(screen.getByRole('button', { name: '创建' }))

    await waitFor(() => expect(screen.queryByText('fresh')).toBeInTheDocument())
  })

  it('publishes a draft workflow on button click', async () => {
    let published = false
    server.use(
      http.get('/admin/workflows', () =>
        HttpResponse.json({
          workflows: [makeWorkflow({ slug: 'p', published })],
        }),
      ),
      http.post('/admin/workflows/p/publish', () => {
        published = true
        return new HttpResponse(null, { status: 204 })
      }),
    )
    renderPage()
    const btn = await screen.findByRole('button', { name: '发布' })
    await userEvent.click(btn)
    await waitFor(() =>
      expect(screen.getByRole('button', { name: '取消发布' })).toBeInTheDocument(),
    )
  })

  it('expands editor and saves a DSL edit', async () => {
    let version = 1
    server.use(
      http.get('/admin/workflows', () =>
        HttpResponse.json({ workflows: [makeWorkflow({ slug: 'edit-me', version })] }),
      ),
      http.get('/admin/workflows/edit-me', () =>
        HttpResponse.json(makeWorkflow({ slug: 'edit-me', version })),
      ),
      http.put('/admin/workflows/edit-me', () => {
        version = 2
        return HttpResponse.json(makeWorkflow({ slug: 'edit-me', version }))
      }),
    )
    renderPage()
    await userEvent.click(await screen.findByRole('button', { name: '编辑' }))

    const editor = await screen.findByTestId('yaml-editor')
    expect(screen.getByTestId('triggers-panel')).toBeInTheDocument()
    // Bypass userEvent.type because the YAML body contains `[]` which the
    // keyboard parser would interpret as key descriptors.
    fireEvent.change(editor, { target: { value: 'id: edit-me\nname: changed\nsteps: []\n' } })

    const saveBtn = await screen.findByRole('button', { name: /保存（将重置/ })
    await userEvent.click(saveBtn)
    // After save, the row’s version label updates from v1 to v2.
    await waitFor(() => expect(screen.getByText(/v2/)).toBeInTheDocument())
  })
})
