import { describe, expect, it } from 'vitest'
import type { RequestLogDetail } from '@/features/requests/api/requests'
import {
  formatLatency,
  requestCostFormula,
  requestCredentialMultiplier,
  requestDownstreamTransportLabel,
  requestFailoverFailureReason,
  requestFailoverCredentialAttempt,
  requestFailoverRouteAttempt,
  requestFailoverTrace,
  requestHasFailover,
  requestHasBillingDetails,
  requestFirstByteLatency,
  requestFirstByteLatencyTone,
  requestElapsedMs,
  requestIsInProgress,
  requestLogDisplayTimestamp,
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
  it('derives elapsed time for in-progress requests', () => {
    const item = requestDetail()
    item.is_live = true
    item.started_at = '2026-08-17T08:00:00.000Z'
    const now = Date.parse('2026-08-17T08:00:01.250Z')

    expect(requestIsInProgress(item)).toBe(true)
    expect(requestElapsedMs(item, now)).toBe(1250)

    item.is_live = false
    expect(requestElapsedMs(item, now)).toBeNull()
    item.is_live = true
    item.started_at = 'invalid'
    expect(requestElapsedMs(item, now)).toBeNull()
  })

  it('uses the effective request start timestamp for display', () => {
    const item = { ...requestDetail(), display_started_at: null as string | null }
    item.started_at = '2026-08-17T08:00:00.000Z'
    expect(requestLogDisplayTimestamp(item)).toBe(item.started_at)

    item.display_started_at = '2026-08-17T08:00:01.000Z'
    expect(requestLogDisplayTimestamp(item)).toBe(item.display_started_at)

    item.display_started_at = null
    item.started_at = null
    expect(requestLogDisplayTimestamp(item)).toBe(item.created_at)
  })

  it('projects route and credential failover details', () => {
    const item = requestDetail()
    item.failover = true
    item.attempt = 2
    item.credential_attempt = 2
    item.credential_total = 3

    expect(requestHasFailover(item)).toBe(true)
    expect(requestFailoverRouteAttempt(item)).toBe(2)
    expect(requestFailoverCredentialAttempt(item)).toEqual({ attempt: 2, total: 3 })
  })

  it('does not project failover details for the first attempt', () => {
    const item = requestDetail()
    item.attempt = 1
    item.credential_attempt = 1

    expect(requestHasFailover(item)).toBe(false)
    expect(requestFailoverRouteAttempt(item)).toBeNull()
    expect(requestFailoverCredentialAttempt(item)).toBeNull()
  })

  it('keeps credential failover detail useful when total is missing', () => {
    const item = requestDetail()
    item.failover = true
    item.credential_attempt = 2

    expect(requestFailoverCredentialAttempt(item)).toEqual({ attempt: 2, total: null })
  })

  it('projects channels in a failover trace and hides invalid channel data', () => {
    const item = requestDetail()
    item.failover_trace = {
      default_channel: {
        success: false,
        site: { name: 'Default', site_type: 'openai' },
        error_type: 'upstream_timeout',
        upstream_status_code: 504,
      },
      intermediate_channels: [
        {
          success: false,
          site: { name: 'Fallback A', site_type: 'openai' },
          error_type: 'upstream_credential_limited',
          upstream_status_code: 429,
        },
        { success: false, site: {} },
      ],
      final_channel: {
        success: true,
        site: { name: 'Fallback B', site_type: 'anthropic' },
      },
      credential_attempts: [
        {
          success: false,
          site: { name: 'Default', site_type: 'openai' },
          credential: { id: 'credential-a', name: 'Primary' },
          credential_attempt: 1,
          credential_total: 2,
          error_type: 'upstream_credential_limited',
          upstream_status_code: 429,
        },
        {
          success: true,
          site: { name: 'Default', site_type: 'openai' },
          credential: { id: 'credential-b', name: 'Backup' },
          credential_attempt: 2,
          credential_total: 2,
        },
      ],
    }

    expect(requestFailoverTrace(item)).toEqual({
      defaultChannel: {
        success: false,
        siteName: 'Default',
        siteType: 'openai',
        errorType: 'upstream_timeout',
        statusCode: 504,
      },
      intermediateChannels: [{
        success: false,
        siteName: 'Fallback A',
        siteType: 'openai',
        errorType: 'upstream_credential_limited',
        statusCode: 429,
      }],
      finalChannel: {
        success: true,
        siteName: 'Fallback B',
        siteType: 'anthropic',
        errorType: null,
        statusCode: null,
      },
      credentialAttempts: [
        {
          success: false,
          siteName: 'Default',
          siteType: 'openai',
          errorType: 'upstream_credential_limited',
          statusCode: 429,
          credentialId: 'credential-a',
          credentialName: 'Primary',
          attempt: 1,
          total: 2,
        },
        {
          success: true,
          siteName: 'Default',
          siteType: 'openai',
          errorType: null,
          statusCode: null,
          credentialId: 'credential-b',
          credentialName: 'Backup',
          attempt: 2,
          total: 2,
        },
      ],
    })
  })

  it('maps failure categories to user-facing translation keys', () => {
    const t = (key: string) => key
    const item = requestDetail()
    item.failover_trace = {
      default_channel: {
        success: false,
        site: { name: 'Default' },
        error_type: 'upstream_timeout',
      },
    }
    const trace = requestFailoverTrace(item)
    expect(trace).not.toBeNull()
    expect(requestFailoverFailureReason(trace!.defaultChannel, t)).toBe('detail.failoverFailureTimeout')
    expect(requestFailoverFailureReason({ ...trace!.defaultChannel, errorType: null, statusCode: 429 }, t)).toBe('detail.failoverFailureRateLimited')
  })

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

  it('shows the fast service multiplier and omits the standard service multiplier', () => {
    const detail = requestDetail()
    detail.pricing = { input_value: 1, output_value: 1 }
    detail.cost_calculation = {
      prompt_tokens: 10,
      completion_tokens: 5,
      base_estimated_cost: 1,
      estimated_cost: 2,
      credential_upstream_cost_multiplier: 1,
      service_tier_multiplier: 2,
      billing_mode: 'fast',
      currency: 'USD',
    }
    expect(requestCredentialMultiplier(detail)).toBe(1)
    expect(requestCostFormula(detail, (key, options) => `${key} ${String(options?.multiplier ?? '')}`)).toContain('credentialMultiplier x1 * detail.formula.serviceTierMultiplier x2')

    detail.cost_calculation.billing_mode = 'standard'
    detail.cost_calculation.service_tier_multiplier = 1
    const standardFormula = requestCostFormula(detail, (key, options) => `${key} ${String(options?.multiplier ?? '')}`)
    expect(standardFormula).toContain('credentialMultiplier x1')
    expect(standardFormula).not.toContain('serviceTierMultiplier')

    detail.cost_calculation.billing_mode = 'fast'
    expect(requestCostFormula(detail, (key, options) => `${key} ${String(options?.multiplier ?? '')}`)).not.toContain('serviceTierMultiplier')

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

  it('shows long-context billing for a charged failed request', () => {
    const detail = requestDetail()
    detail.success = false
    detail.pricing = { input_value: 10, output_value: 45, currency: 'USD' }
    detail.cost_calculation = {
      long_context: true,
      long_context_threshold_tokens: 272000,
      long_context_input_multiplier: 2,
      long_context_output_multiplier: 1.5,
      prompt_tokens: 300000,
      completion_tokens: 10000,
      estimated_cost: 3.45,
      currency: 'USD',
    }

    expect(requestHasBillingDetails(detail)).toBe(true)
    expect(requestCostFormula(detail, (key) => key)).toContain('3.45')
  })

  it('keeps unbilled failed requests collapsed while preserving successful details', () => {
    const failed = requestDetail()
    failed.success = false
    expect(requestHasBillingDetails(failed)).toBe(false)
    expect(requestHasBillingDetails(requestDetail())).toBe(true)
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
