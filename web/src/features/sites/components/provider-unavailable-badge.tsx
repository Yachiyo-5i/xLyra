import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'

export function ProviderUnavailableBadge({ status }: { status?: string }) {
  const { t } = useTranslation('sites')
  if (status !== 'unavailable') return null
  return (
    <Badge variant="warning" className="shrink-0" title={t('table.providerUnavailableHint')}>
      {t('table.providerUnavailable')}
    </Badge>
  )
}
