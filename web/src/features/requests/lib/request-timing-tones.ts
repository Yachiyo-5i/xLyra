import type { RequestLogItem } from '@/features/requests/api/requests'
import {
  formatLatency,
  requestFirstByteLatency,
  requestFirstByteLatencyTone,
  requestTotalLatencyTone,
  type RequestLatencyTone,
} from '@/features/requests/lib/request-utils'

/** 用时的速度色：表格里的圆点与数值、移动端卡片的数值共用 */
export const timingToneClasses: Record<RequestLatencyTone, { dot: string; value: string }> = {
  healthy: { dot: 'bg-emerald-500', value: 'text-success' },
  slow: { dot: 'bg-amber-500', value: 'text-warning' },
  'very-slow': { dot: 'bg-orange-500', value: 'text-orange-500' },
  critical: { dot: 'bg-red-500', value: 'text-danger' },
  muted: { dot: 'bg-label-3', value: 'text-label-2' },
}

/** 移动端卡片用：首字 / 总时长两项，值带与表格一致的速度色 */
export function requestTimingFields(item: RequestLogItem, t: (key: string) => string) {
  const firstByteValue = requestFirstByteLatency(item)
  return [
    {
      label: t('table.headers.firstByte'),
      value: formatLatency(firstByteValue),
      className: timingToneClasses[requestFirstByteLatencyTone(firstByteValue)].value,
    },
    {
      label: t('table.headers.totalDuration'),
      value: formatLatency(item.latency_ms),
      className: timingToneClasses[requestTotalLatencyTone(item.latency_ms)].value,
    },
  ]
}
