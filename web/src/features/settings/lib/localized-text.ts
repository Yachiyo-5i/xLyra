import type { LocalizedText } from '@/features/settings/api/js-plugins'

/**
 * Picks the copy for a language from what a plugin supplied: an exact match
 * (zh-CN), then the language (zh), then English, then the text that applies to
 * every language, then whatever is listed first.
 */
export function resolveText(text: LocalizedText | undefined, language: string): string | undefined {
  if (text === undefined || text === null) return undefined
  if (typeof text === 'string') return text || undefined
  const normalized = language.replace('_', '-')
  const base = normalized.split('-')[0]
  const found = [normalized, base, 'en', ''].map((key) => text[key]).find((value) => value)
  return found ?? Object.values(text).find((value) => value) ?? undefined
}
