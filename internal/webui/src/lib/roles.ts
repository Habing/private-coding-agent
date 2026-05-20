import type { User } from '@/types/api'

export const ROLE_ADMIN = 'admin'

export function isAdmin(u: User | null | undefined): boolean {
  return !!u && u.role === ROLE_ADMIN
}
