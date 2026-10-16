import { useTranslation } from 'react-i18next'
import type { RequestLogItem } from '@/features/requests/api/requests'
import {
  formatLatency,
  requestFirstByteLatency,
  requestFirstByteLatencyTone,
  requestTotalLatencyTone,
  type RequestLatencyTone,
} from '@/features/requests/lib/request-utils'
import { timingToneClasses } from '@/features/requests/lib/request-timing-tones'
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
    <div className={cn('min-w-0 space-y-0 lg:space-y-1', className)}>
      <TimingLine label={t('table.headers.firstByte')} value={firstByteLatency} tone={firstByteTone} />
      <TimingLine label={t('table.headers.totalDuration')} value={totalLatency} tone={totalTone} />
    </div>
  )
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
    <div className="grid min-w-0 grid-cols-[0.375rem_minmax(0,1fr)_auto] items-center gap-x-1 text-footnote leading-4 lg:gap-x-1.5 lg:leading-5">
      <span aria-hidden="true" className={cn('h-1.5 w-1.5 shrink-0 rounded-full', timingToneClasses[tone].dot)} />
      <span className="truncate text-label-2" title={label}>{label}</span>
      <span className={cn('shrink-0 whitespace-nowrap font-medium tabular-nums lg:text-body lg:font-semibold', timingToneClasses[tone].value)}>
        {value}
      </span>
    </div>
  )
}
