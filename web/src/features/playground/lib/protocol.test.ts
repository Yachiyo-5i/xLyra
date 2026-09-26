import { describe, expect, it } from 'vitest'
import { autoProtocol } from '@/features/playground/lib/protocol'
import type { GatewayModel } from '@/features/playground/lib/types'

function model(partial: Partial<GatewayModel>): GatewayModel {
  return {
    id: 'model',
    displayName: 'Model',
    category: 'chat',
    endpointTypes: [],
    ...partial,
  }
}

describe('autoProtocol', () => {
  it('prefers gemini when google-gemini endpoint is backed by antigravity or google_gemini sites', () => {
    expect(autoProtocol(model({
      endpointTypes: ['openai-response', 'google-gemini'],
      siteTypes: ['antigravity'],
    }))).toBe('gemini')
    expect(autoProtocol(model({
      endpointTypes: ['openai', 'google-gemini'],
      siteTypes: ['google_gemini'],
    }))).toBe('gemini')
  })

  it('keeps responses when openai-response is available without gemini sites', () => {
    expect(autoProtocol(model({
      endpointTypes: ['openai-response', 'google-gemini'],
      siteTypes: ['openai'],
    }))).toBe('responses')
  })

  it('falls back to messages then chat', () => {
    expect(autoProtocol(model({ endpointTypes: ['anthropic-messages'] }))).toBe('messages')
    expect(autoProtocol(model({ endpointTypes: ['openai'] }))).toBe('chat')
    expect(autoProtocol(undefined)).toBe('chat')
  })

  it('uses gemini when google-gemini is the only chat endpoint', () => {
    expect(autoProtocol(model({
      endpointTypes: ['google-gemini'],
      siteTypes: ['custom'],
    }))).toBe('gemini')
  })
})
