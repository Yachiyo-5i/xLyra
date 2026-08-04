export function RequestsWorkspaceSkeleton() {
  return (
    <div className="space-y-6">
      <div className="space-y-2">
        <div className="h-4 w-16 rounded-md bg-[hsl(var(--surface-subtle))]" />
        <div className="h-8 w-36 rounded-md bg-[hsl(var(--surface-subtle))]" />
        <div className="h-4 w-80 rounded-md bg-[hsl(var(--surface-subtle))]" />
      </div>
      <div className="space-y-3">
        <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
          <div className="h-10 rounded-md bg-[hsl(var(--surface-subtle))]" />
          <div className="h-10 rounded-md bg-[hsl(var(--surface-subtle))]" />
          <div className="h-10 rounded-md bg-[hsl(var(--surface-subtle))]" />
          <div className="h-10 rounded-md bg-[hsl(var(--surface-subtle))]" />
        </div>
        <div className="flex gap-3">
          <div className="h-10 w-32 rounded-md bg-[hsl(var(--surface-subtle))]" />
          <div className="h-10 w-28 rounded-md bg-[hsl(var(--surface-subtle))]" />
        </div>
      </div>
    </div>
  )
}
