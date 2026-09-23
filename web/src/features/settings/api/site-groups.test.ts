import { describe, expect, it } from 'vitest'

import { applySiteGroupMembership, type SiteGroup } from '@/features/settings/api/site-groups'

function group(id: string, siteIds: string[]): SiteGroup {
  return {
    id,
    name: id,
    slug: id,
    description: '',
    enabled: true,
    sort_order: 0,
    sites: siteIds.map((siteId) => ({ id: siteId, group_id: id, site_id: siteId })),
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  }
}

describe('applySiteGroupMembership', () => {
  it('moves a site to the groups selected in the edit form', () => {
    const current = { items: [group('alpha', ['site-1']), group('beta', [])] }

    const next = applySiteGroupMembership(current, 'site-1', ['beta'])

    expect(next?.items.map((item) => [item.id, item.sites.map((site) => site.site_id)])).toEqual([
      ['alpha', []],
      ['beta', ['site-1']],
    ])
  })
})
