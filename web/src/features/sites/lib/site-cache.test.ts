import { describe, expect, it } from 'vitest'

import type { Site, SiteModel } from '@/features/sites/api/sites'
import { removeListSite, siteModelItems } from '@/features/sites/lib/site-cache'

function site(id: string): Site {
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

describe('siteModelItems', () => {
  it('reads models from the shared query response shape', () => {
    const models = [{ id: 'model-1' }] as SiteModel[]

    expect(siteModelItems({ items: models })).toBe(models)
    expect(siteModelItems(undefined)).toEqual([])
  })
})
