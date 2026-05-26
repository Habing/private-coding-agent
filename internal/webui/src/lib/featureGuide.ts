export interface QuickStartStep {
  title: string
  description: string
}

export interface FeatureGuideItem {
  id: string
  title: string
  /** Short hint for nav link `title` attribute. */
  hint: string
  description: string
  to?: string
  adminOnly?: boolean
  area: 'sidebar' | 'topbar' | 'main'
}

export const QUICK_START_STEPS: QuickStartStep[] = [
  {
    title: '新建会话',
    description:
      '在左侧点击「+ 新建」，或在首页选择智能体类型后创建。每个会话在独立沙箱中运行，适合一项具体任务。',
  },
  {
    title: '用自然语言提需求',
    description:
      '在聊天框描述目标即可，例如「读取 README 并总结」或「帮我写一个 MCP 服务」。Agent 会自动选用工具执行。',
  },
  {
    title: '按需使用扩展功能',
    description:
      '顶部导航提供记忆、工具箱、工作流等能力。不确定时随时点右上角「使用指引」查看说明。',
  },
]

export const FEATURE_GUIDES: FeatureGuideItem[] = [
  {
    id: 'sessions',
    title: '会话',
    hint: '管理对话历史，新建或切换任务',
    description:
      '左侧列表保存你的所有对话。每个会话有独立上下文与沙箱环境；删除或切换会话不会影响其他会话。',
    area: 'sidebar',
  },
  {
    id: 'chat',
    title: '聊天',
    hint: '向 Agent 描述需求，查看回复与工具调用',
    description:
      '主区域是与 Coding Agent 的对话界面。Agent 可读写文件、执行命令、检索记忆，并在需要时调用外部工具。',
    area: 'main',
  },
  {
    id: 'memories',
    title: '记忆',
    hint: '保存偏好与知识，供 Agent 跨会话引用',
    description:
      '手动添加或让 Agent 在对话中写入「偏好、知识、经验」等记忆。后续会话会自动检索相关记忆，减少重复说明。',
    to: '/memories',
    area: 'topbar',
  },
  {
    id: 'toolbox',
    title: '工具箱',
    hint: '查看 Agent 可用的全部工具及参数说明',
    description:
      '列出后端 ToolBus 已注册的工具（读文件、执行命令、HTTP 请求等）。带「可变更」标记的工具会修改沙箱或数据，工作流 Dry-Run 会拦截它们。',
    to: '/toolbox',
    area: 'topbar',
  },
  {
    id: 'audit',
    title: '审计',
    hint: '管理员：查看操作与安全审计日志',
    description:
      '按时间浏览租户内的关键操作记录，用于合规排查与问题追溯。仅管理员可见。',
    to: '/audit',
    adminOnly: true,
    area: 'topbar',
  },
  {
    id: 'skills',
    title: '技能',
    hint: '管理员：配置注入到 Agent 的 Markdown 技能',
    description:
      '定义可复用的技能文档（如代码规范、领域知识），绑定到智能体类型后会在 system prompt 中生效。',
    to: '/admin/skills',
    adminOnly: true,
    area: 'topbar',
  },
  {
    id: 'workflows',
    title: '工作流',
    hint: '管理员：编排可重复执行的自动化任务',
    description:
      '用 YAML 或可视化编辑器定义多步流程（LLM、工具调用、通知等），支持 Dry-Run 试跑、发布与定时触发。',
    to: '/workflows',
    adminOnly: true,
    area: 'topbar',
  },
  {
    id: 'connectors',
    title: '连接器',
    hint: '管理员：安装外部 HTTP 服务并配置访问白名单',
    description:
      '通过 OpenAPI 等描述安装第三方 HTTP 连接器，并管理 http.fetch 允许访问的主机列表，扩展 Agent 的外部数据能力。',
    to: '/admin/connectors',
    adminOnly: true,
    area: 'topbar',
  },
  {
    id: 'mcp',
    title: 'MCP 服务',
    hint: '管理员：注册 Model Context Protocol 外部工具',
    description:
      '接入 MCP 协议的服务端，将其工具暴露给 Agent。适合集成 IDE、数据库、搜索等外部系统。',
    to: '/admin/mcp-servers',
    adminOnly: true,
    area: 'topbar',
  },
  {
    id: 'memory-proposals',
    title: '记忆提议',
    hint: '管理员：审批 Agent 自动提议的新记忆',
    description:
      'Agent 在对话中可能提议写入记忆；在此审阅、编辑后批准或拒绝，避免未经确认的信息入库。',
    to: '/admin/memory-proposals',
    adminOnly: true,
    area: 'topbar',
  },
  {
    id: 'workflow-proposals',
    title: '工作流提议',
    hint: '管理员：审批对话中生成的工作流草案',
    description:
      '当 Agent 在聊天中调用 workflow.propose 时，草案会出现在此处。确认 Dry-Run 结果后可发布为正式工作流。',
    to: '/admin/workflow-proposals',
    adminOnly: true,
    area: 'topbar',
  },
]

export function featureGuidesForUser(admin: boolean): FeatureGuideItem[] {
  return FEATURE_GUIDES.filter((f) => !f.adminOnly || admin)
}

export function featureHintByPath(path: string, admin: boolean): string | undefined {
  const item = featureGuidesForUser(admin).find((f) => f.to === path)
  return item?.hint
}
