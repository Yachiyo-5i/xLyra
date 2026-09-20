import type { ActivityBucket } from '@/features/traffic-flow/lib/activity-buckets'

type TrafficFlowSparklineProps = {
  buckets: ActivityBucket[]
  label: string
}

export function TrafficFlowSparkline({ buckets, label }: TrafficFlowSparklineProps) {
  const width = 1000
  const height = 42
  const max = Math.max(1, ...buckets.map((item) => item.count))
  const points = buckets.map((item, index) => {
    const x = buckets.length <= 1 ? 0 : index / (buckets.length - 1) * width
    const y = height - 4 - item.count / max * (height - 10)
    return `${x},${y}`
  }).join(' ')
  const area = `0,${height} ${points} ${width},${height}`

  return (
    <div className="traffic-flow-sparkline" aria-hidden="true">
      <span className="traffic-flow-sparkline-label">{label}</span>
      <svg viewBox={`0 0 ${width} ${height}`} preserveAspectRatio="none">
        <polygon points={area} fill="rgba(255, 255, 255, .12)" />
        <polyline points={points} fill="none" stroke="#e5e5e5" strokeWidth="1.6" />
      </svg>
    </div>
  )
}
