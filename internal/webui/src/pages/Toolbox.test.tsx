import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { HttpResponse, http } from 'msw'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import { useAuthStore } from '@/stores/auth'
import { server } from '@/test/mswServer'
import type { ToolDef } from '@/types/api'

import { Toolbox } from './Toolbox'

function tool(name: string, mutating: boolean, desc = ''): ToolDef {
  return {
    name,
    description: desc || `desc of ${name}`,
    parameters: { type: 'object', properties: { x: { type: 'integer' } } },
    mutating,
  }
}

function renderToolbox() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <Toolbox />
    </QueryClientProvider>,
  )
}

describe('<Toolbox />', () => {
  beforeEach(() => {
    useAuthStore.getState().setAuth('jwt.demo', {
      id: 'u1',
      tenant_id: 't1',
      email: 'demo@example.com',
      role: 'member',
    })
  })
  afterEach(() => {
    useAuthStore.getState().clear()
  })

  it('renders tool cards and only shows Mutating badge on mutating tools', async () => {
    server.use(
      http.get('/tools', () =>
        HttpResponse.json({
          tools: [tool('fs.write', true), tool('fs.read', false)],
        }),
      ),
    )
    renderToolbox()

    await screen.findByText('fs.write')
    expect(screen.getByText('fs.read')).toBeInTheDocument()

    // Mutating badge appears exactly once (only fs.write).
    const badges = screen.getAllByText('Mutating')
    expect(badges).toHaveLength(1)
  })

  it('filters tools by name and description', async () => {
    server.use(
      http.get('/tools', () =>
        HttpResponse.json({
          tools: [tool('fs.write', true), tool('memory.search', false, 'find memories')],
        }),
      ),
    )
    renderToolbox()
    await screen.findByText('fs.write')

    const input = screen.getByPlaceholderText(/按 name/)
    await userEvent.type(input, 'memor')

    expect(screen.queryByText('fs.write')).not.toBeInTheDocument()
    expect(screen.getByText('memory.search')).toBeInTheDocument()
  })

  it('toggles schema panel on click', async () => {
    server.use(
      http.get('/tools', () => HttpResponse.json({ tools: [tool('fs.read', false)] })),
    )
    renderToolbox()
    await screen.findByText('fs.read')

    expect(screen.queryByText(/"type": "object"/)).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: /查看 schema/ }))
    expect(screen.getByText(/"type": "object"/)).toBeInTheDocument()
  })
})
