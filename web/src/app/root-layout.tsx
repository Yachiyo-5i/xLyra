import { Outlet } from 'react-router-dom'
import { ProtectedRoute } from '@/components/auth/auth-guard'
import { AppShell } from '@/components/layout/app-shell'

export function RootLayout() {
  return (
    <AppShell>
      <ProtectedRoute>
        <Outlet />
      </ProtectedRoute>
    </AppShell>
  )
}
