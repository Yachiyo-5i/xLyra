import { useTranslation } from 'react-i18next'
import { AuthCard } from '@/features/auth/components/auth-card'
import { AuthViewport } from '@/features/auth/components/auth-viewport'
import { LoginForm } from '@/features/auth/components/login-form'
import { LyraAuthScene } from '@/features/auth/components/lyra-auth-scene'

/** 登录页：天琴座夜空 + 「登录」入口按钮，点开后升起登录卡片 */
export function LoginPage() {
  const { t } = useTranslation('auth')

  return (
    <AuthViewport>
      <LyraAuthScene entryLabel={t('login.loginButton')}>
        {() => (
          <AuthCard title={`${t('login.title')} xLyra`} description={t('login.description')}>
            <LoginForm />
          </AuthCard>
        )}
      </LyraAuthScene>
    </AuthViewport>
  )
}
