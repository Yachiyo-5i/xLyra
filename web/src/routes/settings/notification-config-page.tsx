import { useTranslation } from 'react-i18next'
import { PageHeader } from '@/components/common/page-header'

export function NotificationConfigPage() {
  const { t } = useTranslation('settings')

  return (
    <div className="space-y-7">
      <PageHeader
        eyebrow={t('notifications.eyebrow')}
        title={t('notifications.title')}
        description={t('notifications.description')}
      />
    </div>
  )
}
