export function RequestsWorkspaceSkeleton() {
  return (
    <div className="space-y-6">
      <div className="space-y-2">
        <div className="h-4 w-16 rounded-inner bg-fill-3" />
        <div className="h-8 w-36 rounded-inner bg-fill-3" />
        <div className="h-4 w-80 rounded-inner bg-fill-3" />
      </div>
      <div className="space-y-3">
        <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
          <div className="h-10 rounded-inner bg-fill-3" />
          <div className="h-10 rounded-inner bg-fill-3" />
          <div className="h-10 rounded-inner bg-fill-3" />
          <div className="h-10 rounded-inner bg-fill-3" />
        </div>
        <div className="flex gap-3">
          <div className="h-10 w-32 rounded-inner bg-fill-3" />
          <div className="h-10 w-28 rounded-inner bg-fill-3" />
        </div>
      </div>
    </div>
  )
}
