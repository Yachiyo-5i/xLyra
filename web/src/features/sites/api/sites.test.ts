import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  deleteAccount,
  importAccounts,
  listAccountRegions,
  listAccounts,
  pollAccountLogin,
  refreshAccount,
  refreshAccounts,
  sitesQueryKeys,
  startAccountLogin,
  updateAccount,
  updateAccountModel,
  updateSite,
} from './sites'

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

function stubFetch(body: unknown = {}, status = 200) {
  const fetchMock = vi.fn().mockResolvedValue(
    new Response(status === 204 ? null : JSON.stringify(body), {
      status,
      headers: status === 204 ? undefined : { 'Content-Type': 'application/json' },
    }),
  )
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function sentRequest(fetchMock: ReturnType<typeof vi.fn>) {
  const [url, request] = fetchMock.mock.calls[0] as [string, RequestInit]
  return {
    url,
    method: request.method ?? 'GET',
    body: typeof request.body === 'string' ? JSON.parse(request.body) as unknown : undefined,
  }
}

describe('account api', () => {
  it('lists accounts', async () => {
    const fetchMock = stubFetch({ items: [], meta: { count: 0 } })
    await listAccounts('site-1')
    expect(sentRequest(fetchMock)).toEqual({
      url: '/api/v1/sites/site-1/accounts',
      method: 'GET',
      body: undefined,
    })
  })

  it('updates an account', async () => {
    const fetchMock = stubFetch({ account: {} })
    await updateAccount('site-1', 'cred-1', { enabled: false })
    expect(sentRequest(fetchMock)).toEqual({
      url: '/api/v1/sites/site-1/accounts/cred-1',
      method: 'PUT',
      body: { enabled: false },
    })
  })

  it('deletes an account', async () => {
    const fetchMock = stubFetch(undefined, 204)
    await deleteAccount('site-1', 'cred-1')
    expect(sentRequest(fetchMock)).toEqual({
      url: '/api/v1/sites/site-1/accounts/cred-1',
      method: 'DELETE',
      body: undefined,
    })
  })

  it('refreshes one account', async () => {
    const fetchMock = stubFetch({ account: {} })
    await refreshAccount('site-1', 'cred-1')
    expect(sentRequest(fetchMock)).toEqual({
      url: '/api/v1/sites/site-1/accounts/cred-1/refresh',
      method: 'POST',
      body: undefined,
    })
  })

  it('refreshes every account and keeps meta.failed', async () => {
    const fetchMock = stubFetch({
      items: [
        { credential_id: 'cred-1', error: 'boom' },
        { credential_id: 'cred-2', account: { credential_id: 'cred-2', masked_token: 'sk-…', enabled: true } },
      ],
      meta: { count: 2, failed: 1 },
    })
    const result = await refreshAccounts('site-1')
    expect(sentRequest(fetchMock)).toEqual({
      url: '/api/v1/sites/site-1/accounts/refresh',
      method: 'POST',
      body: undefined,
    })
    const failed: number = result.meta.failed
    expect(failed).toBe(1)
    expect(result.meta.count).toBe(2)
    expect(result.items[0]).toEqual({ credential_id: 'cred-1', error: 'boom' })
    expect(result.items[1]?.account?.credential_id).toBe('cred-2')
  })

  it('updates an account model', async () => {
    const fetchMock = stubFetch({ account: {} })
    await updateAccountModel('site-1', 'cred-1', {
      model: 'grok-2',
      enabled: true,
      siteModelId: 'model-1',
      endpointMode: 'allowlist',
      endpointTypes: ['chat'],
    })
    expect(sentRequest(fetchMock)).toEqual({
      url: '/api/v1/sites/site-1/accounts/cred-1/models',
      method: 'PUT',
      body: {
        model: 'grok-2',
        enabled: true,
        site_model_id: 'model-1',
        endpoint_mode: 'allowlist',
        endpoint_types: ['chat'],
      },
    })
  })

  it('starts login without a region', async () => {
    const fetchMock = stubFetch({ login: {} })
    await startAccountLogin('site-1')
    expect(sentRequest(fetchMock)).toEqual({
      url: '/api/v1/sites/site-1/accounts/login/start',
      method: 'POST',
      body: {},
    })
  })

  it('starts login with a region', async () => {
    const fetchMock = stubFetch({ login: {} })
    await startAccountLogin('site-1', 'cn')
    expect(sentRequest(fetchMock)).toEqual({
      url: '/api/v1/sites/site-1/accounts/login/start',
      method: 'POST',
      body: { region: 'cn' },
    })
  })

  it('polls login with session and credential_id', async () => {
    const fetchMock = stubFetch({ login: { status: 'pending' } })
    await pollAccountLogin('site-1', 'sess-1', 'cred-1')
    expect(sentRequest(fetchMock)).toEqual({
      url: '/api/v1/sites/site-1/accounts/login/poll',
      method: 'POST',
      body: { session: 'sess-1', credential_id: 'cred-1' },
    })
  })

  it('polls login without credential_id', async () => {
    const fetchMock = stubFetch({ login: { status: 'complete' } })
    await pollAccountLogin('site-1', 'sess-1')
    expect(sentRequest(fetchMock).body).toEqual({ session: 'sess-1' })
  })

  it('imports account files', async () => {
    const fetchMock = stubFetch({ items: [], meta: { total: 0, imported: 0, failed: 0 } })
    await importAccounts('site-1', [{ name: 'a.json', content: '{"token":"x"}' }])
    expect(sentRequest(fetchMock)).toEqual({
      url: '/api/v1/sites/site-1/accounts/import',
      method: 'POST',
      body: { files: [{ name: 'a.json', content: '{"token":"x"}' }] },
    })
  })

  it('lists account regions', async () => {
    const fetchMock = stubFetch({ items: [], meta: { count: 0, default: 'cn' } })
    await listAccountRegions('site-1')
    expect(sentRequest(fetchMock)).toEqual({
      url: '/api/v1/sites/site-1/accounts/regions',
      method: 'GET',
      body: undefined,
    })
  })

  it('nests account query keys under the site detail', () => {
    expect(sitesQueryKeys.accounts('site-1')).toEqual(['sites', 'detail', 'site-1', 'accounts'])
    expect(sitesQueryKeys.accountRegions('site-1')).toEqual(['sites', 'detail', 'site-1', 'accounts', 'regions'])
  })
})
