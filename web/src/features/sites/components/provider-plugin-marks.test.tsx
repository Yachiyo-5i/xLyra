import { createInstance } from 'i18next'
import { renderToStaticMarkup } from 'react-dom/server'
import { I18nextProvider } from 'react-i18next'
import { beforeAll, describe, expect, it } from 'vitest'
import enSites from '@/locales/en/sites.json'
import jpSites from '@/locales/jp/sites.json'
import zhSites from '@/locales/zh/sites.json'
import { ProviderUnavailableBadge } from '@/features/sites/components/provider-unavailable-badge'
import { SiteTypePluginTag } from '@/features/sites/components/site-type-plugin-tag'

const i18n = createInstance()

beforeAll(async () => {
  await i18n.init({
    lng: 'zh',
    fallbackLng: 'zh',
    resources: { zh: { sites: zhSites } },
    ns: ['sites'],
    defaultNS: 'sites',
    interpolation: { escapeValue: false },
  })
})

function renderBadge(status?: string) {
  return renderToStaticMarkup(
    <I18nextProvider i18n={i18n}>
      <ProviderUnavailableBadge status={status} />
    </I18nextProvider>,
  )
}

function renderTag(pluginId?: string) {
  return renderToStaticMarkup(
    <I18nextProvider i18n={i18n}>
      <SiteTypePluginTag pluginId={pluginId} />
    </I18nextProvider>,
  )
}

function dig(source: unknown, path: string) {
  return path.split('.').reduce<unknown>((node, key) => (
    node && typeof node === 'object' ? (node as Record<string, unknown>)[key] : undefined
  ), source)
}

describe('ProviderUnavailableBadge', () => {
  it('shows the warning only when the provider plugin is unavailable', () => {
    const unavailable = renderBadge('unavailable')
    expect(unavailable).toContain('提供方插件未启用')
    expect(unavailable).toContain('该站点类型由插件提供，插件已停用或卸载；站点数据保留，但不参与路由')
    expect(renderBadge('enabled')).toBe('')
    expect(renderBadge(undefined)).toBe('')
  })
})

describe('SiteTypePluginTag', () => {
  it('shows a plugin tag on the type wall when the type comes from a plugin', () => {
    expect(renderTag('acme.example')).toContain('插件')
    expect(renderTag(undefined)).toBe('')
    expect(renderTag('')).toBe('')
  })
})

describe('site provider locale keys', () => {
  it('exists in zh, en and jp', () => {
    for (const key of ['table.providerUnavailable', 'table.providerUnavailableHint', 'form.pluginTag']) {
      expect(typeof dig(zhSites, key)).toBe('string')
      expect(typeof dig(enSites, key)).toBe('string')
      expect(typeof dig(jpSites, key)).toBe('string')
    }
  })
})
