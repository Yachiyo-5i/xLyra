import { useLayoutEffect, useRef, type ReactNode } from 'react'
import { ArrowUp, Square } from 'lucide-react'
import { cn } from '@/lib/utils'
import { ClawdPet } from '@/features/playground/components/clawd-pet'
import { filesFromClipboard } from '@/features/playground/lib/clipboard-files'

type ComposerProps = {
  value: string
  onChange: (value: string) => void
  onSubmit: () => void
  onStop?: () => void
  streaming?: boolean
  canSubmit: boolean
  placeholder: string
  controls?: ReactNode
  trailingControls?: ReactNode
  attachments?: ReactNode
  onPasteFiles?: (files: File[]) => boolean
  stopButtonClassName?: string
  stopIconClassName?: string
  /** 输入框右上角趴一只会动的小螃蟹，默认开启 */
  mascot?: boolean
}

export function Composer({
  value,
  onChange,
  onSubmit,
  onStop,
  streaming = false,
  canSubmit,
  placeholder,
  controls,
  trailingControls,
  attachments,
  onPasteFiles,
  stopButtonClassName,
  stopIconClassName,
  mascot = true,
}: ComposerProps) {
  const textareaRef = useRef<HTMLTextAreaElement>(null)

  useLayoutEffect(() => {
    const el = textareaRef.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = `${Math.min(el.scrollHeight, 220)}px`
  }, [value])

  const handleKeyDown = (event: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) {
      event.preventDefault()
      if (canSubmit) onSubmit()
    }
  }

  const handlePaste = (event: React.ClipboardEvent<HTMLTextAreaElement>) => {
    if (!onPasteFiles) return
    const pastedFiles = filesFromClipboard(event.clipboardData)
    if (pastedFiles.length > 0 && onPasteFiles(pastedFiles)) event.preventDefault()
  }

  return (
    <div className="relative">
      {mascot ? <ClawdPet value={value} streaming={streaming} /> : null}
      <div
        data-playground-composer="true"
        // 输入框：浮起的面板质感，见 index.css .composer-surface
        className="composer-surface rounded-[26px] px-3 pb-2 pt-3"
      >
        {attachments ? <div className="px-1 pb-2">{attachments}</div> : null}
        <textarea
          ref={textareaRef}
          value={value}
          onChange={(event) => onChange(event.target.value)}
          onKeyDown={handleKeyDown}
          onPaste={handlePaste}
          placeholder={placeholder}
          rows={1}
          className="max-h-[220px] w-full resize-none bg-transparent px-1.5 text-[15px] leading-6 text-label outline-none placeholder:text-label-3"
        />
        <div className="mt-1 flex items-center justify-between gap-2">
          <div className="flex min-w-0 items-center gap-1">{controls}</div>
          <div className="flex min-w-0 items-center gap-1">
            {trailingControls}
            {streaming ? (
              <button
                type="button"
                onClick={onStop}
                className={cn('flex h-11 w-11 shrink-0 items-center justify-center rounded-full bg-label text-canvas transition-opacity hover:opacity-90 md:h-9 md:w-9', stopButtonClassName)}
                aria-label="stop"
              >
                <Square className={cn('h-3.5 w-3.5 fill-current', stopIconClassName)} />
              </button>
            ) : (
              <button
                type="button"
                onClick={onSubmit}
                disabled={!canSubmit}
                className={cn(
                  'flex h-11 w-11 shrink-0 items-center justify-center rounded-full transition-opacity md:h-9 md:w-9',
                  canSubmit
                    ? 'bg-tint text-tint-foreground hover:opacity-90'
                    : 'cursor-not-allowed bg-fill-2 text-label-3',
                )}
                aria-label="send"
              >
                <ArrowUp className="h-4 w-4" />
              </button>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}
