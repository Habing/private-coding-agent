import type { ReactNode } from 'react'
import { Navigate } from 'react-router-dom'

import { isAdmin } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth'

export function AdminGuard({ children }: { children: ReactNode }) {
  const user = useAuthStore((s) => s.user)
  if (!isAdmin(user)) {
    return <Navigate to="/" replace />
  }
  return <>{children}</>
}
