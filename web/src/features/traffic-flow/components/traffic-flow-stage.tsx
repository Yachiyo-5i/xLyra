import { Canvas } from '@react-three/fiber'
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
  relatedNodeIDs,
  requestEndpointIDs,
  requestTouchesEndpoints,
  sameNode,
  wingCapacity,
  type TrafficFlowNodeRef,
} from '@/features/traffic-flow/lib/wing-layout'
import { EnergyScene, GatewayCoreOverlay } from './gateway-core/GatewayCore'
import { FlowNodeCard } from './flow-node-card'
import { TrafficFlowSparkline } from './traffic-flow-sparkline'
import { linkPath } from '@/features/traffic-flow/lib/flow-packets'
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
  sizeRef.current = size
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
    sizeRef,
    paused,
    reducedMotion,
    onDrained: onRequestDrained,
  })

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
  useEffect(() => {
    if (!topology) return
    const expand: Array<'downstream' | 'upstream'> = []
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
    if (expand.length === 0) return
    setExpanded((current) => {
      let next = current
      for (const kind of expand) {
        if (!next[kind]) next = { ...next, [kind]: true }
      }
      return next
    })
  }, [placements, requests, retiringRequestIDs, selectedNode, selectedRequest, topology])
  const visible = placements.filter((item) => !item.clustered)
  const clustered = {
    downstream: placements.filter((item) => item.kind === 'downstream' && item.clustered),
    upstream: placements.filter((item) => item.kind === 'upstream' && item.clustered),
  }
  const seats = useMemo(() => new Map(visible.map((item) => [nodeKey(item.kind, item.id), item])), [visible])
  seatsRef.current = seats

  return (
    <section ref={stageRef} className={`traffic-flow-stage${stageFocused ? ' is-focused' : ''}`} aria-label={t('map.label')} onClick={onClearSelection}>
      <div className="traffic-flow-map-corner traffic-flow-map-corner-tl" />
      <div className="traffic-flow-map-corner traffic-flow-map-corner-tr" />
      <div className="traffic-flow-map-corner traffic-flow-map-corner-bl" />
      <div className="traffic-flow-map-corner traffic-flow-map-corner-br" />
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
      <div className="traffic-flow-sector traffic-flow-sector-center"><span>02</span>{t('lanes.gateway')}</div>
      <svg className="traffic-flow-links" viewBox={`0 0 ${size.width} ${size.height}`} preserveAspectRatio="none">
        {visible.map((placement) => {
          const focused = sameNode(nodeFocus, { kind: placement.kind, id: placement.id })
            || requestTouchesEndpoints(placement.kind, placement.id, selectedRequest)
          const peer = related.has(placement.id) && !focused
          const className = focused ? 'traffic-flow-link is-hot' : peer || placement.lit
            ? `traffic-flow-link${stageFocused && !peer ? ' is-dim' : ' is-active'}`
            : `traffic-flow-link${stageFocused ? ' is-dim' : ''}`
          return <path key={`${placement.kind}:${placement.id}`} className={className} d={linkPath(placement, size)} />
        })}
        {motion.pulses.map((pulse) => (
          <g
            key={pulse.id}
            className={`traffic-flow-pulse is-${pulse.role}${selectedRequest && pulse.requestId !== selectedRequest.request_id ? ' is-dim' : ''}`}
            style={{ color: pulse.color }}
          >
            {pulse.trail.map((point, index) => (
              <circle key={index} className={`traffic-flow-pulse-trail is-${index + 1}`} cx={point.x} cy={point.y} r={pulse.role === 'response' ? 1.5 : 1.8} fill={pulse.color} />
            ))}
            <circle className="traffic-flow-pulse-head" cx={pulse.x} cy={pulse.y} r={pulse.role === 'response' ? 2.4 : 3.1} fill={pulse.color} />
          </g>
        ))}
      </svg>
      <div className="traffic-flow-core-canvas" aria-hidden="true">
        <Canvas
          dpr={[1, 1.4]}
          frameloop={motionPaused ? 'demand' : 'always'}
          gl={{ alpha: true, antialias: false, powerPreference: 'high-performance' }}
          camera={{ position: [0, 0.18, 9.6], fov: 40 }}
          onCreated={({ gl, camera }) => {
            gl.setClearColor('#000000', 0)
            camera.lookAt(0, 0, 0)
          }}
        >
          <EnergyScene active={activeCount > 0 || motion.active} load={Math.min(1, (activeCount || (motion.active ? 1 : 0)) / 8)} color={gatewayColor} pulseKey={pulseKey} paused={motionPaused} />
        </Canvas>
      </div>
      <GatewayCoreOverlay active={activeCount > 0 || motion.active} pulseKey={pulseKey} paused={paused} />
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
