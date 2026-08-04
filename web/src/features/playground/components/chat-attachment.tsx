import {
  Database,
  File,
  FileArchive,
  FileAudio,
  FileCode,
  FileImage,
  FileJson,
  FileSpreadsheet,
  FileText,
  FileVideo,
  Presentation,
  X,
  type LucideIcon,
} from 'lucide-react'
import type { ChatAttachment } from '@/features/playground/lib/types'

type ChatAttachmentItemProps = {
  attachment: ChatAttachment
  removeLabel?: string
  onRemove?: () => void
}

function formatSize(size: number): string {
  if (size < 1024) return `${size} B`
  if (size < 1024 * 1024) return `${Math.ceil(size / 1024)} KB`
  return `${(size / (1024 * 1024)).toFixed(1)} MB`
}

type AttachmentIcon = {
  Icon: LucideIcon
  className: string
}

const DOCUMENT_EXTENSIONS = new Set(['doc', 'docx', 'odt', 'rtf'])
const SPREADSHEET_EXTENSIONS = new Set(['csv', 'ods', 'xls', 'xlsm', 'xlsx'])
const PRESENTATION_EXTENSIONS = new Set(['key', 'odp', 'ppt', 'pptx'])
const ARCHIVE_EXTENSIONS = new Set(['7z', 'bz2', 'gz', 'rar', 'tar', 'tgz', 'xz', 'zip'])
const CODE_EXTENSIONS = new Set([
  'c', 'cc', 'cpp', 'cs', 'css', 'go', 'h', 'hpp', 'html', 'java', 'js', 'jsx', 'kt', 'kts', 'php',
  'py', 'rb', 'rs', 'sh', 'sql', 'swift', 'ts', 'tsx', 'vue', 'xml', 'yaml', 'yml',
])
const TEXT_EXTENSIONS = new Set(['epub', 'log', 'md', 'mobi', 'tex', 'txt'])
const DATABASE_EXTENSIONS = new Set(['db', 'sqlite', 'sqlite3'])
const IMAGE_EXTENSIONS = new Set(['avif', 'bmp', 'gif', 'heic', 'heif', 'ico', 'jpeg', 'jpg', 'png', 'svg', 'webp'])
const AUDIO_EXTENSIONS = new Set(['aac', 'flac', 'm4a', 'mp3', 'ogg', 'wav', 'wma'])
const VIDEO_EXTENSIONS = new Set(['avi', 'm4v', 'mkv', 'mov', 'mp4', 'mpeg', 'mpg', 'webm'])

function attachmentExtension(name: string): string {
  const index = name.lastIndexOf('.')
  return index > 0 && index < name.length - 1 ? name.slice(index + 1).toLowerCase() : ''
}

function attachmentIcon(attachment: ChatAttachment): AttachmentIcon {
  const extension = attachmentExtension(attachment.name)
  const mimeType = attachment.mimeType.toLowerCase()

  if (extension === 'pdf' || mimeType === 'application/pdf') {
    return { Icon: FileText, className: 'bg-red-500/10 text-red-500' }
  }
  if (DOCUMENT_EXTENSIONS.has(extension) || mimeType.includes('word') || mimeType.includes('opendocument.text')) {
    return { Icon: FileText, className: 'bg-blue-500/10 text-blue-500' }
  }
  if (
    SPREADSHEET_EXTENSIONS.has(extension)
    || mimeType === 'text/csv'
    || mimeType.includes('spreadsheet')
    || mimeType.includes('excel')
  ) {
    return { Icon: FileSpreadsheet, className: 'bg-emerald-500/10 text-emerald-500' }
  }
  if (PRESENTATION_EXTENSIONS.has(extension) || mimeType.includes('presentation') || mimeType.includes('powerpoint')) {
    return { Icon: Presentation, className: 'bg-orange-500/10 text-orange-500' }
  }
  if (ARCHIVE_EXTENSIONS.has(extension) || mimeType.includes('zip') || mimeType.includes('compressed')) {
    return { Icon: FileArchive, className: 'bg-amber-500/10 text-amber-500' }
  }
  if (extension === 'json' || extension === 'jsonl' || mimeType === 'application/json') {
    return { Icon: FileJson, className: 'bg-yellow-500/10 text-yellow-600 dark:text-yellow-400' }
  }
  if (DATABASE_EXTENSIONS.has(extension) || mimeType.includes('sqlite')) {
    return { Icon: Database, className: 'bg-cyan-500/10 text-cyan-600 dark:text-cyan-400' }
  }
  if (CODE_EXTENSIONS.has(extension)) {
    return { Icon: FileCode, className: 'bg-cyan-500/10 text-cyan-600 dark:text-cyan-400' }
  }
  if (IMAGE_EXTENSIONS.has(extension) || mimeType.startsWith('image/')) {
    return { Icon: FileImage, className: 'bg-fuchsia-500/10 text-fuchsia-500' }
  }
  if (AUDIO_EXTENSIONS.has(extension) || mimeType.startsWith('audio/')) {
    return { Icon: FileAudio, className: 'bg-violet-500/10 text-violet-500' }
  }
  if (VIDEO_EXTENSIONS.has(extension) || mimeType.startsWith('video/')) {
    return { Icon: FileVideo, className: 'bg-rose-500/10 text-rose-500' }
  }
  if (TEXT_EXTENSIONS.has(extension) || mimeType.startsWith('text/')) {
    return { Icon: FileText, className: 'bg-teal-500/10 text-teal-600 dark:text-teal-400' }
  }
  return { Icon: File, className: 'bg-[hsl(var(--surface-base))] text-muted-soft' }
}

export function ChatAttachmentItem({ attachment, removeLabel, onRemove }: ChatAttachmentItemProps) {
  const imageSource = attachment.mimeType.startsWith('image/') ? attachment.dataURL : undefined
  const { Icon, className } = attachmentIcon(attachment)

  return (
    <div className="relative flex h-14 min-w-0 max-w-52 items-center gap-2 overflow-hidden rounded-lg border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-subtle))] px-2.5 pr-7">
      {imageSource ? (
        <img src={imageSource} alt={attachment.name} className="h-9 w-9 shrink-0 rounded-md object-cover" />
      ) : (
        <span className={`flex h-9 w-9 shrink-0 items-center justify-center rounded-md ${className}`}>
          <Icon className="h-4 w-4" />
        </span>
      )}
      <span className="min-w-0">
        <span className="block truncate text-xs font-medium text-foreground" title={attachment.name}>
          {attachment.name}
        </span>
        <span className="block text-[11px] text-faint">{formatSize(attachment.size)}</span>
      </span>
      {onRemove ? (
        <button
          type="button"
          onClick={onRemove}
          className="absolute right-1 top-1 rounded-full p-0.5 text-muted-soft transition-colors hover:bg-[hsl(var(--surface-base))] hover:text-foreground"
          aria-label={removeLabel}
        >
          <X className="h-3.5 w-3.5" />
        </button>
      ) : null}
    </div>
  )
}
