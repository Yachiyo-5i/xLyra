import { useEffect, useState, type CSSProperties } from 'react'
import { Activity, ShieldCheck } from 'lucide-react'
import type { TrafficFlowRequest } from '@/features/traffic-flow/api/traffic-flow'
import { formatDuration } from '@/features/traffic-flow/lib/traffic-flow-format'
import { modelVisual } from '@/features/traffic-flow/lib/model-visual'
import { requestTouchesNode, type TrafficFlowNodeRef } from '@/features/traffic-flow/lib/wing-layout'

type TrafficFlowFeedProps = {
  requests: TrafficFlowRequest[]
  selectedRequest?: TrafficFlowRequest
  selectedNode: TrafficFlowNodeRef | null
  nodeName?: string
  connected: boolean
  onSelectRequest: (requestID: string) => void
  t: (key: string, options?: Record<string, string | number>) => string
}

export function TrafficFlowFeed({
  requests,
  selectedRequest,
  selectedNode,
  nodeName,
  connected,
  onSelectRequest,
  t,
}: TrafficFlowFeedProps) {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(timer)
  }, [])
  const relatedCount = selectedNode
    ? requests.filter((request) => requestTouchesNode(request, selectedNode.kind, selectedNode.id, false)).length
    : 0

  return (
    <section className="traffic-flow-feed" aria-label={t('activity.label')}>
      <div className="traffic-flow-feed-heading">
        <Activity className="size-4" />
        <div>
          <span>{t('activity.label')}</span>
          {selectedNode && nodeName ? <em>{t('activity.nodeFilter', { name: nodeName, related: relatedCount })}</em> : null}
        </div>
        <strong>{requests.length.toString().padStart(2, '0')}</strong>
      </div>
      <div className="traffic-flow-feed-list">
        {requests.map((request) => {
          const related = selectedNode ? requestTouchesNode(request, selectedNode.kind, selectedNode.id, false) : false
          const selected = selectedRequest?.request_id === request.request_id
          return (
            <FlowActivity
              key={request.request_id}
              request={request}
              selected={selected}
              related={related}
              dimmed={selectedRequest ? !selected : Boolean(selectedNode) && !related}
              now={now}
              onSelect={() => onSelectRequest(request.request_id)}
              t={t}
            />
          )
        })}
        {requests.length === 0 ? <span className="traffic-flow-activity-empty">{t('activity.empty')}</span> : null}
      </div>
      <div className="traffic-flow-activity-state">
        <ShieldCheck className="size-4" />
        <span>{connected ? t('footer.synced') : t('status.reconnecting')}</span>
      </div>
    </section>
  )
}

function FlowActivity({
  request,
  selected,
  related,
  dimmed,
  now,
  onSelect,
  t,
}: {
  request: TrafficFlowRequest
  selected: boolean
  related: boolean
  dimmed: boolean
  now: number
  onSelect: () => void
  t: (key: string, options?: Record<string, string | number>) => string
}) {
  const visual = modelVisual(request.model_provider, request.model_key)
  const elapsed = formatDuration(now - Date.parse(request.started_at))
  const className = [
    'traffic-flow-activity-item',
    selected ? 'is-selected' : '',
    related ? 'is-related' : '',
    dimmed ? 'is-dim' : '',
  ].filter(Boolean).join(' ')
  return (
    <button type="button" className={className} onClick={onSelect} style={{ '--model-color': visual.color } as CSSProperties}>
      <span className="traffic-flow-activity-model">{visual.iconPath ? <img src={visual.iconPath} alt="" /> : visual.fallback}</span>
      <span className="traffic-flow-activity-copy">
        <strong>{request.model_key || t('inspector.unknownModel')}</strong>
        <i>{request.api_key_name} <b>→</b> {request.upstream_site_name || t('inspector.waiting')}</i>
        <span className="traffic-flow-activity-meta">
          <em>{elapsed}</em>
          {request.stream ? <em>{t('activity.stream')}</em> : null}
          {request.attempt > 1 ? <em>{t('inspector.attempt')} {request.attempt}</em> : null}
          {request.upstream_site_type ? <em>{request.upstream_site_type}</em> : null}
        </span>
      </span>
      <span className="traffic-flow-activity-phase">{t(`phase.${request.phase}`)}</span>
    </button>
  )
}
