import { Activity, Database, Network, Waypoints, type LucideIcon } from 'lucide-react'
import type { Ref } from 'react'
import { formatCompactTokens } from '@/features/traffic-flow/lib/traffic-flow-format'

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
        value={inFlight.toLocaleString()}
        zero={inFlight === 0}
        aside={rpmLimit == null ? undefined : `/ ${rpmLimit.toLocaleString()}`}
        asideTitle={rpmLimit == null ? undefined : `${t('metrics.rpm')} ${rpmLimit.toLocaleString()}`}
      />
      <Metric icon={Waypoints} label={t('metrics.routed')} value={routed.toLocaleString()} zero={routed === 0} />
      <Metric icon={Network} label={t('metrics.nodes')} value={nodes.toLocaleString()} zero={nodes === 0} />
      <Metric
        buttonRef={tokensButtonRef}
        icon={Database}
        label={t('metrics.tokens')}
        value={formatCompactTokens(tokens)}
        zero={tokens === 0}
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
  zero,
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
  /** 数值为 0：数字降为第三级文字，有数据后再变亮 */
  zero?: boolean
  onClick?: () => void
}) {
  const content = (
    <>
      <span className="traffic-flow-metric-icon"><Icon aria-hidden="true" /></span>
      <span className="traffic-flow-metric-copy">
        <span>{label}</span>
        <b className="traffic-flow-metric-value">
          <strong>{value}</strong>
          {aside ? <small title={asideTitle}>{aside}</small> : null}
        </b>
      </span>
    </>
  )
  if (interactive) {
    return (
      <button
        ref={buttonRef}
        type="button"
        className={`traffic-flow-metric traffic-flow-surface is-interactive${zero ? ' is-zero' : ''}`}
        aria-haspopup={ariaHasPopup}
        aria-expanded={expanded}
        onClick={onClick}
      >
        {content}
      </button>
    )
  }
  return <div className={`traffic-flow-metric traffic-flow-surface${zero ? ' is-zero' : ''}`}>{content}</div>
}
