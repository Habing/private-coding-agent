import { QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter, Route, Routes } from 'react-router-dom'

import { AdminGuard } from '@/components/AdminGuard'
import { ProtectedShell } from '@/components/ProtectedShell'
import { Audit } from '@/pages/Audit'
import { Chat } from '@/pages/Chat'
import { Memories } from '@/pages/Memories'
import { Home } from '@/pages/Home'
import { Login } from '@/pages/Login'
import { NotFound } from '@/pages/NotFound'
import { queryClient } from '@/queryClient'

export function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <Routes>
          <Route path="/login" element={<Login />} />
          <Route element={<ProtectedShell />}>
            <Route path="/" element={<Home />} />
            <Route path="/sessions/:id" element={<Chat />} />
            <Route path="/memories" element={<Memories />} />
            <Route
              path="/audit"
              element={
                <AdminGuard>
                  <Audit />
                </AdminGuard>
              }
            />
          </Route>
          <Route path="*" element={<NotFound />} />
        </Routes>
      </BrowserRouter>
    </QueryClientProvider>
  )
}
