import { LoaderCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { AuthShell } from '@/features/auth/components/auth-shell'
import { inferStatusCode } from '@/components/auth/auth-utils'

type AuthStatusScreenProps = {
  title: string
  description: string
  state?: 'loading' | 'error'
  statusCode?: number | null
}

export function AuthStatusScreen({
  title,
  description,
  state = 'loading',
  statusCode,
}: AuthStatusScreenProps) {
  const { t } = useTranslation('components')
  const displayStatusCode = statusCode ?? inferStatusCode(description) ?? 502

  if (state === 'error') {
    return (
      <AuthShell mode="status" eyebrow="xLyra Auth" title={title} description={description}>
        <div className="text-center">
          <p className="text-[clamp(4rem,5vw,5.75rem)] font-semibold leading-none tracking-tight text-foreground">
            {displayStatusCode}
          </p>
          <h1 className="mt-5 text-lg font-semibold tracking-tight text-foreground md:text-xl">
            {t('authGuard.serviceErrorTitle')}
          </h1>
          <p className="mx-auto mt-4 max-w-[22rem] text-sm leading-6 text-muted-soft">
            {t('authGuard.serviceErrorDesc')}
          </p>
          <p className="mt-3 text-xs font-medium text-faint">
            HTTP {displayStatusCode}
          </p>
        </div>
      </AuthShell>
    )
  }

  return (
    <AuthShell mode="status" eyebrow="xLyra Auth" title={title} description={description}>
      <div className="text-center">
        <div className="mx-auto mb-6 grid size-12 place-items-center rounded-2xl bg-[hsl(var(--primary)/0.1)] text-primary">
          <LoaderCircle className="h-5 w-5 animate-spin" />
        </div>
        <p className="mx-auto max-w-[22rem] text-sm leading-6 text-muted-soft">
          {description}
        </p>
      </div>
    </AuthShell>
  )
}
