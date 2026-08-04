import { useState } from 'react'
import { createPortal } from 'react-dom'
import { cn } from '@/lib/utils'

type PointerPosition = {
  x: number
  y: number
}

export function OAuthRefreshWarningBadge({
  label,
  message,
  className,
}: {
  label: string
  message: string
  className?: string
}) {
  const [open, setOpen] = useState(false)
  const [position, setPosition] = useState<PointerPosition | null>(null)

  const tooltip = open && position && typeof document !== 'undefined'
    ? createPortal(
        <div
          className="glass-panel-strong pointer-events-none fixed z-[160] max-w-[min(360px,calc(100vw-32px))] rounded-lg px-3 py-2 text-xs leading-5 text-foreground shadow-lg"
          style={{
            left: Math.min(position.x + 12, Math.max(12, window.innerWidth - 376)),
            top: Math.min(position.y + 14, Math.max(12, window.innerHeight - 96)),
          }}
        >
          <div className="font-medium text-amber-300">{label}</div>
          <div className="text-muted-foreground">{message}</div>
        </div>,
        document.body,
      )
    : null

  return (
    <>
      <span
        className={cn(
          'inline-flex size-5 shrink-0 cursor-default items-center justify-center rounded-full border border-amber-500/35 bg-amber-500/12 text-[13px] font-black leading-none text-amber-300',
          className,
        )}
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
        !
      </span>
      {tooltip}
    </>
  )
}
