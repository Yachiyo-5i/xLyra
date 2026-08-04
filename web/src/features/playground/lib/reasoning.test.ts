import { describe, expect, it } from 'vitest'
import {
  normalizeReasoningEffort,
  reasoningEffortsForModel,
  supportsMaxReasoning,
} from '@/features/playground/lib/reasoning'

describe('playground reasoning effort rules', () => {
  it('offers max only for the three gpt-5.6 models', () => {
    expect(supportsMaxReasoning('gpt-5.6-sol')).toBe(true)
    expect(supportsMaxReasoning('gpt-5.6-terra')).toBe(true)
    expect(supportsMaxReasoning('gpt-5.6-luna')).toBe(true)
    expect(supportsMaxReasoning('gpt-5.6')).toBe(false)
    expect(supportsMaxReasoning('gpt-5.5')).toBe(false)

    expect(reasoningEffortsForModel('gpt-5.6-sol')).toEqual(['low', 'medium', 'high', 'xhigh', 'max'])
    expect(reasoningEffortsForModel('gpt-5.5')).toEqual(['low', 'medium', 'high', 'xhigh'])
  })

  it('downgrades max when switching to a model without max support', () => {
    expect(normalizeReasoningEffort('gpt-5.5', 'max')).toBe('xhigh')
    expect(normalizeReasoningEffort('gpt-5.6-luna', 'max')).toBe('max')
    expect(normalizeReasoningEffort('gpt-5.5', 'high')).toBe('high')
  })
})
