import type { TrafficFlowPhase, TrafficFlowRequest } from '@/features/traffic-flow/api/traffic-flow'
import { modelVisual } from '@/features/traffic-flow/lib/model-visual'
import { nodeKey, type LaneKind } from '@/features/traffic-flow/lib/wing-layout'

export const packetTravelMs = 2200
export const packetSpawnMs = 375
export const packetArriveAt = 0.74
export const packetTrailGap = 0.055

export type FlowPulse = {
  id: string
  requestId: string
  apiKeyId: string
  upstreamSiteId?: string
  role: 'request' | 'response'
  hop: 0 | 1
  color: string
  hopBornAt: number
  duration: number
}

export type FlowMotionState = {
  nextSeq: number
  pulses: FlowPulse[]
  lastSpawn: Record<string, number>
  drained: Set<string>
}

type PacketSeat = {
  x: number
  y: number
  kind: LaneKind
}

type Point = { x: number; y: number }

const terminalPhases = new Set<TrafficFlowPhase>(['completed', 'failed', 'cancelled'])

export function linkPath(placement: { x: number; y: number }, size: { width: number; height: number }) {
  const { p0, p1, p2, p3 } = linkControls(placement, size)
  return `M ${p0.x} ${p0.y} C ${p1.x} ${p1.y}, ${p2.x} ${p2.y}, ${p3.x} ${p3.y}`
}

export function linkControls(placement: { x: number; y: number }, size: { width: number; height: number }) {
  const p0 = { x: placement.x / 100 * size.width, y: placement.y / 100 * size.height }
  const p3 = { x: size.width / 2, y: size.height / 2 }
  const p1 = { x: p0.x + (p3.x - p0.x) * 0.42, y: p0.y }
  const p2 = { x: p3.x - (p3.x - p0.x) * 0.18, y: p3.y }
  return { p0, p1, p2, p3 }
}

export function pointOnCubic(p0: Point, p1: Point, p2: Point, p3: Point, t: number) {
  const u = 1 - t
  return {
    x: u * u * u * p0.x + 3 * u * u * t * p1.x + 3 * u * t * t * p2.x + t * t * t * p3.x,
    y: u * u * u * p0.y + 3 * u * u * t * p1.y + 3 * u * t * t * p2.y + t * t * t * p3.y,
  }
}

export function pulseProgress(pulse: Pick<FlowPulse, 'hopBornAt' | 'duration'>, now: number) {
  if (pulse.duration <= 0) return 1
  return Math.min(1, Math.max(0, (now - pulse.hopBornAt) / pulse.duration))
}

export function isFlowDraining(request: TrafficFlowRequest, retiring: boolean) {
  return retiring || terminalPhases.has(request.phase)
}

export function emptyFlowMotion(): FlowMotionState {
  return { nextSeq: 1, pulses: [], lastSpawn: {}, drained: new Set() }
}

export function spawnRole(request: TrafficFlowRequest, draining: boolean, hasRequestPulses: boolean): FlowPulse['role'] | null {
  if (draining) return null
  if (request.phase === 'accepted' || request.phase === 'routed') return 'request'
  if (request.phase === 'responding' && request.upstream_site_id && !hasRequestPulses) return 'response'
  return null
}

export function pulseSeatKind(pulse: Pick<FlowPulse, 'role' | 'hop'>): LaneKind {
  if (pulse.role === 'request') return pulse.hop === 0 ? 'downstream' : 'upstream'
  return pulse.hop === 0 ? 'upstream' : 'downstream'
}

export function pulseSeatKey(pulse: Pick<FlowPulse, 'role' | 'hop' | 'apiKeyId' | 'upstreamSiteId'>) {
  const kind = pulseSeatKind(pulse)
  if (kind === 'downstream') return nodeKey('downstream', pulse.apiKeyId)
  if (!pulse.upstreamSiteId) return null
  return nodeKey('upstream', pulse.upstreamSiteId)
}

export function advancePulse(pulse: FlowPulse, now: number, siteId: string | undefined, draining: boolean): FlowPulse | null {
  const nextSite = siteId ?? pulse.upstreamSiteId
  let current = nextSite && nextSite !== pulse.upstreamSiteId ? { ...pulse, upstreamSiteId: nextSite } : pulse
  while (now >= current.hopBornAt + current.duration) {
    if (current.hop === 0) {
      if (current.role === 'request' && !nextSite) return draining ? null : { ...current, hopBornAt: now - current.duration }
      current = {
        ...current,
        hop: 1,
        hopBornAt: current.hopBornAt + current.duration,
        upstreamSiteId: current.role === 'request' ? nextSite : current.upstreamSiteId,
      }
      continue
    }
    return null
  }
  return current
}

export function litNodeKeys(pulses: FlowPulse[], now: number) {
  const lit = new Set<string>()
  for (const pulse of pulses) {
    const progress = pulseProgress(pulse, now)
    if (pulse.role === 'request') {
      lit.add(nodeKey('downstream', pulse.apiKeyId))
      if (pulse.hop === 1 && pulse.upstreamSiteId && progress >= packetArriveAt) lit.add(nodeKey('upstream', pulse.upstreamSiteId))
    } else if (pulse.upstreamSiteId) {
      lit.add(nodeKey('upstream', pulse.upstreamSiteId))
      if (pulse.hop === 1 && progress >= packetArriveAt) lit.add(nodeKey('downstream', pulse.apiKeyId))
    }
  }
  return lit
}

export function spawnKey(requestID: string, role: FlowPulse['role']) {
  return `${requestID}:${role}`
}

export function stepFlowMotion(
  state: FlowMotionState,
  requests: TrafficFlowRequest[],
  retiringRequestIDs: Set<string>,
  now: number,
  reducedMotion = false,
): { state: FlowMotionState; litKeys: Set<string>; drained: string[] } {
  const lastSpawn = { ...state.lastSpawn }
  const byID = new Map(requests.map((request) => [request.request_id, request]))

  let nextSeq = state.nextSeq
  const live: FlowPulse[] = []
  if (!reducedMotion) {
    for (const pulse of state.pulses) {
      const request = byID.get(pulse.requestId)
      const draining = !request || isFlowDraining(request, retiringRequestIDs.has(pulse.requestId))
      const next = advancePulse(pulse, now, request?.upstream_site_id, draining)
      if (next) live.push(next)
    }
    const requestLive = new Set(live.filter((pulse) => pulse.role === 'request').map((pulse) => pulse.requestId))
    for (const request of requests) {
      const draining = isFlowDraining(request, retiringRequestIDs.has(request.request_id))
      const role = spawnRole(request, draining, requestLive.has(request.request_id))
      if (!role) continue
      const key = spawnKey(request.request_id, role)
      const previous = lastSpawn[key]
      if (previous != null && now - previous < packetSpawnMs) continue
      lastSpawn[key] = now
      live.push({
        id: `${request.request_id}-${role}-${nextSeq}`,
        requestId: request.request_id,
        apiKeyId: request.api_key_id,
        upstreamSiteId: request.upstream_site_id,
        role,
        hop: 0,
        color: modelVisual(request.model_provider, request.model_key).color,
        hopBornAt: now,
        duration: packetTravelMs,
      })
      nextSeq += 1
    }
  }

  const drained: string[] = []
  const drainedSet = new Set(state.drained)
  for (const request of requests) {
    const id = request.request_id
    if (!isFlowDraining(request, retiringRequestIDs.has(id))) {
      drainedSet.delete(id)
      continue
    }
    if (live.some((pulse) => pulse.requestId === id)) continue
    if (drainedSet.has(id)) continue
    drainedSet.add(id)
    drained.push(id)
  }

  const next = { nextSeq, pulses: live, lastSpawn, drained: drainedSet }
  return {
    state: next,
    litKeys: reducedMotion
      ? reducedLitKeys(requests, retiringRequestIDs)
      : litNodeKeys(live, now),
    drained,
  }
}

export function reducedLitKeys(requests: TrafficFlowRequest[], retiringRequestIDs: Set<string>) {
  const lit = new Set<string>()
  for (const request of requests) {
    if (isFlowDraining(request, retiringRequestIDs.has(request.request_id))) continue
    lit.add(nodeKey('downstream', request.api_key_id))
    if (request.upstream_site_id && request.phase !== 'accepted') lit.add(nodeKey('upstream', request.upstream_site_id))
  }
  return lit
}

export function pulsePoints(
  pulse: FlowPulse,
  seats: Map<string, PacketSeat>,
  size: { width: number; height: number },
  now: number,
) {
  const seatKey = pulseSeatKey(pulse)
  const seat = seatKey ? seats.get(seatKey) : undefined
  if (!seat) return null
  const controls = linkControls(seat, size)
  const progress = pulseProgress(pulse, now)
  const towardGateway = pulse.hop === 0
  const t = towardGateway ? progress : 1 - progress
  const head = pointOnCubic(controls.p0, controls.p1, controls.p2, controls.p3, t)
  const trail = [1, 2].map((step) => {
    const offset = towardGateway ? Math.max(0, t - packetTrailGap * step) : Math.min(1, t + packetTrailGap * step)
    return pointOnCubic(controls.p0, controls.p1, controls.p2, controls.p3, offset)
  })
  return { head, trail, d: linkPath(seat, size), towardGateway }
}

export function sameLitKeys(left: Set<string>, right: Set<string>) {
  if (left === right) return true
  if (left.size !== right.size) return false
  for (const key of left) {
    if (!right.has(key)) return false
  }
  return true
}
