import { beforeEach, describe, expect, it, vi } from 'vitest'
import { loadConversations, loadSettings, saveConversations, saveSettings } from '@/features/playground/lib/storage'

const values = new Map<string, string>()

beforeEach(() => {
  values.clear()
  vi.stubGlobal('localStorage', {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => values.set(key, value),
  })
})

describe('loadSettings', () => {
  it('fills fields missing from older stored settings', () => {
    values.set('xlyra-playground-settings', JSON.stringify({ mode: 'image' }))

    expect(loadSettings()).toEqual({
      apiKeyId: null,
      mode: 'image',
      chatModel: null,
      reasoningEffort: 'medium',
      imageModel: null,
    })
  })

  it('replaces an invalid reasoning effort with the default', () => {
    values.set('xlyra-playground-settings', JSON.stringify({ reasoningEffort: 'undefined' }))

    expect(loadSettings().reasoningEffort).toBe('medium')
  })

  it('keeps max reasoning effort from stored settings', () => {
    values.set('xlyra-playground-settings', JSON.stringify({
      chatModel: 'gpt-5.6-sol',
      reasoningEffort: 'max',
    }))

    expect(loadSettings().reasoningEffort).toBe('max')
  })

  it('migrates the obsolete light reasoning effort to low', () => {
    values.set('xlyra-playground-settings', JSON.stringify({ reasoningEffort: 'light' }))

    expect(loadSettings().reasoningEffort).toBe('low')
  })

  it('keeps ultra reasoning effort from stored settings', () => {
    values.set('xlyra-playground-settings', JSON.stringify({
      chatModel: 'gpt-5.6-terra',
      reasoningEffort: 'ultra',
    }))

    expect(loadSettings().reasoningEffort).toBe('ultra')
  })

  it('preserves extended reasoning until model metadata is available', () => {
    values.set('xlyra-playground-settings', JSON.stringify({
      chatModel: 'gpt-5.5',
      reasoningEffort: 'ultra',
    }))

    expect(loadSettings().reasoningEffort).toBe('ultra')
  })

  it('persists the selected downstream key', () => {
    saveSettings({
      apiKeyId: 'key-2',
      mode: 'chat',
      chatModel: null,
      reasoningEffort: 'medium',
      imageModel: null,
    })

    expect(loadSettings().apiKeyId).toBe('key-2')
  })
})

describe('conversation storage', () => {
  it('persists attachment metadata without storing the binary payload', () => {
    saveConversations([{
      id: 'conversation-1',
      title: 'Files',
      model: 'gpt-test',
      systemPrompt: '',
      messages: [{
        id: 'message-1',
        role: 'user',
        content: 'summarize',
        attachments: [{
          id: 'attachment-1',
          name: 'report.pdf',
          mimeType: 'application/pdf',
          size: 4,
          dataURL: 'data:application/pdf;base64,cGRm',
        }],
        createdAt: 1,
      }],
      createdAt: 1,
      updatedAt: 1,
    }])

    expect(loadConversations()[0].messages[0].attachments).toEqual([{
      id: 'attachment-1',
      name: 'report.pdf',
      mimeType: 'application/pdf',
      size: 4,
    }])
  })

  it('persists the final response duration', () => {
    saveConversations([{
      id: 'conversation-1',
      title: 'Timing',
      model: 'gpt-test',
      systemPrompt: '',
      messages: [{
        id: 'message-1',
        role: 'assistant',
        content: 'done',
        responseDurationMs: 6_420,
        createdAt: 1,
      }],
      createdAt: 1,
      updatedAt: 1,
    }])

    expect(loadConversations()[0].messages[0].responseDurationMs).toBe(6_420)
  })
})
