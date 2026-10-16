import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { AuthCard } from '@/features/auth/components/auth-card'
import { AuthViewport } from '@/features/auth/components/auth-viewport'
import { LyraAuthScene } from '@/features/auth/components/lyra-auth-scene'
import { RegisterForm } from '@/features/auth/components/register-form'

/** 注册页（实例初始化）：管理员创建成功时织女星亮起 */
export function RegisterPage() {
  const { t } = useTranslation('auth')
  const [created, setCreated] = useState(false)

  return (
    <AuthViewport>
      <LyraAuthScene entryLabel={t('register.title')} vegaLit={created}>
        {() => (
          <AuthCard
            title={created ? t('register.createdTitle') : t('register.title')}
            description={created ? undefined : t('register.description')}
          >
            <RegisterForm onCreated={() => setCreated(true)} />
          </AuthCard>
        )}
      </LyraAuthScene>
    </AuthViewport>
  )
}
