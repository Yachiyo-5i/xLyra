import { describe, expect, it } from 'vitest'
import type { TrafficFlowRequest } from '@/features/traffic-flow/api/traffic-flow'
import { flowRoles, packetsForRequest } from '@/features/traffic-flow/lib/flow-packets'
import { nodeKey } from '@/features/traffic-flow/lib/wing-layout'

function request(partial: Partial<TrafficFlowRequest> & Pick<TrafficFlowRequest, 'request_id'>): TrafficFlowRequest {
  return {
    api_key_id: 'key-a',
    api_key_name: 'Key A',
    model_key: 'gpt-test',
    model_provider: 'openai',
    attempt: 1,
    stream: true,
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
  it('sends only the downstream request while the call is accepted', () => {
    expect(flowRoles('accepted', true, false)).toEqual({ outbound: true, inbound: false })
    const packets = packetsForRequest(request({ request_id: 'r1', phase: 'accepted' }), false, seats, size)
    expect(packets.map((item) => item.id)).toEqual(['r1-down-out'])
    expect(packets[0]?.role).toBe('request')
    expect(packets[0]?.reverse).toBe(false)
  })

  it('keeps outbound packets on both wings after routing', () => {
    const packets = packetsForRequest(request({ request_id: 'r2', phase: 'routed', upstream_site_id: 'site-1' }), false, seats, size)
    expect(packets.map((item) => `${item.role}:${item.reverse}`).sort()).toEqual(['request:false', 'request:true'])
  })

  it('turns around into response packets while the upstream is responding', () => {
    const packets = packetsForRequest(request({ request_id: 'r3', phase: 'responding', stream: false, upstream_site_id: 'site-1' }), false, seats, size)
    expect(packets.every((item) => item.role === 'response')).toBe(true)
    expect(packets).toHaveLength(2)
  })
})
