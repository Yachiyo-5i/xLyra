import * as React from 'react'
import * as DialogPrimitive from '@radix-ui/react-dialog'
import { cva, type VariantProps } from 'class-variance-authority'
import { DRAW_HEADER_CLASS, DrawContent, DrawFooter } from '@/components/ui/draw'
import { OverlayModeContext, useOverlayMode } from '@/components/ui/overlay-mode'
import { useMobileLayout } from '@/hooks/use-media-query'
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
      'sheet-overlay-animate fixed inset-0 z-50 bg-black/25',
      className,
    )}
    {...props}
  />
))
SheetOverlay.displayName = DialogPrimitive.Overlay.displayName

/**
 * Sheet：从右侧滑入的面板（左侧仅供移动端导航抽屉使用）。
 * 右侧：Liquid Glass thick，与屏幕边缘留出间距浮动、圆角 24；左侧导航抽屉：贴边全高、只有右侧圆角。
 * 没有关闭按钮：点遮罩或按 Esc 关闭；页脚固定为「确认在左、取消在右」，整体靠左。
 *
 * 其他两个覆盖层：居中弹窗用 Dialog，从底部展开的抽屉用 Draw。
 */
const sheetVariants = cva(
  'lg-thick sheet-content-animate fixed z-50 flex flex-col overflow-hidden rounded-shell p-0 will-change-transform',
  {
    variants: {
      side: {
        right:
          'sheet-content-right bottom-[calc(env(safe-area-inset-bottom,0px)+12px)] right-3 top-[calc(env(safe-area-inset-top,0px)+12px)] w-[min(calc(100vw-24px),560px)]',
        // 左侧导航抽屉：像普通侧边栏那样贴边、全高，只有右侧圆角；刘海 / 状态栏与 Home 指示条的安全区用内边距让出，
        // 玻璃仍然铺满整列（不要再叠加 top 偏移，否则 PWA 里顶部会出现一截空白）
        left:
          'sheet-content-left inset-y-0 left-0 w-[min(82vw,300px)] rounded-l-none rounded-r-shell pb-[env(safe-area-inset-bottom,0px)] pt-[env(safe-area-inset-top,0px)]',
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
>(({ side, className, children, ...props }, ref) => {
  const isMobile = useMobileLayout()

  // 右侧面板在移动端（< 1024）变成底部 Draw：有 grabber 与左上角关闭按钮；
  // 没有页脚的只读面板自动补一个全宽「关闭」。左侧导航抽屉保持原样。
  if (isMobile && side !== 'left') {
    const hasFooter = React.Children.toArray(children).some(
      (child) => React.isValidElement(child) && child.type === SheetFooter,
    )
    const drawClassName = className?.split(/\s+/).filter((token) => token && !/^(w-|max-w-|min-w-|overflow-)/.test(token)).join(' ')
    return (
      <DrawContent ref={ref} className={drawClassName} {...props}>
        {children}
        {hasFooter ? null : <DrawFooter />}
      </DrawContent>
    )
  }

  return (
    <SheetPortal>
      <SheetOverlay />
      <DialogPrimitive.Content ref={ref} className={cn(sheetVariants({ side }), className)} {...props}>
        <OverlayModeContext.Provider value="side">{children}</OverlayModeContext.Provider>
      </DialogPrimitive.Content>
    </SheetPortal>
  )
})
SheetContent.displayName = DialogPrimitive.Content.displayName

function SheetHeader({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  // 作为 Draw 呈现时，左上角有关闭按钮，标题区左侧要留出位置
  const draw = useOverlayMode() === 'draw'
  return <div className={cn('border-b border-separator', draw ? DRAW_HEADER_CLASS : 'px-6 py-5', className)} {...props} />
}

function SheetBody({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn('min-h-0 flex-1 overflow-y-auto px-6 py-5', className)} {...props} />
}

type SheetFooterProps = React.HTMLAttributes<HTMLDivElement> & {
  /** 确认动作，固定在最左侧 */
  confirm?: React.ReactNode
  /** 取消动作，紧跟在确认之后 */
  cancel?: React.ReactNode
}

/**
 * 页脚：右侧面板里确认在左、取消在右，整体靠左；
 * 作为 Draw 呈现时全宽等分，取消在左、确认在右（DOM 顺序不变，用 flex-row-reverse 翻转）。
 */
function SheetFooter({ className, confirm, cancel, children, ...props }: SheetFooterProps) {
  const draw = useOverlayMode() === 'draw'
  return (
    <div
      className={cn(
        'border-t border-separator px-6 py-4',
        draw ? 'flex flex-row-reverse gap-3 [&>*]:flex-1' : 'flex items-center justify-start gap-2',
        className,
      )}
      {...props}
    >
      {confirm}
      {cancel}
      {children}
    </div>
  )
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
