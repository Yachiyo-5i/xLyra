import { describe, expect, it } from 'vitest'

import type { Site, SiteModel } from '@/features/sites/api/sites'
import {
  editSiteFormResetKey,
  mergeSavedSiteDetail,
  removeListSite,
  selectEditingSite,
  siteModelItems,
} from '@/features/sites/lib/site-cache'

function site(id: string, overrides: Partial<Site> = {}): Site {
  return {
    id,
    name: id,
    slug: id,
    site_type: 'openai',
    base_url: 'https://example.invalid',
    status: 'active',
    enabled: true,
    routing_priority: 0,
    meta: {},
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...overrides,
  }
}

describe('removeListSite', () => {
  it('removes only the target and updates the list count', () => {
    const result = removeListSite(
      { items: [site('site-1'), site('site-2')], meta: { count: 2 } },
      'site-1',
    )

    expect(result).toEqual({
      items: [site('site-2')],
      meta: { count: 1 },
    })
  })
})

describe('mergeSavedSiteDetail', () => {
  it('keeps the saved row and the credentials already loaded for editing', () => {
    const previous = site('site-1', {
      name: 'Old',
      updated_at: '2026-04-01T00:00:00Z',
      auth_config: { newapi: { access_token: 'token-old', user_id: 7 } },
    })
    const saved = site('site-1', {
      name: 'New',
      base_url: 'https://new.example',
      updated_at: '2026-04-02T00:00:00Z',
      meta: { request_headers: [{ key: 'X-Trace', value: 'on' }] },
    })

    const merged = mergeSavedSiteDetail(previous, saved)

    expect(merged.name).toBe('New')
    expect(merged.base_url).toBe('https://new.example')
    expect(merged.meta).toEqual({ request_headers: [{ key: 'X-Trace', value: 'on' }] })
    expect(merged.auth_config?.newapi).toEqual({ access_token: 'token-old', user_id: 7 })
  })

  it('replaces credentials that were part of this submit', () => {
    const previous = site('site-1', {
      auth_config: {
        newapi: { access_token: 'token-old', user_id: 7 },
        xlyra: { auth_mode: 'access_token', access_token: 'x-old' },
      },
    })

    const merged = mergeSavedSiteDetail(previous, site('site-1'), {
      newapi: { accessToken: 'token-new', userId: 9 },
      xlyra: { authMode: 'api_key' },
    })

    expect(merged.auth_config).toEqual({
      newapi: { access_token: 'token-new', user_id: 9 },
      xlyra: { auth_mode: 'api_key' },
    })
  })
})

describe('selectEditingSite', () => {
  it('uses the detail snapshot when it is at least as new as the list row', () => {
    const detail = site('site-1', {
      name: 'Saved',
      updated_at: '2026-04-02T00:00:00Z',
      auth_config: { newapi: { access_token: 'token', user_id: 3 } },
    })
    const listed = site('site-1', { name: 'Listed', updated_at: '2026-04-02T00:00:00Z' })

    expect(selectEditingSite(detail, listed)).toBe(detail)
  })

  it('overlays a newer list row without dropping credentials from the detail snapshot', () => {
    const detail = site('site-1', {
      name: 'Old',
      updated_at: '2026-04-01T00:00:00Z',
      auth_config: { newapi: { access_token: 'token', user_id: 3 } },
    })
    const listed = site('site-1', { name: 'Renamed', updated_at: '2026-04-03T00:00:00Z' })

    const selected = selectEditingSite(detail, listed)

    expect(selected?.name).toBe('Renamed')
    expect(selected?.auth_config?.newapi?.access_token).toBe('token')
  })
})

describe('edit form refresh', () => {
  it('changes the reset key when the saved revision or group membership changes', () => {
    const current = site('site-1', { updated_at: '2026-04-01T00:00:00Z' })
    const renamed = site('site-1', { name: 'Renamed', updated_at: '2026-04-02T00:00:00Z' })

    expect(editSiteFormResetKey(current, ['group-a'])).not.toBe(editSiteFormResetKey(renamed, ['group-a']))
    expect(editSiteFormResetKey(current, ['group-a'])).not.toBe(editSiteFormResetKey(current, ['group-b']))
  })
})

describe('siteModelItems', () => {
  it('reads models from the shared query response shape', () => {
    const models = [{ id: 'model-1' }] as SiteModel[]

    expect(siteModelItems({ items: models })).toBe(models)
    expect(siteModelItems(undefined)).toEqual([])
  })
})
