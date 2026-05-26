import { CircleHelp, PanelLeftClose, PanelLeftOpen } from 'lucide-react'
import type { ReactNode } from 'react'
import { Link, useNavigate } from 'react-router-dom'

import { QuotaBar } from '@/components/QuotaBar'
import { useGuide } from '@/hooks/useGuide'
import { Button } from '@/components/ui/button'
import { featureHintByPath } from '@/lib/featureGuide'
import { isAdmin } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth'

function NavLink({
  to,
  children,
}: {
  to: string
  children: ReactNode
}) {
  const user = useAuthStore((s) => s.user)
  const title = featureHintByPath(to, isAdmin(user))

  return (
    <Link
      to={to}
      title={title}
      className="text-muted-foreground hover:text-foreground"
    >
      {children}
    </Link>
  )
}

export interface TopBarProps {
  sessionSidebarOpen?: boolean
  onToggleSessionSidebar?: () => void
}

export function TopBar({
  sessionSidebarOpen,
  onToggleSessionSidebar,
}: TopBarProps = {}) {
  const navigate = useNavigate()
  const user = useAuthStore((s) => s.user)
  const clear = useAuthStore((s) => s.clear)
  const { openGuide } = useGuide()

  function onLogout() {
    clear()
    navigate('/login', { replace: true })
  }

  const sidebarToggleLabel =
    sessionSidebarOpen === false ? '显示会话栏' : '隐藏会话栏'

  return (
    <header className="flex h-12 shrink-0 items-center justify-between border-b px-4">
      <div className="flex items-center gap-3 text-sm">
        {onToggleSessionSidebar ? (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="h-8 gap-1.5 px-2"
            onClick={onToggleSessionSidebar}
            aria-expanded={sessionSidebarOpen ?? true}
            aria-label={sidebarToggleLabel}
            title={sidebarToggleLabel}
          >
            {sessionSidebarOpen === false ? (
              <PanelLeftOpen className="h-4 w-4" aria-hidden />
            ) : (
              <PanelLeftClose className="h-4 w-4" aria-hidden />
            )}
            <span className="hidden sm:inline">会话</span>
          </Button>
        ) : null}
        <Link to="/" className="font-semibold" title="返回首页">
          Private Coding Agent
        </Link>
        <NavLink to="/memories">记忆</NavLink>
        <NavLink to="/toolbox">工具箱</NavLink>
        {isAdmin(user) && (
          <>
            <NavLink to="/audit">审计</NavLink>
            <NavLink to="/admin/skills">技能</NavLink>
            <NavLink to="/workflows">工作流</NavLink>
            <NavLink to="/admin/connectors">连接器</NavLink>
            <NavLink to="/admin/mcp-servers">MCP 服务</NavLink>
            <NavLink to="/admin/memory-proposals">记忆提议</NavLink>
            <NavLink to="/admin/workflow-proposals">工作流提议</NavLink>
          </>
        )}
      </div>
      <div className="flex items-center gap-3 text-sm">
        <QuotaBar />
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="h-8 gap-1.5"
          onClick={openGuide}
        >
          <CircleHelp className="h-3.5 w-3.5" aria-hidden="true" />
          使用指引
        </Button>
        {user && <span className="text-muted-foreground">{user.email}</span>}
        <Button variant="ghost" size="sm" onClick={onLogout}>
          退出
        </Button>
      </div>
    </header>
  )
}
