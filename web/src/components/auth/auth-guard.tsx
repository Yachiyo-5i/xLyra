import { useEffect, type PropsWithChildren } from 'react'
import { Navigate, useLocation } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { LoaderCircle } from 'lucide-react'
import { AuthStatusScreen } from '@/components/auth/auth-status-screen'
import { inferStatusCode } from '@/components/auth/auth-utils'
import { buildRedirectSearch, resolveAuthRedirectTarget } from '@/lib/navigation'
import { useAuthStore } from '@/stores/auth-store'

function useInitializeAuth() {
  const status = useAuthStore((state) => state.status)
  const initialize = useAuthStore((state) => state.initialize)

  useEffect(() => {
    if (status === 'idle') {
      void initialize()
    }
  }, [initialize, status])

  return initialize
}

export function ProtectedRoute({ children }: PropsWithChildren) {
  const location = useLocation()
  const { t } = useTranslation('components')
  useInitializeAuth()
  const status = useAuthStore((state) => state.status)
  const errorMessage = useAuthStore((state) => state.errorMessage)
  const errorStatus = useAuthStore((state) => state.errorStatus)

  if (status === 'idle' || status === 'checking') {
    return <ProtectedAuthStatus title={t('authGuard.checkingTitle')} description={t('authGuard.checkingDesc')} />
  }

  if (status === 'error') {
    return <ProtectedAuthStatus state="error" title={t('authGuard.errorTitle')} description={errorMessage ?? t('authGuard.errorDesc')} statusCode={errorStatus} />
  }

  if (status === 'registration-required') {
    return <Navigate to={`/register?${buildRedirectSearch(location)}`} replace />
  }

  if (status === 'unauthenticated') {
    return <Navigate to={`/login?${buildRedirectSearch(location)}`} replace />
  }

  return <>{children}</>
}

type ProtectedAuthStatusProps = {
  title: string
  description: string
  state?: 'loading' | 'error'
  statusCode?: number | null
}

function ProtectedAuthStatus({
  title,
  description,
  state = 'loading',
  statusCode,
}: ProtectedAuthStatusProps) {
  const { t } = useTranslation('components')
  const displayStatusCode = statusCode ?? inferStatusCode(description) ?? 502

  if (state === 'error') {
    return (
      <div className="flex min-h-full items-center justify-center px-4 py-10 text-center">
        <div>
          <p className="text-[clamp(3.75rem,6vw,5.5rem)] font-semibold leading-none tracking-tight text-foreground">
            {displayStatusCode}
          </p>
          <h1 className="mt-5 text-lg font-semibold tracking-tight text-foreground md:text-xl">
            {t('authGuard.serviceErrorTitle')}
          </h1>
          <p className="mx-auto mt-3 max-w-md text-sm leading-6 text-muted-soft">
            {t('authGuard.serviceErrorDesc')}
          </p>
          <p className="mt-3 text-xs font-medium text-faint">HTTP {displayStatusCode}</p>
        </div>
      </div>
    )
  }

  return (
    <div className="flex min-h-full items-center justify-center px-4 py-10">
      <div className="flex items-center gap-3 rounded-full border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-subtle))] px-4 py-3 text-sm text-muted-soft">
        <LoaderCircle className="h-4 w-4 animate-spin text-primary" />
        <span>{title}</span>
        <span className="hidden text-faint sm:inline">{description}</span>
      </div>
    </div>
  )
}

type PublicOnlyRouteProps = PropsWithChildren<{
  mode: 'login' | 'register'
}>

export function PublicOnlyRoute({ children, mode }: PublicOnlyRouteProps) {
  const location = useLocation()
  const { t } = useTranslation('components')
  useInitializeAuth()
  const status = useAuthStore((state) => state.status)
  const errorMessage = useAuthStore((state) => state.errorMessage)
  const errorStatus = useAuthStore((state) => state.errorStatus)

  if (status === 'idle' || status === 'checking') {
    return (
      <AuthStatusScreen
        title={t('authGuard.preparingTitle')}
        description={t('authGuard.preparingDesc')}
      />
    )
  }

  if (status === 'error') {
    return (
      <AuthStatusScreen
        state="error"
        title={t('authGuard.unavailableTitle')}
        description={errorMessage ?? t('authGuard.unavailableDesc')}
        statusCode={errorStatus}
      />
    )
  }

  if (status === 'authenticated') {
    return <Navigate to={resolveAuthRedirectTarget(location.search)} replace />
  }

  if (mode === 'login' && status === 'registration-required') {
    return <Navigate to={`/register${location.search}`} replace />
  }

  if (mode === 'register' && status !== 'registration-required') {
    return <Navigate to={`/login${location.search}`} replace />
  }

  return <>{children}</>
}
