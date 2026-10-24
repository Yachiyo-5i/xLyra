import { createInstance } from 'i18next'
import { renderToStaticMarkup } from 'react-dom/server'
import { I18nextProvider } from 'react-i18next'
import { beforeAll, describe, expect, it } from 'vitest'
import { siteProviderOf } from '@/features/settings/api/js-plugins'
import { JSPluginSiteProviderSection } from '@/features/settings/components/js-plugins/js-plugin-site-provider'
import { pickCurrentVersionRow, type CurrentVersionRow } from '@/features/settings/components/js-plugins/site-provider-version'
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

function providerVersion(fields: {
  version: string
  status: string
  created_at?: string
  siteType?: string
  displayName?: string
  hosts?: string[]
  grantedHosts?: string[]
  siteProvider?: boolean
}) {
  return {
    version: fields.version,
    status: fields.status,
    created_at: fields.created_at,
    granted_permissions: (fields.grantedHosts ?? []).map((host) => `host:${host}`),
    manifest: fields.siteProvider === false
      ? { kind: 'quota_probe' }
      : {
          kind: 'site_provider',
          siteProvider: {
            siteType: fields.siteType ?? '',
            display: { name: fields.displayName ?? '' },
            hosts: fields.hosts ?? [],
          },
        },
  }
}

function renderSection(versions: readonly {
  version: string
  status: string
  created_at?: string
  manifest?: Record<string, unknown>
  granted_permissions?: string[]
}[]) {
  return renderToStaticMarkup(
    <I18nextProvider i18n={i18n}>
      <JSPluginSiteProviderSection versions={versions} />
    </I18nextProvider>,
  )
}

describe('pickCurrentVersionRow', () => {
  it('prefers an enabled row, then the newest enabled row', () => {
    const versions: CurrentVersionRow[] = [
      { version: '9.0.0', status: 'disabled', created_at: '2026-06-01T00:00:00Z' },
      { version: '1.0.0', status: 'enabled', created_at: '2026-01-01T00:00:00Z' },
      { version: '1.1.0', status: 'enabled', created_at: '2026-02-01T00:00:00Z' },
    ]
    expect(pickCurrentVersionRow(versions)?.version).toBe('1.1.0')
    expect(versions.map((row) => row.version)).toEqual(['9.0.0', '1.0.0', '1.1.0'])
  })

  it('uses created_at descending when nothing is enabled', () => {
    expect(pickCurrentVersionRow([
      { version: '2.0.0', status: 'disabled', created_at: '2026-01-01T00:00:00Z' },
      { version: '1.0.0', status: 'disabled', created_at: '2026-03-01T00:00:00Z' },
    ])?.version).toBe('1.0.0')
    expect(pickCurrentVersionRow([
      { version: '1.0.0', status: 'disabled', created_at: '2026-01-01T00:00:00Z' },
      { version: '1.1.0', status: 'disabled', created_at: '2026-01-01T00:00:00Z' },
    ])?.version).toBe('1.1.0')
  })

  it('falls back to semver, then the version string, when created_at is missing', () => {
    expect(pickCurrentVersionRow([
      { version: '1.9.0', status: 'disabled' },
      { version: '1.10.0', status: 'disabled' },
    ])?.version).toBe('1.10.0')
    expect(pickCurrentVersionRow([
      { version: 'v1.2.0', status: 'disabled' },
      { version: 'v1.10.0', status: 'disabled' },
    ])?.version).toBe('v1.10.0')
    expect(pickCurrentVersionRow([
      { version: '1.0.0-beta', status: 'disabled', created_at: 'not-a-date' },
      { version: '1.0.0', status: 'disabled' },
    ])?.version).toBe('1.0.0')
    expect(pickCurrentVersionRow([
      { version: 'alpha', status: 'disabled' },
      { version: 'beta', status: 'disabled' },
    ])?.version).toBe('beta')
    expect(pickCurrentVersionRow([
      { version: '9.0.0', status: 'disabled' },
      { version: '1.0.0', status: 'disabled', created_at: '2020-01-01T00:00:00Z' },
    ])?.version).toBe('1.0.0')
    expect(pickCurrentVersionRow([])).toBeNull()
  })
})

describe('JSPluginSiteProviderSection', () => {
  it('shows a dash for a missing site type, name, or host list', () => {
    const markup = renderSection([{
      version: '1.0.0',
      status: 'enabled',
      manifest: { kind: 'site_provider', siteProvider: {} },
    }])
    expect(markup).toContain('站点类型')
    expect(markup).toContain('1.0.0')
    expect(markup).toContain('当前启用')
    expect(markup.match(/—/g)?.length).toBe(3)
    expect(markup).not.toContain('已授予的主机权限')
  })

  it('renders the enabled version once', () => {
    const markup = renderSection([
      providerVersion({
        version: '1.0.0',
        status: 'disabled',
        created_at: '2026-03-01T00:00:00Z',
        siteType: 'acme.example/pool',
        displayName: '示例池',
        hosts: ['old.example'],
        grantedHosts: ['old.example'],
      }),
      providerVersion({
        version: '2.0.0',
        status: 'enabled',
        created_at: '2026-01-01T00:00:00Z',
        siteType: 'acme.example/pool',
        displayName: '示例池',
        hosts: ['api.example.com'],
        grantedHosts: ['files.example'],
      }),
    ])
    expect(markup.match(/acme\.example\/pool/g)?.length).toBe(1)
    expect(markup.match(/api\.example\.com/g)?.length).toBe(1)
    expect(markup).not.toContain('old.example')
    expect(markup).not.toContain('1.0.0')
    expect(markup).toContain('2.0.0')
    expect(markup).toContain('当前启用')
    expect(markup).not.toContain('最新版本(未启用)')
    expect(markup).toContain('host:files.example')
    expect(markup).toContain('示例池')
  })

  it('renders the latest version when none is enabled', () => {
    const markup = renderSection([
      providerVersion({
        version: '1.2.0',
        status: 'disabled',
        created_at: '2026-01-01T00:00:00Z',
        siteType: 'acme.example/pool',
        displayName: '示例池',
        hosts: ['old.example'],
      }),
      providerVersion({
        version: '1.0.0',
        status: 'disabled',
        created_at: '2026-04-01T00:00:00Z',
        siteType: 'acme.example/pool',
        displayName: '示例池',
        hosts: ['api.example.com'],
      }),
    ])
    expect(markup.match(/acme\.example\/pool/g)?.length).toBe(1)
    expect(markup.match(/api\.example\.com/g)?.length).toBe(1)
    expect(markup).not.toContain('old.example')
    expect(markup).not.toContain('1.2.0')
    expect(markup).toContain('1.0.0')
    expect(markup).toContain('最新版本(未启用)')
    expect(markup).not.toContain('当前启用')
    expect(markup).not.toContain('已授予的主机权限')
  })

  it('hides the section when the current version has no site provider', () => {
    const enabledOther = renderSection([
      providerVersion({
        version: '2.0.0',
        status: 'enabled',
        created_at: '2026-02-01T00:00:00Z',
        siteProvider: false,
      }),
      providerVersion({
        version: '1.0.0',
        status: 'disabled',
        created_at: '2026-01-01T00:00:00Z',
        siteType: 'acme.example/pool',
        hosts: ['api.example.com'],
      }),
    ])
    expect(enabledOther).toBe('')

    const latestOther = renderSection([
      providerVersion({
        version: '3.0.0',
        status: 'disabled',
        created_at: '2026-05-01T00:00:00Z',
        siteProvider: false,
      }),
      providerVersion({
        version: '1.4.0',
        status: 'disabled',
        created_at: '2026-01-01T00:00:00Z',
        siteType: 'acme.example/pool',
        hosts: ['api.example.com'],
      }),
    ])
    expect(latestOther).toBe('')
  })
})
