import type { ReactNode } from 'react'
import { RefreshCw } from 'lucide-react'
import { cn } from '@/lib/utils'
import { ConfirmPopover } from '@/features/playground/components/confirm-popover'

const ACTION_BUTTON_CLASS =
  'flex h-7 w-7 items-center justify-center rounded-md text-muted-soft transition-colors hover:bg-[hsl(var(--surface-subtle))] hover:text-foreground disabled:opacity-40'

type PlaygroundMessageActionProps = {
  label: string
  onClick: () => void
  disabled?: boolean
  className?: string
  children: ReactNode
}

export function PlaygroundMessageAction({
  label,
  onClick,
  disabled,
  className,
  children,
}: PlaygroundMessageActionProps) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      aria-label={label}
      title={label}
      className={cn(ACTION_BUTTON_CLASS, className)}
    >
      {children}
    </button>
  )
}

type PlaygroundRegenerateActionProps = {
  label: string
  message: string
  disabled?: boolean
  className?: string
  onConfirm: () => void
}

export function PlaygroundRegenerateAction({
  label,
  message,
  disabled,
  className,
  onConfirm,
}: PlaygroundRegenerateActionProps) {
  return (
    <ConfirmPopover message={message} confirmLabel={label} onConfirm={onConfirm} side="top" align="end">
      <button
        type="button"
        className={cn(ACTION_BUTTON_CLASS, className)}
        disabled={disabled}
        aria-label={label}
        title={label}
      >
        <RefreshCw className="h-3.5 w-3.5" />
      </button>
    </ConfirmPopover>
  )
}
