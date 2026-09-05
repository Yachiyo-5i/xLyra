import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { RequestLogItem } from '@/features/requests/api/requests'
import {
  formatLatency,
  requestFirstByteLatency,
  requestFirstByteLatencyTone,
  requestElapsedMs,
  requestIsInProgress,
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
  const inProgress = requestIsInProgress(item)
  const [now, setNow] = useState(() => Date.now())
  const firstByteLatencyValue = requestFirstByteLatency(item)
  const firstByteLatency = formatLatency(firstByteLatencyValue)
  const totalLatencyValue = inProgress ? requestElapsedMs(item, now) : item.latency_ms
  const totalLatency = formatLatency(totalLatencyValue)
  const firstByteTone = requestFirstByteLatencyTone(firstByteLatencyValue)
  const totalTone = requestTotalLatencyTone(totalLatencyValue)

  useEffect(() => {
    if (!inProgress) return
    const timer = window.setInterval(() => setNow(Date.now()), 1_000)
    return () => window.clearInterval(timer)
  }, [inProgress, item.started_at])

  return (
    <div className={cn('min-w-0 space-y-0 lg:space-y-1', className)}>
      <TimingLine label={t('table.headers.firstByte')} value={firstByteLatency} tone={firstByteTone} />
      <TimingLine label={t('table.headers.totalDuration')} value={totalLatency} tone={totalTone} />
    </div>
  )
}

const timingToneClasses: Record<RequestLatencyTone, { dot: string; value: string }> = {
  healthy: { dot: 'bg-emerald-500', value: 'text-emerald-500' },
  slow: { dot: 'bg-amber-500', value: 'text-amber-500' },
  'very-slow': { dot: 'bg-orange-500', value: 'text-orange-500' },
  critical: { dot: 'bg-red-500', value: 'text-red-500' },
  muted: { dot: 'bg-[hsl(var(--text-muted-soft))]', value: 'text-muted-soft' },
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
    <div className="grid min-w-0 grid-cols-[0.375rem_minmax(0,1fr)_auto] items-center gap-x-1 text-xs leading-4 lg:gap-x-1.5 lg:leading-5">
      <span aria-hidden="true" className={cn('h-1.5 w-1.5 shrink-0 rounded-full', timingToneClasses[tone].dot)} />
      <span className="truncate text-muted-soft" title={label}>{label}</span>
      <span className={cn('shrink-0 whitespace-nowrap font-medium tabular-nums lg:text-sm lg:font-semibold', timingToneClasses[tone].value)}>
        {value}
      </span>
    </div>
  )
}
