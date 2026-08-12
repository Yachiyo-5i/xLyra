import { useLayoutEffect, useRef, type ReactNode } from 'react'
import { ArrowUp, Square } from 'lucide-react'
import { cn } from '@/lib/utils'
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
    <div
      data-playground-composer="true"
      className="rounded-[26px] border border-[hsl(var(--glass-border-strong))] bg-[hsl(var(--surface-field))] px-3 pb-2 pt-3 shadow-[0_8px_30px_rgba(0,0,0,0.06)] transition-colors focus-within:border-[hsl(var(--primary)/0.4)]"
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
        className="max-h-[220px] w-full resize-none bg-transparent px-1.5 text-[15px] leading-6 text-foreground outline-none placeholder:text-[hsl(var(--input-placeholder))]"
      />
      <div className="mt-1 flex items-center justify-between gap-2">
        <div className="flex min-w-0 items-center gap-1">{controls}</div>
        <div className="flex min-w-0 items-center gap-1">
          {trailingControls}
          {streaming ? (
            <button
              type="button"
              onClick={onStop}
              className="flex h-11 w-11 shrink-0 items-center justify-center rounded-full bg-foreground text-background transition-opacity hover:opacity-90 md:h-9 md:w-9"
              aria-label="stop"
            >
              <Square className="h-3.5 w-3.5 fill-current" />
            </button>
          ) : (
            <button
              type="button"
              onClick={onSubmit}
              disabled={!canSubmit}
              className={cn(
                'flex h-11 w-11 shrink-0 items-center justify-center rounded-full transition-opacity md:h-9 md:w-9',
                canSubmit
                  ? 'bg-primary text-primary-foreground hover:opacity-90'
                  : 'cursor-not-allowed bg-[hsl(var(--surface-subtle))] text-muted-soft',
              )}
              aria-label="send"
            >
              <ArrowUp className="h-4 w-4" />
            </button>
          )}
        </div>
      </div>
    </div>
  )
}
