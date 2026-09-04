import { describe, expect, it } from 'vitest'
import { attachmentMimeType, normalizeAttachmentDataURL } from '@/features/playground/lib/attachments'

describe('attachment MIME normalization', () => {
  it('infers text MIME from the extension when the browser omits it', () => {
    expect(attachmentMimeType({ name: '从 ReAct 到 Agent.md', type: '' } as File)).toBe('text/plain')
  })

  it('replaces the generic Data URL MIME with the inferred MIME', () => {
    expect(normalizeAttachmentDataURL('data:application/octet-stream;base64,QQ==', 'text/plain'))
      .toBe('data:text/plain;base64,QQ==')
  })
})
