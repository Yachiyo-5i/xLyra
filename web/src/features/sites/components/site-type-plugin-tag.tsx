import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'

export function SiteTypePluginTag({ pluginId }: { pluginId?: string }) {
  const { t } = useTranslation('sites')
  if (!pluginId) return null
  return <Badge variant="neutral">{t('form.pluginTag')}</Badge>
}
