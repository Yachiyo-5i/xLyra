type ClipboardFileData = Pick<DataTransfer, 'files' | 'items'>

function fileKey(file: File): string {
  return `${file.name}:${file.size}:${file.type}`
}

export function filesFromClipboard(data: ClipboardFileData): File[] {
  const files = Array.from(data.files)
  const itemFiles = Array.from(data.items)
    .filter((item) => item.kind === 'file')
    .map((item) => item.getAsFile())
    .filter((file): file is File => Boolean(file))
  const seen = new Set<string>()
  return [...files, ...itemFiles].filter((file) => {
    const key = fileKey(file)
    if (seen.has(key)) return false
    seen.add(key)
    return true
  })
}
