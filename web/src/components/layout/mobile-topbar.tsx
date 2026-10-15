import { lazy, Suspense, useState, type CSSProperties } from 'react'
import { Link } from 'react-router-dom'
import { Check, Globe, Search } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { menuButtonClass } from '@/components/ui/field-styles'
import { AppLogo } from '@/components/common/app-logo'
import { MorphToolbar } from '@/components/layout/morph-toolbar'
import { TopbarInlineTitle } from '@/components/layout/topbar-inline-title'
import { AccountMenuContent, CurrentAccountAvatar } from '@/components/layout/user-account-menu'

const CommandMenu = lazy(() =>
  import('@/components/common/command-menu').then((module) => ({ default: module.CommandMenu })),
)

/**
 * 移动端顶栏：不铺整条背景，只放浮动的圆形玻璃按钮。
 * 左上角：App 图标（回到概览）；右上角三个：搜索（命令菜单）、语言、头像。
 * 语言与头像点开后，按钮变形为菜单（仿 iOS 工具栏菜单，见 MorphToolbar）。
 * 内容从下方滚过时由外壳的 scroll edge effect 过渡。
 */
export function MobileTopbar() {
  const { t } = useTranslation(['common', 'components'])
  const [commandOpen, setCommandOpen] = useState(false)

  return (
    <>
      <header
        className="mobile-safe-top pointer-events-none absolute inset-x-0 top-0 z-30 px-4"
        style={{ '--mobile-safe-top-extra': '0.75rem' } as CSSProperties}
      >
        <div className="relative flex items-center justify-between gap-3">
          {/* 与右侧按钮同一样式：玻璃圆 + 透明底的 Logo */}
          <Button asChild variant="glass" size="icon" className="pointer-events-auto size-10 p-0">
            <Link to="/dashboard" aria-label={t('common:nav.dashboard')}>
              <AppLogo className="size-full rounded-full bg-transparent" decorative />
            </Link>
          </Button>

          {/* 大标题滚出后的行内标题：右侧有三个按钮，居中放不下，改为紧跟 Logo 左对齐 */}
          <TopbarInlineTitle className="left-[52px] max-w-[calc(100%-52px-148px)] translate-x-0" />

          <MorphToolbar
            className="pointer-events-auto"
            actions={[
              {
                key: 'search',
                label: t('common:actions.search'),
                icon: <Search />,
                onSelect: () => setCommandOpen(true),
              },
              {
                key: 'language',
                label: t('components:topbar.language.label'),
                icon: <Globe />,
                menu: { width: 192, render: (close) => <LanguageMenuContent onClose={close} /> },
              },
              {
                key: 'account',
                label: t('common:userMenu.profile'),
                icon: <CurrentAccountAvatar className="size-full rounded-full bg-transparent" />,
                buttonClassName: 'overflow-hidden p-0.5',
                menu: { width: 248, render: (close) => <AccountMenuContent onClose={close} /> },
              },
            ]}
          />
        </div>
      </header>

      {commandOpen ? (
        <Suspense fallback={null}>
          <CommandMenu open={commandOpen} onOpenChange={setCommandOpen} />
        </Suspense>
      ) : null}
    </>
  )
}

function LanguageMenuContent({ onClose }: { onClose: () => void }) {
  const { i18n } = useTranslation()
  const { t } = useTranslation('components')
  const current = normalizeLanguage(i18n.language)
  const languages: Array<{ value: SupportedLanguage; label: string }> = [
    { value: 'zh', label: t('topbar.language.zh') },
    { value: 'en', label: t('topbar.language.en') },
    { value: 'jp', label: t('topbar.language.jp') },
  ]

  function selectLanguage(value: SupportedLanguage) {
    if (value !== current) void i18n.changeLanguage(value)
    onClose()
  }

  return (
    <>
      {languages.map((language) => (
        <button
          key={language.value}
          type="button"
          role="menuitem"
          onClick={() => selectLanguage(language.value)}
          className={menuButtonClass}
        >
          <span className="flex size-4 shrink-0 items-center justify-center">
            {language.value === current ? <Check className="size-3.5" strokeWidth={2.5} /> : null}
          </span>
          <span className="min-w-0 flex-1">{language.label}</span>
        </button>
      ))}
    </>
  )
}

type SupportedLanguage = 'zh' | 'en' | 'jp'

function normalizeLanguage(language?: string): SupportedLanguage {
  if (language?.startsWith('zh')) return 'zh'
  if (language?.startsWith('jp') || language?.startsWith('ja')) return 'jp'
  return 'en'
}
