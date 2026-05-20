import { useParams } from 'react-router-dom'

export function Chat() {
  const { id } = useParams<{ id: string }>()
  return (
    <div className="flex h-full items-center justify-center">
      <div className="text-muted-foreground">聊天页 (session={id}) — 待 Task 6 / 7 实现</div>
    </div>
  )
}
