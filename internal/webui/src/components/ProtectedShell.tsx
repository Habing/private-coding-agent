import { Navigate, Outlet } from 'react-router-dom'

import { useAuthStore } from '@/stores/auth'

export function ProtectedShell() {
  const token = useAuthStore((s) => s.token)
  if (!token) return <Navigate to="/login" replace />

  return (
    <div className="flex h-screen w-screen flex-col">
      <header className="flex h-12 items-center border-b px-4 text-sm font-semibold">
        Private Coding Agent
      </header>
      <div className="flex flex-1 overflow-hidden">
        <aside className="w-64 border-r p-2 text-sm text-muted-foreground">
          {/* SessionList — Task 5 */}
          <div>会话列表（待 Task 5 接入）</div>
        </aside>
        <main className="flex-1 overflow-hidden">
          <Outlet />
        </main>
      </div>
    </div>
  )
}
