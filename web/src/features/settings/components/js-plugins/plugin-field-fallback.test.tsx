import { createInstance } from 'i18next'
import { renderToStaticMarkup } from 'react-dom/server'
import { I18nextProvider } from 'react-i18next'
import { beforeAll, describe, expect, it } from 'vitest'
import { siteProviderOf } from '@/features/settings/api/js-plugins'
import { JSPluginSiteProviderSection } from '@/features/settings/components/js-plugins/js-plugin-site-provider'
import { resolveText } from '@/features/settings/lib/localized-text'
import translations from '@/locales/zh/settings.json'

const i18n = createInstance()

beforeAll(async () => {
  await i18n.init({
    lng: 'zh',
    resources: { zh: { settings: translations } },
    ns: ['settings'],
    defaultNS: 'settings',
    interpolation: { escapeValue: false },
  })
})

describe('siteProviderOf', () => {
  it('degrades when the site provider section omits fields', () => {
    expect(siteProviderOf(undefined)).toBeNull()
    expect(siteProviderOf({ kind: 'quota_probe' })).toBeNull()
    expect(siteProviderOf({ kind: 'site_provider' })).toBeNull()
    expect(siteProviderOf({ kind: 'site_provider', siteProvider: [] })).toBeNull()
    expect(siteProviderOf({
      kind: 'site_provider',
      siteProvider: { hosts: [' api.example.com ', '', 1, 'api.example.com'] },
    })).toEqual({ siteType: '', displayName: undefined, hosts: [' api.example.com ', 'api.example.com'] })
    expect(siteProviderOf({
      kind: 'site_provider',
      siteProvider: { siteType: 'acme.example/chat', display: { name: '示例' }, hosts: 'api.example.com' },
    })).toEqual({ siteType: 'acme.example/chat', displayName: '示例', hosts: [] })
  })

  it('falls back when display text has no entry for the language', () => {
    const view = siteProviderOf({
      kind: 'site_provider',
      siteProvider: { siteType: 'acme.example/chat', display: { name: { en: 'Example' } } },
    })
    expect(resolveText(view?.displayName, 'zh')).toBe('Example')
    expect(resolveText(undefined, 'zh') ?? '').toBe('')
    expect(resolveText({ zh: '', en: '' }, 'zh')).toBeUndefined()
  })
})

describe('JSPluginSiteProviderSection', () => {
  it('shows a dash for a missing site type, name, or host list', () => {
    const markup = renderToStaticMarkup(
      <I18nextProvider i18n={i18n}>
        <JSPluginSiteProviderSection items={[{
          version: '1.0.0',
          siteType: '',
          displayName: '',
          hosts: [],
          grantedHosts: [],
        }]} />
      </I18nextProvider>,
    )
    expect(markup).toContain('站点类型')
    expect(markup.match(/—/g)?.length).toBe(3)
    expect(markup).not.toContain('1.0.0')
  })
})
