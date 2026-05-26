import { render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { SequentialWorkflowDesignerPane } from '@/components/SequentialWorkflowDesignerPane'
import type { WorkflowDesign } from '@/types/api'

const emptyDesign: WorkflowDesign = {
  id: 'demo',
  name: 'Demo',
  steps: [],
}

describe('SequentialWorkflowDesignerPane', () => {
  it('renders toolbox step labels', async () => {
    render(
      <SequentialWorkflowDesignerPane
        design={emptyDesign}
        height={480}
        onDesignChange={() => {}}
      />,
    )
    await waitFor(
      () => {
        expect(screen.getByText('调用工具')).toBeInTheDocument()
        expect(screen.getByText(/设置变量/)).toBeInTheDocument()
        expect(screen.getByText('条件分支')).toBeInTheDocument()
      },
      { timeout: 3000 },
    )
  })
})
