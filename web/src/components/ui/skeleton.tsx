import type { HTMLAttributes } from 'react'
import { cn } from '@/lib/utils'

type SkeletonProps = HTMLAttributes<HTMLDivElement>

/** 占位：fill-4 底 + 1.6s 微光扫过；减弱动态效果时静止 */
export function Skeleton({ className, ...props }: SkeletonProps) {
  return <div aria-hidden="true" className={cn('skeleton-shimmer rounded-small', className)} {...props} />
}
