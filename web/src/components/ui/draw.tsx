import * as React from 'react'
import * as DialogPrimitive from '@radix-ui/react-dialog'
import { cva, type VariantProps } from 'class-variance-authority'
import { cn } from '@/lib/utils'

const Draw = DialogPrimitive.Root
const DrawTrigger = DialogPrimitive.Trigger
const DrawPortal = DialogPrimitive.Portal

const DrawOverlay = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Overlay>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Overlay>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Overlay
    ref={ref}
    className={cn(
      'sheet-overlay-animate fixed inset-0 z-50 bg-[rgba(10,14,24,0.36)] backdrop-blur-[14px]',
      className,
    )}
    {...props}
  />
))
DrawOverlay.displayName = DialogPrimitive.Overlay.displayName

const drawVariants = cva(
  'glass-panel-strong sheet-content-animate fixed z-50 flex flex-col overflow-hidden border-[hsl(var(--glass-border))] p-0 shadow-[var(--shadow-dialog)] will-change-transform',
  {
    variants: {
      side: {
        bottom: 'sheet-content-bottom inset-x-0 bottom-0 h-auto max-h-[88dvh] w-full rounded-t-[28px]',
        right:
          'sheet-content-bottom inset-x-0 bottom-0 h-auto max-h-[88dvh] w-full rounded-t-[28px] lg:sheet-content-right lg:inset-y-0 lg:left-auto lg:right-0 lg:h-full lg:max-h-none lg:rounded-l-2xl lg:rounded-tr-none',
      },
      size: {
        default: 'lg:w-[min(92vw,380px)]',
        wide: 'lg:w-[min(92vw,560px)]',
      },
    },
    defaultVariants: {
      side: 'bottom',
      size: 'default',
    },
  },
)

const DrawContent = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Content> & VariantProps<typeof drawVariants>
>(({ side, size, className, children, ...props }, ref) => (
  <DrawPortal>
    <DrawOverlay />
    <DialogPrimitive.Content ref={ref} className={cn(drawVariants({ side, size }), className)} {...props}>
      <div className="flex justify-center pb-1 pt-3 lg:hidden">
        <span className="h-1 w-11 rounded-full bg-[hsl(var(--text-muted-soft)/0.34)]" />
      </div>
      {children}
    </DialogPrimitive.Content>
  </DrawPortal>
))
DrawContent.displayName = DialogPrimitive.Content.displayName

function DrawHeader({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn('border-b border-[hsl(var(--divider))] px-5 py-4 lg:py-5', className)} {...props} />
}

function DrawBody({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn('min-h-0 flex-1 overflow-y-auto px-5 py-5', className)} {...props} />
}

function DrawFooter({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn('border-t border-[hsl(var(--divider))] px-5 py-4', className)} {...props} />
}

const DrawTitle = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Title>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Title>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Title ref={ref} className={cn('text-lg font-semibold text-foreground', className)} {...props} />
))
DrawTitle.displayName = DialogPrimitive.Title.displayName

const DrawDescription = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Description>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Description>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Description
    ref={ref}
    className={cn('text-muted-soft mt-1 text-sm leading-6', className)}
    {...props}
  />
))
DrawDescription.displayName = DialogPrimitive.Description.displayName

export {
  Draw,
  DrawBody,
  DrawContent,
  DrawDescription,
  DrawFooter,
  DrawHeader,
  DrawTitle,
  DrawTrigger,
}
