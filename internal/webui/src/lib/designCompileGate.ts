import { PCA_STEP_ID_PATTERN } from '@/lib/swdAdapter'
import type { WorkflowDesign, WorkflowDesignStep } from '@/types/api'

/** Client-side checks aligned with design/compile + PUT validation (avoids noisy 400s). */
export function designCompileBlockReason(design: WorkflowDesign): string | null {
  if (!design.name?.trim()) {
    return '工作流名称不能为空'
  }
  if (!design.steps?.length) {
    return '至少需要一个步骤'
  }
  const seen = new Set<string>()
  const walk = (steps: WorkflowDesignStep[]): string | null => {
    for (const s of steps) {
      const id = s.id?.trim()
      if (!id) return '存在未命名步骤'
      if (!PCA_STEP_ID_PATTERN.test(id)) {
        return `步骤 ID「${id}」格式无效（须以字母或 _ 开头）`
      }
      if (seen.has(id)) return `重复步骤 ID：${id}`
      seen.add(id)
      if (s.kind === 'tool' && !s.tool?.trim()) {
        return `步骤「${id}」：请选择工具`
      }
      if (s.kind === 'assign' && !(s.assignments?.length ?? 0)) {
        return `步骤「${id}」：请添加至少一条变量（设置变量）`
      }
      if (s.kind === 'if') {
        if (!s.condition?.op?.trim()) {
          return `步骤「${id}」：请设置条件运算符`
        }
        if (!(s.then?.length ?? 0)) {
          return `步骤「${id}」：then 分支至少需要一个步骤（拖入工具到 then 区域）`
        }
        const thenErr = walk(s.then ?? [])
        if (thenErr) return thenErr
        const elseErr = walk(s.else ?? [])
        if (elseErr) return elseErr
      }
    }
    return null
  }
  return walk(design.steps)
}
