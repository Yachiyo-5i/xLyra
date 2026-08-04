import { modelNameIconInfo } from '@/features/sites/lib/model-icon'

export type ModelVisual = {
  color: string
  glow: string
  iconPath?: string
  fallback: string
  label: string
}

const providerVisuals: Array<{ match: string[]; color: string; glow: string }> = [
  { match: ['openai', 'gpt', 'o1', 'o3', 'o4'], color: '#62e7bd', glow: 'rgba(98, 231, 189, 0.56)' },
  { match: ['anthropic', 'claude'], color: '#f4b86a', glow: 'rgba(244, 184, 106, 0.56)' },
  { match: ['google', 'gemini'], color: '#7bb7ff', glow: 'rgba(123, 183, 255, 0.56)' },
  { match: ['deepseek'], color: '#5de0df', glow: 'rgba(93, 224, 223, 0.56)' },
  { match: ['xai', 'grok'], color: '#f08aa5', glow: 'rgba(240, 138, 165, 0.56)' },
  { match: ['qwen'], color: '#c9a7ff', glow: 'rgba(201, 167, 255, 0.56)' },
  { match: ['moonshot', 'kimi'], color: '#f38bca', glow: 'rgba(243, 139, 202, 0.56)' },
]

export function modelVisual(provider: string, modelKey: string): ModelVisual {
  const tokens = `${provider} ${modelKey}`.toLowerCase().split(/[^a-z0-9]+/).filter(Boolean)
  const visual = providerVisuals.find((item) => item.match.some((candidate) => tokens.some((token) => token.startsWith(candidate)))) ?? {
    color: '#a5b8c8',
    glow: 'rgba(165, 184, 200, 0.48)',
  }
  const icon = modelNameIconInfo([provider, modelKey], modelKey)
  return {
    ...visual,
    iconPath: icon.iconPath,
    fallback: icon.fallbackText ?? icon.fallback.slice(0, 2).toUpperCase(),
    label: icon.label,
  }
}
