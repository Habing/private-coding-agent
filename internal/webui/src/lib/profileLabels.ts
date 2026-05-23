/** UI labels for built-in agent profiles. API values stay English (`coding`, etc.). */

const PROFILE_LABELS: Record<string, string> = {
  coding: '编码助手',
  review: '代码评审',
  research: '资料调研',
  'workflow-authoring': '工作流编写',
}

const PROFILE_DESCRIPTIONS: Record<string, string> = {
  coding:
    '全能力编码智能体，可使用沙箱；可委派评审、调研与工作流编写子任务。',
  review: '只读代码/文档评审，不修改沙箱；通常由编码助手委派调用。',
  research: '资料检索与归纳，可读写记忆，不访问沙箱。',
  'workflow-authoring':
    '根据自然语言起草工作流 YAML；发布需在管理端由人工操作。',
}

export function profileLabel(name: string): string {
  return PROFILE_LABELS[name] ?? name
}

export function profileDescription(name: string, apiFallback?: string): string {
  return PROFILE_DESCRIPTIONS[name] ?? apiFallback ?? ''
}
