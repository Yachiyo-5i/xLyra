import * as React from 'react'
import * as DialogPrimitive from '@radix-ui/react-dialog'
import { X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { OverlayModeContext } from '@/components/ui/overlay-mode'
import { cn } from '@/lib/utils'

const Draw = DialogPrimitive.Root
const DrawTrigger = DialogPrimitive.Trigger
const DrawPortal = DialogPrimitive.Portal
const DrawClose = DialogPrimitive.Close

const DrawOverlay = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Overlay>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Overlay>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Overlay
    ref={ref}
    className={cn('sheet-overlay-animate fixed inset-0 z-50 bg-black/25', className)}
    {...props}
  />
))
DrawOverlay.displayName = DialogPrimitive.Overlay.displayName

/**
 * Draw：从底部向上展开的抽屉（Liquid Glass thick，圆角 24）。
 *
 * - 手机：左右与底部各内缩 8，最高 88dvh；
 * - 桌面：同样贴底，但宽度收窄并居中（最宽 640）；
 * - 顶部固定有 grabber，左上角固定有关闭按钮（与主界面右上角的圆形玻璃图标按钮同款），标题让出左侧位置；
 * - 页脚固定为全宽等分：取消在左、确认在右；只有一个关闭动作时单个全宽按钮。
 *
 * 其他两个覆盖层：居中弹窗用 Dialog，右侧滑入的面板用 Sheet。
 */
/**
 * 抽屉标题区的内边距：左侧给关闭按钮让位（左边距 16 + 按钮 40 + 间距 12 = 68），
 * 右侧 16 与左侧按钮的边距对称，把空间留给标题和右侧的图标按钮。
 * Sheet / Dialog 在移动端以抽屉呈现时复用同一个值，保持标题对齐。
 */
export const DRAW_HEADER_CLASS = 'py-4 pl-[68px] pr-4'

const DrawContent = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Content> & { overlayClassName?: string }
>(({ className, children, overlayClassName, ...props }, ref) => {
  const { t } = useTranslation('common')

  return (
    <DrawPortal>
      <DrawOverlay className={overlayClassName} />
      <DialogPrimitive.Content
        ref={ref}
        className={cn(
          'lg-thick draw-content fixed inset-x-2 bottom-[calc(env(safe-area-inset-bottom,0px)+8px)] z-50 flex h-auto max-h-[88dvh] flex-col overflow-hidden rounded-shell p-0 will-change-transform',
          'lg:inset-x-0 lg:mx-auto lg:w-[min(calc(100vw-16px),640px)]',
          className,
        )}
        {...props}
      >
        <div className="flex shrink-0 justify-center pt-2">
          <span className="h-[5px] w-9 rounded-full bg-fill-1" />
        </div>
        <DialogPrimitive.Close asChild>
          <Button
            type="button"
            variant="glass"
            size="icon"
            // top 23 = grabber(13) + 标题区上边距(16) + 单行标题半高(14) - 按钮半径(20)，与首行标题垂直居中
            className="absolute left-4 top-[23px] z-10 size-10 rounded-full"
            aria-label={t('actions.close')}
          >
            <X />
          </Button>
        </DialogPrimitive.Close>
        <OverlayModeContext.Provider value="draw">{children}</OverlayModeContext.Provider>
      </DialogPrimitive.Content>
    </DrawPortal>
  )
})
DrawContent.displayName = DialogPrimitive.Content.displayName

function DrawHeader({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn('border-b border-separator', DRAW_HEADER_CLASS, className)} {...props} />
}

function DrawBody({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn('min-h-0 flex-1 overflow-y-auto px-6 py-5', className)} {...props} />
}

type DrawFooterProps = Omit<React.HTMLAttributes<HTMLDivElement>, 'children'> & {
  /** 取消动作，固定在左侧 */
  cancel?: React.ReactNode
  /** 确认动作，固定在右侧 */
  confirm?: React.ReactNode
}

/**
 * 页脚：全宽等分。
 * - 同时有 cancel 与 confirm：两列，取消在左、确认在右；
 * - 只给其中一个：单个按钮全宽；
 * - 两个都不给（没有保存动作的抽屉）：自动放一个全宽「关闭」按钮。
 */
function DrawFooter({ className, cancel, confirm, ...props }: DrawFooterProps) {
  const { t } = useTranslation('common')
  const hasAction = Boolean(cancel || confirm)
  const columns = cancel && confirm ? 'grid-cols-2' : 'grid-cols-1'

  return (
    <div
      className={cn(
        'grid gap-3 border-t border-separator px-6 py-4',
        columns,
        '[&>*]:w-full',
        className,
      )}
      {...props}
    >
      {hasAction ? (
        <>
          {cancel}
          {confirm}
        </>
      ) : (
        <DialogPrimitive.Close asChild>
          <Button type="button" variant="secondary">
            {t('actions.close')}
          </Button>
        </DialogPrimitive.Close>
      )}
    </div>
  )
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
  <DialogPrimitive.Description ref={ref} className={cn('mt-1 text-body text-label-2', className)} {...props} />
))
DrawDescription.displayName = DialogPrimitive.Description.displayName

export {
  Draw,
  DrawBody,
  DrawClose,
  DrawContent,
  DrawDescription,
  DrawFooter,
  DrawHeader,
  DrawTitle,
  DrawTrigger,
}
