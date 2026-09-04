const TEXT_ATTACHMENT_EXTENSIONS = new Set([
  'txt', 'md', 'csv', 'tsv', 'xml', 'html', 'js', 'ts', 'tsx', 'jsx', 'py', 'go', 'java', 'c', 'cpp', 'h',
  'hpp', 'rs', 'rb', 'php', 'sh', 'yaml', 'yml', 'toml', 'log',
])

const ATTACHMENT_MIME_TYPES: Record<string, string> = {
  doc: 'application/msword',
  docx: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
  odt: 'application/vnd.oasis.opendocument.text',
  ppt: 'application/vnd.ms-powerpoint',
  pptx: 'application/vnd.openxmlformats-officedocument.presentationml.presentation',
  rtf: 'application/rtf',
  xls: 'application/vnd.ms-excel',
  xlsx: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
}

export function attachmentMimeType(file: File): string {
  const extension = file.name.split('.').pop()?.toLowerCase()
  if (extension && ATTACHMENT_MIME_TYPES[extension]) return ATTACHMENT_MIME_TYPES[extension]
  if (extension === 'pdf') return 'application/pdf'
  if (extension === 'json') return 'application/json'
  if (extension === 'csv' || extension === 'tsv') return 'text/csv'
  if (extension && TEXT_ATTACHMENT_EXTENSIONS.has(extension)) return 'text/plain'
  if (file.type) return file.type
  return 'application/octet-stream'
}

export function normalizeAttachmentDataURL(dataURL: string, mimeType: string): string {
  return dataURL.replace(/^data:[^;,]+/, `data:${mimeType}`)
}
