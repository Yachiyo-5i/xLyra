import * as React from 'react'
import * as SelectPrimitive from '@radix-ui/react-select'
import { Check, ChevronDown, ChevronUp, ChevronsUpDown, CirclePlus, Search } from 'lucide-react'
import { useLiquidGlassRef } from '@/components/glass/use-liquid-glass'
import {
  fieldIconClass,
  fieldTriggerClass,
  filterTriggerActiveClass,
  filterTriggerClass,
  menuItemClass,
  panelSearchClass,
  popoverPanelClass,
} from '@/components/ui/field-styles'
import { cn } from '@/lib/utils'

const Select = SelectPrimitive.Root
const SelectValue = SelectPrimitive.Value

const SelectSearchContext = React.createContext<string>('')

type SelectTriggerProps = React.ComponentPropsWithoutRef<typeof SelectPrimitive.Trigger> & {
  variant?: 'default' | 'filter'
  filterLabel?: React.ReactNode
  active?: boolean
}

const SelectTrigger = React.forwardRef<
  React.ElementRef<typeof SelectPrimitive.Trigger>,
  SelectTriggerProps
>(({ className, children, variant = 'default', filterLabel, active = false, ...props }, ref) => (
  <SelectPrimitive.Trigger
    ref={ref}
    className={cn(
      variant === 'filter'
        ? filterTriggerClass
        : cn(fieldTriggerClass, 'data-[placeholder]:text-label-3'),
      variant === 'filter' && active && filterTriggerActiveClass,
      className,
    )}
    {...props}
  >
    {variant === 'filter' ? (
      <>
        <CirclePlus className="size-3.5 shrink-0" strokeWidth={2} />
        <span className="min-w-0 truncate">{active ? children : filterLabel ?? children}</span>
      </>
    ) : (
      <>
        <span className="min-w-0 flex-1 truncate">{children}</span>
        <SelectPrimitive.Icon asChild>
          <ChevronsUpDown className={fieldIconClass} />
        </SelectPrimitive.Icon>
      </>
    )}
  </SelectPrimitive.Trigger>
))
SelectTrigger.displayName = SelectPrimitive.Trigger.displayName

type SelectContentProps = React.ComponentPropsWithoutRef<typeof SelectPrimitive.Content> & {
  searchable?: boolean
  widthMode?: 'trigger' | 'content'
}

const SelectContent = React.forwardRef<
  React.ElementRef<typeof SelectPrimitive.Content>,
  SelectContentProps
>(({ className, children, searchable = true, widthMode = 'trigger', position = 'popper', style, ...props }, ref) => {
  const [search, setSearch] = React.useState('')
  // 下拉面板属于功能层：展开时挂上 Liquid Glass 折射
  const glassRef = useLiquidGlassRef('regular', ref)
  const inputRef = React.useRef<HTMLInputElement>(null)
  const contentWidthStyle = widthMode === 'content'
    ? {
        width: 'max-content',
        minWidth: 'var(--radix-select-trigger-width)',
        maxWidth: 'min(32rem, calc(100vw - 2rem))',
      }
    : {
        width: 'var(--radix-select-trigger-width)',
        minWidth: 'var(--radix-select-trigger-width)',
      }

  return (
    <SelectSearchContext.Provider value={search}>
      <SelectPrimitive.Portal>
        <SelectPrimitive.Content
          ref={glassRef}
          position={position}
          sideOffset={8}
          onCloseAutoFocus={() => setSearch('')}
          // 阻止滚轮/触摸事件冒泡到 document，否则弹层在 Dialog/Sheet 内打开时滚动会被其滚动锁拦截
          onWheel={(event) => event.stopPropagation()}
          onTouchMove={(event) => event.stopPropagation()}
          className={cn(
            popoverPanelClass,
            'relative z-[120] max-h-[min(var(--radix-select-content-available-height),32rem)] origin-(--radix-select-content-transform-origin) overflow-hidden p-0',
            className,
          )}
          style={{
            ...contentWidthStyle,
            ...style,
          }}
          {...props}
        >
          {searchable ? (
            <div className="p-1.5 pb-0">
              <div className={panelSearchClass}>
                <Search className={fieldIconClass} />
                <input
                  ref={inputRef}
                  type="text"
                  placeholder="Search..."
                  className="w-full bg-transparent text-callout text-label outline-none placeholder:text-label-3"
                  value={search}
                  onChange={(e) => setSearch(e.target.value)}
                  onKeyDown={(e) => e.stopPropagation()}
                />
              </div>
            </div>
          ) : null}
          <SelectPrimitive.ScrollUpButton className="flex cursor-default items-center justify-center py-1.5 text-label-3">
            <ChevronUp className="h-4 w-4" />
          </SelectPrimitive.ScrollUpButton>
          <SelectPrimitive.Viewport className="p-1.5">{children}</SelectPrimitive.Viewport>
          <SelectPrimitive.ScrollDownButton className="flex cursor-default items-center justify-center py-1.5 text-label-3">
            <ChevronDown className="h-4 w-4" />
          </SelectPrimitive.ScrollDownButton>
        </SelectPrimitive.Content>
      </SelectPrimitive.Portal>
    </SelectSearchContext.Provider>
  )
})
SelectContent.displayName = SelectPrimitive.Content.displayName

type SelectItemProps = React.ComponentPropsWithoutRef<typeof SelectPrimitive.Item> & {
  hideIndicator?: boolean
  icon?: React.ReactNode
}

const SelectItem = React.forwardRef<
  React.ElementRef<typeof SelectPrimitive.Item>,
  SelectItemProps
>(({ className, children, hideIndicator = false, icon, textValue, ...props }, ref) => {
  const search = React.useContext(SelectSearchContext)
  const text =
    textValue ??
    (typeof children === 'string' ? children : typeof props.value === 'string' ? props.value : '')
  const visible = !search || text.toLowerCase().includes(search.toLowerCase())

  return (
    <SelectPrimitive.Item
      ref={ref}
      className={cn(
        menuItemClass,
        hideIndicator ? 'pl-2.5' : 'pl-8',
        !visible && 'hidden',
        className,
      )}
      textValue={text}
      {...props}
    >
      {hideIndicator ? null : (
        <span className="absolute left-2.5 flex size-4 items-center justify-center">
          <SelectPrimitive.ItemIndicator>
            <Check className="size-3.5 text-label" strokeWidth={2.5} />
          </SelectPrimitive.ItemIndicator>
        </span>
      )}
      <SelectPrimitive.ItemText>
        <span className="flex min-w-0 items-center gap-2">
          {icon ? <span className="flex h-4 w-4 shrink-0 items-center justify-center">{icon}</span> : null}
          <span className={cn('min-w-0', typeof children === 'string' && 'truncate')}>{children}</span>
        </span>
      </SelectPrimitive.ItemText>
    </SelectPrimitive.Item>
  )
})
SelectItem.displayName = SelectPrimitive.Item.displayName

export {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
}
