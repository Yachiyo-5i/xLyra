import type { ReactNode } from 'react'
import { Boxes, Route, ServerCog } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { AppLogo } from '@/components/common/app-logo'

type AuthShellProps = {
  eyebrow?: string
  title: string
  description?: string
  mode?: 'login' | 'register' | 'status'
  children: ReactNode
}

export function AuthShell({
  mode,
  title,
  children,
}: AuthShellProps) {
  const { t } = useTranslation(['auth', 'common'])
  const featureItems = [
    { icon: ServerCog, title: t('shell.featureSites'), caption: t('shell.featureSitesCaption') },
    { icon: Boxes, title: t('shell.featureModels'), caption: t('shell.featureModelsCaption') },
    { icon: Route, title: t('shell.featureRoutes'), caption: t('shell.featureRoutesCaption') },
  ]

  return (
    <div className="auth-page app-viewport">
      <div className="pointer-events-none absolute inset-0">
        <div className="auth-background-base absolute inset-0" />
        <div className="auth-orb-primary absolute left-[42%] top-[18%] h-[28rem] w-[28rem] rounded-full blur-[90px]" />
        <div className="auth-orb-secondary absolute right-[-9rem] bottom-[-12rem] h-[42rem] w-[42rem] rounded-full" />
        <div className="auth-orbit absolute right-[8%] top-[54%] h-[42rem] w-[42rem] rounded-full border" />
        <div className="auth-background-wash absolute inset-0" />
      </div>

      <div className="relative z-10 grid h-full min-h-0 w-full grid-rows-[auto_1fr_auto] overflow-y-auto px-6 py-7 sm:px-10 lg:px-[8vw]">
        <header className="flex items-center justify-between">
          <div className="flex items-center gap-3">
            <AppLogo className="size-11 rounded-xl shadow-[0_16px_36px_hsl(var(--primary)/0.24)]" decorative />
            <div className="min-w-0">
              <p className="auth-heading-text text-xl font-semibold">xLyra</p>
              <p className="auth-muted-text text-sm font-medium">{t('common:app.adminConsole')}</p>
            </div>
          </div>
        </header>

        <main className="grid min-h-0 items-center gap-12 py-12 lg:grid-cols-[1.05fr_0.95fr] lg:py-0">
          <section className="hidden lg:block">
            <div className="max-w-[660px]">
              <h2 className="auth-heading-text text-[clamp(3.6rem,4.2vw,5.4rem)] font-semibold leading-[1.08] tracking-normal">
                {t('shell.tagline')}
              </h2>
              <p className="auth-subtitle-text mt-8 text-[clamp(1.25rem,1.4vw,1.65rem)] font-semibold leading-8">
                {t('shell.subtitle')}
              </p>
            </div>

            <div className="mt-20 grid max-w-[680px] grid-cols-3">
              {featureItems.map((item) => {
                const Icon = item.icon

                return (
                  <div className="auth-feature-item relative px-8 text-left last:after:hidden" key={item.title}>
                    <div className="auth-feature-icon mb-5 grid size-14 place-items-center rounded-2xl text-primary">
                      <Icon className="h-7 w-7" />
                    </div>
                    <p className="auth-heading-text whitespace-nowrap text-lg font-semibold">{item.title}</p>
                    <p className="auth-muted-text mt-2 text-sm font-medium">{item.caption}</p>
                  </div>
                )
              })}
            </div>
          </section>

          <section className="flex w-full items-center justify-center">
            <div className="auth-card w-full max-w-[520px] rounded-[28px] px-[clamp(2rem,3.2vw,4rem)] py-[clamp(2.5rem,4vw,4.8rem)] backdrop-blur-2xl">
              {mode === 'status' ? null : (
                <div className="mb-[clamp(1.75rem,2.5vw,2.75rem)]">
                  <h1 className="auth-heading-text text-[clamp(2rem,2.4vw,2.7rem)] font-semibold">{title}</h1>
                </div>
              )}

              {children}
            </div>
          </section>
        </main>

        <section className="auth-footer-text flex items-end justify-between text-xs font-medium">
          <span>© {new Date().getFullYear()} xLyra. All rights reserved.</span>
        </section>
      </div>
    </div>
  )
}
