export function localeFromLanguage(language?: string) {
  if (language?.startsWith('zh')) return 'zh-CN'
  if (language?.startsWith('jp') || language?.startsWith('ja')) return 'ja-JP'
  return 'en-US'
}
