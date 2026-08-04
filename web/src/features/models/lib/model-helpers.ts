type TFunction = (key: string, options?: Record<string, unknown>) => string

export function formatEndpointTypeLabel(value: string): string {
  switch (value) {
    case 'openai':
      return '/v1/chat/completions'
    case 'openai-response':
      return '/v1/responses'
    case 'openai-image':
      return '/v1/images/generations'
    case 'openai-embedding':
      return '/v1/embeddings'
    case 'openai-audio-speech':
      return '/v1/audio/speech'
    case 'openai-response-compact':
      return '/v1/responses-compact'
    case 'anthropic':
    case 'anthropic-messages':
      return '/v1/messages'
    case 'google-gemini':
      return '/v1beta/models/{model}:generateContent'
    default:
      return value.startsWith('/v1/') ? value : `/v1/${value}`
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
