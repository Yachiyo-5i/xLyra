import { createInstance } from 'i18next'
import { renderToStaticMarkup } from 'react-dom/server'
import { I18nextProvider } from 'react-i18next'
import { beforeAll, describe, expect, it } from 'vitest'
import type { Site, SiteAPIKey } from '@/features/sites/api/sites'
import translations from '@/locales/zh/sites.json'
import { MobileSitesList } from './mobile-sites-list'
import { SiteBalanceCell, SiteBalanceDetailsContent } from './site-balance-cell'

const i18n = createInstance()
const site: Site = {
  id: 'glm-site',
  name: 'GLM',
  slug: 'glm',
  site_type: 'glm_code',
  base_url: 'https://open.bigmodel.cn/api/coding/paas/v4',
  status: 'active',
  enabled: true,
  routing_priority: 0,
  meta: {},
  created_at: '2026-09-19T00:00:00Z',
  updated_at: '2026-09-19T00:00:00Z',
  quota_probe: {
    probe_type: 'glm',
    unit: 'percent',
    entries: [
      { label: 'monthly', unit: 'percent', remaining: 0, reset_at: '2026-10-19T00:00:00Z' },
      { label: 'weekly', unit: 'percent', remaining: 73.5 },
      { label: 'five_hour', unit: 'percent', remaining: 85.7 },
    ],
  },
}

beforeAll(async () => {
  await i18n.init({ lng: 'zh', resources: { zh: { sites: translations } }, interpolation: { escapeValue: false } })
})

describe('GLM quota details', () => {
  it('renders separate quota rows in the shared tooltip and drawer content', () => {
    const markup = renderToStaticMarkup(
      <I18nextProvider i18n={i18n}>
        <SiteBalanceDetailsContent site={site} />
      </I18nextProvider>,
    )
    const rows = [...markup.matchAll(/<div[^>]*><span[^>]*>([^<]+)<\/span><span[^>]*>([^<]+)<\/span><\/div>/g)]
      .map((match) => [match[1], match[2]])
    expect(rows).toEqual([
      ['5 小时额度', '剩余 85.7%'],
      ['周额度', '剩余 73.5%'],
      ['MCP 月度额度', expect.stringContaining('剩余 0% · ')],
    ])
  })

  it('includes all quota rows in the desktop tooltip description', () => {
    const markup = renderToStaticMarkup(
      <I18nextProvider i18n={i18n}>
        <SiteBalanceCell site={site} />
      </I18nextProvider>,
    )
    expect(markup).toContain('role="tooltip"')
    expect(markup).toContain('5 小时额度 剩余 85.7%, 周额度 剩余 73.5%, MCP 月度额度 剩余 0%')
  })

  it('makes the mobile GLM balance a drawer trigger without requiring loaded API keys', () => {
    const noop = () => {}
    const markup = renderToStaticMarkup(
      <I18nextProvider i18n={i18n}>
        <MobileSitesList
          items={[site]}
          modelsMap={{}}
          apiKeysMap={{}}
          validationSnapshots={{}}
          refreshingSiteIds={[]}
          togglingSiteId={null}
          deletingSiteId={null}
          updatedAtMode="absolute"
          now={Date.parse(site.updated_at)}
          onUpdatedAtModeChange={noop}
          onRefresh={noop}
          onToggleEnabled={noop}
          onEdit={noop}
          onDelete={noop}
          onOpenModels={noop}
          onOpenAPIKeys={noop}
          onOpenGrokAccounts={noop}
          onOpenTest={noop}
          onOpenUsageSplit={noop}
          resolvedMode="light"
          siteTypes={[]}
        />
      </I18nextProvider>,
    )
    const buttons = markup.match(/<button\b[^>]*>[\s\S]*?<\/button>/g) ?? []
    const balance = buttons.find((button) => button.includes('85.7% / 73.5%'))
    expect(balance).toContain('aria-haspopup="dialog"')
  })
})

describe('DeepSeek balance details', () => {
  const site: Site = {
    id: 'deepseek-site',
    name: 'DeepSeek',
    slug: 'deepseek',
    site_type: 'deepseek',
    base_url: 'https://api.deepseek.com',
    status: 'active',
    enabled: true,
    routing_priority: 0,
    meta: {},
    created_at: '2026-09-19T00:00:00Z',
    updated_at: '2026-09-19T00:00:00Z',
    quota_probe: {
      probe_type: 'deepseek',
      unit: 'cny',
      entries: [
        { label: 'balance', unit: 'cny', remaining: -0.5, granted_balance: 0, topped_up_balance: -0.5 },
        { label: 'balance', unit: 'usd', remaining: 2.5, granted_balance: 1, topped_up_balance: 1.5 },
      ],
    },
  }

  it.each([false, true])('renders separate quota rows with API keys loaded: %s', (keysLoaded) => {
    const apiKeys: SiteAPIKey[] = keysLoaded ? [{
      id: 'deepseek-key',
      name: 'DeepSeek key',
      key: 'sk-***',
      routing_priority: 0,
      upstream_cost_multiplier: 1,
      status: 'active',
      enabled: true,
      models: [],
      quota_probe: { status: 'ok', kind: 'balance', entries: site.quota_probe?.entries },
    }] : []
    const markup = renderToStaticMarkup(
      <I18nextProvider i18n={i18n}>
        <SiteBalanceDetailsContent site={site} apiKeys={apiKeys} />
      </I18nextProvider>,
    )
    const rows = [...markup.matchAll(/<div[^>]*><span[^>]*>([^<]+)<\/span><span[^>]*>([^<]+)<\/span><\/div>/g)]
      .map((match) => [match[1], match[2]])
    expect(rows).toEqual([
      ['账户余额', '-¥0.50'],
      ['赠送余额', '¥0.00'],
      ['充值余额', '-¥0.50'],
      ['账户余额', '$2.50'],
      ['赠送余额', '$1.00'],
      ['充值余额', '$1.50'],
    ])
  })

  it('includes all quota rows in the desktop tooltip description', () => {
    const markup = renderToStaticMarkup(
      <I18nextProvider i18n={i18n}>
        <SiteBalanceCell site={site} />
      </I18nextProvider>,
    )
    expect(markup).toContain('role="tooltip"')
    expect(markup).toContain('账户余额 -¥0.50, 赠送余额 ¥0.00, 充值余额 -¥0.50, 账户余额 $2.50')
  })

  it('makes the mobile DeepSeek balance a drawer trigger without requiring loaded API keys', () => {
    const noop = () => {}
    const markup = renderToStaticMarkup(
      <I18nextProvider i18n={i18n}>
        <MobileSitesList
          items={[site]}
          modelsMap={{}}
          apiKeysMap={{}}
          validationSnapshots={{}}
          refreshingSiteIds={[]}
          togglingSiteId={null}
          deletingSiteId={null}
          updatedAtMode="absolute"
          now={Date.parse(site.updated_at)}
          onUpdatedAtModeChange={noop}
          onRefresh={noop}
          onToggleEnabled={noop}
          onEdit={noop}
          onDelete={noop}
          onOpenModels={noop}
          onOpenAPIKeys={noop}
          onOpenGrokAccounts={noop}
          onOpenTest={noop}
          onOpenUsageSplit={noop}
          resolvedMode="light"
          siteTypes={[]}
        />
      </I18nextProvider>,
    )
    const buttons = markup.match(/<button\b[^>]*>[\s\S]*?<\/button>/g) ?? []
    const balance = buttons.find((button) => button.includes('-¥0.50 / $2.50'))
    expect(balance).toContain('aria-haspopup="dialog"')
  })
})
