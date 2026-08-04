import { useTranslation } from 'react-i18next'

export function NotFoundPage() {
  const { t } = useTranslation('common')

  return (
    <div className="flex min-h-[calc(100dvh-12rem)] items-center justify-center px-4 py-12 text-center">
      <div>
        <p className="text-[7rem] font-semibold leading-none tracking-tight text-foreground sm:text-[8.5rem] md:text-[10rem]">
          404
        </p>
        <h1 className="mt-6 text-xl font-semibold tracking-tight text-foreground md:text-2xl">
          {t('notFound.title')}
        </h1>
        <p className="text-muted-soft mx-auto mt-4 max-w-md text-base leading-7 md:text-lg">
          {t('notFound.description')}
        </p>
      </div>
    </div>
  )
}
