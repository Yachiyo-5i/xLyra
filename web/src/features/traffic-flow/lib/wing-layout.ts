import type { TrafficFlowNode, TrafficFlowRequest } from '@/features/traffic-flow/api/traffic-flow'

export type LaneKind = 'downstream' | 'upstream'

export type TrafficFlowNodeRef = {
  kind: LaneKind
  id: string
}

export type WingPlacement = {
  id: string
  kind: LaneKind
  name: string
  siteType?: string
  node: TrafficFlowNode
  clustered: boolean
  inflight: number
  home: { x: number; y: number }
  x: number
  y: number
  scale: number
  zIndex: number
  opacity: number
  lift: number
  lit: boolean
}

const terminalPhases = new Set(['completed', 'failed', 'cancelled'])
const nudgeRadius = 24
const maxNudge = 2.8

export function nodeKey(kind: LaneKind, id: string) {
  return `${kind}:${id}`
}

export function sameNode(left: TrafficFlowNodeRef | null | undefined, right: TrafficFlowNodeRef | null | undefined) {
  return Boolean(left && right && left.kind === right.kind && left.id === right.id)
}

export function requestTouchesNode(request: TrafficFlowRequest, kind: LaneKind, id: string, retiring: boolean) {
  if (retiring || terminalPhases.has(request.phase)) return false
  if (kind === 'downstream') return request.api_key_id === id
  if (request.phase === 'accepted' || !request.upstream_site_id) return false
  return request.upstream_site_id === id
}

export function requestsForNode(
  kind: LaneKind,
  id: string,
  requests: TrafficFlowRequest[],
  retiringRequestIDs: Set<string>,
) {
  return requests.filter((request) => requestTouchesNode(request, kind, id, retiringRequestIDs.has(request.request_id)))
}

export function primaryRequestForNode(
  kind: LaneKind,
  id: string,
  requests: TrafficFlowRequest[],
  retiringRequestIDs: Set<string>,
) {
  const matches = requestsForNode(kind, id, requests, retiringRequestIDs)
  if (matches.length === 0) return null
  return [...matches].sort((left, right) => Date.parse(right.updated_at) - Date.parse(left.updated_at) || left.request_id.localeCompare(right.request_id))[0]
}

export function allowedUpstreamIDs(node: TrafficFlowNode | undefined, upstream: TrafficFlowNode[]) {
  if (!node) return []
  if (node.site_policy === 'allow_list') return node.allowed_upstream_ids ?? []
  if (node.allowed_upstream_ids?.length) return node.allowed_upstream_ids
  return upstream.map((item) => item.id)
}

export function requestEndpointIDs(request: TrafficFlowRequest | null | undefined) {
  const ids = new Set<string>()
  if (!request) return ids
  ids.add(request.api_key_id)
  if (request.upstream_site_id) ids.add(request.upstream_site_id)
  return ids
}

export function requestTouchesEndpoints(kind: LaneKind, id: string, request: TrafficFlowRequest | null | undefined) {
  if (!request) return false
  if (kind === 'downstream') return request.api_key_id === id
  return Boolean(request.upstream_site_id && request.upstream_site_id === id)
}

export function relatedNodeIDs(
  kind: LaneKind,
  id: string,
  requests: TrafficFlowRequest[],
  retiringRequestIDs: Set<string>,
  topology?: { downstream: TrafficFlowNode[]; upstream: TrafficFlowNode[] } | null,
) {
  const related = new Set<string>()
  for (const request of requestsForNode(kind, id, requests, retiringRequestIDs)) {
    if (kind === 'downstream' && request.upstream_site_id) related.add(request.upstream_site_id)
    if (kind === 'upstream') related.add(request.api_key_id)
  }
  if (!topology) return related
  if (kind === 'downstream') {
    for (const siteID of allowedUpstreamIDs(topology.downstream.find((item) => item.id === id), topology.upstream)) {
      related.add(siteID)
    }
    return related
  }
  for (const node of topology.downstream) {
    if (allowedUpstreamIDs(node, topology.upstream).includes(id)) related.add(node.id)
  }
  return related
}

export function wingCapacity(height: number) {
  const rows = Math.max(4, Math.min(9, Math.floor((Math.max(320, height) - 96) / 52)))
  return rows * 2
}

export function layoutWings(
  downstream: TrafficFlowNode[],
  upstream: TrafficFlowNode[],
  requests: TrafficFlowRequest[],
  retiringRequestIDs: Set<string>,
  options: {
    capacity: number
    expanded: { downstream: boolean; upstream: boolean }
    hovered?: TrafficFlowNodeRef | null
    selected?: TrafficFlowNodeRef | null
    selectedKeys?: Set<string>
    litKeys?: Set<string>
    reducedMotion?: boolean
  },
) {
  return [
    ...layoutWing('downstream', downstream, requests, retiringRequestIDs, { ...options, expanded: options.expanded.downstream }),
    ...layoutWing('upstream', upstream, requests, retiringRequestIDs, { ...options, expanded: options.expanded.upstream }),
  ]
}

export function layoutWing(
  kind: LaneKind,
  nodes: TrafficFlowNode[],
  requests: TrafficFlowRequest[],
  retiringRequestIDs: Set<string>,
  options: {
    capacity: number
    expanded: boolean
    hovered?: TrafficFlowNodeRef | null
    selected?: TrafficFlowNodeRef | null
    selectedKeys?: Set<string>
    litKeys?: Set<string>
    reducedMotion?: boolean
  },
) {
  const scored = nodes.map((node, index) => {
    const inflight = requestsForNode(kind, node.id, requests, retiringRequestIDs).length
    return { node, inflight, index }
  })
  const visible = pickVisible(scored, options.capacity, options.expanded)
  const visibleIDs = new Set(visible.map((item) => item.node.id))
  const homes = visible.map((item, index) => ({
    item,
    home: seatForIndex(kind, index, visible.length),
  }))

  const lifted = new Set<string>()
  const draft: WingPlacement[] = homes.map(({ item, home }) => {
    const hovered = sameNode(options.hovered, { kind, id: item.node.id })
    const selected = sameNode(options.selected, { kind, id: item.node.id })
      || Boolean(options.selectedKeys?.has(nodeKey(kind, item.node.id)))
    const lit = options.litKeys ? options.litKeys.has(nodeKey(kind, item.node.id)) : item.inflight > 0
    const lift = (lit ? 1 : 0) + (hovered ? 1 : 0) + (selected ? 1 : 0)
    if (lift > 0) lifted.add(item.node.id)
    const inward = options.reducedMotion ? 0 : lift * (kind === 'downstream' ? 1.5 : -1.5)
    return {
      id: item.node.id,
      kind,
      name: item.node.name,
      siteType: item.node.site_type,
      node: item.node,
      clustered: false,
      inflight: item.inflight,
      home,
      x: home.x + inward,
      y: home.y,
      scale: 1 + lift * 0.045,
      zIndex: 20 + lift * 12 + item.inflight,
      opacity: lift > 0 ? 1 : 0.78,
      lift,
      lit,
    }
  })

  const nudged = options.reducedMotion ? draft : applyNudge(draft, lifted)

  const clustered = scored
    .filter((item) => !visibleIDs.has(item.node.id))
    .map((item) => ({
      id: item.node.id,
      kind,
      name: item.node.name,
      siteType: item.node.site_type,
      node: item.node,
      clustered: true,
      inflight: item.inflight,
      home: clusterSeat(kind),
      x: clusterSeat(kind).x,
      y: clusterSeat(kind).y,
      scale: 1,
      zIndex: 8,
      opacity: 0.7,
      lift: 0,
      lit: false,
    }))

  return [...nudged, ...clustered]
}

function pickVisible<T extends { node: TrafficFlowNode; inflight: number; index: number }>(
  scored: T[],
  capacity: number,
  expanded: boolean,
) {
  if (expanded || scored.length <= capacity) return scored
  const hot = scored.filter((item) => item.inflight > 0)
  if (hot.length >= capacity) return hot.slice(0, capacity)
  const remaining = capacity - hot.length
  const hotIDs = new Set(hot.map((item) => item.node.id))
  const idle = scored.filter((item) => !hotIDs.has(item.node.id)).slice(0, remaining)
  return [...hot, ...idle].sort((left, right) => left.index - right.index)
}

function seatForIndex(kind: LaneKind, index: number, count: number) {
  const columns = count <= 5 ? 1 : 2
  const col = columns === 1 ? 0 : index % 2
  const row = columns === 1 ? index : Math.floor(index / 2)
  const rows = columns === 1 ? count : Math.ceil(count / columns)
  const t = rows <= 1 ? 0.5 : row / (rows - 1)
  const arc = (t - 0.5) ** 2 * 4
  const ySpan = 64
  const rowStep = rows <= 1 ? 0 : ySpan / Math.max(1, rows - 1)
  const y = 18 + t * ySpan + (col === 1 ? rowStep * 0.28 : 0)
  if (kind === 'downstream') {
    return { x: (col === 0 ? 27.4 : 12.2) - arc * (col === 0 ? 3.2 : 4.4), y }
  }
  return { x: (col === 0 ? 72.6 : 87.8) + arc * (col === 0 ? 3.2 : 4.4), y }
}

export function clusterSeat(kind: LaneKind) {
  return { x: kind === 'downstream' ? 16 : 84, y: 92 }
}

function applyNudge(placements: WingPlacement[], lifted: Set<string>) {
  return placements.map((placement) => {
    let dx = 0
    let dy = 0
    for (const other of placements) {
      if (other.id === placement.id || !lifted.has(other.id)) continue
      const vx = placement.x - other.x
      const vy = placement.y - other.y
      const dist = Math.hypot(vx, vy)
      if (dist >= nudgeRadius || dist < 0.01) continue
      const force = (1 - dist / nudgeRadius) * 2.4
      dx += (vx / dist) * force
      dy += (vy / dist) * force
    }
    const magnitude = Math.hypot(dx, dy)
    if (magnitude > maxNudge) {
      dx *= maxNudge / magnitude
      dy *= maxNudge / magnitude
    }
    return { ...placement, x: placement.x + dx, y: placement.y + dy }
  })
}
