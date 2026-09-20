import type { TrafficFlowRequest } from '@/features/traffic-flow/api/traffic-flow'
import { modelVisual } from '@/features/traffic-flow/lib/model-visual'
import { nodeKey, type LaneKind } from '@/features/traffic-flow/lib/wing-layout'

export type FlowPacket = {
  id: string
  d: string
  color: string
  role: 'request' | 'response'
  reverse: boolean
}

type PacketSeat = {
  x: number
  y: number
  kind: LaneKind
}

export function linkPath(placement: { x: number; y: number }, size: { width: number; height: number }) {
  const x1 = placement.x / 100 * size.width
  const y1 = placement.y / 100 * size.height
  const x2 = size.width / 2
  const y2 = size.height / 2
  const cx1 = x1 + (x2 - x1) * 0.42
  const cx2 = x2 - (x2 - x1) * 0.18
  return `M ${x1} ${y1} C ${cx1} ${y1}, ${cx2} ${y2}, ${x2} ${y2}`
}

export function flowRoles(phase: TrafficFlowRequest['phase'], stream: boolean, retiring: boolean) {
  const outbound = phase === 'accepted' || phase === 'routed' || (phase === 'responding' && stream)
  const inbound = phase === 'responding' || phase === 'completed' || phase === 'failed' || retiring
  return { outbound, inbound }
}

export function packetsForRequest(
  request: TrafficFlowRequest,
  retiring: boolean,
  seats: Map<string, PacketSeat>,
  size: { width: number; height: number },
) {
  const { outbound, inbound } = flowRoles(request.phase, request.stream, retiring)
  if (!outbound && !inbound) return []
  const color = modelVisual(request.model_provider, request.model_key).color
  const packets: FlowPacket[] = []
  const down = seats.get(nodeKey('downstream', request.api_key_id))
  const up = request.upstream_site_id ? seats.get(nodeKey('upstream', request.upstream_site_id)) : undefined
  if (down && outbound) {
    packets.push({ id: `${request.request_id}-down-out`, d: linkPath(down, size), color, role: 'request', reverse: false })
  }
  if (down && inbound) {
    packets.push({ id: `${request.request_id}-down-back`, d: linkPath(down, size), color, role: 'response', reverse: true })
  }
  if (up && outbound && request.phase !== 'accepted') {
    packets.push({ id: `${request.request_id}-up-out`, d: linkPath(up, size), color, role: 'request', reverse: true })
  }
  if (up && inbound) {
    packets.push({ id: `${request.request_id}-up-back`, d: linkPath(up, size), color, role: 'response', reverse: false })
  }
  return packets
}
