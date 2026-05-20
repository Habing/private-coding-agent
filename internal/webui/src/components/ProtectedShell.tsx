import { Navigate, Outlet } from 'react-router-dom'

import { SessionList } from '@/components/SessionList'
import { TopBar } from '@/components/TopBar'
import { useAuthStore } from '@/stores/auth'

export function ProtectedShell() {
  const token = useAuthStore((s) => s.token)
  if (!token) return <Navigate to="/login" replace />

  return (
    <div className="flex h-screen w-screen flex-col bg-background text-foreground">
      <TopBar />
      <div className="flex flex-1 overflow-hidden">
        <aside className="w-64 shrink-0 border-r">
          <SessionList />
        </aside>
        <main className="flex-1 overflow-hidden">
          <Outlet />
        </main>
      </div>
    </div>
  )
}
