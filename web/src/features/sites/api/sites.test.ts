import { afterEach, describe, expect, it, vi } from 'vitest'
import { updateSite } from './sites'

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('updateSite', () => {
  it('sends an empty request_headers array when all custom headers are removed', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ site: {} }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    )
    vi.stubGlobal('fetch', fetchMock)

    await updateSite('site-1', {
      name: 'Example',
      slug: 'example',
      siteType: 'openai',
      baseUrl: 'https://example.com',
      enabled: true,
      routingPriority: 1,
      requestHeaders: [],
    })

    const [, request] = fetchMock.mock.calls[0]
    expect(JSON.parse(request.body)).toMatchObject({ request_headers: [] })
  })
})
