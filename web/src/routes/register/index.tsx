import { useTranslation } from 'react-i18next'
import { AuthShell } from '@/features/auth/components/auth-shell'
import { MobileAuthShell } from '@/features/auth/components/mobile-auth-shell'
import { RegisterForm } from '@/features/auth/components/register-form'
import { useMobileLayout } from '@/hooks/use-media-query'

export function RegisterPage() {
  const { t } = useTranslation('auth')
  const isMobile = useMobileLayout()

  if (isMobile) {
    return (
      <MobileAuthShell title={t('register.title')} description={t('register.description')}>
        <RegisterForm variant="mobile" />
      </MobileAuthShell>
    )
  }

  return (
    <AuthShell
      mode="register"
      eyebrow={t('register.eyebrow')}
      title={t('register.title')}
      description={t('register.description')}
    >
      <RegisterForm />
    </AuthShell>
  )
}
