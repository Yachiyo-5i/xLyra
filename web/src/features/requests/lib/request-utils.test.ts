import { describe, expect, it } from 'vitest'
import type { RequestLogDetail } from '@/features/requests/api/requests'
import {
  formatLatency,
  requestCostFormula,
  requestCredentialMultiplier,
  requestDownstreamTransportLabel,
  requestFirstByteLatency,
  requestFirstByteLatencyTone,
  requestReasoningEffort,
  requestTotalLatencyTone,
} from '@/features/requests/lib/request-utils'

describe('requestDownstreamTransportLabel', () => {
  it('labels websocket request metadata as WS', () => {
    const detail = requestDetail({ downstream_transport: 'websocket' })
    expect(requestDownstreamTransportLabel(detail)).toBe('WS')
  })

  it('labels HTTP and legacy request metadata as HTTP', () => {
    const httpDetail = requestDetail({ downstream_transport: 'http' })
    const legacyDetail = requestDetail()
    expect(requestDownstreamTransportLabel(httpDetail)).toBe('HTTP')
    expect(requestDownstreamTransportLabel(legacyDetail)).toBe('HTTP')
  })
})

describe('request log display helpers', () => {
  it('keeps missing first-byte latency unknown', () => {
    expect(formatLatency()).toBe('-')
    expect(formatLatency(12.6)).toBe('13 ms')
    expect(formatLatency(Number.NaN)).toBe('-')

    const nonStream = requestDetail()
    nonStream.stream = false
    nonStream.first_byte_latency_ms = 12
    expect(requestFirstByteLatency(nonStream)).toBeNull()

    const stream = requestDetail()
    stream.stream = true
    stream.first_byte_latency_ms = 12
    expect(requestFirstByteLatency(stream)).toBe(12)

    stream.first_byte_latency_ms = Number.NaN
    expect(requestFirstByteLatency(stream)).toBeNull()

    const legacy = requestDetail()
    legacy.first_byte_latency_ms = 12
    expect(requestFirstByteLatency(legacy)).toBeNull()
  })

  it.each([
    [0, 'healthy'],
    [5000, 'healthy'],
    [5001, 'slow'],
    [10000, 'slow'],
    [10001, 'very-slow'],
    [20000, 'very-slow'],
    [20001, 'critical'],
    [null, 'muted'],
    [undefined, 'muted'],
    [Number.NaN, 'muted'],
    [-1, 'muted'],
  ] as const)('assigns first-byte latency %s to the %s tone', (latency, tone) => {
    expect(requestFirstByteLatencyTone(latency)).toBe(tone)
  })

  it.each([
    [0, 'healthy'],
    [20000, 'healthy'],
    [20001, 'slow'],
    [40000, 'slow'],
    [40001, 'very-slow'],
    [60000, 'very-slow'],
    [60001, 'critical'],
    [null, 'muted'],
    [undefined, 'muted'],
    [Number.NaN, 'muted'],
    [-1, 'muted'],
  ] as const)('assigns total latency %s to the %s tone', (latency, tone) => {
    expect(requestTotalLatencyTone(latency)).toBe(tone)
  })

  it('only exposes a non-empty typed reasoning effort', () => {
    const detail = requestDetail()
    detail.reasoning_effort = ' high '
    expect(requestReasoningEffort(detail)).toBe('high')
    detail.reasoning_effort = 2 as unknown as string
    expect(requestReasoningEffort(detail)).toBeNull()
    detail.reasoning_effort = null
    expect(requestReasoningEffort(detail)).toBeNull()
  })

  it('shows saved component multipliers, including x1 values', () => {
    const detail = requestDetail()
    detail.pricing = { input_value: 1, output_value: 1 }
    detail.cost_calculation = {
      prompt_tokens: 10,
      completion_tokens: 5,
      base_estimated_cost: 1,
      estimated_cost: 2,
      credential_upstream_cost_multiplier: 1,
      service_tier_multiplier: 2,
      currency: 'USD',
    }
    expect(requestCredentialMultiplier(detail)).toBe(1)
    expect(requestCostFormula(detail, (key, options) => `${key} ${String(options?.multiplier ?? '')}`)).toContain('credentialMultiplier x1 * detail.formula.serviceTierMultiplier x2')

    detail.cost_calculation.service_tier_multiplier = 1
    expect(requestCostFormula(detail, (key, options) => `${key} ${String(options?.multiplier ?? '')}`)).toContain('credentialMultiplier x1 * detail.formula.serviceTierMultiplier x1')

    detail.cost_calculation.credential_upstream_cost_multiplier = -1
    expect(requestCredentialMultiplier(detail)).toBeNull()
  })

  it('falls back to the legacy aggregate multiplier', () => {
    const detail = requestDetail()
    detail.cost_calculation = {
      base_estimated_cost: 1,
      estimated_cost: 2,
      cost_multiplier: 2,
      currency: 'USD',
    }
    detail.pricing = { input_value: 1, output_value: 1 }
    detail.usage.prompt_tokens = 10
    detail.usage.completion_tokens = 5
    expect(requestCostFormula(detail, (key) => key)).toContain('* 2')
  })
})

function requestDetail(metadata?: Record<string, unknown>): RequestLogDetail {
  return {
    id: 'log_1',
    request_id: 'req_1',
    success: true,
    api_key: {},
    site: {},
    model: {},
    usage: {},
    metadata,
    created_at: '2026-07-19T00:00:00Z',
  }
}
