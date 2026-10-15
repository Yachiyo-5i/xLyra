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
      'sheet-overlay-animate fixed inset-0 z-50 bg-black/30',
      className,
    )}
    {...props}
  />
))
DrawOverlay.displayName = DialogPrimitive.Overlay.displayName

/**
 * 抽屉：覆盖层 Liquid Glass（.lg-thick），浮动、圆角 24。
 * - bottom：底部 Sheet，左右与底部内缩 8，顶部有 grabber；
 * - right：移动端同 bottom，桌面（≥ 1024）为右侧浮动面板，距边 12、宽 560。
 * 进场方向由 index.css 中的 .draw-content[data-side] 按屏幕宽度决定。
 */
const drawVariants = cva(
  'lg-thick draw-content fixed z-50 flex flex-col overflow-hidden rounded-shell p-0 will-change-transform',
  {
    variants: {
      side: {
        bottom: 'inset-x-2 bottom-[calc(env(safe-area-inset-bottom,0px)+8px)] h-auto max-h-[88dvh]',
        right:
          'inset-x-2 bottom-[calc(env(safe-area-inset-bottom,0px)+8px)] h-auto max-h-[88dvh] lg:left-auto lg:right-3 lg:top-3 lg:bottom-3 lg:max-h-none lg:w-[min(calc(100vw-24px),560px)]',
      },
    },
    defaultVariants: {
      side: 'bottom',
    },
  },
)

const DrawContent = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Content> & VariantProps<typeof drawVariants>
>(({ side, className, children, ...props }, ref) => (
  <DrawPortal>
    <DrawOverlay />
    <DialogPrimitive.Content ref={ref} data-side={side ?? 'bottom'} className={cn(drawVariants({ side }), className)} {...props}>
      <div className={cn('flex shrink-0 justify-center pt-2', side === 'right' && 'lg:hidden')}>
        <span className="h-[5px] w-9 rounded-full bg-fill-1" />
      </div>
      {children}
    </DialogPrimitive.Content>
  </DrawPortal>
))
DrawContent.displayName = DialogPrimitive.Content.displayName

function DrawHeader({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn('border-b border-separator px-6 py-4 lg:py-5', className)} {...props} />
}

function DrawBody({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn('min-h-0 flex-1 overflow-y-auto px-6 py-5', className)} {...props} />
}

function DrawFooter({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn('border-t border-separator px-6 py-4', className)} {...props} />
}

const DrawTitle = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Title>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Title>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Title ref={ref} className={cn('text-title-2 text-label', className)} {...props} />
))
DrawTitle.displayName = DialogPrimitive.Title.displayName

const DrawDescription = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Description>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Description>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Description
    ref={ref}
    className={cn('mt-1 text-body text-label-2', className)}
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
