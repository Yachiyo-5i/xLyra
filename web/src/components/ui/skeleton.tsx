import type { HTMLAttributes } from 'react'
import { cn } from '@/lib/utils'

type SkeletonProps = HTMLAttributes<HTMLDivElement>

export function Skeleton({ className, ...props }: SkeletonProps) {
  return (
    <div
      className={cn(
        'animate-pulse rounded-lg bg-[linear-gradient(110deg,hsl(var(--surface-subtle)),hsl(var(--surface-soft-hover)),hsl(var(--surface-subtle)))] bg-[length:200%_100%]',
        className,
      )}
      {...props}
    />
  )
}
