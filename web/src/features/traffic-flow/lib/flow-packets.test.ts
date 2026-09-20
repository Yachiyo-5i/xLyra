import { describe, expect, it } from 'vitest'
import type { TrafficFlowRequest } from '@/features/traffic-flow/api/traffic-flow'
import {
  advancePulse,
  emptyFlowMotion,
  linkControls,
  packetArriveAt,
  packetTravelMs,
  pointOnCubic,
  pulsePoints,
  pulseSeatKind,
  spawnRole,
  stepFlowMotion,
} from '@/features/traffic-flow/lib/flow-packets'
import { nodeKey } from '@/features/traffic-flow/lib/wing-layout'

function request(partial: Partial<TrafficFlowRequest> & Pick<TrafficFlowRequest, 'request_id'>): TrafficFlowRequest {
  return {
    api_key_id: 'key-a',
    api_key_name: 'Key A',
    model_key: 'gpt-test',
    model_provider: 'openai',
    attempt: 1,
    stream: false,
    phase: 'accepted',
    started_at: '2026-09-20T00:00:00Z',
    updated_at: '2026-09-20T00:00:01Z',
    ...partial,
  }
}

const seats = new Map([
  [nodeKey('downstream', 'key-a'), { x: 20, y: 40, kind: 'downstream' as const }],
  [nodeKey('upstream', 'site-1'), { x: 80, y: 40, kind: 'upstream' as const }],
])
const size = { width: 1000, height: 600 }

describe('flow packets', () => {
  it('maps SSE accepted/routed to downstream-origin request particles', () => {
    expect(spawnRole(request({ request_id: 'r1', phase: 'accepted' }), false, false)).toBe('request')
    expect(spawnRole(request({ request_id: 'r1', phase: 'routed', upstream_site_id: 'site-1' }), false, false)).toBe('request')
  })

  it('maps SSE responding to upstream-origin response particles after the request has crossed', () => {
    const responding = request({ request_id: 'r1', phase: 'responding', upstream_site_id: 'site-1' })
    expect(spawnRole(responding, false, true)).toBe(null)
    expect(spawnRole(responding, false, false)).toBe('response')
  })

  it('starts every request particle on the downstream wing, not at the gateway', () => {
    const accepted = request({ request_id: 'r1', phase: 'accepted' })
    const result = stepFlowMotion(emptyFlowMotion(), [accepted], new Set(), 0)
    expect(result.state.pulses).toHaveLength(1)
    const pulse = result.state.pulses[0]!
    expect(pulse.role).toBe('request')
    expect(pulse.hop).toBe(0)
    expect(pulseSeatKind(pulse)).toBe('downstream')
    const points = pulsePoints(pulse, seats, size, 0)!
    const down = linkControls({ x: 20, y: 40 }, size)
    expect(points.head).toEqual(down.p0)
    expect(points.towardGateway).toBe(true)
    expect(result.litKeys.has(nodeKey('downstream', 'key-a'))).toBe(true)
    expect(result.litKeys.has(nodeKey('upstream', 'site-1'))).toBe(false)
  })

  it('continues a request through the gateway onto the routed upstream site', () => {
    const accepted = request({ request_id: 'r2', phase: 'accepted' })
    let result = stepFlowMotion(emptyFlowMotion(), [accepted], new Set(), 0)
    const routed = request({ request_id: 'r2', phase: 'routed', upstream_site_id: 'site-1' })
    result = stepFlowMotion(result.state, [routed], new Set(), packetTravelMs)
    const crossing = result.state.pulses.find((pulse) => pulse.role === 'request' && pulse.hop === 1)
    expect(crossing).toBeTruthy()
    expect(pulseSeatKind(crossing!)).toBe('upstream')
    const start = pulsePoints(crossing!, seats, size, packetTravelMs)!
    const up = linkControls({ x: 80, y: 40 }, size)
    expect(start.head).toEqual(up.p3)
    expect(start.towardGateway).toBe(false)
    expect(result.litKeys.has(nodeKey('upstream', 'site-1'))).toBe(false)

    result = stepFlowMotion(result.state, [routed], new Set(), packetTravelMs + packetTravelMs * packetArriveAt)
    expect(result.litKeys.has(nodeKey('upstream', 'site-1'))).toBe(true)
    const arrived = result.state.pulses.find((pulse) => pulse.id === crossing!.id)!
    const end = pulsePoints(arrived, seats, size, packetTravelMs + packetTravelMs)!
    expect(end.head).toEqual(up.p0)
  })

  it('starts every response particle on the upstream wing and crosses back to downstream', () => {
    const accepted = request({ request_id: 'r3', phase: 'accepted' })
    let result = stepFlowMotion(emptyFlowMotion(), [accepted], new Set(), 0)
    const routed = request({ request_id: 'r3', phase: 'routed', upstream_site_id: 'site-1' })
    result = stepFlowMotion(result.state, [routed], new Set(), packetTravelMs)
    const responding = request({ request_id: 'r3', phase: 'responding', upstream_site_id: 'site-1' })
    result = stepFlowMotion(result.state, [responding], new Set(), packetTravelMs)
    expect(result.state.pulses.every((pulse) => pulse.role === 'request')).toBe(true)

    result = stepFlowMotion(result.state, [responding], new Set(), packetTravelMs * 3)
    expect(result.state.pulses.some((pulse) => pulse.role === 'request')).toBe(false)
    const returning = result.state.pulses.find((pulse) => pulse.role === 'response')
    expect(returning?.hop).toBe(0)
    expect(pulseSeatKind(returning!)).toBe('upstream')
    const start = pulsePoints(returning!, seats, size, packetTravelMs * 3)!
    const up = linkControls({ x: 80, y: 40 }, size)
    expect(start.head).toEqual(up.p0)
    expect(start.towardGateway).toBe(true)

    result = stepFlowMotion(result.state, [responding], new Set(), packetTravelMs * 4)
    const inbound = result.state.pulses.find((pulse) => pulse.role === 'response' && pulse.hop === 1)
    expect(inbound).toBeTruthy()
    expect(pulseSeatKind(inbound!)).toBe('downstream')
    const throughGateway = pulsePoints(inbound!, seats, size, packetTravelMs * 4)!
    const down = linkControls({ x: 20, y: 40 }, size)
    expect(throughGateway.head).toEqual(down.p3)
    expect(throughGateway.towardGateway).toBe(false)
  })

  it('does not spawn opposite-direction particles at the same time, including streams', () => {
    const accepted = request({ request_id: 'r4', phase: 'accepted', stream: true })
    let result = stepFlowMotion(emptyFlowMotion(), [accepted], new Set(), 0)
    const responding = request({ request_id: 'r4', phase: 'responding', stream: true, upstream_site_id: 'site-1' })
    result = stepFlowMotion(result.state, [responding], new Set(), 80)
    expect(result.state.pulses.every((pulse) => pulse.role === 'request')).toBe(true)
    result = stepFlowMotion(result.state, [responding], new Set(), 80 + packetTravelMs * 2)
    expect(result.state.pulses.every((pulse) => pulse.role === 'response')).toBe(true)
  })

  it('lets an in-flight request finish crossing after the call ends', () => {
    const routed = request({ request_id: 'r5', phase: 'routed', upstream_site_id: 'site-1' })
    let result = stepFlowMotion(emptyFlowMotion(), [routed], new Set(), 0)
    expect(result.state.pulses[0]?.hop).toBe(0)
    const done = request({ request_id: 'r5', phase: 'completed', upstream_site_id: 'site-1' })
    result = stepFlowMotion(result.state, [done], new Set(), 40)
    expect(result.drained).toEqual([])
    expect(result.state.pulses).toHaveLength(1)
    result = stepFlowMotion(result.state, [done], new Set(), packetTravelMs)
    expect(result.state.pulses[0]?.hop).toBe(1)
    result = stepFlowMotion(result.state, [done], new Set(), packetTravelMs * 2)
    expect(result.drained).toEqual(['r5'])
    expect(result.state.pulses).toHaveLength(0)
  })

  it('holds a request at the gateway until SSE provides an upstream site', () => {
    const pulse = {
      id: 'p1',
      requestId: 'r6',
      apiKeyId: 'key-a',
      role: 'request' as const,
      hop: 0 as const,
      color: '#fff',
      hopBornAt: 0,
      duration: packetTravelMs,
    }
    const held = advancePulse(pulse, packetTravelMs, undefined, false)
    expect(held?.hop).toBe(0)
    const continued = advancePulse(held!, packetTravelMs, 'site-1', false)
    expect(continued?.hop).toBe(1)
    expect(continued?.upstreamSiteId).toBe('site-1')
  })

  it('maps cubic endpoints onto the node and gateway', () => {
    const controls = linkControls({ x: 20, y: 40 }, size)
    expect(pointOnCubic(controls.p0, controls.p1, controls.p2, controls.p3, 0)).toEqual(controls.p0)
    expect(pointOnCubic(controls.p0, controls.p1, controls.p2, controls.p3, 1)).toEqual(controls.p3)
  })
})
