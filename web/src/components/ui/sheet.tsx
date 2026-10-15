import * as React from 'react'
import * as DialogPrimitive from '@radix-ui/react-dialog'
import { cva, type VariantProps } from 'class-variance-authority'
import { cn } from '@/lib/utils'

const Sheet = DialogPrimitive.Root
const SheetTrigger = DialogPrimitive.Trigger
const SheetPortal = DialogPrimitive.Portal

const SheetOverlay = React.forwardRef<
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
SheetOverlay.displayName = DialogPrimitive.Overlay.displayName

/**
 * Sheet：覆盖层 Liquid Glass（.lg-thick），与屏幕边缘留出间距浮动、圆角 24，
 * 而不是贴边铺满（HIG：half sheet 与屏幕边缘内缩，让下方内容透出来）。
 */
const sheetVariants = cva(
  'lg-thick sheet-content-animate fixed z-50 flex flex-col overflow-hidden rounded-shell p-0 will-change-transform',
  {
    variants: {
      side: {
        right:
          'sheet-content-right bottom-[calc(env(safe-area-inset-bottom,0px)+12px)] right-3 top-[calc(env(safe-area-inset-top,0px)+12px)] w-[min(calc(100vw-24px),560px)]',
        left:
          'sheet-content-left bottom-[calc(env(safe-area-inset-bottom,0px)+12px)] left-3 top-[calc(env(safe-area-inset-top,0px)+12px)] w-[min(calc(100vw-24px),560px)]',
        top: 'sheet-content-top inset-x-2 top-[calc(env(safe-area-inset-top,0px)+8px)] h-auto max-h-[85vh]',
        bottom: 'sheet-content-bottom inset-x-2 bottom-[calc(env(safe-area-inset-bottom,0px)+8px)] h-auto max-h-[88dvh]',
      },
    },
    defaultVariants: {
      side: 'right',
    },
  },
)

const SheetContent = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Content> & VariantProps<typeof sheetVariants>
>(({ side, className, children, ...props }, ref) => (
  <SheetPortal>
    <SheetOverlay />
    <DialogPrimitive.Content ref={ref} className={cn(sheetVariants({ side }), className)} {...props}>
      {children}
    </DialogPrimitive.Content>
  </SheetPortal>
))
SheetContent.displayName = DialogPrimitive.Content.displayName

function SheetHeader({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn('border-b border-separator px-6 py-5', className)} {...props} />
}

function SheetBody({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn('min-h-0 flex-1 overflow-y-auto px-6 py-5', className)} {...props} />
}

function SheetFooter({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn('border-t border-separator px-6 py-4', className)} {...props} />
}

const SheetTitle = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Title>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Title>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Title ref={ref} className={cn('text-title-2 text-label', className)} {...props} />
))
SheetTitle.displayName = DialogPrimitive.Title.displayName

const SheetDescription = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Description>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Description>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Description
    ref={ref}
    className={cn('mt-1 text-body text-label-2', className)}
    {...props}
  />
))
SheetDescription.displayName = DialogPrimitive.Description.displayName

export {
  Sheet,
  SheetBody,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
}
