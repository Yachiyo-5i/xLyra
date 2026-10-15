import type { HTMLAttributes } from 'react'
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
