import { Link, useNavigate } from 'react-router-dom'

import { Button } from '@/components/ui/button'
import { isAdmin } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth'

export function TopBar() {
  const navigate = useNavigate()
  const user = useAuthStore((s) => s.user)
  const clear = useAuthStore((s) => s.clear)

  function onLogout() {
    clear()
    navigate('/login', { replace: true })
  }

  return (
    <header className="flex h-12 shrink-0 items-center justify-between border-b px-4">
      <div className="flex items-center gap-4 text-sm">
        <Link to="/" className="font-semibold">
          Private Coding Agent
        </Link>
        <Link
          to="/memories"
          className="text-muted-foreground hover:text-foreground"
        >
          记忆
        </Link>
        {isAdmin(user) && (
          <>
            <Link
              to="/audit"
              className="text-muted-foreground hover:text-foreground"
            >
              审计
            </Link>
            <Link
              to="/admin/skills"
              className="text-muted-foreground hover:text-foreground"
            >
              Skills
            </Link>
          </>
        )}
      </div>
      <div className="flex items-center gap-3 text-sm">
        {user && <span className="text-muted-foreground">{user.email}</span>}
        <Button variant="ghost" size="sm" onClick={onLogout}>
          退出
        </Button>
      </div>
    </header>
  )
}
