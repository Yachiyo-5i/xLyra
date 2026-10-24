import { useMemo, type CSSProperties } from 'react'
import type { TrafficFlowNode, TrafficFlowRequest, TrafficFlowUsageTotal } from '@/features/traffic-flow/api/traffic-flow'
import { formatCompactTokens, formatDuration, formatLatency, formatPercent } from '@/features/traffic-flow/lib/traffic-flow-format'
import { modelVisual } from '@/features/traffic-flow/lib/model-visual'
import { primaryRequestForNode, type TrafficFlowNodeRef, type WingPlacement } from '@/features/traffic-flow/lib/wing-layout'

type FlowNodeCardProps = {
  placement: WingPlacement
  requests: TrafficFlowRequest[]
  retiringRequestIDs: Set<string>
  usage?: TrafficFlowUsageTotal
  selected: boolean
  expanded: boolean
  hovered: boolean
  related: boolean
  now: number
  reducedMotion: boolean
  onHover: (node: TrafficFlowNodeRef | null) => void
  onSelect: (node: TrafficFlowNodeRef) => void
  t: (key: string) => string
}

export function FlowNodeCard({
  placement,
  requests,
  retiringRequestIDs,
  usage,
  selected,
  expanded,
  hovered,
  related,
  now,
  reducedMotion,
  onHover,
  onSelect,
  t,
}: FlowNodeCardProps) {
  const request = useMemo(
    () => primaryRequestForNode(placement.kind, placement.id, requests, retiringRequestIDs),
    [placement.id, placement.kind, requests, retiringRequestIDs],
  )
  const visual = request ? modelVisual(request.model_provider, request.model_key) : undefined
  const color = visual?.color ?? '#d8d8d8'
  const node = { kind: placement.kind, id: placement.id }
  const scale = reducedMotion ? 1 : placement.scale
  const className = [
    'traffic-flow-node',
    placement.kind === 'downstream' ? 'is-downstream' : 'is-upstream',
    placement.lit ? 'is-hot' : 'is-idle',
    selected ? 'is-selected' : '',
    expanded ? 'is-expanded' : '',
    hovered ? 'is-hovered' : '',
    related ? 'is-related' : '',
  ].filter(Boolean).join(' ')

  return (
    <div
      className={className}
      style={{
        left: `${placement.x}%`,
        top: `${placement.y}%`,
        zIndex: placement.zIndex + (expanded ? 24 : 0),
        opacity: placement.opacity,
        transform: `translate(-50%, -50%)${scale === 1 ? '' : ` scale(${scale})`}`,
        '--node-color': color,
      } as CSSProperties}
      onMouseEnter={() => onHover(node)}
      onMouseLeave={() => onHover(null)}
    >
      <button
        type="button"
        className="traffic-flow-node-star"
        aria-label={`${placement.name} ${request?.model_key ?? nodeCaption(placement.node, placement.kind, t)}`}
        onClick={(event) => {
          event.stopPropagation()
          onSelect(node)
        }}
      >
        <span className="traffic-flow-star-dot" aria-hidden="true" />
        <span className="traffic-flow-star-label">
          <strong>{placement.name}</strong>
          <i>{request?.model_key ?? nodeCaption(placement.node, placement.kind, t)}</i>
        </span>
      </button>
      {placement.lit && placement.inflight > 0 ? (
        <span className="traffic-flow-star-count">{placement.inflight}</span>
      ) : null}
      {expanded ? (
        <div className="traffic-flow-star-popover traffic-flow-surface" onClick={(event) => event.stopPropagation()}>
          <span><span>{t('inspector.inflightCount')}</span><strong>{placement.inflight}</strong></span>
          <span><span>{t('inspector.tokens')}</span><strong>{formatCompactTokens(usage?.total_tokens ?? 0)}</strong></span>
          {placement.node.recent_avg_latency_ms != null ? (
            <span><span>{t('inspector.avgLatency')}</span><strong>{formatLatency(placement.node.recent_avg_latency_ms)}</strong></span>
          ) : null}
          {placement.node.recent_success_rate != null ? (
            <span><span>{t('inspector.successRate')}</span><strong>{formatPercent(placement.node.recent_success_rate)}</strong></span>
          ) : null}
          {request ? (
            <>
              <span><span>{t('inspector.phase')}</span><strong>{t(`phase.${request.phase}`)}</strong></span>
              <span><span>{t('inspector.duration')}</span><strong>{formatDuration(now - Date.parse(request.started_at))}</strong></span>
            </>
          ) : null}
        </div>
      ) : null}
    </div>
  )
}

function nodeCaption(node: TrafficFlowNode, kind: 'downstream' | 'upstream', t: (key: string) => string) {
  if (kind === 'upstream' && node.recent_avg_latency_ms != null) return formatLatency(node.recent_avg_latency_ms)
  if (kind === 'upstream' && node.recent_success_rate != null) return formatPercent(node.recent_success_rate)
  return t(`lanes.${kind}`)
}
