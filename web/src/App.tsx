import { Suspense } from 'react'
import { RouterProvider } from 'react-router-dom'
import { AppProviders } from '@/app/providers'
import { appRouter } from '@/app/router'
import { PwaUpdatePrompt } from '@/components/common/pwa-update-prompt'

export default function App() {
  return (
    <AppProviders>
      <PwaUpdatePrompt />
      <Suspense fallback={<AppFallback />}>
        <RouterProvider router={appRouter} />
      </Suspense>
    </AppProviders>
  )
}

function AppFallback() {
  return (
    <div className="flex min-h-screen items-center justify-center bg-canvas text-body text-label-2">
      Loading...
    </div>
  )
}
