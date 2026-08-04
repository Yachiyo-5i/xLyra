import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  generateImage,
  listGatewayModels,
  streamChatCompletion,
  streamMessages,
  streamResponses,
} from '@/features/playground/api/playground'

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('listGatewayModels', () => {
  it('bypasses the browser cache when the authorization key changes', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ data: [] }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    )
    vi.stubGlobal('fetch', fetchMock)

    await listGatewayModels('sk-test')

    expect(fetchMock).toHaveBeenCalledWith('/api/playground/v1/models', {
      method: 'GET',
      headers: { Authorization: 'Bearer sk-test' },
      cache: 'no-store',
    })
  })

  it('sorts models alphabetically without case sensitivity', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(
      new Response(JSON.stringify({
        data: [
          { id: 'zeta-model' },
          { id: 'Beta-model' },
          { id: 'alpha-model' },
        ],
      }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    ))

    const models = await listGatewayModels('sk-test')

    expect(models.map((model) => model.id)).toEqual(['alpha-model', 'Beta-model', 'zeta-model'])
  })

  it('reports a clear error when the development server returns the SPA document', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(
      new Response('<!doctype html><html></html>', {
        status: 200,
        headers: { 'Content-Type': 'text/html' },
      }),
    ))

    await expect(listGatewayModels('sk-test')).rejects.toThrow(
      'Playground API returned text/html instead of JSON',
    )
  })
})

describe('gateway route metadata', () => {
  it('reports the routed site for streaming chat responses', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(
      new Response('data: [DONE]\n\n', {
        status: 200,
        headers: {
          'Content-Type': 'text/event-stream',
          'X-Xlyra-Route-Site': 'tokenfree',
        },
      }),
    ))
    const onRouteSite = vi.fn()

    await streamChatCompletion({
      apiKey: 'sk-test',
      model: 'gpt-5.6-sol',
      messages: [{ role: 'user', content: 'hello' }],
      onContent: vi.fn(),
      onReasoning: vi.fn(),
      onUsage: vi.fn(),
      onRouteSite,
    })

    expect(onRouteSite).toHaveBeenCalledWith('tokenfree')
  })

  it('parses CRLF-delimited streaming chat events', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(
      new Response(
        'data: {"choices":[{"delta":{"content":"hello"}}]}\r\n\r\n' +
          'data: {"choices":[{"delta":{"content":" world"}}]}\r\n\r\n' +
          'data: [DONE]\r\n\r\n',
        { status: 200, headers: { 'Content-Type': 'text/event-stream' } },
      ),
    ))
    const onContent = vi.fn()

    await streamChatCompletion({
      apiKey: 'sk-test',
      model: 'gpt-test',
      messages: [{ role: 'user', content: 'hello' }],
      onContent,
      onReasoning: vi.fn(),
      onUsage: vi.fn(),
      onRouteSite: vi.fn(),
    })

    expect(onContent.mock.calls.flat()).toEqual(['hello', ' world'])
  })

  it('rejects an error event after partial streaming content', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(
      new Response(
        'data: {"choices":[{"delta":{"content":"partial"}}]}\n\n' +
          'data: {"error":{"message":"upstream interrupted"}}\n\n',
        { status: 200, headers: { 'Content-Type': 'text/event-stream' } },
      ),
    ))

    await expect(streamChatCompletion({
      apiKey: 'sk-test',
      model: 'gpt-test',
      messages: [{ role: 'user', content: 'hello' }],
      onContent: vi.fn(),
      onReasoning: vi.fn(),
      onUsage: vi.fn(),
      onRouteSite: vi.fn(),
    })).rejects.toThrow('upstream interrupted')
  })

  it('returns the routed site with generated images', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ data: [{ b64_json: 'aW1hZ2U=' }] }), {
        status: 200,
        headers: {
          'Content-Type': 'application/json',
          'X-Xlyra-Route-Site': '%E5%9B%BE%E7%89%87%E7%AB%99%E7%82%B9',
        },
      }),
    ))

    const result = await generateImage({
      apiKey: 'sk-test',
      model: 'gpt-image-1',
      prompt: 'a lighthouse',
    })

    expect(result.siteName).toBe('图片站点')
    expect(result.images).toHaveLength(1)
  })
})

describe('chat attachments', () => {
  const attachment = {
    id: 'attachment-1',
    name: 'report.pdf',
    mimeType: 'application/pdf',
    size: 4,
    dataURL: 'data:application/pdf;base64,cGRm',
  }

  function streamInput() {
    return {
      apiKey: 'sk-test',
      model: 'test-model',
      messages: [{ role: 'user' as const, content: 'summarize', attachments: [attachment] }],
      onContent: vi.fn(),
      onReasoning: vi.fn(),
      onUsage: vi.fn(),
      onRouteSite: vi.fn(),
    }
  }

  it('sends files using the Chat Completions file content part', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response('data: [DONE]\n\n', {
      status: 200,
      headers: { 'Content-Type': 'text/event-stream' },
    }))
    vi.stubGlobal('fetch', fetchMock)

    await streamChatCompletion(streamInput())

    const body = JSON.parse(fetchMock.mock.calls[0][1].body as string)
    expect(body.messages[0].content).toEqual([
      { type: 'text', text: 'summarize' },
      { type: 'file', file: { filename: 'report.pdf', file_data: attachment.dataURL } },
    ])
  })

  it('sends files using the Responses input_file content part', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response('data: {"type":"response.completed"}\n\n', {
      status: 200,
      headers: { 'Content-Type': 'text/event-stream' },
    }))
    vi.stubGlobal('fetch', fetchMock)

    await streamResponses(streamInput())

    const body = JSON.parse(fetchMock.mock.calls[0][1].body as string)
    expect(body.input[0].content).toEqual([
      { type: 'input_text', text: 'summarize' },
      { type: 'input_file', filename: 'report.pdf', file_data: attachment.dataURL },
    ])
  })

  it('sends files using the Anthropic document content block', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response('data: {"type":"message_stop"}\n\n', {
      status: 200,
      headers: { 'Content-Type': 'text/event-stream' },
    }))
    vi.stubGlobal('fetch', fetchMock)

    await streamMessages(streamInput())

    const body = JSON.parse(fetchMock.mock.calls[0][1].body as string)
    expect(body.messages[0].content).toEqual([
      { type: 'text', text: 'summarize' },
      {
        type: 'document',
        title: 'report.pdf',
        source: { type: 'base64', media_type: 'application/pdf', data: 'cGRm' },
      },
    ])
  })
})
