import type { StepDefinition, ToolboxConfiguration } from 'sequential-workflow-designer'

/** If toolbox template: branches hold StepDefinition (no canvas id yet). */
type IfStepDefinition = StepDefinition & {
  branches: {
    then: StepDefinition[]
    else: StepDefinition[]
  }
}

/** Toolbox templates must omit `id` (StepDefinition). */
export function taskStepDef(
  type: string,
  name: string,
  properties: Record<string, string>,
): StepDefinition {
  return {
    componentType: 'task',
    type,
    name,
    properties,
  }
}

const DEFAULT_IF_CONDITION = JSON.stringify({
  left: 'true',
  op: 'eq',
  right: 'true',
  rightKind: 'literal',
})

/** Placeholder so a newly dropped if passes compile (then must be non-empty). */
const PCA_SWD_IF_THEN_PLACEHOLDER = taskStepDef('tool', 'then：请替换', {
  stepId: 'step_then_1',
  tool: 'mcp.e2e-mock.echo',
  argsJson: '[]',
})

export const PCA_SWD_IF_TEMPLATE: IfStepDefinition = {
  componentType: 'switch',
  type: 'if',
  name: '条件分支',
  properties: {
    stepId: 'step_if',
    conditionJson: DEFAULT_IF_CONDITION,
  },
  branches: {
    then: [PCA_SWD_IF_THEN_PLACEHOLDER],
    else: [],
  },
}

/** Toolbox steps for SWD canvas Beta. */
export const PCA_SWD_TOOLBOX: ToolboxConfiguration = {
  groups: [
    {
      name: '步骤',
      steps: [
        taskStepDef('tool', '调用工具', {
          stepId: 'step_tool',
          tool: 'mcp.e2e-mock.echo',
          argsJson: '[]',
        }),
        taskStepDef('assign', '设置变量（如 pick）', {
          stepId: 'step_assign',
          assignmentsJson: JSON.stringify([
            { var: 'health', expr: '${steps.status.output.content.0.text}' },
          ]),
        }),
        PCA_SWD_IF_TEMPLATE,
      ],
    },
  ],
}
