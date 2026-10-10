import { describe, expect, it } from 'vitest'
import { resolveText } from '@/features/settings/lib/localized-text'

describe('resolveText', () => {
  it('uses a plain string for every language', () => {
    expect(resolveText('Reset', 'zh-CN')).toBe('Reset')
  })

  it('prefers the exact language, then the base language, then English', () => {
    const text = { 'zh-CN': '简体', zh: '中文', en: 'English' }
    expect(resolveText(text, 'zh-CN')).toBe('简体')
    expect(resolveText(text, 'zh-TW')).toBe('中文')
    expect(resolveText({ en: 'English', jp: '日本語' }, 'zh')).toBe('English')
    expect(resolveText({ jp: '日本語' }, 'zh')).toBe('日本語')
  })

  it('has nothing when the plugin said nothing, so the default wording is used', () => {
    expect(resolveText(undefined, 'en')).toBeUndefined()
    expect(resolveText('', 'en')).toBeUndefined()
    expect(resolveText({}, 'en')).toBeUndefined()
  })
})
