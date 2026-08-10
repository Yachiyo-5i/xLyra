import { useTranslation } from 'react-i18next'
import type { RequestLogItem } from '@/features/requests/api/requests'
import {
  formatLatency,
  requestFirstByteLatency,
  requestFirstByteLatencyTone,
  requestTotalLatencyTone,
  type RequestLatencyTone,
} from '@/features/requests/lib/request-utils'
import { cn } from '@/lib/utils'

type RequestTimingProps = {
  item: RequestLogItem
  className?: string
}

export function RequestTiming({ item, className }: RequestTimingProps) {
  const { t } = useTranslation('requests')
  const firstByteLatencyValue = requestFirstByteLatency(item)
  const firstByteLatency = formatLatency(firstByteLatencyValue)
  const totalLatency = formatLatency(item.latency_ms)
  const firstByteTone = requestFirstByteLatencyTone(firstByteLatencyValue)
  const totalTone = requestTotalLatencyTone(item.latency_ms)

  return (
    <div className={cn('relative min-w-0 space-y-1 border-l-2 border-transparent pl-3', className)}>
      <span aria-hidden="true" className="absolute inset-y-0 left-0 flex w-1.5 flex-col overflow-hidden rounded-full">
        <span className={cn('flex-1', timingToneClasses[firstByteTone].bar)} />
        <span className={cn('flex-1', timingToneClasses[totalTone].bar)} />
      </span>
      <TimingLine label={t('table.headers.firstByte')} value={firstByteLatency} tone={firstByteTone} />
      <TimingLine label={t('table.headers.totalDuration')} value={totalLatency} tone={totalTone} />
    </div>
  )
}

const timingToneClasses: Record<RequestLatencyTone, { bar: string; value: string }> = {
  healthy: { bar: 'bg-emerald-500', value: 'text-emerald-500' },
  slow: { bar: 'bg-amber-500', value: 'text-amber-500' },
  'very-slow': { bar: 'bg-orange-500', value: 'text-orange-500' },
  critical: { bar: 'bg-red-500', value: 'text-red-500' },
  muted: { bar: 'bg-[hsl(var(--text-muted-soft))]', value: 'text-muted-soft' },
}

function TimingLine({
  label,
  value,
  tone,
}: {
  label: string
  value: string
  tone: RequestLatencyTone
}) {
  return (
    <div className="grid min-w-0 grid-cols-[minmax(0,1fr)_auto] items-center gap-x-1.5 text-[10px] leading-4">
      <span className="truncate text-muted-soft" title={label}>{label}</span>
      <span className={cn('shrink-0 whitespace-nowrap font-semibold tabular-nums', timingToneClasses[tone].value)}>
        {value}
      </span>
    </div>
  )
}
