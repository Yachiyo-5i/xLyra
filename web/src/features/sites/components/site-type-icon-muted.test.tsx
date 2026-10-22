import { createInstance } from 'i18next'
import type { ReactNode } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { I18nextProvider } from 'react-i18next'
import { beforeAll, describe, expect, it } from 'vitest'
import type { Site } from '@/features/sites/api/sites'
import translations from '@/locales/zh/sites.json'
import { MobileSitesList } from '@/features/sites/components/mobile-sites-list'
import { SiteTypeIcon } from '@/features/sites/components/site-type-icon'
import { SitesTable } from '@/features/sites/components/sites-table'

const i18n = createInstance()
const iconURL = '/api/v1/plugin-icons/acme.example'

beforeAll(async () => {
  await i18n.init({ lng: 'zh', resources: { zh: { sites: translations } }, interpolation: { escapeValue: false } })
})

function pluginSite(status: 'enabled' | 'unavailable'): Site {
  return {
    id: `acme-${status}`,
    name: 'Acme Pool',
    slug: 'acme-pool',
    site_type: 'acme.example/pool',
    icon_url: iconURL,
    provider_status: status,
    base_url: 'https://api.example.com',
    status: 'active',
    enabled: true,
    routing_priority: 0,
    meta: {},
    created_at: '2026-10-21T00:00:00Z',
    updated_at: '2026-10-21T00:00:00Z',
  }
}

function renderWithSites(node: ReactNode) {
  return renderToStaticMarkup(<I18nextProvider i18n={i18n}>{node}</I18nextProvider>)
}

function iconTag(markup: string) {
  const tags = markup.match(/<img\b[^>]*>/g) ?? []
  return tags.find((tag) => tag.includes(iconURL)) ?? ''
}

const listProps = {
  modelsMap: {},
  apiKeysMap: {},
  validationSnapshots: {},
  refreshingSiteIds: [] as string[],
  togglingSiteId: null,
  deletingSiteId: null,
  updatedAtMode: 'absolute' as const,
  now: Date.parse('2026-10-21T00:00:00Z'),
  onUpdatedAtModeChange: () => {},
  onRefresh: () => {},
  onToggleEnabled: () => {},
  onEdit: () => {},
  onDelete: () => {},
  onOpenModels: () => {},
  onOpenAPIKeys: () => {},
  onOpenAccounts: () => {},
  onOpenTest: () => {},
  onOpenUsageSplit: () => {},
  siteTypes: [],
}

describe('SiteTypeIcon muted', () => {
  it('grays the image and the letter fallback, and leaves an available icon alone', () => {
    const image = renderToStaticMarkup(
      <SiteTypeIcon src="https://cdn.example/icon.webp" name="Acme" muted className="h-8 w-8" />,
    )
    expect(image).toContain('<img')
    expect(image).toContain('grayscale')
    expect(image).toContain('opacity-60')

    const letter = renderToStaticMarkup(<SiteTypeIcon src="" name="Acme" muted className="h-8 w-8" />)
    expect(letter).not.toContain('<img')
    expect(letter).toContain('A')
    expect(letter).toContain('grayscale')
    expect(letter).toContain('opacity-60')

    const plainImage = renderToStaticMarkup(
      <SiteTypeIcon src="https://cdn.example/icon.webp" name="Acme" className="h-8 w-8" />,
    )
    const plainLetter = renderToStaticMarkup(<SiteTypeIcon src="" name="Acme" className="h-8 w-8" />)
    expect(plainImage).not.toContain('grayscale')
    expect(plainImage).not.toContain('opacity-60')
    expect(plainLetter).toContain('A')
    expect(plainLetter).not.toContain('grayscale')
    expect(plainLetter).not.toContain('opacity-60')
  })
})

describe('unavailable provider icon on the sites page', () => {
  it('passes muted from the table when the provider is unavailable', () => {
    const unavailable = iconTag(renderWithSites(<SitesTable items={[pluginSite('unavailable')]} {...listProps} />))
    const enabled = iconTag(renderWithSites(<SitesTable items={[pluginSite('enabled')]} {...listProps} />))
    expect(unavailable).toContain(iconURL)
    expect(unavailable).toContain('grayscale')
    expect(unavailable).toContain('opacity-60')
    expect(enabled).toContain(iconURL)
    expect(enabled).not.toContain('grayscale')
    expect(enabled).not.toContain('opacity-60')
  })

  it('passes muted from the mobile list when the provider is unavailable', () => {
    const unavailable = iconTag(renderWithSites(<MobileSitesList items={[pluginSite('unavailable')]} {...listProps} />))
    const enabled = iconTag(renderWithSites(<MobileSitesList items={[pluginSite('enabled')]} {...listProps} />))
    expect(unavailable).toContain(iconURL)
    expect(unavailable).toContain('grayscale')
    expect(unavailable).toContain('opacity-60')
    expect(enabled).toContain(iconURL)
    expect(enabled).not.toContain('grayscale')
    expect(enabled).not.toContain('opacity-60')
  })
})
