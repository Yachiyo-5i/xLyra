import { useEffect, useRef, useState } from 'react'
import { FileJson, Upload, X } from 'lucide-react'
import { cn } from '@/lib/utils'
import type { JsonFileItem } from '@/components/common/use-json-files'

/**
 * Shared JSON file drop zone: drag & drop, click to choose and paste, plus the
 * list of the chosen files. Used by the account import dialog.
 */

export const JSON_DROP_AREA_CLASS = 'h-[156px] min-h-[156px]'

function filesFromClipboard(dataTransfer: DataTransfer) {
  const files = Array.from(dataTransfer.files)
  const itemFiles = Array.from(dataTransfer.items)
    .filter((item) => item.kind === 'file')
    .map((item) => item.getAsFile())
    .filter((file): file is File => Boolean(file))

  if (files.length === 0) return itemFiles
  const seen = new Set(files.map((file) => `${file.name}:${file.size}:${file.lastModified}`))
  return [
    ...files,
    ...itemFiles.filter((file) => !seen.has(`${file.name}:${file.size}:${file.lastModified}`)),
  ]
}

function isEditablePasteTarget(target: EventTarget | null) {
  const element = target as HTMLElement | null
  const tagName = element?.tagName.toLowerCase()
  return tagName === 'input' || tagName === 'textarea' || Boolean(element?.isContentEditable)
}

function formatFileSize(bytes: number) {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

export function JsonFileDropArea({
  onFiles,
  active = true,
  disabled = false,
  title,
  hint,
  className,
}: {
  onFiles: (files: FileList | File[]) => void
  /** Paste is only accepted while the surrounding dialog shows this area. */
  active?: boolean
  disabled?: boolean
  title: string
  hint: string
  className?: string
}) {
  const inputRef = useRef<HTMLInputElement>(null)
  const [dragging, setDragging] = useState(false)

  useEffect(() => {
    if (!active || disabled) return
    const listener = (event: ClipboardEvent) => {
      if (isEditablePasteTarget(event.target)) return
      const clipboardData = event.clipboardData
      if (!clipboardData) return
      const pasted = filesFromClipboard(clipboardData)
      if (pasted.length === 0) return
      event.preventDefault()
      onFiles(pasted)
    }
    document.addEventListener('paste', listener)
    return () => document.removeEventListener('paste', listener)
  }, [active, disabled, onFiles])

  return (
    <div
      onDrop={(event) => {
        event.preventDefault()
        setDragging(false)
        if (!disabled && event.dataTransfer.files.length > 0) onFiles(event.dataTransfer.files)
      }}
      onDragOver={(event) => {
        event.preventDefault()
        setDragging(true)
      }}
      onDragLeave={(event) => {
        event.preventDefault()
        setDragging(false)
      }}
      onClick={() => {
        if (!disabled) inputRef.current?.click()
      }}
      className={cn(
        JSON_DROP_AREA_CLASS,
        'flex cursor-pointer flex-col items-center justify-center gap-2 rounded-inner border-2 border-dashed px-4 py-8 transition-colors',
        dragging ? 'border-tint bg-tint/5' : 'border-separator bg-elevated-1 hover:border-tint/50 hover:bg-fill-3',
        disabled && 'pointer-events-none opacity-60',
        className,
      )}
    >
      <Upload className="h-8 w-8 text-label-2" />
      <div className="text-body font-medium text-label">{title}</div>
      <div className="text-footnote text-label-2">{hint}</div>
      <input
        ref={inputRef}
        type="file"
        multiple
        accept=".json,application/json"
        className="hidden"
        onChange={(event) => {
          if (event.target.files) {
            onFiles(event.target.files)
            event.target.value = ''
          }
        }}
      />
    </div>
  )
}

export function JsonFileList({
  files,
  countLabel,
  onRemove,
}: {
  files: JsonFileItem[]
  countLabel: string
  onRemove: (id: string) => void
}) {
  if (files.length === 0) return null
  return (
    <div className="flex min-h-0 flex-1 flex-col gap-2">
      <div className="text-body font-medium text-label">{countLabel}</div>
      <div className="min-h-0 flex-1 space-y-1 overflow-y-auto pr-1">
        {files.map((item) => (
          <div
            key={item.id}
            className="flex items-center gap-3 rounded-inner border border-separator bg-elevated-1 px-3 py-2"
          >
            <FileJson className="h-4 w-4 shrink-0 text-label-2" />
            <div className="min-w-0 flex-1">
              <div className="truncate text-body text-label">{item.file.name}</div>
              <div className="text-footnote text-label-2">{formatFileSize(item.file.size)}</div>
            </div>
            <button
              type="button"
              onClick={(event) => {
                event.stopPropagation()
                onRemove(item.id)
              }}
              className="shrink-0 rounded p-1 text-label-2 transition-colors hover:bg-fill-3 hover:text-label"
            >
              <X className="h-3.5 w-3.5" />
            </button>
          </div>
        ))}
      </div>
    </div>
  )
}
