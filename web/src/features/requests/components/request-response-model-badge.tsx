import { useTranslation } from 'react-i18next'
import type { RequestLogItem } from '@/features/requests/api/requests'
import { requestResponseModelDifference } from '@/features/requests/lib/request-utils'
import { Badge } from '@/components/ui/badge'

export function RequestResponseModelBadge({ item, showMatching = false }: { item: RequestLogItem; showMatching?: boolean }) {
  const { t } = useTranslation('requests')
  const model = item.upstream_response_model?.trim()
  const difference = requestResponseModelDifference(item)
  if (!model || (!difference && !showMatching)) return null
  const title = difference
    ? t(difference === 'namespace' ? 'modelMapping.responseNamespace' : 'modelMapping.responseMismatch', { model })
    : `${t('detail.responseModel')}: ${model}`

  return (
    <Badge
      variant={difference === 'namespace' ? 'warning' : difference === 'model' ? 'danger' : 'neutral'}
      className="max-w-full"
      title={title}
      aria-label={title}
    >
      <span className="truncate">{model}</span>
    </Badge>
  )
}
