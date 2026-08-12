import { describe, expect, it } from 'vitest'
import { filesFromClipboard } from '@/features/playground/lib/clipboard-files'

function clipboardData(files: File[], itemFiles: Array<File | null>) {
  return {
    files,
    items: itemFiles.map((file) => ({
      kind: 'file',
      getAsFile: () => file,
    })),
  } as unknown as Pick<DataTransfer, 'files' | 'items'>
}

describe('playground clipboard files', () => {
  it('reads files exposed through clipboard items', () => {
    const image = new File(['image'], 'pasted.png', { type: 'image/png' })

    expect(filesFromClipboard(clipboardData([], [image]))).toEqual([image])
  })

  it('deduplicates files exposed by both clipboard collections', () => {
    const document = new File(['text'], 'notes.txt', { type: 'text/plain', lastModified: 10 })
    const itemDocument = new File(['text'], 'notes.txt', { type: 'text/plain', lastModified: 20 })

    expect(filesFromClipboard(clipboardData([document], [itemDocument, null]))).toEqual([document])
  })

  it('deduplicates repeated clipboard items', () => {
    const image = new File(['image'], 'pasted.png', { type: 'image/png', lastModified: 10 })
    const repeatedImage = new File(['image'], 'pasted.png', { type: 'image/png', lastModified: 20 })

    expect(filesFromClipboard(clipboardData([], [image, repeatedImage]))).toEqual([image])
  })
})
