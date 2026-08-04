import {
  AlertCircle,
  AlertTriangle,
  CheckCircle2,
  Info,
  LoaderCircle,
  X,
} from 'lucide-react'
import { Toaster } from 'sonner'

/** Sonner 用 --offset-top / --mobile-offset-top，不是 --y-position */
const TOAST_EDGE_OFFSET = 16
const toastViewportOffset = {
  top: `calc(env(safe-area-inset-top, 0px) + ${TOAST_EDGE_OFFSET}px)`,
  right: TOAST_EDGE_OFFSET,
} as const

export function AppToaster() {
  return (
    <Toaster
      position="top-right"
      offset={toastViewportOffset}
      mobileOffset={toastViewportOffset}
      className="app-toaster"
      richColors
      icons={{
        success: <CheckCircle2 className="h-4 w-4 text-[hsl(var(--badge-success-text))]" />,
        info: <Info className="h-4 w-4 text-[hsl(var(--badge-info-text))]" />,
        warning: <AlertTriangle className="h-4 w-4 text-[hsl(var(--badge-warning-text))]" />,
        error: <AlertCircle className="h-4 w-4 text-[hsl(var(--destructive))]" />,
        loading: <LoaderCircle className="h-4 w-4 animate-spin text-[hsl(var(--badge-accent-text))]" />,
        close: <X className="h-4 w-4" />,
      }}
      toastOptions={{
        classNames: {
          toast:
            '!border-[hsl(var(--glass-border))] !bg-[hsl(var(--surface-elevated))] !text-[hsl(var(--foreground))] !shadow-[0_18px_40px_rgba(4,8,18,0.24)] !backdrop-blur-[16px]',
          content: '!gap-0',
          title: '!text-inherit',
          description: '!text-[hsl(var(--text-muted-soft))]',
          icon: '!mt-0.5 !mr-3 !self-start',
          closeButton:
            '!border-[hsl(var(--glass-border))] !bg-[hsl(var(--surface-subtle))] !text-[hsl(var(--text-muted-soft))] hover:!bg-[hsl(var(--surface-soft-hover))] hover:!text-[hsl(var(--foreground))]',
          actionButton: '!bg-primary !text-primary-foreground',
          cancelButton: '!bg-[hsl(var(--surface-subtle))] !text-[hsl(var(--foreground))]',
          success:
            '!border-[hsl(var(--badge-success-border))] !bg-[linear-gradient(135deg,hsl(var(--badge-success-bg)),hsl(var(--surface-elevated)))]',
          info:
            '!border-[hsl(var(--badge-info-border))] !bg-[linear-gradient(135deg,hsl(var(--badge-info-bg)),hsl(var(--surface-elevated)))]',
          warning:
            '!border-[hsl(var(--badge-warning-border))] !bg-[linear-gradient(135deg,hsl(var(--badge-warning-bg)),hsl(var(--surface-elevated)))]',
          error:
            '!border-[hsl(var(--destructive)/0.24)] !bg-[linear-gradient(135deg,hsl(var(--destructive)/0.14),hsl(var(--surface-elevated)))]',
          loading:
            '!border-[hsl(var(--badge-accent-border))] !bg-[linear-gradient(135deg,hsl(var(--badge-accent-bg)),hsl(var(--surface-elevated)))]',
        },
      }}
    />
  )
}
