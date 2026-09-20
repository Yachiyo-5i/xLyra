import { describe, expect, it } from 'vitest'
import {
  FALLBACK_FLOW_COLOR,
  VENDOR_FLOW_COLORS,
  flowVendorBrand,
  modelVisual,
} from '@/features/traffic-flow/lib/model-visual'

describe('modelVisual vendor colors', () => {
  it.each([
    ['openai', 'gpt-5.4', 'OpenAI'],
    ['openai', 'gpt-4.1', 'OpenAI'],
    ['codex', 'codex-auto-review', 'OpenAI'],
    ['anthropic', 'claude-sonnet-4.6', 'Anthropic'],
    ['google', 'gemini-2.5-pro', 'Google'],
    ['deepseek', 'deepseek-chat', 'DeepSeek'],
    ['xai', 'grok-4', 'xAI'],
    ['qwen', 'qwen3-max', 'Qwen'],
    ['moonshot', 'kimi-k2.5', 'Moonshot'],
    ['minimax', 'MiniMax-M2.5', 'MiniMax'],
  ])('maps %s / %s to %s', (provider, model, brand) => {
    const visual = modelVisual(provider, model)
    expect(visual.brand).toBe(brand)
    expect(visual.color).toBe(VENDOR_FLOW_COLORS[brand])
  })

  it('keeps gpt-4.1 and gpt-5.4 on the same OpenAI hue', () => {
    expect(modelVisual('openai', 'gpt-4.1').color).toBe(modelVisual('openai', 'gpt-5.4').color)
  })

  it('reads vendor from the model key when the provider is a site type', () => {
    expect(flowVendorBrand('openai', 'claude-opus-4.6')).toBe('Anthropic')
    expect(modelVisual('halion', 'claude-opus-4.6').color).toBe(VENDOR_FLOW_COLORS.Anthropic)
    expect(modelVisual('zeroapi', 'MiniMax-M2.5').color).toBe(VENDOR_FLOW_COLORS.MiniMax)
  })

  it('falls back to the provider when the model key is not a known family', () => {
    expect(flowVendorBrand('anthropic', 'custom-proxy-7b')).toBe('Anthropic')
    expect(modelVisual('anthropic', 'custom-proxy-7b').color).toBe(VENDOR_FLOW_COLORS.Anthropic)
  })

  it('does not treat gpt embedded in another word as OpenAI', () => {
    expect(flowVendorBrand('', 'mygptproxy')).toBe('Other Brands')
    expect(modelVisual('', 'mygptproxy').color).toBe(FALLBACK_FLOW_COLOR)
  })

  it('folds leftover vendors into silver instead of inventing more hues', () => {
    expect(modelVisual('zhipu', 'glm-4.6').color).toBe(FALLBACK_FLOW_COLOR)
    expect(modelVisual('bytedance', 'doubao-seed-1.6').color).toBe(FALLBACK_FLOW_COLOR)
    expect(modelVisual('unknown', 'mystery-model').color).toBe(FALLBACK_FLOW_COLOR)
  })
})
