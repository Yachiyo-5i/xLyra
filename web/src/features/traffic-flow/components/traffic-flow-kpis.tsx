import { Activity, Database, Network, Waypoints, type LucideIcon } from 'lucide-react'
import type { Ref } from 'react'
import { formatCompactTokens, formatPaddedCount } from '@/features/traffic-flow/lib/traffic-flow-format'

type TrafficFlowKpisProps = {
  inFlight: number
  routed: number
  nodes: number
  tokens: number
  rpmLimit: number | null
  tokensOpen?: boolean
  kpisRef?: Ref<HTMLElement>
  tokensButtonRef?: Ref<HTMLButtonElement>
  t: (key: string) => string
  onTokensClick?: () => void
}

export function TrafficFlowKpis({ inFlight, routed, nodes, tokens, rpmLimit, tokensOpen, kpisRef, tokensButtonRef, t, onTokensClick }: TrafficFlowKpisProps) {
  return (
    <section ref={kpisRef} className="traffic-flow-kpis" aria-label={t('metrics.label')}>
      <Metric
        icon={Activity}
        label={t('metrics.inFlight')}
        value={formatPaddedCount(inFlight)}
        aside={rpmLimit == null ? undefined : `/ ${rpmLimit.toLocaleString()}`}
        asideTitle={rpmLimit == null ? undefined : `${t('metrics.rpm')} ${rpmLimit.toLocaleString()}`}
      />
      <Metric icon={Waypoints} label={t('metrics.routed')} value={formatPaddedCount(routed)} />
      <Metric icon={Network} label={t('metrics.nodes')} value={formatPaddedCount(nodes)} />
      <Metric
        buttonRef={tokensButtonRef}
        icon={Database}
        label={t('metrics.tokens')}
        value={formatCompactTokens(tokens)}
        interactive
        expanded={tokensOpen}
        ariaHasPopup="dialog"
        onClick={onTokensClick}
      />
    </section>
  )
}

function Metric({
  buttonRef,
  icon: Icon,
  label,
  value,
  aside,
  asideTitle,
  interactive,
  expanded,
  ariaHasPopup,
  onClick,
}: {
  buttonRef?: Ref<HTMLButtonElement>
  icon: LucideIcon
  label: string
  value: string
  aside?: string
  asideTitle?: string
  interactive?: boolean
  expanded?: boolean
  ariaHasPopup?: 'dialog'
  onClick?: () => void
}) {
  const content = (
    <>
      <span>{label}</span>
      <b className="traffic-flow-metric-value">
        <strong>{value}</strong>
        {aside ? <small title={asideTitle}>{aside}</small> : null}
      </b>
      <Icon className="traffic-flow-metric-icon" aria-hidden="true" />
      <i />
    </>
  )
  if (interactive) {
    return (
      <button
        ref={buttonRef}
        type="button"
        className="traffic-flow-metric is-interactive"
        aria-haspopup={ariaHasPopup}
        aria-expanded={expanded}
        onClick={onClick}
      >
        {content}
      </button>
    )
  }
  return <div className="traffic-flow-metric">{content}</div>
}
