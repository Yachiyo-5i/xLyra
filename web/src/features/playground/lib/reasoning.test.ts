import { describe, expect, it } from 'vitest'
import {
  normalizeReasoningEffort,
  reasoningEffortsForModel,
  supportsMaxReasoning,
  supportsUltraReasoning,
} from '@/features/playground/lib/reasoning'

describe('playground reasoning effort rules', () => {
  it('offers max across the gpt-5.6 family and ultra only for sol and terra', () => {
    expect(supportsMaxReasoning({ id: 'gpt-5.6' })).toBe(true)
    expect(supportsMaxReasoning({ id: 'gpt-5.6-luna' })).toBe(true)
    expect(supportsMaxReasoning({ id: 'gpt-5.5' })).toBe(false)
    expect(supportsUltraReasoning({ id: 'gpt-5.6-sol' })).toBe(true)
    expect(supportsUltraReasoning({ id: ' GPT-5.6-Terra ' })).toBe(true)
    expect(supportsUltraReasoning({ id: 'gpt-5.6-luna' })).toBe(false)

    expect(reasoningEffortsForModel({ id: 'gpt-5.6-sol' })).toEqual(['low', 'medium', 'high', 'xhigh', 'max', 'ultra'])
    expect(reasoningEffortsForModel({ id: 'gpt-5.6-luna' })).toEqual(['low', 'medium', 'high', 'xhigh', 'max'])
    expect(reasoningEffortsForModel({ id: 'gpt-5.5' })).toEqual(['low', 'medium', 'high', 'xhigh'])
  })

  it('downgrades unsupported max and ultra to the strongest available effort', () => {
    expect(normalizeReasoningEffort({ id: 'gpt-5.5' }, 'max')).toBe('xhigh')
    expect(normalizeReasoningEffort({ id: 'gpt-5.5' }, 'ultra')).toBe('xhigh')
    expect(normalizeReasoningEffort({ id: 'gpt-5.6-luna' }, 'ultra')).toBe('max')
    expect(normalizeReasoningEffort({ id: 'gpt-5.6-terra' }, 'ultra')).toBe('ultra')
    expect(normalizeReasoningEffort({ id: 'gpt-5.4' }, 'high')).toBe('high')
  })

  it('uses the mapped target when checking model aliases', () => {
    const mappedTo56 = { id: 'codex-pro', mappedModel: 'gpt-5.6-sol' }
    const mappedTo55 = { id: 'gpt-5.6-custom', mappedModel: 'gpt-5.5' }

    expect(reasoningEffortsForModel(mappedTo56)).toEqual(['low', 'medium', 'high', 'xhigh', 'max', 'ultra'])
    expect(normalizeReasoningEffort(mappedTo56, 'ultra')).toBe('ultra')
    expect(reasoningEffortsForModel(mappedTo55)).toEqual(['low', 'medium', 'high', 'xhigh'])
    expect(normalizeReasoningEffort(mappedTo55, 'ultra')).toBe('xhigh')
  })
})
