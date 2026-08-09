import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
} from '@/components/ui/select'
import { cn } from '@/lib/utils'
import { PlaygroundModelIcon } from '@/features/playground/components/playground-model-icon'
import type { GatewayModel } from '@/features/playground/lib/types'

type ModelPickerProps = {
  models: GatewayModel[]
  value: string | null
  onChange: (id: string) => void
  disabled?: boolean
  placeholder: string
  className?: string
}

export function ModelPicker({
  models,
  value,
  onChange,
  disabled,
  placeholder,
  className,
}: ModelPickerProps) {
  const active = models.find((model) => model.id === value)

  return (
    <Select value={value ?? undefined} onValueChange={onChange} disabled={disabled}>
      <SelectTrigger
        className={cn(
          'inline-flex h-8 w-[13rem] max-w-[13rem] gap-1 rounded-full border-none bg-transparent px-2.5 text-xs font-medium text-muted-soft outline-none transition-colors hover:bg-[hsl(var(--surface-subtle))] hover:text-foreground focus-visible:ring-2 focus-visible:ring-[hsl(var(--ring-soft))] disabled:opacity-50',
          className,
        )}
      >
        {active ? (
          <span className="flex min-w-0 items-center gap-1.5">
            <PlaygroundModelIcon
              modelId={active.id}
              displayName={active.displayName}
              ownedBy={active.ownedBy}
            />
            <span className="truncate">{active.id}</span>
          </span>
        ) : placeholder}
      </SelectTrigger>
      <SelectContent
        searchable={false}
        widthMode="content"
        className="max-h-[min(var(--radix-select-content-available-height),24rem)]"
      >
        {models.map((model) => (
          <SelectItem key={model.id} value={model.id} textValue={model.id}>
            <span className="flex min-w-0 items-center gap-1.5">
              <PlaygroundModelIcon
                modelId={model.id}
                displayName={model.displayName}
                ownedBy={model.ownedBy}
              />
              <span className="truncate">{model.id}</span>
            </span>
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}
