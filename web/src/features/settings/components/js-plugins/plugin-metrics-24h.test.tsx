import { createInstance, type i18n as I18n } from 'i18next'
import { renderToStaticMarkup } from 'react-dom/server'
import { I18nextProvider } from 'react-i18next'
import { beforeAll, describe, expect, it } from 'vitest'
import type { JSPluginMetrics24h } from '@/features/settings/api/js-plugins'
import en from '@/locales/en/settings.json'
import jp from '@/locales/jp/settings.json'
import zh from '@/locales/zh/settings.json'
import { metricsHealthLevel } from './metrics-health'
import { PluginMetrics24h } from './plugin-metrics-24h'

const zhI18n = createInstance()
const enI18n = createInstance()
const jpI18n = createInstance()

beforeAll(async () => {
  await Promise.all([
    zhI18n.init({ lng: 'zh', resources: { zh: { settings: zh } }, interpolation: { escapeValue: false } }),
    enI18n.init({ lng: 'en', resources: { en: { settings: en } }, interpolation: { escapeValue: false } }),
    jpI18n.init({ lng: 'jp', resources: { jp: { settings: jp } }, interpolation: { escapeValue: false } }),
  ])
})

function renderMetrics(metrics: JSPluginMetrics24h | undefined, i18n: I18n) {
  return renderToStaticMarkup(
    <I18nextProvider i18n={i18n}>
      <PluginMetrics24h metrics={metrics} />
    </I18nextProvider>,
  )
}

describe('metricsHealthLevel', () => {
  it('is none when nothing has been called', () => {
    expect(metricsHealthLevel(0, 0)).toBe('none')
    expect(metricsHealthLevel(0, 5)).toBe('none')
    expect(metricsHealthLevel(-1, 0)).toBe('none')
  })

  it('is ok when every call succeeded', () => {
    expect(metricsHealthLevel(5, 0)).toBe('ok')
    expect(metricsHealthLevel(1, 0)).toBe('ok')
  })

  it('is warn when failures do not exceed successes, including a tie', () => {
    expect(metricsHealthLevel(4, 2)).toBe('warn')
    expect(metricsHealthLevel(5, 2)).toBe('warn')
  })

  it('is bad when failures exceed successes', () => {
    expect(metricsHealthLevel(5, 3)).toBe('bad')
    expect(metricsHealthLevel(1, 1)).toBe('bad')
    expect(metricsHealthLevel(2, 3)).toBe('bad')
  })
})

describe('PluginMetrics24h', () => {
  it('renders a gray dot and empty copy when there are no calls', () => {
    const markup = renderMetrics({ calls: 0, errors: 0, error_rate: 0 }, zhI18n)
    expect(markup).toContain('bg-label-3')
    expect(markup).toContain('aria-label="暂无调用"')
    expect(markup).toContain('title="暂无调用"')
    expect(markup).toContain('暂无调用')
    expect(markup).not.toContain('成功')
    expect(markup).not.toContain('%')
  })

  it('renders a green dot and success / failure counts without a rate', () => {
    const markup = renderMetrics({ calls: 5, errors: 0, error_rate: 0 }, zhI18n)
    expect(markup).toContain('bg-success')
    expect(markup).toContain('aria-label="运行正常"')
    expect(markup).toContain('成功 5 · 失败 0')
    expect(markup).toContain('aria-label="近 24 小时: 成功 5 · 失败 0"')
    expect(markup).toContain('每次插件代码执行（登录、刷新、拉取额度、拉取模型等）记一次；插件代码报错才算失败，不代表账号本身是否可用；服务重启后清零。')
    expect(markup).not.toContain('%')
  })

  it('renders a yellow dot when failures equal successes', () => {
    const markup = renderMetrics({ calls: 4, errors: 2, error_rate: 0.5 }, zhI18n)
    expect(markup).toContain('bg-warning')
    expect(markup).toContain('aria-label="有少量失败"')
    expect(markup).toContain('成功 2 · 失败 2')
    expect(markup).not.toContain('%')
  })

  it('renders a red dot when failures exceed successes', () => {
    const markup = renderMetrics({ calls: 5, errors: 3, error_rate: 0.6 }, zhI18n)
    expect(markup).toContain('bg-danger')
    expect(markup).toContain('aria-label="失败较多"')
    expect(markup).toContain('成功 2 · 失败 3')
    expect(markup).not.toContain('%')
  })

  it('uses the same empty copy when metrics are missing', () => {
    const markup = renderMetrics(undefined, zhI18n)
    expect(markup).toContain('bg-label-3')
    expect(markup).toContain('aria-label="暂无调用"')
  })

  it('renders English counts and status', () => {
    const markup = renderMetrics({ calls: 5, errors: 0, error_rate: 0 }, enI18n)
    expect(markup).toContain('5 succeeded · 0 failed')
    expect(markup).toContain('aria-label="Running normally"')
    expect(markup).toContain('aria-label="Last 24 hours: 5 succeeded · 0 failed"')
    expect(markup).toContain('Each plugin code run (sign-in, refresh, quota fetch, model list, and similar) counts once. Only a plugin code error counts as a failure; it does not mean the account itself is unavailable. Counts reset when the service restarts.')
    expect(markup).not.toContain('%')
  })

  it('renders Japanese counts and status', () => {
    const markup = renderMetrics({ calls: 5, errors: 2, error_rate: 0.4 }, jpI18n)
    expect(markup).toContain('成功 3 · 失敗 2')
    expect(markup).toContain('aria-label="失敗が少なめ"')
    expect(markup).toContain('プラグインのコードが実行されるたび（ログイン、更新、利用枠の取得、モデル一覧の取得など）に 1 回と数えます。失敗になるのはプラグインのコードがエラーになった場合だけで、アカウント自体が使えるかどうかとは関係ありません。サービスを再起動するとゼロに戻ります。')
    expect(markup).not.toContain('%')
  })
})
