import { useTranslation } from 'react-i18next'
import type { RequestLogItem } from '@/features/requests/api/requests'
import { requestResponseModelDifference } from '@/features/requests/lib/request-utils'
import { cn } from '@/lib/utils'

export function RequestResponseModelBadge({ item, showMatching = false }: { item: RequestLogItem; showMatching?: boolean }) {
  const { t } = useTranslation('requests')
  const model = item.upstream_response_model?.trim()
  const difference = requestResponseModelDifference(item)
  if (!model || (!difference && !showMatching)) return null
  const title = difference
    ? t(difference === 'namespace' ? 'modelMapping.responseNamespace' : 'modelMapping.responseMismatch', { model })
    : `${t('detail.responseModel')}: ${model}`

  return (
    <span
      className={cn('inline-flex max-w-full rounded-md border px-1.5 py-0.5 text-xs font-medium',
        difference === 'namespace'
          ? 'border-[hsl(var(--badge-warning-border))] bg-[hsl(var(--badge-warning-bg))] text-[hsl(var(--badge-warning-text))]'
          : difference === 'model'
            ? 'border-[hsl(var(--destructive)/0.25)] bg-[hsl(var(--destructive)/0.12)] text-[hsl(var(--destructive))]'
            : 'border-[hsl(var(--badge-neutral-border))] bg-[hsl(var(--badge-neutral-bg))] text-[hsl(var(--badge-neutral-text))]')}
      title={title}
      aria-label={title}
    >
      <span className="truncate">{model}</span>
    </span>
  )
}
