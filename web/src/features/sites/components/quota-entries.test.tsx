import { createInstance } from 'i18next'
import { renderToStaticMarkup } from 'react-dom/server'
import { I18nextProvider } from 'react-i18next'
import { beforeAll, describe, expect, it } from 'vitest'
import type { SiteQuotaProbeEntry } from '@/features/sites/api/sites'
import translations from '@/locales/zh/sites.json'
import { QuotaEntries } from './quota-entries'

const i18n = createInstance()

beforeAll(async () => {
  await i18n.init({ lng: 'zh', resources: { zh: { sites: translations } }, interpolation: { escapeValue: false } })
})

function renderQuota(entries: SiteQuotaProbeEntry[], props?: { summary?: { remaining: number; limit: number; unit?: string }; showUnknown?: boolean }) {
  return renderToStaticMarkup(
    <I18nextProvider i18n={i18n}>
      <QuotaEntries entries={entries} summary={props?.summary} showUnknown={props?.showUnknown} />
    </I18nextProvider>,
  )
}

function quotaDate(value: string) {
  const date = new Date(value.includes('T') ? value : value.replace(' ', 'T') + '+08:00')
  return date.toLocaleDateString()
}

describe('QuotaEntries', () => {
  it('renders a window as a percent meter and a reset date', () => {
    const resetAt = '2026-10-19T17:04:45Z'
    const markup = renderQuota([
      { label: 'usage', kind: 'window', name: 'GrokBuild', unit: 'percent', remaining: 95, limit: 100, reset_at: resetAt },
    ])
    expect(markup).toContain('GrokBuild')
    expect(markup).toContain('95% / 100%')
    expect(markup).toContain('bg-success')
    expect(markup).toContain('width:95%')
    expect(markup).toContain(`${quotaDate(resetAt)} 重置`)
    expect(markup).toContain('text-right text-footnote text-label-2')
  })

  it('derives the remaining amount of a window from limit and used', () => {
    const markup = renderQuota([{ label: 'weekly', kind: 'window', unit: 'percent', limit: 100, used: 30 }])
    expect(markup).toContain('70% / 100%')
    expect(markup).toContain('width:70%')
    expect(markup).not.toContain('text-danger')
  })

  it('renders a low window without a name as the allowance fallback', () => {
    const markup = renderQuota([
      { label: 'five_hour', unit: 'percent', remaining: 5, limit: 100 },
    ])
    expect(markup).toContain('套餐额度')
    expect(markup).toContain('5% / 100%')
    expect(markup).toContain('text-danger')
    expect(markup).toContain('bg-danger')
    expect(markup).toContain('width:5%')
    expect(markup).not.toContain('重置')
  })

  it('renders packages as name, remaining / total, and reset or expiry', () => {
    const resetAt = '2026-01-31 23:59:59'
    const expiresAt = '2035-01-18T12:56:29Z'
    const markup = renderQuota([
      { label: 'package', kind: 'package', name: 'Free', unit: 'credits', remaining: 120, limit: 500, reset_at: resetAt, expires_at: expiresAt },
      { label: 'package', kind: 'package', name: 'Gift', unit: 'credits', remaining: 10, limit: 10, expires_at: expiresAt },
    ])
    expect(markup).toContain('space-y-0.5 text-footnote text-label-2')
    expect(markup).toContain('min-w-0 truncate')
    expect(markup).toContain('Free')
    expect(markup).toContain(`120 / 500 · ${quotaDate(resetAt)} 重置`)
    expect(markup).not.toContain(`120 / 500 · ${quotaDate(expiresAt)} 到期`)
    expect(markup).toContain('Gift')
    expect(markup).toContain(`10 / 10 · ${quotaDate(expiresAt)} 到期`)
    expect(markup).not.toContain('h-1.5')
  })

  it('renders a balance as a single amount row', () => {
    const markup = renderQuota([
      { label: 'balance', kind: 'balance', unit: 'usd', remaining: 12.5 },
    ])
    expect(markup).toContain('余额')
    expect(markup).toContain('$12.50')
    expect(markup).toContain('flex items-baseline justify-between text-footnote')
    expect(markup).not.toContain('h-1.5')
    expect(markup).not.toContain(' / ')
  })

  it('renders the credits summary above packages and the unsynced note when empty', () => {
    const markup = renderQuota(
      [{ label: 'package', kind: 'package', name: 'Free', unit: 'credits', remaining: 120, limit: 500 }],
      { summary: { remaining: 1999, limit: 2000, unit: 'credits' } },
    )
    expect(markup.indexOf('积分')).toBeLessThan(markup.indexOf('Free'))
    expect(markup).toContain('1,999 / 2,000')
    expect(markup).toContain('120 / 500')
    expect(markup).toContain('h-1.5')
    expect(markup).toContain('bg-success')

    expect(renderQuota([], { showUnknown: true })).toContain('额度未同步，点击刷新账号获取')
    expect(renderQuota([])).toBe('')
  })
})