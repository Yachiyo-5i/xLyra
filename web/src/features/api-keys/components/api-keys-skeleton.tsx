import { Skeleton } from '@/components/ui/skeleton'

export function APIKeysSkeleton() {
  return (
    <div className="space-y-6">
      <div className="space-y-2">
        <Skeleton className="h-4 w-16" />
        <Skeleton className="h-8 w-32" />
        <Skeleton className="h-4 w-64" />
      </div>
      <div className="flex gap-3">
        <Skeleton className="h-11 w-52" />
        <Skeleton className="h-11 w-32" />
        <Skeleton className="h-11 w-32" />
        <Skeleton className="h-11 w-28" />
      </div>
      <Skeleton className="h-80 w-full" />
    </div>
  )
}
