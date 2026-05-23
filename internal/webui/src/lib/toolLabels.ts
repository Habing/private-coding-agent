/** 工具箱展示用中文说明。工具名 (name) 保持 API 英文；description 优先用后端中文。 */

const TOOL_DESCRIPTIONS: Record<string, string> = {
  'fs.read':
    '读取沙箱工作区 /workspace 内的 UTF-8 文本文件。路径相对于 workspace 根目录。',
  'fs.write':
    '向沙箱工作区写入文件，自动创建中间目录；若文件已存在则覆盖。（可变更）',
  'fs.list': '列出沙箱指定目录下的文件和子目录（非递归）。',
  'fs.glob':
    '按 glob 模式匹配沙箱内文件，例如 **/*.go、src/**/*.test.ts。',
  grep: '在沙箱内用正则搜索文件内容，返回匹配行及文件:行号。',
  'shell.exec':
    '在沙箱内执行 shell 命令，返回退出码、标准输出与标准错误。（可变更）',
  'memory.save':
    '为当前用户保存一条记忆（画像/偏好/知识/经验），返回新记录 ID。（可变更）',
  'memory.search':
    '检索当前用户的记忆。默认向量语义搜索；mode=keyword 时用关键词匹配。',
  'memory.list':
    '浏览当前用户的记忆列表，可按类型、标签过滤；精确查找请用 memory.search。',
  'memory.delete': '按 ID 删除一条记忆。（可变更）',
  'llm.chat': '调用已配置的大模型完成一次对话补全，返回 assistant 回复文本。',
  'llm.embed': '对一段或多段文本计算 embedding 向量。',
  'agent.delegate':
    '将子任务委派给其他智能体类型（如代码评审、资料调研、工作流编写）。子智能体继承同一沙箱与模型，仅返回最终答案。（可变更）',
  'workflow.create':
    '起草新工作流 DSL（仅 admin）。创建后为未发布状态，需管理员在 REST/Web 上发布。',
  'workflow.update':
    '更新工作流的名称、描述或 DSL（仅 admin）。版本号递增；若曾发布会强制取消发布。（可变更）',
  'workflow.list':
    '列出本租户全部工作流（仅 admin）：slug、名称、版本、发布状态，不含 DSL 正文。',
  'workflow.get':
    '获取单个工作流详情含 DSL 正文（仅 admin），适合在 update 前先读取当前内容。',
}

const TOOL_TITLES: Record<string, string> = {
  'fs.read': '读取文件',
  'fs.write': '写入文件',
  'fs.list': '列出目录',
  'fs.glob': '按模式找文件',
  grep: '内容搜索',
  'shell.exec': '执行命令',
  'memory.save': '保存记忆',
  'memory.search': '搜索记忆',
  'memory.list': '列出记忆',
  'memory.delete': '删除记忆',
  'llm.chat': '大模型对话',
  'llm.embed': '文本向量化',
  'agent.delegate': '委派子智能体',
  'workflow.create': '创建工作流',
  'workflow.update': '更新工作流',
  'workflow.list': '工作流列表',
  'workflow.get': '获取工作流',
}

export function toolTitleZh(name: string): string {
  if (TOOL_TITLES[name]) return TOOL_TITLES[name]
  if (name.startsWith('workflow.')) {
    return `工作流：${name.slice('workflow.'.length)}`
  }
  if (name.startsWith('mcp.')) {
    const parts = name.split('.')
    if (parts.length >= 3) {
      return `MCP · ${parts[1]} · ${parts.slice(2).join('.')}`
    }
  }
  return name
}

export function toolDescriptionZh(name: string, apiDescription: string): string {
  if (TOOL_DESCRIPTIONS[name]) return TOOL_DESCRIPTIONS[name]

  if (name.startsWith('workflow.')) {
    const slug = name.slice('workflow.'.length)
    const desc = apiDescription.trim()
    if (desc.startsWith('Published workflow:')) {
      return `已发布的工作流「${slug}」。可在 Agent 或工作流编排中作为工具调用。（可变更）`
    }
    if (desc.startsWith('已发布的工作流：')) {
      return desc
    }
    if (desc) {
      return `工作流「${slug}」：${desc}（可变更）`
    }
    return `已发布的工作流「${slug}」，按 DSL 定义执行多步自动化。（可变更）`
  }

  if (name.startsWith('mcp.')) {
    const parts = name.split('.')
    const server = parts[1] ?? 'unknown'
    const tool = parts.slice(2).join('.') || 'tool'
    const desc = apiDescription.trim()
    if (
      desc &&
      !desc.startsWith('External MCP tool from') &&
      !desc.startsWith('外部 MCP 工具')
    ) {
      return `外部 MCP「${server}」· ${tool}：${desc}`
    }
    return `调用外部 MCP 服务「${server}」上的工具「${tool}」。`
  }

  if (apiDescription.trim()) return apiDescription.trim()
  return '（无描述）'
}
