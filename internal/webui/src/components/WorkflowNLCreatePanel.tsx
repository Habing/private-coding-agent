import { Link } from 'react-router-dom'

import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'

const EXAMPLE_PROMPTS = [
  '帮我做一个每天 9 点抓取 HN 头条并摘要的工作流',
  '创建一个 webhook 触发的工作流：收到 JSON 后调用 http.fetch 再写入变量',
  '用模板 news-digest 的思路，生成一个可试运行的工作流草案',
]

export function WorkflowNLCreatePanel() {
  return (
    <Card>
      <CardHeader>
        <CardTitle>用自然语言创建</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-4 text-sm">
        <p className="text-muted-foreground">
          在对话里描述你想自动化的流程，助手会调用{' '}
          <span className="font-mono text-xs">workflow.propose</span>{' '}
          生成草案。确认后工作流会出现在「我的工作流」和下方草案收件箱。
        </p>
        <ol className="flex list-decimal flex-col gap-2 pl-5 text-muted-foreground">
          <li>打开首页，新建或进入一个对话</li>
          <li>用中文说明触发方式、步骤和期望输出</li>
          <li>在对话卡片中点「在设计器中打开」精修，或「确认发布」</li>
          <li>回到本页「我的工作流」继续编辑、试运行、发布</li>
        </ol>
        <div className="flex flex-col gap-2">
          <span className="text-xs font-medium text-muted-foreground">示例提示词</span>
          <ul className="flex flex-col gap-2">
            {EXAMPLE_PROMPTS.map((text) => (
              <li
                key={text}
                className="rounded-md border bg-muted/40 px-3 py-2 font-mono text-xs leading-relaxed"
              >
                {text}
              </li>
            ))}
          </ul>
        </div>
        <Button size="sm" className="w-fit" asChild>
          <Link to="/">前往对话创建</Link>
        </Button>
      </CardContent>
    </Card>
  )
}
