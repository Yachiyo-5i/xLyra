import { createInstance } from 'i18next'
import { renderToStaticMarkup } from 'react-dom/server'
import { I18nextProvider } from 'react-i18next'
import { beforeAll, describe, expect, it } from 'vitest'
import type { ReactNode } from 'react'
import type { AccountLoginStart, SiteAccount } from '@/features/sites/api/sites'
import { AccountHeaderActions } from '@/features/sites/components/account-dialogs'
import { AccountLoginDialog, AccountRegionDialog } from '@/features/sites/components/account-provider-draw'
import {
  accountCopyPrefix,
  loginOpenURL,
  loginPollIntervalMs,
  loginPollOutcome,
} from '@/features/sites/components/account-provider-login'
import { grokRowView } from '@/features/sites/components/account-provider-rows'
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
  it('uses the grok account copy namespace', () => {
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

  it('does not show a user code for browser login', () => {
    const info: AccountLoginStart = {
      ...base,
      kind: 'custom_poll',
      user_code: 'SHOULD-NOT-SHOW',
      auth_url: 'https://example.test/auth',
    }
    const markup = render(
      <AccountLoginDialog login={{ status: 'waiting', purpose: 'add', info }} kind="custom_poll" onClose={() => {}} />,
    )
    expect(markup).not.toContain('SHOULD-NOT-SHOW')
    expect(markup).toContain('打开登录页面')
  })
})

describe('AccountRegionDialog', () => {
  it('renders the regions as a radio group with the selected value', () => {
    const markup = render(
      <AccountRegionDialog
        open
        copyPrefix="grokAccounts"
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
