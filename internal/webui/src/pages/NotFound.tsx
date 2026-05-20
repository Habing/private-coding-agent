import { Link } from 'react-router-dom'

export function NotFound() {
  return (
    <div className="flex h-screen w-screen flex-col items-center justify-center gap-2 text-center">
      <h1 className="text-4xl font-bold">404</h1>
      <p className="text-muted-foreground">页面未找到</p>
      <Link to="/" className="text-primary underline-offset-4 hover:underline">
        回到首页
      </Link>
    </div>
  )
}
