import { useTranslation } from 'react-i18next'
import { AuthShell } from '@/features/auth/components/auth-shell'
import { LoginForm } from '@/features/auth/components/login-form'
import { MobileAuthShell } from '@/features/auth/components/mobile-auth-shell'
import { useMobileLayout } from '@/hooks/use-media-query'

export function LoginPage() {
  const { t } = useTranslation('auth')
  const isMobile = useMobileLayout()

  if (isMobile) {
    return (
      <MobileAuthShell title={t('login.title')} description={t('login.description')}>
        <LoginForm variant="mobile" />
      </MobileAuthShell>
    )
  }

  return (
    <AuthShell
      mode="login"
      eyebrow={t('login.eyebrow')}
      title={t('login.title')}
      description={t('login.description')}
    >
      <LoginForm />
    </AuthShell>
  )
}
