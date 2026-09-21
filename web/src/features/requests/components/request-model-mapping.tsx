import { ArrowRightLeft } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { RequestLogItem } from '@/features/requests/api/requests'
import { RequestResponseModelBadge } from './request-response-model-badge'
import {
  requestMappedModel,
  requestModelName,
  requestReasoningEffort,
} from '@/features/requests/lib/request-utils'
import { cn } from '@/lib/utils'

type RequestModelMappingProps = {
  item: RequestLogItem
  className?: string
  secondaryClassName?: string
  inline?: boolean
}

export function RequestModelMapping(props: RequestModelMappingProps) {
  return (
    <span className={cn('flex min-w-0 gap-1', props.inline ? 'flex-wrap items-center gap-x-2' : 'flex-col items-start')}>
      <RequestRouteModelMapping {...props} />
      <RequestResponseModelBadge item={props.item} />
    </span>
  )
}

function RequestRouteModelMapping({
  item,
  className,
  secondaryClassName,
  inline,
}: RequestModelMappingProps) {
  const { t } = useTranslation('requests')
  const requestedModel = requestModelName(item)
  const upstreamModel = requestMappedModel(item)
  const isSoftFallback = item.mapping_mode === 'soft'
  const reasoningEffort = requestReasoningEffort(item)
  const modelLabel = (
    <span className="inline-flex min-w-0 max-w-full items-baseline gap-1.5">
      <span className="min-w-0 truncate" title={requestedModel}>{requestedModel}</span>
      {reasoningEffort ? (
        <span
          className="shrink-0 text-[10px] font-normal text-muted-soft"
          title={`${t('table.headers.reasoning')}: ${reasoningEffort}`}
          aria-label={`${t('table.headers.reasoning')}: ${reasoningEffort}`}
        >
          {reasoningEffort}
        </span>
      ) : null}
    </span>
  )

  if (!upstreamModel) {
    return (
      <span className={cn('inline-flex min-w-0 max-w-full', className)}>
        {modelLabel}
      </span>
    )
  }

  return (
    <span className={cn('group/model-map relative inline-flex min-w-0', inline ? 'max-w-full flex-row items-center gap-1.5 overflow-x-auto' : 'w-full flex-col', className)}>
      <span className={cn('inline-flex min-w-0 items-center gap-1.5', inline && 'shrink-0')}>
        {modelLabel}
        <ArrowRightLeft className="h-3.5 w-3.5 shrink-0 text-[hsl(var(--accent))]" />
        {isSoftFallback ? (
          <span className="shrink-0 rounded border border-amber-500/40 bg-amber-500/10 px-1 text-[10px] leading-4 text-amber-500">
            {t('modelMapping.softFallback')}
          </span>
        ) : null}
      </span>
      <span className={cn('text-muted-soft min-w-0 text-xs', inline ? 'shrink-0 whitespace-nowrap' : 'truncate', secondaryClassName)}>
        {upstreamModel}
      </span>
      <span className="pointer-events-none absolute left-0 top-full z-50 mt-2 hidden min-w-64 max-w-96 rounded-md border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-panel))] px-3 py-2 text-xs shadow-lg group-hover/model-map:block">
        <span className="grid grid-cols-[64px_minmax(0,1fr)] gap-x-3 gap-y-1 text-left">
          <span className="text-[hsl(var(--text-muted-soft))]">{t('modelMapping.downstreamRequest')}</span>
          <span className="min-w-0 break-all text-foreground">{requestedModel}</span>
          <span className="text-[hsl(var(--text-muted-soft))]">{t('modelMapping.actualUpstream')}</span>
          <span className="min-w-0 break-all text-foreground">{upstreamModel}</span>
          {isSoftFallback ? (
            <>
              <span className="text-[hsl(var(--text-muted-soft))]">{t('modelMapping.mode')}</span>
              <span className="min-w-0 break-all text-foreground">{t('modelMapping.softFallbackDesc')}</span>
            </>
          ) : null}
        </span>
      </span>
    </span>
  )
}
