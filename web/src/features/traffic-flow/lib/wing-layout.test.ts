import { describe, expect, it } from 'vitest'
import type { TrafficFlowNode, TrafficFlowRequest } from '@/features/traffic-flow/api/traffic-flow'
import { bumpActivityBucket, emptyActivityBuckets, syncActivityBuckets } from '@/features/traffic-flow/lib/activity-buckets'
import {
  layoutWing,
  relatedNodeIDs,
  requestEndpointIDs,
  requestTouchesEndpoints,
  requestTouchesNode,
  wingCapacity,
} from '@/features/traffic-flow/lib/wing-layout'

function node(id: string, name = id): TrafficFlowNode {
  return { id, name }
}

function request(partial: Partial<TrafficFlowRequest> & Pick<TrafficFlowRequest, 'request_id'>): TrafficFlowRequest {
  return {
    api_key_id: 'key-a',
    api_key_name: 'Key A',
    model_key: 'gpt-test',
    model_provider: 'openai',
    attempt: 1,
    stream: true,
    phase: 'routed',
    started_at: '2026-09-20T00:00:00Z',
    updated_at: '2026-09-20T00:00:01Z',
    ...partial,
  }
}

describe('requestTouchesNode', () => {
  it('keeps accepted traffic on the downstream key only', () => {
    const accepted = request({ request_id: 'r1', phase: 'accepted', upstream_site_id: 'site-1' })
    expect(requestTouchesNode(accepted, 'downstream', 'key-a', false)).toBe(true)
    expect(requestTouchesNode(accepted, 'upstream', 'site-1', false)).toBe(false)
  })

  it('lights both lanes after the request is routed', () => {
    const routed = request({ request_id: 'r2', phase: 'routed', upstream_site_id: 'site-1' })
    expect(requestTouchesNode(routed, 'downstream', 'key-a', false)).toBe(true)
    expect(requestTouchesNode(routed, 'upstream', 'site-1', false)).toBe(true)
  })

  it('ignores terminal and retiring requests', () => {
    const done = request({ request_id: 'r3', phase: 'completed', upstream_site_id: 'site-1' })
    expect(requestTouchesNode(done, 'downstream', 'key-a', false)).toBe(false)
    expect(requestTouchesNode(request({ request_id: 'r4', phase: 'routed', upstream_site_id: 'site-1' }), 'upstream', 'site-1', true)).toBe(false)
  })
})

describe('layoutWing', () => {
  it('keeps topology order even when later nodes carry traffic', () => {
    const requests = [request({ request_id: 'r1', api_key_id: 'c', phase: 'accepted' })]
    const placements = layoutWing('downstream', [node('a'), node('b'), node('c')], requests, new Set(), {
      capacity: 8,
      expanded: false,
    })
    expect(placements.filter((item) => !item.clustered).map((item) => item.id)).toEqual(['a', 'b', 'c'])
    expect(placements.find((item) => item.id === 'c')?.zIndex ?? 0).toBeGreaterThan(placements.find((item) => item.id === 'a')?.zIndex ?? 0)
  })

  it('does not swap home seats when traffic moves between nodes', () => {
    const nodes = [node('a'), node('b'), node('c'), node('d')]
    const idle = layoutWing('upstream', nodes, [], new Set(), { capacity: 8, expanded: false })
    const hot = layoutWing('upstream', nodes, [request({ request_id: 'r1', upstream_site_id: 'd', phase: 'routed' })], new Set(), {
      capacity: 8,
      expanded: false,
    })
    for (const id of ['a', 'b', 'c', 'd']) {
      expect(idle.find((item) => item.id === id)?.home).toEqual(hot.find((item) => item.id === id)?.home)
    }
  })

  it('parks cold idle overflow in a cluster and keeps hot nodes seated', () => {
    const nodes = Array.from({ length: 12 }, (_, index) => node(`n${index}`))
    const requests = [request({ request_id: 'r1', api_key_id: 'n11', phase: 'accepted' })]
    const placements = layoutWing('downstream', nodes, requests, new Set(), { capacity: 8, expanded: false })
    const visible = placements.filter((item) => !item.clustered)
    const clustered = placements.filter((item) => item.clustered)
    expect(visible).toHaveLength(8)
    expect(visible.map((item) => item.id)).toContain('n11')
    expect(visible.map((item) => item.id)).toEqual(['n0', 'n1', 'n2', 'n3', 'n4', 'n5', 'n6', 'n11'])
    expect(clustered.every((item) => item.inflight === 0)).toBe(true)
    expect(clustered).toHaveLength(4)
  })

  it('nudges neighbors away from a hovered node without overlapping name boxes', () => {
    const nodes = Array.from({ length: 8 }, (_, index) => node(`n${index}`))
    const idle = layoutWing('downstream', nodes, [], new Set(), { capacity: 8, expanded: false })
    const hovered = layoutWing('downstream', nodes, [], new Set(), {
      capacity: 8,
      expanded: false,
      hovered: { kind: 'downstream', id: 'n3' },
    })
    const target = hovered.find((item) => item.id === 'n3')
    expect(target?.lift).toBeGreaterThan(0)
    const neighbors = hovered.filter((item) => item.id !== 'n3' && !item.clustered)
    const moved = neighbors.filter((item) => {
      const home = idle.find((candidate) => candidate.id === item.id)
      return home && (Math.abs(item.x - home.x) > 0.05 || Math.abs(item.y - home.y) > 0.05)
    })
    expect(moved.length).toBeGreaterThan(0)
    for (let index = 0; index < hovered.length; index += 1) {
      const left = hovered[index]
      if (left.clustered) continue
      for (const right of hovered.slice(index + 1)) {
        if (right.clustered) continue
        expect(Math.hypot(left.x - right.x, left.y - right.y)).toBeGreaterThan(2)
      }
    }
  })

  it('skips positional nudge when reduced motion is requested', () => {
    const nodes = Array.from({ length: 6 }, (_, index) => node(`n${index}`))
    const hovered = layoutWing('downstream', nodes, [], new Set(), {
      capacity: 8,
      expanded: false,
      hovered: { kind: 'downstream', id: 'n2' },
      reducedMotion: true,
    })
    const target = hovered.find((item) => item.id === 'n2')
    expect(target?.x).toBe(target?.home.x)
    expect(hovered.filter((item) => item.id !== 'n2').every((item) => item.x === item.home.x && item.y === item.home.y)).toBe(true)
  })

  it('collects opposite-lane peers for an active node', () => {
    const routed = request({ request_id: 'r1', api_key_id: 'key-a', upstream_site_id: 'site-1', phase: 'routed' })
    expect([...relatedNodeIDs('downstream', 'key-a', [routed], new Set())]).toEqual(['site-1'])
    expect([...relatedNodeIDs('upstream', 'site-1', [routed], new Set())]).toEqual(['key-a'])
  })

  it('does not lift a logically inflight node until litKeys say it has arrived', () => {
    const nodes = [node('site-1'), node('site-2')]
    const requests = [request({ request_id: 'r1', upstream_site_id: 'site-1', phase: 'routed' })]
    const dark = layoutWing('upstream', nodes, requests, new Set(), {
      capacity: 8,
      expanded: false,
      litKeys: new Set(),
    })
    const lit = layoutWing('upstream', nodes, requests, new Set(), {
      capacity: 8,
      expanded: false,
      litKeys: new Set(['upstream:site-1']),
    })
    expect(dark.find((item) => item.id === 'site-1')?.lit).toBe(false)
    expect(dark.find((item) => item.id === 'site-1')?.lift).toBe(0)
    expect(lit.find((item) => item.id === 'site-1')?.lit).toBe(true)
    expect(lit.find((item) => item.id === 'site-1')?.lift).toBe(1)
  })

  it('collects only the request endpoints, not allow-list peers', () => {
    const routed = request({ request_id: 'r1', api_key_id: 'key-a', upstream_site_id: 'site-1', phase: 'routed' })
    expect([...requestEndpointIDs(routed)].sort()).toEqual(['key-a', 'site-1'])
    expect(requestTouchesEndpoints('downstream', 'key-a', routed)).toBe(true)
    expect(requestTouchesEndpoints('upstream', 'site-1', routed)).toBe(true)
    expect(requestTouchesEndpoints('upstream', 'site-2', routed)).toBe(false)
    expect([...requestEndpointIDs(request({ request_id: 'r2', phase: 'accepted' }))]).toEqual(['key-a'])
  })

  it('lifts both request endpoints when selectedKeys are provided', () => {
    const downstream = layoutWing('downstream', [node('key-a'), node('key-b')], [], new Set(), {
      capacity: 8,
      expanded: false,
      selectedKeys: new Set(['downstream:key-a']),
    })
    const upstream = layoutWing('upstream', [node('site-1'), node('site-2')], [], new Set(), {
      capacity: 8,
      expanded: false,
      selectedKeys: new Set(['upstream:site-1']),
    })
    expect(downstream.find((item) => item.id === 'key-a')?.lift).toBe(1)
    expect(downstream.find((item) => item.id === 'key-b')?.lift).toBe(0)
    expect(upstream.find((item) => item.id === 'site-1')?.lift).toBe(1)
    expect(upstream.find((item) => item.id === 'site-2')?.lift).toBe(0)
  })

  it('highlights allowed upstream sites for a downstream key without inflight traffic', () => {
    const downstream = [
      node('key-a'),
      { id: 'key-b', name: 'key-b', site_policy: 'allow_list' as const, allowed_upstream_ids: ['site-1', 'site-3'] },
    ]
    const upstream = [node('site-1'), node('site-2'), node('site-3')]
    const topology = { downstream, upstream }
    expect([...relatedNodeIDs('downstream', 'key-b', [], new Set(), topology)].sort()).toEqual(['site-1', 'site-3'])
    expect([...relatedNodeIDs('upstream', 'site-1', [], new Set(), topology)].sort()).toEqual(['key-a', 'key-b'])
    expect([...relatedNodeIDs('upstream', 'site-2', [], new Set(), topology)]).toEqual(['key-a'])
  })
})

describe('wingCapacity', () => {
  it('keeps a two-column seat count within the stage height', () => {
    expect(wingCapacity(520)).toBeGreaterThanOrEqual(8)
    expect(wingCapacity(900)).toBeLessThanOrEqual(18)
    expect(wingCapacity(900) % 2).toBe(0)
  })
})

describe('activity buckets', () => {
  it('rolls empty minutes forward and increments the current bucket', () => {
    const start = 1_700_000_000_000
    const first = bumpActivityBucket(emptyActivityBuckets(start), start)
    expect(first.at(-1)?.count).toBe(1)
    const later = syncActivityBuckets(first, start + 60_000)
    expect(later.at(-1)?.count).toBe(0)
    expect(later.at(-2)?.count).toBe(1)
  })
})
