import { afterEach, describe, expect, it, vi } from 'vitest'
import { getAnalyticsUsage } from './analytics'

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('analytics api', () => {
  it('can omit contribution data for bounded server aggregation fallback', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response('{}', {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    }))
    vi.stubGlobal('fetch', fetchMock)

    await getAnalyticsUsage({ from: '2026-08-01', to: '2026-08-17', include_contributions: false })

    expect(fetchMock).toHaveBeenCalledOnce()
    expect(fetchMock.mock.calls[0]?.[0]).toBe('/api/v1/analytics/usage?from=2026-08-01&to=2026-08-17&include_contributions=false')
  })
})
