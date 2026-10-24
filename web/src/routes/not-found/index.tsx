import { useTranslation } from 'react-i18next'

export function NotFoundPage() {
  const { t } = useTranslation('common')

  return (
    <div className="flex min-h-[calc(100dvh-12rem)] items-center justify-center px-4 py-12 text-center">
      <div>
        {/* 展示字号 + 等宽数字；颜色用第三级文字，让标题成为视线焦点 */}
        <p className="font-mono text-[7rem] font-semibold leading-none tabular-nums text-label-3 sm:text-[8.5rem] md:text-[10rem]">
          404
        </p>
        <h1 className="mt-6 text-title-2 text-label">{t('notFound.title')}</h1>
        <p className="mx-auto mt-3 max-w-md text-body text-label-2">{t('notFound.description')}</p>
      </div>
    </div>
  )
}
