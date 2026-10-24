export function siteTypeIconPath(siteType: string): string | undefined {
  const type = siteType.trim().toLowerCase()
  if (type === 'openai' || type === 'openai_compatible') return '/brand-icons/openai.png'
  if (type === 'anthropic') return '/brand-icons/anthropic.png'
  if (type === 'google_gemini') return '/brand-icons/google.png'
  if (type === 'grok') return '/brand-icons/grok.png'
  if (type === 'opencode_go') return '/brand-icons/opencode.png'
  if (type === 'deepseek') return '/brand-icons/deepseek.png'
  if (type === 'minimax') return '/brand-icons/minimax.png'
  if (type === 'xiaomi_mimo') return '/brand-icons/xiaomi.png'
  if (type === 'moonshot') return '/brand-icons/moonshot.png'
  if (type === 'kimi_code') return '/brand-icons/moonshot.png'
  if (type === 'zhipu' || type === 'glm_code') return '/brand-icons/zhipu.png'
  if (type === 'newapi') return '/brand-icons/newapi.png'
  if (type === 'xlyra') return '/brand-icons/xlyra.png'
  if (type === 'codex') return '/oauth-icons/codex.svg'
  if (type === 'antigravity') return '/oauth-icons/antigravity.png'
  if (type === 'claude_code') return '/oauth-icons/claudecode.png'
  if (type === 'typesafe') return '/brand-icons/typesafe-dark.png'
  return undefined
}

export function buildModelGlyph(name: string): string {
  const segments = name
    .trim()
    .replace(/([a-z0-9])([A-Z])/g, '$1 $2')
    .replace(/[_/.-]+/g, ' ')
    .replace(/\s+/g, ' ')
    .split(' ')
    .map((s) => s.trim())
    .filter(Boolean)

  if (segments.length >= 2) return `${segments[0][0]}${segments[1][0]}`.toUpperCase()
  if (segments.length === 1) return segments[0].slice(0, 2).toUpperCase()
  return '?'
}

