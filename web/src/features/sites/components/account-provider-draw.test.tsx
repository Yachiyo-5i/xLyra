import { createInstance } from 'i18next'
import { renderToStaticMarkup } from 'react-dom/server'
import { I18nextProvider } from 'react-i18next'
import { beforeAll, describe, expect, it } from 'vitest'
import type { ReactNode } from 'react'
import type { AccountLoginStart, SiteAccount } from '@/features/sites/api/sites'
import { AccountHeaderActions } from '@/features/sites/components/account-dialogs'
import { AccountLoginDialog, AccountRegionDialog } from '@/features/sites/components/account-provider-draw'
import type { SiteTypeInfo } from '@/features/sites/api/sites'
import {
  accountCopyPrefix,
  accountCreateHint,
  accountOperationFlags,
  accountUIText,
  fillAccountText,
  loginOpenURL,
  loginPollIntervalMs,
  loginPollOutcome,
} from '@/features/sites/components/account-provider-login'
import { grokRowView, pluginRowView } from '@/features/sites/components/account-provider-rows'
import { SiteTypeIcon } from '@/features/sites/components/site-type-icon'
import { resolveSiteTypeIcon } from '@/features/sites/lib/site-utils'
import translations from '@/locales/zh/sites.json'

const i18n = createInstance()

beforeAll(async () => {
  await i18n.init({ lng: 'zh', resources: { zh: { sites: translations } }, interpolation: { escapeValue: false } })
})

function render(node: ReactNode) {
  return renderToStaticMarkup(<I18nextProvider i18n={i18n}>{node}</I18nextProvider>)
}

function account(extra: Record<string, unknown> = {}): SiteAccount {
  return {
    credential_id: 'cred-1',
    masked_token: 'sk-…',
    enabled: true,
    ...extra,
  } as SiteAccount
}

describe('loginOpenURL', () => {
  it('opens the completed verification url for a device-code login', () => {
    expect(loginOpenURL({
      kind: 'oauth2_device_code',
      verification_uri: 'https://example.test/device',
      verification_uri_complete: 'https://example.test/device?user_code=ABCD',
      auth_url: 'https://example.test/ignored',
    })).toBe('https://example.test/device?user_code=ABCD')
  })

  it('falls back to verification_uri when the completed url is missing', () => {
    expect(loginOpenURL({
      kind: 'oauth2_device_code',
      verification_uri: 'https://example.test/device',
    })).toBe('https://example.test/device')
  })

  it('opens auth_url for a browser login', () => {
    expect(loginOpenURL({
      kind: 'custom_poll',
      auth_url: 'https://example.test/auth',
      verification_uri: 'https://example.test/device',
      verification_uri_complete: 'https://example.test/device?user_code=ABCD',
    })).toBe('https://example.test/auth')
  })
})

describe('loginPollIntervalMs', () => {
  it('uses a 5s fallback for device-code and a 3s fallback for browser login', () => {
    expect(loginPollIntervalMs('oauth2_device_code')).toBe(5000)
    expect(loginPollIntervalMs('oauth2_device_code', 0)).toBe(5000)
    expect(loginPollIntervalMs('custom_poll')).toBe(3000)
    expect(loginPollIntervalMs('custom_poll', 0)).toBe(3000)
  })

  it('raises an interval below 2 seconds to 2 seconds', () => {
    expect(loginPollIntervalMs('oauth2_device_code', 1)).toBe(2000)
    expect(loginPollIntervalMs('custom_poll', 1)).toBe(2000)
    expect(loginPollIntervalMs('oauth2_device_code', 8)).toBe(8000)
  })
})

describe('loginPollOutcome', () => {
  it('completes, keeps waiting through pending and slow_down, and reports anything else as an error', () => {
    expect(loginPollOutcome({ status: 'complete' })).toBe('complete')
    expect(loginPollOutcome({ status: 'pending' })).toBe('continue')
    expect(loginPollOutcome({ status: 'slow_down' })).toBe('continue')
    expect(loginPollOutcome({ status: 'expired' })).toBe('error')
    expect(loginPollOutcome(null)).toBe('error')
  })
})

describe('accountCopyPrefix', () => {
  it('uses the grok copy namespace for built-in account sites', () => {
    expect(accountCopyPrefix('grok')).toBe('grokAccounts')
    expect(accountCopyPrefix('openai')).toBe('grokAccounts')
    expect(accountCopyPrefix(null)).toBe('grokAccounts')
  })
})

describe('row views', () => {
  it('hides grok quota when there are no entries and quota is unknown', () => {
    const view = grokRowView(account({ quota_known: false, tier: 'Super' }))
    expect(view.detail).toBeUndefined()
    expect(render(view.detail)).not.toContain('额度未同步')
    expect(render(view.pills)).toContain('Super')
  })

  it('renders grok quota from entries when they are present', () => {
    const view = grokRowView(account({
      quota_known: false,
      entries: [{ label: 'usage', kind: 'window', name: 'GrokBuild', unit: 'percent', remaining: 95, limit: 100 }],
    }))
    expect(render(view.detail)).toContain('GrokBuild')
    expect(render(view.detail)).toContain('95% / 100%')
  })
})

describe('AccountLoginDialog', () => {
  const base = {
    session: 'sess-1',
    interval: 5,
    expires_at: '2026-10-21T00:00:00Z',
  }

  it('shows the user code and an open button for device-code login', () => {
    const info: AccountLoginStart = {
      ...base,
      kind: 'oauth2_device_code',
      user_code: 'ABCD-EFGH',
      verification_uri: 'https://example.test/device',
      verification_uri_complete: 'https://example.test/device?user_code=ABCD-EFGH',
    }
    const markup = render(
      <AccountLoginDialog login={{ status: 'waiting', purpose: 'add', info }} kind="oauth2_device_code" onClose={() => {}} />,
    )
    expect(markup).toContain('ABCD-EFGH')
    expect(markup).toContain('打开授权页面')
  })

  it('keeps the built-in device dialog from printing the verification url', () => {
    const info: AccountLoginStart = {
      ...base,
      kind: 'oauth2_device_code',
      user_code: 'ABCD-EFGH',
      verification_uri: 'https://example.test/device',
      verification_uri_complete: 'https://example.test/device?user_code=ABCD-EFGH',
    }
    const markup = render(
      <AccountLoginDialog login={{ status: 'waiting', purpose: 'add', info }} kind="oauth2_device_code" onClose={() => {}} />,
    )
    expect(markup).toContain('在 xAI 授权页面输入以下代码并批准访问：')
    expect(markup).not.toContain('https://example.test/device')
  })

  it('does not show a user code for browser login', () => {
    const info: AccountLoginStart = {
      ...base,
      kind: 'custom_poll',
      user_code: 'SHOULD-NOT-SHOW',
      auth_url: 'https://example.test/auth',
    }
    const markup = render(
      <AccountLoginDialog login={{ status: 'waiting', purpose: 'add', info }} kind="custom_poll" copyPrefix="accounts" onClose={() => {}} />,
    )
    expect(markup).not.toContain('SHOULD-NOT-SHOW')
    expect(markup).toContain('打开授权页面')
    expect(markup).toContain('在打开的页面完成授权')
  })
})

describe('AccountRegionDialog', () => {
  it('renders the regions as a radio group with the selected value', () => {
    const markup = render(
      <AccountRegionDialog
        open
        copyPrefix="accounts"
        regions={[
          { id: 'cn', label: '国内站', hint: '适用于国内账号' },
          { id: 'intl', label: '国际站', hint: '适用于海外账号' },
        ]}
        selectedId="cn"
        onSelect={() => {}}
        onConfirm={() => {}}
        onClose={() => {}}
      />,
    )
    expect(markup).toContain('role="radiogroup"')
    expect(markup).toContain('role="radio"')
    expect(markup).toContain('aria-checked="true"')
    expect(markup).toContain('国内站')
    expect(markup).toContain('国际站')
    expect(markup).toContain('继续登录')
  })
})

function pluginSiteType(extra: Partial<SiteTypeInfo> = {}): SiteTypeInfo {
  return {
    site_type: 'acme.pool/chat',
    display_name: 'Acme',
    subtitle: '',
    credential_key: 'acme',
    credential_type: 'account',
    requires_base_url: false,
    show_in_create_dialog: true,
    icon_url: 'https://cdn.example/icon.webp',
    accounts: {
      login_kind: 'oauth2_device_code',
      supports_import: true,
      supports_regions: true,
      ui: {
        row_pills: [{ field: 'realm', labels: { cn: { zh: '国内', en: 'China' } } }],
        operations: ['add', 'import', 'refresh'],
        texts: { login_hint: { zh: '用设备码登录', en: 'Use a device code' }, add: '登录' },
      },
    },
    ...extra,
  }
}

describe('plugin account pool', () => {
  it('renders row pills from fields, with label text when it is provided', () => {
    const labeled = pluginRowView(
      account({ fields: { realm: 'cn' } }),
      pluginSiteType().accounts?.ui?.row_pills,
      'zh',
    )
    expect(render(labeled.pills)).toContain('国内')
    expect(render(labeled.detail)).toContain('额度未同步，点击刷新账号获取')

    const summary = pluginRowView(account({
      remaining_quota: 1999,
      quota_limit: 2000,
      quota_unit: 'credits',
    }), pluginSiteType().accounts?.ui?.row_pills, 'zh')
    const summaryMarkup = render(summary.detail)
    expect(summaryMarkup).toContain('1,999 / 2,000')
    expect(summaryMarkup).not.toContain('额度未同步')

    const raw = pluginRowView(account({ fields: { realm: 'cn' } }), [{ field: 'realm' }], 'en')
    expect(render(raw.pills)).toContain('cn')
  })

  it('reads a same-named primitive when fields is missing, and skips unknown or empty fields', () => {
    expect(render(pluginRowView(account({ region: 'intl' }), [{ field: 'region' }], 'zh').pills)).toContain('intl')
    expect(pluginRowView(account(), undefined, 'zh').pills).toBeUndefined()
    expect(pluginRowView(account({ fields: { realm: '' } }), [{ field: 'realm' }], 'zh').pills).toBeUndefined()
    expect(pluginRowView(account({ fields: { realm: 'cn' } }), [{ field: 'missing' }], 'zh').pills).toBeUndefined()
  })

  it('shows login, import, and refresh only for the declared operations', () => {
    const flags = accountOperationFlags(['add', 'import', 'refresh'], true)
    expect(flags).toEqual({
      add: true,
      import: true,
      refresh: true,
      reauth: false,
      delete: false,
      toggle: false,
      models: false,
    })
    const shown = render(
      <AccountHeaderActions
        addLabel="登录"
        onAdd={() => {}}
        importLabel="导入账号"
        onImport={() => {}}
        refreshAllLabel="全部刷新"
        showRefreshAll
        onRefreshAll={() => {}}
      />,
    )
    expect(shown).toContain('登录')
    expect(shown).toContain('导入账号')
    expect(shown).toContain('全部刷新')

    const hidden = accountOperationFlags(['add'], true)
    const markup = render(
      <AccountHeaderActions
        addLabel="登录"
        onAdd={hidden.add ? () => {} : undefined}
        importLabel={hidden.import ? '导入账号' : undefined}
        onImport={hidden.import ? () => {} : undefined}
        refreshAllLabel="全部刷新"
        showRefreshAll={hidden.refresh}
        onRefreshAll={() => {}}
      />,
    )
    expect(markup).toContain('登录')
    expect(markup).not.toContain('导入账号')
    expect(markup).not.toContain('全部刷新')
  })

  it('keeps every action when operations is missing, and still hides import without the capability', () => {
    expect(accountOperationFlags(undefined, true)).toEqual({
      add: true,
      import: true,
      refresh: true,
      reauth: true,
      delete: true,
      toggle: true,
      models: true,
    })
    expect(accountOperationFlags(undefined, false).import).toBe(false)
    expect(accountOperationFlags(['import'], false).import).toBe(false)
    expect(accountOperationFlags([], true).add).toBe(false)
  })

  it('shows the device code, verification url, and the polling wait state', () => {
    const info: AccountLoginStart = {
      session: 'sess-1',
      interval: 5,
      expires_at: '2026-10-21T00:00:00Z',
      kind: 'oauth2_device_code',
      user_code: 'ABCD-EFGH',
      verification_uri: 'https://example.test/device',
      verification_uri_complete: 'https://example.test/device?user_code=ABCD-EFGH',
    }
    const markup = render(
      <AccountLoginDialog
        login={{ status: 'waiting', purpose: 'add', info }}
        kind="oauth2_device_code"
        copyPrefix="accounts"
        showVerificationURL
        onClose={() => {}}
      />,
    )
    expect(markup).toContain('ABCD-EFGH')
    expect(markup).toContain('https://example.test/device?user_code=ABCD-EFGH')
    expect(markup).toContain('等待授权中…')
    expect(markup).toContain('打开授权页面')
    expect(markup).not.toContain('xAI')
  })

  it('uses plugin text for the region picker', () => {
    const markup = render(
      <AccountRegionDialog
        open
        copyPrefix="accounts"
        regions={[{ id: 'cn', label: '国内', hint: '国内节点' }]}
        selectedId="cn"
        onSelect={() => {}}
        onConfirm={() => {}}
        onClose={() => {}}
      />,
    )
    expect(markup).toContain('选择区域')
    expect(markup).toContain('国内')
    expect(markup).toContain('继续登录')
    expect(markup).toContain('role="radiogroup"')
  })

  it('falls back to the letter tile when the icon is missing or fails to load', () => {
    const image = render(<SiteTypeIcon src="https://cdn.example/icon.webp" name="Acme" className="h-8 w-8" />)
    expect(image).toContain('src="https://cdn.example/icon.webp"')
    expect(image).toContain('<img')

    const missing = render(<SiteTypeIcon src="" name="Acme" className="h-8 w-8" />)
    expect(missing).not.toContain('<img')
    expect(missing).toContain('A')

    expect(resolveSiteTypeIcon('https://cdn.example/icon.webp', true, 'hidden', 'Acme')).toEqual({ kind: 'letter', text: 'A' })
    expect(resolveSiteTypeIcon('', false, 'hidden', 'Acme')).toEqual({ kind: 'hidden' })
    expect(resolveSiteTypeIcon(null, false, 'letter', 'Acme')).toEqual({ kind: 'letter', text: 'A' })
  })

  it('resolves plugin copy and leaves built-in create hints on their own keys', () => {
    const info = pluginSiteType()
    expect(accountUIText(info.accounts?.ui?.texts, 'add', 'en')).toBe('登录')
    expect(accountUIText(info.accounts?.ui?.texts, 'login_hint', 'zh-CN')).toBe('用设备码登录')
    expect(accountUIText(undefined, 'title', 'zh')).toBeUndefined()
    expect(fillAccountText('切换 {{name}}', { name: 'a@b' })).toBe('切换 a@b')
    expect(accountCreateHint(info.site_type, info, 'zh')).toEqual({
      key: 'form.accountLoginHint',
      override: '用设备码登录',
    })
    expect(accountCreateHint('grok', { ...info, site_type: 'grok' }, 'zh')).toEqual({
      key: 'form.grokDeviceHint',
    })
  })
})

describe('AccountHeaderActions', () => {
  it('hides the import control unless onImport is provided', () => {
    const shared = {
      addLabel: '添加账号',
      refreshAllLabel: '全部刷新',
      onRefreshAll: () => {},
      onAdd: () => {},
    }
    expect(render(<AccountHeaderActions {...shared} />)).not.toContain('导入账号')
    expect(render(
      <AccountHeaderActions {...shared} importLabel="导入账号" onImport={() => {}} />,
    )).toContain('导入账号')
  })
})
