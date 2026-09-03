import { useEffect, useRef, useState } from 'react'
import * as PopoverPrimitive from '@radix-ui/react-popover'
import { useTranslation } from 'react-i18next'
import { Check, ChevronDown, ChevronRight } from 'lucide-react'
import { cn } from '@/lib/utils'
import { useMobileLayout } from '@/hooks/use-media-query'
import { PlaygroundModelIcon } from '@/features/playground/components/playground-model-icon'
import type { GatewayModel } from '@/features/playground/lib/types'

type APIKeyOption = {
  id: string
  name: string
}

type ParameterOption<T extends string> = {
  value: T
  label: string
}

type ModelParameterPickerProps<T extends string> = {
  apiKeys?: APIKeyOption[]
  apiKeyId?: string | null
  onAPIKeyChange?: (id: string) => void
  models: GatewayModel[]
  model: string | null
  onModelChange: (id: string) => void
  parameterLabel: string
  parameterValue: T
  parameterOptions: ParameterOption<T>[]
  onParameterChange: (value: T) => void
  disabled?: boolean
  triggerClassName?: string
  panelClassName?: string
  subPanelClassName?: string
  panelRenderer?: (children: React.ReactNode, kind: 'panel' | 'subpanel') => React.ReactNode
}

function SubPanel<T extends string>({
  items,
  selected,
  onSelect,
  emptyLabel,
  mobile,
  className,
  panelRenderer,
}: {
  items: { value: T; label: string; icon?: React.ReactNode }[]
  selected: T | null
  onSelect: (value: T) => void
  emptyLabel?: string
  mobile: boolean
  className?: string
  panelRenderer?: (children: React.ReactNode, kind: 'panel' | 'subpanel') => React.ReactNode
}) {
  const content = (
    <div
      className={cn(
        'glass-panel-strong max-h-[min(60vh,24rem)] min-w-[10rem] overflow-y-auto overscroll-contain rounded-xl p-1.5 shadow-[var(--shadow-dialog)]',
        mobile && 'w-52 max-w-[calc(100vw-1.5rem)]',
        className,
      )}
    >
      {items.length === 0 && emptyLabel ? (
        <p className="px-2.5 py-1.5 text-xs text-muted-soft">{emptyLabel}</p>
      ) : null}
      {items.map((item) => (
        <button
          key={item.value}
          type="button"
          onClick={() => onSelect(item.value)}
          className={cn(
            'flex w-full items-center gap-2 rounded-lg px-2.5 py-1.5 text-left text-xs transition-colors',
            item.value === selected
              ? 'text-foreground'
              : 'text-muted-soft hover:bg-[hsl(var(--surface-subtle))] hover:text-foreground',
          )}
        >
          {item.icon}
          <span className="min-w-0 flex-1 truncate">{item.label}</span>
          {item.value === selected ? <Check className="h-3.5 w-3.5 shrink-0 text-primary" /> : null}
        </button>
      ))}
    </div>
  )
  return panelRenderer ? panelRenderer(content, 'subpanel') : content
}

function PickerRow({
  label,
  value,
  overlap,
  children,
}: {
  label: string
  value: string
  overlap: boolean
  children: React.ReactNode
}) {
  const [open, setOpen] = useState(false)
  const closeTimer = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => () => {
    if (closeTimer.current) clearTimeout(closeTimer.current)
  }, [])

  const openPanel = () => {
    if (closeTimer.current) clearTimeout(closeTimer.current)
    setOpen(true)
  }

  const closePanel = () => {
    if (closeTimer.current) clearTimeout(closeTimer.current)
    closeTimer.current = setTimeout(() => setOpen(false), 100)
  }

  return (
    <PopoverPrimitive.Root open={open} onOpenChange={setOpen}>
      <PopoverPrimitive.Trigger asChild>
        <button
          type="button"
          onMouseEnter={openPanel}
          onMouseLeave={closePanel}
          className="flex w-full items-center gap-2 rounded-lg px-2.5 py-1.5 text-xs transition-colors hover:bg-[hsl(var(--surface-subtle))]"
        >
          <span className="flex-1 text-left text-muted-soft">{label}</span>
          <span className="max-w-[7rem] truncate text-foreground">{value}</span>
          <ChevronRight className="h-3 w-3 shrink-0 text-muted-soft opacity-60" />
        </button>
      </PopoverPrimitive.Trigger>
      <PopoverPrimitive.Portal>
        <PopoverPrimitive.Content
          side={overlap ? 'left' : 'right'}
          align="start"
          sideOffset={overlap ? -72 : 4}
          collisionPadding={12}
          onMouseEnter={openPanel}
          onMouseLeave={closePanel}
          className="z-[140]"
          onClick={(event) => event.stopPropagation()}
        >
          {children}
        </PopoverPrimitive.Content>
      </PopoverPrimitive.Portal>
    </PopoverPrimitive.Root>
  )
}

export function ModelParameterPicker<T extends string>({
  apiKeys,
  apiKeyId,
  onAPIKeyChange,
  models,
  model,
  onModelChange,
  parameterLabel,
  parameterValue,
  parameterOptions,
  onParameterChange,
  disabled,
  triggerClassName,
  panelClassName,
  subPanelClassName,
  panelRenderer,
}: ModelParameterPickerProps<T>) {
  const { t } = useTranslation('playground')
  const isMobile = useMobileLayout()
  const [open, setOpen] = useState(false)
  const selectedModel = models.find((item) => item.id === model)
  const selectedAPIKey = apiKeys?.find((item) => item.id === apiKeyId)
  const selectedParameter = parameterOptions.find((item) => item.value === parameterValue)
  const modelLabel = model ?? t('credential.modelPlaceholder')

  const selectAPIKey = (id: string) => {
    onAPIKeyChange?.(id)
    setOpen(false)
  }

  const selectModel = (id: string) => {
    onModelChange(id)
  }

  const selectParameter = (value: T) => {
    onParameterChange(value)
  }

  const panelChildren = (
    <>
      {apiKeys && onAPIKeyChange ? (
        <PickerRow
          label={t('credential.keyLabel')}
          value={selectedAPIKey?.name ?? t('credential.keyPlaceholder')}
          overlap={isMobile}
        >
          <SubPanel
            items={apiKeys.map((item) => ({ value: item.id, label: item.name }))}
            selected={apiKeyId ?? null}
            onSelect={selectAPIKey}
            mobile={isMobile}
            className={subPanelClassName}
            panelRenderer={panelRenderer}
          />
        </PickerRow>
      ) : null}
      <PickerRow label={t('picker.model')} value={modelLabel} overlap={isMobile}>
        <SubPanel
          items={models.map((item) => ({ value: item.id, label: item.id, icon: <PlaygroundModelIcon modelId={item.id} displayName={item.displayName} ownedBy={item.ownedBy} /> }))}
          selected={model}
          onSelect={selectModel}
          emptyLabel={t('picker.noModels')}
          mobile={isMobile}
          className={subPanelClassName}
          panelRenderer={panelRenderer}
        />
      </PickerRow>
      <PickerRow label={parameterLabel} value={selectedParameter?.label ?? parameterValue} overlap={isMobile}>
        <SubPanel
          items={parameterOptions}
          selected={parameterValue}
          onSelect={selectParameter}
          mobile={isMobile}
          className={subPanelClassName}
          panelRenderer={panelRenderer}
        />
      </PickerRow>
    </>
  )

  const panelContent = (
    <PopoverPrimitive.Root open={open} onOpenChange={setOpen}>
      <PopoverPrimitive.Trigger asChild>
        <button
          type="button"
          disabled={disabled}
          className={cn(
            'inline-flex h-8 w-max max-w-none items-center justify-end gap-1 rounded-full px-2.5 text-xs font-medium text-muted-soft outline-none transition-colors hover:bg-[hsl(var(--surface-subtle))] hover:text-foreground focus-visible:ring-2 focus-visible:ring-[hsl(var(--ring-soft))] disabled:opacity-50',
            triggerClassName,
          )}
        >
          {selectedModel ? (
            <PlaygroundModelIcon
              modelId={selectedModel.id}
              displayName={selectedModel.displayName}
              ownedBy={selectedModel.ownedBy}
            />
          ) : null}
          <span className="shrink-0 whitespace-nowrap text-foreground">{modelLabel}</span>
          <span className="shrink-0 opacity-60">· {selectedParameter?.label ?? parameterValue}</span>
          <ChevronDown className="h-3.5 w-3.5 shrink-0 opacity-60" />
        </button>
      </PopoverPrimitive.Trigger>
      <PopoverPrimitive.Portal>
        <PopoverPrimitive.Content
          side="top"
          align="end"
          sideOffset={8}
          collisionPadding={12}
          onClick={(event) => event.stopPropagation()}
          className={cn('glass-panel-strong z-[130] w-52 rounded-xl p-1.5 shadow-[var(--shadow-dialog)]', panelClassName)}
        >
          {panelRenderer ? panelRenderer(panelChildren, 'panel') : panelChildren}
        </PopoverPrimitive.Content>
      </PopoverPrimitive.Portal>
    </PopoverPrimitive.Root>
  )
  return panelRenderer ? <>{panelContent}</> : panelContent
}
