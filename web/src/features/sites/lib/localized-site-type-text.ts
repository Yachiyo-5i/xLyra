import { resolveText } from '@/features/settings/lib/localized-text'
import type { SiteTypeInfo } from '@/features/sites/api/sites'

/**
 * Picks plugin site-type copy for the current UI language.
 * Exact tag, then the language prefix (`zh-CN` → `zh`). Japanese matches both
 * `ja` and `jp`. Then English, then Chinese, then the first remaining text in
 * language-tag order. `resolveText` handles the exact, prefix, and Japanese
 * slice; it does not prefer `zh` ahead of an arbitrary leftover.
 */
export function localizedSiteTypeText(
  text: Record<string, string> | undefined,
  language: string,
): string | undefined {
  if (!text) return undefined
  const normalized = language.trim().replaceAll('_', '-')
  const base = normalized.split('-')[0]?.toLowerCase() ?? ''
  const primary = uniqueKeys([
    normalized,
    normalized.toLowerCase(),
    base,
    ...(base === 'ja' || base === 'jp' ? ['ja', 'jp'] : []),
  ])
  const matched = resolveText(pickedText(text, primary), normalized)
  if (matched) return matched
  const english = text.en?.trim()
  if (english) return english
  const chinese = text.zh?.trim()
  if (chinese) return chinese
  const skip = new Set([...primary, 'en', 'zh'])
  for (const key of Object.keys(text).sort()) {
    if (skip.has(key)) continue
    const value = text[key]?.trim()
    if (value) return value
  }
  return undefined
}

export function siteTypeDisplayName(
  siteType: Pick<SiteTypeInfo, 'display_name' | 'display_name_i18n'>,
  language: string,
): string {
  return localizedSiteTypeText(siteType.display_name_i18n, language) ?? siteType.display_name
}

export function providerSubtitle(
  siteType: SiteTypeInfo,
  language: string,
  t: (key: string) => string,
): string | undefined {
  const key = `form.providerSubtitles.${siteType.site_type}`
  const translated = t(key)
  if (translated !== key) return translated
  if (hasLocalizedMap(siteType.subtitle_i18n)) return localizedSiteTypeText(siteType.subtitle_i18n, language)
  if (!language.startsWith('zh') && /[\u4e00-\u9fff]/.test(siteType.subtitle)) return undefined
  return siteType.subtitle
}

function hasLocalizedMap(text: Record<string, string> | undefined): boolean {
  if (!text) return false
  return Object.values(text).some((value) => Boolean(value?.trim()))
}

function pickedText(text: Record<string, string>, keys: string[]): Record<string, string> | undefined {
  const out: Record<string, string> = {}
  for (const key of keys) {
    const value = text[key]?.trim()
    if (value) out[key] = value
  }
  return Object.keys(out).length > 0 ? out : undefined
}

function uniqueKeys(keys: string[]): string[] {
  const seen = new Set<string>()
  const out: string[] = []
  for (const key of keys) {
    if (!key || seen.has(key)) continue
    seen.add(key)
    out.push(key)
  }
  return out
}
