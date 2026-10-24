import { useState, type ComponentType } from 'react'
import * as PopoverPrimitive from '@radix-ui/react-popover'
import { Ellipsis, LoaderCircle, PencilLine, RefreshCw, RotateCcw, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'

export function APIKeyActionsMenu({
  busy,
  resetDisabled,
  rotateDisabled,
  onEdit,
  onRotate,
  onReset,
  onDelete,
}: {
  busy?: boolean
  resetDisabled?: boolean
  rotateDisabled?: boolean
  onEdit: () => void
  onRotate: () => void
  onReset: () => void
  onDelete: () => void
}) {
  const { t } = useTranslation('api-keys')
  const [open, setOpen] = useState(false)

  function run(action: () => void) {
    setOpen(false)
    action()
  }

  return (
    <PopoverPrimitive.Root open={open} onOpenChange={setOpen}>
      <PopoverPrimitive.Trigger asChild>
        <Button
          size="icon-sm"
          variant="ghost"
          className="size-7 text-label-2 hover:text-label"
          disabled={busy}
          aria-label={t('table.actions')}
        >
          {busy ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <Ellipsis className="h-4 w-4" />}
        </Button>
      </PopoverPrimitive.Trigger>
      <PopoverPrimitive.Portal>
        <PopoverPrimitive.Content
          align="end"
          sideOffset={6}
          className="z-[120] w-44 overflow-hidden lg-regular rounded-inner py-1"
        >
          <ActionItem icon={PencilLine} label={t('table.edit')} onClick={() => run(onEdit)} />
          <ActionItem
            icon={RefreshCw}
            label={t('table.rotate')}
            disabled={rotateDisabled}
            title={rotateDisabled ? t('table.rotateCustomDisabled') : undefined}
            onClick={() => run(onRotate)}
          />
          <ActionItem
            icon={RotateCcw}
            label={t('table.resetQuota')}
            disabled={resetDisabled}
            onClick={() => run(onReset)}
          />
          <ActionItem icon={Trash2} label={t('table.delete')} destructive onClick={() => run(onDelete)} />
        </PopoverPrimitive.Content>
      </PopoverPrimitive.Portal>
    </PopoverPrimitive.Root>
  )
}

function ActionItem({
  icon: Icon,
  label,
  disabled,
  destructive,
  title,
  onClick,
}: {
  icon: ComponentType<{ className?: string }>
  label: string
  disabled?: boolean
  destructive?: boolean
  title?: string
  onClick: () => void
}) {
  return (
    <button
      type="button"
      disabled={disabled}
      title={title}
      onClick={onClick}
      className={`flex w-full items-center gap-3 px-3 py-2 text-left text-body font-medium transition-colors hover:bg-fill-3 disabled:cursor-not-allowed disabled:opacity-40 ${destructive ? 'text-danger' : 'text-label'}`}
    >
      <Icon className="h-4 w-4 text-current" />
      {label}
    </button>
  )
}
