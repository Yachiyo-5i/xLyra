import { useState } from 'react'
import { createPortal } from 'react-dom'
import { cn } from '@/lib/utils'

type PointerPosition = {
  x: number
  y: number
}

export function OAuthErrorPreview({
  label,
  message,
  compact = false,
}: {
  label: string
  message: string
  compact?: boolean
}) {
  const [open, setOpen] = useState(false)
  const [position, setPosition] = useState<PointerPosition | null>(null)

  const tooltip = open && position && typeof document !== 'undefined'
    ? createPortal(
        <div
          className="glass-panel-strong pointer-events-none fixed z-[160] max-h-[min(52vh,360px)] w-[min(560px,calc(100vw-32px))] overflow-y-auto rounded-lg px-3 py-2 text-xs leading-5 text-foreground shadow-lg"
          style={{
            left: Math.min(position.x + 12, Math.max(12, window.innerWidth - 576)),
            top: Math.min(position.y + 14, Math.max(12, window.innerHeight - 376)),
          }}
        >
          <div className="mb-1 font-medium text-red-300">{label}</div>
          <div className="whitespace-pre-wrap break-words text-muted-foreground">{message}</div>
        </div>,
        document.body,
      )
    : null

  return (
    <>
      <div className={cn('font-medium text-foreground', compact ? 'mb-1 text-xs' : 'mb-2 text-sm')}>{label}</div>
      <div
        className={cn('rounded-md border border-red-500/20 bg-red-500/5 px-3 py-2', compact ? 'h-[54px]' : 'h-[64px]')}
        aria-label={`${label}: ${message}`}
        tabIndex={0}
        onMouseEnter={(event) => {
          setOpen(true)
          setPosition({ x: event.clientX, y: event.clientY })
        }}
        onMouseMove={(event) => {
          setPosition({ x: event.clientX, y: event.clientY })
        }}
        onMouseLeave={() => setOpen(false)}
        onFocus={(event) => {
          const rect = event.currentTarget.getBoundingClientRect()
          setOpen(true)
          setPosition({ x: rect.left + rect.width / 2, y: rect.bottom })
        }}
        onBlur={() => setOpen(false)}
      >
        <div
          className={cn(
            'overflow-hidden break-words text-red-300',
            compact ? 'line-clamp-2 text-xs leading-5' : 'line-clamp-2 text-sm leading-5',
          )}
        >
          {message}
        </div>
      </div>
      {tooltip}
    </>
  )
}
