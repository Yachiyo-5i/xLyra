import { CircleHelp, Radio } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { TrafficFlowRequest, TrafficFlowTopology, TrafficFlowUsageTotal } from '@/features/traffic-flow/api/traffic-flow'
import type { ActivityBucket } from '@/features/traffic-flow/lib/activity-buckets'
import { useReducedMotion } from '@/features/traffic-flow/lib/use-reduced-motion'
import {
  clusterSeat,
  layoutWings,
  nodeKey,
  primaryRequestForNode,
  relatedNodeIDs,
  requestEndpointIDs,
  requestTouchesEndpoints,
  sameNode,
  wingCapacity,
  type TrafficFlowNodeRef,
} from '@/features/traffic-flow/lib/wing-layout'
import { FlowCanvas, type LaneState, type LaneView } from './flow-canvas'
import { LyraHub } from './lyra-hub'
import { FlowNodeCard } from './flow-node-card'
import { TrafficFlowSparkline } from './traffic-flow-sparkline'
import { modelVisual } from '@/features/traffic-flow/lib/model-visual'
import { useFlowMotion } from '@/features/traffic-flow/lib/use-flow-motion'

type TrafficFlowStageProps = {
  topology: TrafficFlowTopology | null
  requests: TrafficFlowRequest[]
  retiringRequestIDs: Set<string>
  selectedRequest?: TrafficFlowRequest
  selectedNode: TrafficFlowNodeRef | null
  hoveredNode: TrafficFlowNodeRef | null
  paused: boolean
  pulseKey: number
  gatewayColor: string
  activeCount: number
  downstreamUsage: Record<string, TrafficFlowUsageTotal>
  upstreamUsage: Record<string, TrafficFlowUsageTotal>
  activityBuckets: ActivityBucket[]
  onHoverNode: (node: TrafficFlowNodeRef | null) => void
  onSelectNode: (node: TrafficFlowNodeRef) => void
  onClearSelection: () => void
  onRequestDrained: (requestID: string) => void
}

export function TrafficFlowStage({
  topology,
  requests,
  retiringRequestIDs,
  selectedRequest,
  selectedNode,
  hoveredNode,
  paused,
  pulseKey,
  gatewayColor,
  activeCount,
  downstreamUsage,
  upstreamUsage,
  activityBuckets,
  onHoverNode,
  onSelectNode,
  onClearSelection,
  onRequestDrained,
}: TrafficFlowStageProps) {
  const { t } = useTranslation('traffic-flow')
  const reducedMotion = useReducedMotion()
  const motionPaused = paused || reducedMotion
  const stageRef = useRef<HTMLElement>(null)
  const seatsRef = useRef(new Map<string, { x: number; y: number; kind: 'downstream' | 'upstream' }>())
  const [size, setSize] = useState({ width: 1, height: 640 })
  const sizeRef = useRef(size)
  const [expanded, setExpanded] = useState({ downstream: false, upstream: false })
  const [now, setNow] = useState(() => Date.now())
  const nodeFocus = hoveredNode ?? selectedNode
  const stageFocused = Boolean(nodeFocus || selectedRequest)
  const selectedKeys = useMemo(() => {
    const keys = new Set<string>()
    if (!selectedRequest) return keys
    keys.add(nodeKey('downstream', selectedRequest.api_key_id))
    if (selectedRequest.upstream_site_id) keys.add(nodeKey('upstream', selectedRequest.upstream_site_id))
    return keys
  }, [selectedRequest])
  const motion = useFlowMotion({
    requests,
    retiringRequestIDs,
    seatsRef,
    paused,
    reducedMotion,
    onDrained: onRequestDrained,
  })

  useEffect(() => {
    sizeRef.current = size
  }, [size])

  useEffect(() => {
    const element = stageRef.current
    if (!element) return
    const observer = new ResizeObserver((entries) => {
      const rect = entries[0]?.contentRect
      if (!rect) return
      setSize({ width: Math.max(1, rect.width), height: Math.max(1, rect.height) })
    })
    observer.observe(element)
    return () => observer.disconnect()
  }, [])

  useEffect(() => {
    if (reducedMotion) return
    const timer = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(timer)
  }, [reducedMotion])

  const capacity = wingCapacity(size.height)
  const placements = useMemo(
    () => layoutWings(topology?.downstream ?? [], topology?.upstream ?? [], requests, retiringRequestIDs, {
      capacity,
      expanded,
      hovered: hoveredNode,
      selected: selectedNode,
      selectedKeys,
      litKeys: motion.litKeys,
      reducedMotion,
    }),
    [capacity, expanded, hoveredNode, motion.litKeys, reducedMotion, requests, retiringRequestIDs, selectedKeys, selectedNode, topology],
  )
  const related = useMemo(() => {
    if (nodeFocus) return relatedNodeIDs(nodeFocus.kind, nodeFocus.id, requests, retiringRequestIDs, topology)
    return requestEndpointIDs(selectedRequest)
  }, [nodeFocus, requests, retiringRequestIDs, selectedRequest, topology])
  const expandKinds = useMemo(() => {
    const expand: Array<'downstream' | 'upstream'> = []
    if (!topology) return expand
    if (selectedRequest) {
      if (placements.some((item) => item.kind === 'downstream' && item.clustered && item.id === selectedRequest.api_key_id)) {
        expand.push('downstream')
      }
      if (selectedRequest.upstream_site_id && placements.some((item) => item.kind === 'upstream' && item.clustered && item.id === selectedRequest.upstream_site_id)) {
        expand.push('upstream')
      }
    } else if (selectedNode) {
      const peers = relatedNodeIDs(selectedNode.kind, selectedNode.id, requests, retiringRequestIDs, topology)
      const opposite = selectedNode.kind === 'downstream' ? 'upstream' : 'downstream'
      if (placements.some((item) => item.kind === opposite && item.clustered && peers.has(item.id))) expand.push(opposite)
    }
    return expand
  }, [placements, requests, retiringRequestIDs, selectedNode, selectedRequest, topology])
  if (expandKinds.some((kind) => !expanded[kind])) {
    setExpanded((current) => {
      let next = current
      for (const kind of expandKinds) {
        if (!next[kind]) next = { ...next, [kind]: true }
      }
      return next
    })
  }
  const visible = useMemo(() => placements.filter((item) => !item.clustered), [placements])
  const clustered = {
    downstream: placements.filter((item) => item.kind === 'downstream' && item.clustered),
    upstream: placements.filter((item) => item.kind === 'upstream' && item.clustered),
  }
  const seats = useMemo(() => new Map(visible.map((item) => [nodeKey(item.kind, item.id), item])), [visible])
  useEffect(() => {
    seatsRef.current = seats
  }, [seats])

  const lanes = useMemo<LaneView[]>(() => visible.map((placement) => {
    const request = primaryRequestForNode(placement.kind, placement.id, requests, retiringRequestIDs)
    return {
      key: nodeKey(placement.kind, placement.id),
      kind: placement.kind,
      id: placement.id,
      x: placement.x,
      y: placement.y,
      state: laneState(placement, nodeFocus, selectedRequest, related, stageFocused),
      color: request ? modelVisual(request.model_provider, request.model_key).color : '#e7ebf3',
    }
  }), [nodeFocus, related, requests, retiringRequestIDs, selectedRequest, stageFocused, visible])

  return (
    <section ref={stageRef} className={`traffic-flow-stage traffic-flow-surface${stageFocused ? ' is-focused' : ''}`} aria-label={t('map.label')} onClick={onClearSelection}>
      <div className="traffic-flow-lane-panel traffic-flow-lane-panel-downstream" aria-hidden="true">
        <div className="traffic-flow-lane-panel-heading">
          <i />
          <div>
            <strong>{t('lanes.downstream')}</strong>
            <span>{t('lanes.downstreamCaption')}</span>
          </div>
        </div>
      </div>
      <div className="traffic-flow-lane-panel traffic-flow-lane-panel-upstream" aria-hidden="true">
        <div className="traffic-flow-lane-panel-heading">
          <i />
          <div>
            <strong>{t('lanes.upstream')}</strong>
            <span>{t('lanes.upstreamCaption')}</span>
          </div>
        </div>
      </div>
      <div className="traffic-flow-sector traffic-flow-sector-center">{t('lanes.gateway')}</div>
      <FlowCanvas
        lanes={lanes}
        pulsesRef={motion.pulsesRef}
        selectedRequestId={selectedRequest?.request_id}
        requests={requests}
        paused={motionPaused}
        reducedMotion={reducedMotion}
      />
      <LyraHub
        active={activeCount > 0 || motion.active}
        load={Math.min(1, (activeCount || (motion.active ? 1 : 0)) / 8)}
        color={gatewayColor}
        pulseKey={pulseKey}
        paused={motionPaused}
      />
      {visible.map((placement) => (
        <FlowNodeCard
          key={`${placement.kind}:${placement.id}`}
          placement={placement}
          requests={requests}
          retiringRequestIDs={retiringRequestIDs}
          usage={(placement.kind === 'downstream' ? downstreamUsage : upstreamUsage)[placement.id]}
          selected={sameNode(selectedNode, { kind: placement.kind, id: placement.id })
            || requestTouchesEndpoints(placement.kind, placement.id, selectedRequest)}
          expanded={sameNode(selectedNode, { kind: placement.kind, id: placement.id })}
          hovered={sameNode(hoveredNode, { kind: placement.kind, id: placement.id })}
          related={Boolean(nodeFocus && related.has(placement.id) && placement.kind !== nodeFocus.kind
            && !requestTouchesEndpoints(placement.kind, placement.id, selectedRequest))}
          now={now}
          reducedMotion={reducedMotion}
          onHover={onHoverNode}
          onSelect={onSelectNode}
          t={t}
        />
      ))}
      {(['downstream', 'upstream'] as const).map((kind) => {
        const hidden = clustered[kind].length
        const total = (kind === 'downstream' ? topology?.downstream : topology?.upstream)?.length ?? 0
        if (hidden === 0 && !(expanded[kind] && total > capacity)) return null
        const seat = clusterSeat(kind)
        return (
          <button
            key={kind}
            type="button"
            className="traffic-flow-cluster"
            style={{ left: `${seat.x}%`, top: `${seat.y}%` }}
            onClick={(event) => {
              event.stopPropagation()
              setExpanded((current) => ({ ...current, [kind]: !current[kind] }))
            }}
          >
            {expanded[kind] ? t('status.collapseOverflow') : t('status.overflow', { count: hidden })}
          </button>
        )
      })}
      <TrafficFlowSparkline buckets={activityBuckets} label={t('status.sessionActivity')} />
      {!topology ? <div className="traffic-flow-empty"><Radio className="size-5" />{t('status.loading')}</div> : null}
      {topology && activeCount === 0 && !motion.active ? <div className="traffic-flow-idle"><CircleHelp className="size-4" />{t('status.waiting')}</div> : null}
    </section>
  )
}

function laneState(
  placement: { kind: 'downstream' | 'upstream'; id: string; lit: boolean },
  nodeFocus: TrafficFlowNodeRef | null,
  selectedRequest: TrafficFlowRequest | undefined,
  related: Set<string>,
  stageFocused: boolean,
): LaneState {
  const focused = sameNode(nodeFocus, { kind: placement.kind, id: placement.id })
    || requestTouchesEndpoints(placement.kind, placement.id, selectedRequest)
  if (focused) return 'hot'
  const peer = related.has(placement.id)
  if (peer) return 'hot'
  if (stageFocused) return 'dim'
  return placement.lit ? 'active' : 'idle'
}
