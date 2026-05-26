/** Tracks whether expert YAML and visual designer are in sync. */

export function isDesignerOutOfSync(dsl: string, designerBasisDsl: string): boolean {
  return dsl.trim() !== designerBasisDsl.trim()
}

export const SYNC_YAML_TO_DESIGNER_CONFIRM =
  '专家 YAML 已与设计器不一致。同步到设计器将按当前 YAML 重新解析（foreach/parallel 等复杂结构可能无法还原）。是否继续？'
