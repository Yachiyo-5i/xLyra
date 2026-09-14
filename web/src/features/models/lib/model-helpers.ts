type TFunction = (key: string, options?: Record<string, unknown>) => string

export function formatEndpointTypeLabel(value: string): string {
  switch (value) {
    case 'openai':
      return 'completions'
    case 'openai-response':
      return 'responses'
    case 'openai-image':
      return 'images'
    case 'openai-embedding':
      return 'embeddings'
    case 'openai-audio-speech':
      return 'audio/speech'
    case 'openai-response-compact':
      return 'responses-compact'
    case 'anthropic':
    case 'anthropic-messages':
      return 'messages'
    case 'google-gemini':
      return 'gemini'
    default:
      return value
  }
}

export function formatMatchSource(source: string, t?: TFunction): string {
  if (source === 'manual') return t ? t('matchSource.manual') : '手动'
  if (source === 'auto') return t ? t('matchSource.auto') : '自动'
  if (source === 'unmatched') return t ? t('matchSource.unmatched') : '未命中'
  return source || '-'
}

export function isNewAPISite(siteType: string): boolean {
  return siteType === 'newapi'
}
