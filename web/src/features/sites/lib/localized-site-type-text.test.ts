import { describe, expect, it } from 'vitest'
import type { SiteTypeInfo } from '@/features/sites/api/sites'
import { localizedSiteTypeText, providerSubtitle, siteTypeDisplayName } from '@/features/sites/lib/localized-site-type-text'
import { formatSiteTypeLabel } from '@/features/sites/lib/site-utils'

const identity = (key: string) => key

function siteType(partial: Partial<SiteTypeInfo> & Pick<SiteTypeInfo, 'site_type'>): SiteTypeInfo {
  return {
    display_name: '',
    subtitle: '',
    credential_key: partial.site_type,
    credential_type: 'api_key',
    requires_base_url: false,
    show_in_create_dialog: true,
    ...partial,
  }
}

const pluginCopy = {
  zh: '中文副标题',
  en: 'English subtitle',
  jp: '日本語の副題',
}

describe('localizedSiteTypeText', () => {
  it('uses English, Chinese, Japanese, and a zh-CN prefix', () => {
    expect(localizedSiteTypeText(pluginCopy, 'en')).toBe('English subtitle')
    expect(localizedSiteTypeText(pluginCopy, 'zh')).toBe('中文副标题')
    expect(localizedSiteTypeText(pluginCopy, 'jp')).toBe('日本語の副題')
    expect(localizedSiteTypeText(pluginCopy, 'ja')).toBe('日本語の副題')
    expect(localizedSiteTypeText(pluginCopy, 'ja-JP')).toBe('日本語の副題')
    expect(localizedSiteTypeText({ zh: '中文副标题', en: 'English subtitle' }, 'jp')).toBe('English subtitle')
    expect(localizedSiteTypeText({ zh: '中文副标题', en: 'English subtitle' }, 'ja')).toBe('English subtitle')
    expect(localizedSiteTypeText({ 'zh-CN': '简体副标题', zh: '中文副标题', en: 'English subtitle' }, 'zh-CN')).toBe('简体副标题')
    expect(localizedSiteTypeText({ zh: '中文副标题', en: 'English subtitle' }, 'zh-CN')).toBe('中文副标题')
    expect(localizedSiteTypeText({ de: 'Untertitel', zh: '中文副标题' }, 'fr')).toBe('中文副标题')
    expect(localizedSiteTypeText({ de: 'Untertitel' }, 'fr')).toBe('Untertitel')
    expect(localizedSiteTypeText({ en: '  ', zh: '中文副标题' }, 'en')).toBe('中文副标题')
    expect(localizedSiteTypeText(undefined, 'en')).toBeUndefined()
  })
})

describe('providerSubtitle', () => {
  const plugin = siteType({
    site_type: 'acme.sample/chat',
    display_name: '示例',
    subtitle: '中文副标题',
    subtitle_i18n: pluginCopy,
    plugin_id: 'acme.sample',
  })

  it('shows the subtitle for the current language when the plugin sent a map', () => {
    expect(providerSubtitle(plugin, 'en', identity)).toBe('English subtitle')
    expect(providerSubtitle(plugin, 'zh', identity)).toBe('中文副标题')
    expect(providerSubtitle(plugin, 'jp', identity)).toBe('日本語の副題')
    expect(providerSubtitle(plugin, 'ja', identity)).toBe('日本語の副題')
    expect(providerSubtitle({ ...plugin, subtitle_i18n: { zh: '中文副标题', en: 'English subtitle' } }, 'jp', identity)).toBe('English subtitle')
    expect(providerSubtitle(plugin, 'zh-CN', identity)).toBe('中文副标题')
  })

  it('keeps the old subtitle when a plugin or built-in type has no language map', () => {
    const translated = (key: string) => key === 'form.providerSubtitles.openai' ? 'OpenAI API key' : key
    const builtin = siteType({ site_type: 'openai', display_name: 'OpenAI', subtitle: 'OpenAI 平台 API Key 接入' })
    const plainPlugin = siteType({
      site_type: 'acme.example/pool',
      display_name: '池',
      subtitle: '账号池',
      plugin_id: 'acme.example',
    })

    expect(providerSubtitle(builtin, 'en', translated)).toBe('OpenAI API key')
    expect(providerSubtitle(builtin, 'zh', identity)).toBe('OpenAI 平台 API Key 接入')
    expect(providerSubtitle(builtin, 'en', identity)).toBeUndefined()
    expect(providerSubtitle(builtin, 'jp', identity)).toBeUndefined()
    expect(providerSubtitle(plainPlugin, 'en', identity)).toBeUndefined()
    expect(providerSubtitle(plainPlugin, 'zh-CN', identity)).toBe('账号池')
    expect(providerSubtitle({ ...plainPlugin, subtitle: 'Account pool' }, 'en', identity)).toBe('Account pool')
  })
})

describe('site type display names', () => {
  const info = siteType({
    site_type: 'acme.sample/chat',
    display_name: '示例',
    display_name_i18n: { zh: '示例', en: 'Sample', jp: 'サンプル' },
    plugin_id: 'acme.sample',
  })

  it('localizes plugin names and leaves built-in names on the single string', () => {
    expect(siteTypeDisplayName(info, 'en')).toBe('Sample')
    expect(siteTypeDisplayName(info, 'zh')).toBe('示例')
    expect(siteTypeDisplayName(info, 'ja')).toBe('サンプル')
    expect(formatSiteTypeLabel(info.site_type, [info], 'en')).toBe('Sample')
    expect(formatSiteTypeLabel(info.site_type, [info], 'zh-CN')).toBe('示例')
    expect(formatSiteTypeLabel(info.site_type, [info])).toBe('示例')
    expect(formatSiteTypeLabel('openai', [siteType({ site_type: 'openai', display_name: 'OpenAI', subtitle: 'OpenAI 平台 API Key 接入' })], 'en')).toBe('OpenAI')
  })
})
