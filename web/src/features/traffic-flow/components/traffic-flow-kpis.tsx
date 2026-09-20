import { Activity, Database, Network, Waypoints, type LucideIcon } from 'lucide-react'
import { formatCompactTokens, formatPaddedCount } from '@/features/traffic-flow/lib/traffic-flow-format'

type TrafficFlowKpisProps = {
  inFlight: number
  routed: number
  nodes: number
  tokens: number
  rpmLimit: number | null
  t: (key: string) => string
}

export function TrafficFlowKpis({ inFlight, routed, nodes, tokens, rpmLimit, t }: TrafficFlowKpisProps) {
  return (
    <section className="traffic-flow-kpis" aria-label={t('metrics.label')}>
      <Metric
        icon={Activity}
        label={t('metrics.inFlight')}
        value={formatPaddedCount(inFlight)}
        aside={rpmLimit == null ? undefined : `/ ${rpmLimit.toLocaleString()}`}
        asideTitle={rpmLimit == null ? undefined : `${t('metrics.rpm')} ${rpmLimit.toLocaleString()}`}
      />
      <Metric icon={Waypoints} label={t('metrics.routed')} value={formatPaddedCount(routed)} />
      <Metric icon={Network} label={t('metrics.nodes')} value={formatPaddedCount(nodes)} />
      <Metric icon={Database} label={t('metrics.tokens')} value={formatCompactTokens(tokens)} />
    </section>
  )
}

function Metric({
  icon: Icon,
  label,
  value,
  aside,
  asideTitle,
}: {
  icon: LucideIcon
  label: string
  value: string
  aside?: string
  asideTitle?: string
}) {
  return (
    <div className="traffic-flow-metric">
      <span>{label}</span>
      <b className="traffic-flow-metric-value">
        <strong>{value}</strong>
        {aside ? <small title={asideTitle}>{aside}</small> : null}
      </b>
      <Icon className="traffic-flow-metric-icon" aria-hidden="true" />
      <i />
    </div>
  )
}
