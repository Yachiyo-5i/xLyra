import { forwardRef, type ComponentPropsWithoutRef, type HTMLAttributes, type ReactNode } from 'react'
import type * as React from 'react'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

type CardProps = HTMLAttributes<HTMLDivElement> & {
  /**
   * regular：默认，图表、指标等内容卡片；
   * thick：承载表格或大段文字（更不透明）
   */
  material?: 'regular' | 'thick'
  /** 整张卡片可点击时才有 hover 反馈；内容卡片不做 hover 上浮 */
  interactive?: boolean
}

/** 内容层卡片：标准材质、圆角 20 */
export function Card({ className, material = 'regular', interactive = false, ...props }: CardProps) {
  return (
    <div
      className={cn(
        material === 'thick' ? 'mat-thick' : 'mat-regular',
        'rounded-card',
        interactive && 'cursor-pointer transition-colors duration-150 hover:bg-fill-4 active:scale-[0.995]',
        className,
      )}
      {...props}
    />
  )
}

export function CardContent({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div className={cn('px-5 pb-5', className)} {...props} />
}

/**
 * 列表项卡片：移动端列表、OAuth 连接卡共用的骨架。
 * 固定由上到下四段，段间距 16、内边距 16，由组件规定，调用方只填内容：
 *   CardHeading（名称 / 标签 / 右侧开关）→ CardMetrics（键值网格）→ 可选的自由内容 → CardFooter（操作）
 */
export function ItemCard({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <Card className={cn('flex min-w-0 flex-col gap-4 p-4', className)} {...props} />
}

type CardHeadingProps = {
  /** 左侧图标 / 头像 */
  leading?: ReactNode
  /** 名称，独占一行、过长截断 */
  title: ReactNode
  /** 名称下方的标签行（Badge、状态），可换行 */
  tags?: ReactNode
  /** 标签下方的次要信息一行（地址、邮箱、密钥） */
  meta?: ReactNode
  /** 右侧：开关或单个图标按钮 */
  trailing?: ReactNode
}

/** 卡片头部：图标 + 名称 + 标签 + 次要信息，右侧一个开关；名称独占标题行，不会被标签挤压。 */
export function CardHeading({ leading, title, tags, meta, trailing }: CardHeadingProps) {
  return (
    <div className="flex min-w-0 items-start gap-3">
      {leading}
      <div className="min-w-0 flex-1">
        <h3 className="truncate text-headline text-label">{title}</h3>
        {tags ? <div className="mt-1.5 flex flex-wrap items-center gap-1.5">{tags}</div> : null}
        {meta ? <div className="mt-1.5 min-w-0 truncate text-footnote text-label-2">{meta}</div> : null}
      </div>
      {trailing ? <div className="shrink-0">{trailing}</div> : null}
    </div>
  )
}

/** 键值网格：2 列（手机默认）或 3 列。 */
export function CardMetrics({ columns = 2, className, ...props }: HTMLAttributes<HTMLDivElement> & { columns?: 2 | 3 }) {
  return (
    <div
      className={cn('grid items-start gap-x-4 gap-y-3', columns === 3 ? 'grid-cols-3' : 'grid-cols-2', className)}
      {...props}
    />
  )
}

type CardMetricProps = Omit<ComponentPropsWithoutRef<'button'>, 'children' | 'title' | 'value'> & {
  /** 标签（Caption 1、第三级文字）；可带图标 */
  label: ReactNode
  value: ReactNode
  /** 值下方的第二行（等宽、第三级文字），例如用量里的费用 */
  secondary?: ReactNode
  valueClassName?: string
  /** 值的 title，悬停提示 */
  title?: string
  /** 没有 onClick 但需要作为按钮触发器时（如 HoverDetails 的 asChild）设为 true */
  interactive?: boolean
}

/**
 * 键值项：小标签 + 值（Footnote 加粗，等宽数字）；全项目只保留这一份。
 * 有 onClick（或 interactive）时渲染为按钮，值悬停变强调色；可 forwardRef 给 HoverDetails 等触发器。
 */
export const CardMetric = forwardRef<HTMLElement, CardMetricProps>(
  ({ label, value, secondary, valueClassName, title, interactive, onClick, className, ...props }, ref) => {
    const body = (
      <>
        <span className="flex min-w-0 items-center gap-1.5 truncate text-caption-1 text-label-3">{label}</span>
        <span
          className={cn(
            'mt-0.5 block truncate text-footnote font-medium tabular-nums text-label transition-colors group-hover/metric:text-tint group-disabled/metric:text-label',
            valueClassName,
          )}
          title={title ?? (typeof value === 'string' ? value : undefined)}
        >
          {value ?? '-'}
        </span>
        {secondary ? <span className="block truncate font-mono text-caption-1 tabular-nums text-label-3">{secondary}</span> : null}
      </>
    )
    if (!onClick && !interactive) {
      return <div ref={ref as React.Ref<HTMLDivElement>} className={cn('min-w-0', className)}>{body}</div>
    }
    return (
      <button
        ref={ref as React.Ref<HTMLButtonElement>}
        type="button"
        className={cn('group/metric min-w-0 text-left disabled:cursor-not-allowed disabled:opacity-60', className)}
        title={title}
        onClick={onClick}
        {...props}
      >
        {body}
      </button>
    )
  },
)
CardMetric.displayName = 'CardMetric'

/** 卡片页脚：分隔线 + 一排操作，整体靠右；图标按钮用 CardAction，文字按钮用 size="sm" 的 secondary。 */
export function CardFooter({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn('mt-auto flex items-center justify-end gap-1 border-t border-separator pt-3', className)}
      {...props}
    />
  )
}

/** 页脚里的图标按钮：幽灵样式，必须有 label（用作 title 与 aria-label）。 */
export function CardAction({
  label,
  className,
  disabled,
  onClick,
  children,
}: {
  label: string
  className?: string
  disabled?: boolean
  onClick: () => void
  children: ReactNode
}) {
  return (
    <Button
      type="button"
      variant="ghost"
      size="icon-sm"
      className={cn('text-label-2 hover:text-label', className)}
      disabled={disabled}
      onClick={onClick}
      title={label}
      aria-label={label}
    >
      {children}
    </Button>
  )
}
