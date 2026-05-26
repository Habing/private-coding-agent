import { PanelLeftClose, PanelLeftOpen } from 'lucide-react'
import { useState } from 'react'
import { Navigate, Outlet } from 'react-router-dom'

import { GuideProvider } from '@/components/GuideProvider'
import { SessionList } from '@/components/SessionList'
import { TopBar } from '@/components/TopBar'
import { Button } from '@/components/ui/button'
import {
  readSessionSidebarOpen,
  writeSessionSidebarOpen,
} from '@/lib/sessionSidebarStorage'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth'

export function ProtectedShell() {
  const token = useAuthStore((s) => s.token)
  const [sessionSidebarOpen, setSessionSidebarOpen] = useState(() =>
    readSessionSidebarOpen(),
  )

  if (!token) return <Navigate to="/login" replace />

  function toggleSessionSidebar() {
    setSessionSidebarOpen((open) => {
      const next = !open
      writeSessionSidebarOpen(next)
      return next
    })
  }

  return (
    <GuideProvider>
      <div className="flex h-screen w-screen flex-col bg-background text-foreground">
        <TopBar
          sessionSidebarOpen={sessionSidebarOpen}
          onToggleSessionSidebar={toggleSessionSidebar}
        />
        <div className="flex flex-1 overflow-hidden">
          {sessionSidebarOpen ? (
            <aside className="w-64 shrink-0 border-r">
              <SessionList />
            </aside>
          ) : null}
          <div className="relative flex min-w-0 flex-1 flex-col">
            <Button
              type="button"
              variant="outline"
              size="icon"
              className={cn(
                'absolute top-3 z-20 h-8 w-7 rounded-md shadow-sm',
                sessionSidebarOpen ? '-left-3.5' : 'left-2',
              )}
              onClick={toggleSessionSidebar}
              aria-expanded={sessionSidebarOpen}
              aria-label={sessionSidebarOpen ? '隐藏会话栏' : '显示会话栏'}
              title={sessionSidebarOpen ? '隐藏会话栏' : '显示会话栏'}
            >
              {sessionSidebarOpen ? (
                <PanelLeftClose className="h-4 w-4" aria-hidden />
              ) : (
                <PanelLeftOpen className="h-4 w-4" aria-hidden />
              )}
            </Button>
            <main className="flex-1 overflow-hidden">
              <Outlet />
            </main>
          </div>
        </div>
      </div>
    </GuideProvider>
  )
}
