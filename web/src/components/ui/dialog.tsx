import * as React from 'react'
import * as DialogPrimitive from '@radix-ui/react-dialog'
import { DRAW_HEADER_CLASS, DrawContent } from '@/components/ui/draw'
import { OverlayModeContext, useOverlayMode } from '@/components/ui/overlay-mode'
import { useMobileLayout } from '@/hooks/use-media-query'
import { cn } from '@/lib/utils'

const Dialog = DialogPrimitive.Root
const DialogPortal = DialogPrimitive.Portal

/** 遮罩：30% 黑色，不模糊 */
const DialogOverlay = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Overlay>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Overlay>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Overlay
    ref={ref}
    className={cn('sheet-overlay-animate fixed inset-0 z-50 bg-black/25', className)}
    {...props}
  />
))
DialogOverlay.displayName = DialogPrimitive.Overlay.displayName

// Every size is capped by the visible viewport height (dvh follows the mobile
// browser's collapsing toolbars). The sizes other than `default` are flex
// columns, so the header and footer stay put and DialogBody scrolls. `default`
// is a plain grid that other dialogs rely on to overflow, so it only gets a
// cap on small screens, where a tall dialog would otherwise run off the page.
const dialogSizeClassName = {
  default: 'grid w-[min(92vw,720px)] overflow-visible max-lg:max-h-[88dvh] max-lg:overflow-y-auto',
  sm: 'flex w-[min(92vw,520px)] max-h-[min(88dvh,880px)] flex-col overflow-hidden',
  md: 'flex w-[min(92vw,640px)] max-h-[min(88dvh,880px)] flex-col overflow-hidden',
  lg: 'flex w-[min(94vw,768px)] max-h-[min(88dvh,880px)] flex-col overflow-hidden',
  form: 'flex w-[min(96vw,907px)] max-h-[min(88dvh,880px)] flex-col overflow-hidden',
} as const

type DialogSize = keyof typeof dialogSizeClassName

type DialogContentProps = React.ComponentPropsWithoutRef<typeof DialogPrimitive.Content> & {
  overlayClassName?: string
  container?: HTMLElement | null
  size?: DialogSize
  /**
   * glass：覆盖层 Liquid Glass（`.lg-thick`），默认；
   * none：不带任何外观，由调用方自己绘制（图片查看器、Agent 页自带液态玻璃的弹窗）
   */
  surface?: 'glass' | 'none'
  /**
   * auto：桌面居中；移动端（< 1024）变为底部 Draw，默认——用于各类配置、编辑弹窗；
   * center：所有屏幕都居中——只用于确认类弹窗（删除确认、重置确认、结果提示）
   */
  presentation?: 'auto' | 'center'
}

const DialogContent = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Content>,
  DialogContentProps
>(({ className, children, overlayClassName, container, size = 'default', surface = 'glass', presentation, ...props }, ref) => {
  const isMobile = useMobileLayout()
  // 自绘外观的弹窗布局由调用方控制，始终居中
  const resolvedPresentation = presentation ?? (surface === 'none' ? 'center' : 'auto')

  if (isMobile && resolvedPresentation === 'auto') {
    // 移动端配置弹窗 → 底部 Draw；宽度、尺寸类只对居中弹窗有意义，这里去掉，保留 z-index 等其他类
    const drawClassName = className?.split(/\s+/).filter((token) => token && !/^(w-|max-w-|min-w-|overflow-)/.test(token)).join(' ')
    return (
      <DrawContent ref={ref} className={drawClassName} overlayClassName={overlayClassName} {...props}>
        {children}
      </DrawContent>
    )
  }

  return (
    <DialogPortal container={container ?? undefined}>
      <DialogOverlay className={overlayClassName} />
      <DialogPrimitive.Content
        ref={ref}
        className={cn(
          'dialog-content fixed left-1/2 top-1/2 z-50 -translate-x-1/2 -translate-y-1/2 gap-0 p-0 outline-none',
          surface === 'glass' && 'lg-thick rounded-shell',
          dialogSizeClassName[size],
          className,
        )}
        {...props}
      >
        <OverlayModeContext.Provider value="center">{children}</OverlayModeContext.Provider>
      </DialogPrimitive.Content>
    </DialogPortal>
  )
})
DialogContent.displayName = DialogPrimitive.Content.displayName

function DialogHeader({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  // 作为 Draw 呈现时，左上角有关闭按钮，标题区左侧要留出位置
  const draw = useOverlayMode() === 'draw'
  return (
    <div
      className={cn('border-b border-separator text-left', draw ? DRAW_HEADER_CLASS : 'px-6 py-5', className)}
      {...props}
    />
  )
}

function DialogBody({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn('min-h-0 flex-1 overflow-y-auto px-6 py-5', className)} {...props} />
}

type DialogFooterProps = React.HTMLAttributes<HTMLDivElement> & {
  // 取消动作固定渲染在左侧、确认动作固定渲染在右侧；按钮文本与样式由调用方决定。
  // 单个按钮或非「取消/确认」形态的页脚继续用 children。
  cancel?: React.ReactNode
  confirm?: React.ReactNode
}

/**
 * 页脚：居中弹窗整体靠右，取消在左、确认在右；
 * 作为 Draw 呈现时全宽等分，取消在左、确认在右。
 */
function DialogFooter({ className, cancel, confirm, children, ...props }: DialogFooterProps) {
  const draw = useOverlayMode() === 'draw'
  return (
    <div
      className={cn(
        'border-t border-separator px-6 py-4',
        draw ? 'grid grid-flow-col auto-cols-fr gap-3 [&>*]:w-full' : 'flex items-center justify-end gap-2',
        className,
      )}
      {...props}
    >
      {children}
      {cancel}
      {confirm}
    </div>
  )
}

const DialogTitle = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Title>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Title>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Title ref={ref} className={cn('text-title-2 text-label', className)} {...props} />
))
DialogTitle.displayName = DialogPrimitive.Title.displayName

const DialogDescription = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Description>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Description>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Description ref={ref} className={cn('mt-1 text-body text-label-2', className)} {...props} />
))
DialogDescription.displayName = DialogPrimitive.Description.displayName

export {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
}
