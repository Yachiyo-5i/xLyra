import { LoaderCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { inferStatusCode } from '@/components/auth/auth-utils'
import { AuthCard } from '@/features/auth/components/auth-card'
import { AuthViewport } from '@/features/auth/components/auth-viewport'
import { LyraAuthScene } from '@/features/auth/components/lyra-auth-scene'

type AuthStatusScreenProps = {
  title: string
  description: string
  state?: 'loading' | 'error'
  statusCode?: number | null
}

/** 鉴权检查中 / 服务不可用时的全屏状态页：与登录页同一片夜空，卡片始终显示、不可关闭 */
export function AuthStatusScreen({ title, description, state = 'loading', statusCode }: AuthStatusScreenProps) {
  const { t } = useTranslation('components')
  const displayStatusCode = statusCode ?? inferStatusCode(description) ?? 502

  return (
    <AuthViewport>
      <LyraAuthScene>
        {() =>
          state === 'error' ? (
            <AuthCard title={t('authGuard.serviceErrorTitle')} description={t('authGuard.serviceErrorDesc')}>
              <div className="flex items-baseline justify-between gap-3 rounded-inner bg-fill-3 px-4 py-3">
                <span className="font-mono text-kpi text-label">{displayStatusCode}</span>
                <span className="font-mono text-footnote text-label-3">HTTP {displayStatusCode}</span>
              </div>
              <p className="mt-3 break-words text-footnote text-label-3">{description}</p>
            </AuthCard>
          ) : (
            <AuthCard title={title}>
              <div className="flex items-center gap-3 text-subhead text-label-2">
                <LoaderCircle className="size-4 shrink-0 animate-spin text-label-3" />
                {description}
              </div>
            </AuthCard>
          )
        }
      </LyraAuthScene>
    </AuthViewport>
  )
}
