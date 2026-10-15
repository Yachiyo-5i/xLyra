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

/**
 * Toast：功能层 Liquid Glass（外观见 index.css 的 .app-toast），圆角 20。
 * 不再按类型铺彩色渐变底，只用系统色图标区分成功 / 警告 / 错误（HIG：玻璃上的颜色要克制）。
 */
export function AppToaster() {
  return (
    <Toaster
      position="top-right"
      offset={toastViewportOffset}
      mobileOffset={toastViewportOffset}
      className="app-toaster"
      icons={{
        success: <CheckCircle2 className="size-[18px] text-success" />,
        info: <Info className="size-[18px] text-info" />,
        warning: <AlertTriangle className="size-[18px] text-warning" />,
        error: <AlertCircle className="size-[18px] text-danger" />,
        loading: <LoaderCircle className="size-[18px] animate-spin text-label-2" />,
        close: <X className="size-3.5" />,
      }}
      toastOptions={{
        classNames: {
          toast: 'app-toast',
          content: '!gap-0.5',
          title: '!text-subhead !font-semibold !text-label',
          description: '!text-footnote !text-label-2',
          icon: '!mt-px !mr-2.5 !self-start',
          closeButton: '!border-0 !bg-fill-2 !text-label-2 hover:!bg-fill-1 hover:!text-label',
          actionButton: '!rounded-full !bg-tint !px-3 !text-callout !font-medium !text-tint-foreground',
          cancelButton: '!rounded-full !bg-fill-2 !px-3 !text-callout !font-medium !text-label',
        },
      }}
    />
  )
}
