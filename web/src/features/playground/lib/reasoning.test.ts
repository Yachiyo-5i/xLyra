import { describe, expect, it } from 'vitest'
import {
  normalizeReasoningEffort,
  reasoningEffortsForModel,
  supportsExtendedReasoning,
} from '@/features/playground/lib/reasoning'

describe('playground reasoning effort rules', () => {
  it('offers max and ultra only for the gpt-5.6 family', () => {
    expect(supportsExtendedReasoning({ id: 'gpt-5.6' })).toBe(true)
    expect(supportsExtendedReasoning({ id: 'gpt-5.6-sol' })).toBe(true)
    expect(supportsExtendedReasoning({ id: ' GPT-5.6-Terra ' })).toBe(true)
    expect(supportsExtendedReasoning({ id: 'gpt-5.5' })).toBe(false)

    expect(reasoningEffortsForModel({ id: 'gpt-5.6-luna' })).toEqual(['light', 'medium', 'high', 'xhigh', 'max', 'ultra'])
    expect(reasoningEffortsForModel({ id: 'gpt-5.5' })).toEqual(['light', 'medium', 'high', 'xhigh'])
  })

  it('downgrades max and ultra outside the gpt-5.6 family', () => {
    expect(normalizeReasoningEffort({ id: 'gpt-5.5' }, 'max')).toBe('xhigh')
    expect(normalizeReasoningEffort({ id: 'gpt-5.5' }, 'ultra')).toBe('xhigh')
    expect(normalizeReasoningEffort({ id: 'gpt-5.6-luna' }, 'ultra')).toBe('ultra')
    expect(normalizeReasoningEffort({ id: 'gpt-5.4' }, 'high')).toBe('high')
  })

  it('uses the mapped target when checking model aliases', () => {
    const mappedTo56 = { id: 'codex-pro', mappedModel: 'gpt-5.6-sol' }
    const mappedTo55 = { id: 'gpt-5.6-custom', mappedModel: 'gpt-5.5' }

    expect(reasoningEffortsForModel(mappedTo56)).toEqual(['light', 'medium', 'high', 'xhigh', 'max', 'ultra'])
    expect(normalizeReasoningEffort(mappedTo56, 'ultra')).toBe('ultra')
    expect(reasoningEffortsForModel(mappedTo55)).toEqual(['light', 'medium', 'high', 'xhigh'])
    expect(normalizeReasoningEffort(mappedTo55, 'ultra')).toBe('xhigh')
  })
})
