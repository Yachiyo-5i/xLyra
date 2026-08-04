import { describe, expect, it } from 'vitest'
import type { RequestLogDetail } from '@/features/requests/api/requests'
import { requestDownstreamTransportLabel } from '@/features/requests/lib/request-utils'

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
