import { useTranslation } from 'react-i18next'
import { HoverDetails } from '@/components/common/hover-details'
import type { JSPluginMetrics24h } from '@/features/settings/api/js-plugins'
import { cn } from '@/lib/utils'
import { metricsHealthLevel, type MetricsHealthLevel } from './metrics-health'

const dotClassName: Record<MetricsHealthLevel, string> = {
  none: 'bg-label-3',
  ok: 'bg-success',
  warn: 'bg-warning',
  bad: 'bg-danger',
}

export function PluginMetrics24h({ metrics, className }: { metrics?: JSPluginMetrics24h; className?: string }) {
  const { t } = useTranslation(['settings'])
  const calls = metrics?.calls ?? 0
  const errors = metrics?.errors ?? 0
  const level = metricsHealthLevel(calls, errors)
  const status = t(`settings:jsPlugins.metricsHealth.${level}`)
  const summary = level === 'none'
    ? t('settings:jsPlugins.metricsEmpty')
    : t('settings:jsPlugins.metricsSummary', { success: calls - errors, failed: errors })
  const hint = t('settings:jsPlugins.metricsHint')

  return (
    <HoverDetails
      asChild
      title={t('settings:jsPlugins.metricsLabel')}
      description={summary}
      accessibleDescription={hint}
      content={hint}
    >
      <span className={cn('inline-flex items-center gap-1.5 whitespace-nowrap tabular-nums', className)}>
        <span
          role="img"
          aria-label={status}
          title={status}
          className={cn('inline-block h-2 w-2 shrink-0 rounded-full', dotClassName[level])}
        />
        {summary}
      </span>
    </HoverDetails>
  )
}
