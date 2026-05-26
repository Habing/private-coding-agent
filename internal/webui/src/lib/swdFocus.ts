import type { Designer } from 'sequential-workflow-designer'
import type { SequentialWorkflowDesignerController } from 'sequential-workflow-designer-react'

import {
  findSwdCanvasIdForPcaStepId,
  mapPcaStepIdToSwdCanvasId,
  type PcaSwdDefinition,
} from '@/lib/swdAdapter'
import type { WorkflowDesign } from '@/types/api'

export function getSwdDesigner(
  controller: SequentialWorkflowDesignerController,
): Designer | null {
  if (!controller.isReady()) return null
  try {
    return (
      controller as unknown as { getDesigner(): Designer }
    ).getDesigner()
  } catch {
    return null
  }
}

function resetSwdFolderToRoot(designer: Designer): void {
  const state = (
    designer as unknown as { state: { folderPath: string[]; setFolderPath(p: string[]): void } }
  ).state
  if (state.folderPath.length > 0) {
    state.setFolderPath([])
  }
}

/** Select + pan viewport to a PCA step during invoke streaming. */
export function focusPcaStepOnSwd(
  controller: SequentialWorkflowDesignerController,
  definition: PcaSwdDefinition,
  base: WorkflowDesign,
  pcaStepId: string,
): boolean {
  const swdId =
    findSwdCanvasIdForPcaStepId(definition, pcaStepId) ??
    mapPcaStepIdToSwdCanvasId(definition, base, pcaStepId)
  if (!swdId) return false

  const designer = getSwdDesigner(controller)
  if (!designer) return false

  try {
    // Do NOT setFolderPath(parentIfStepId): switch/if steps are not "single sequence"
    // folders and throw "Cannot find single sequence in folder step".
    // Switch branches are rendered on the root canvas; findById works from root.
    resetSwdFolderToRoot(designer)
    designer.selectStepById(swdId)
    designer.moveViewportToStep(swdId)
    return true
  } catch {
    try {
      resetSwdFolderToRoot(designer)
      controller.moveViewportToStep(swdId)
      return true
    } catch {
      return false
    }
  }
}
