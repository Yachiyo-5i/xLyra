import * as React from 'react'
import * as SelectPrimitive from '@radix-ui/react-select'
import { Check, ChevronDown, ChevronUp, CirclePlus, Search } from 'lucide-react'
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
        ? 'inline-flex h-10 w-fit shrink-0 items-center justify-center gap-1.5 rounded-lg border border-dashed border-[hsl(var(--glass-border-strong))] bg-[hsl(var(--surface-subtle))] px-4 text-left text-sm font-medium text-foreground outline-none transition-[border-color,background-color,box-shadow,color] duration-150 hover:border-[hsl(var(--field-border-focus))] hover:bg-[hsl(var(--surface-soft-hover))] focus-visible:ring-2 focus-visible:ring-[hsl(var(--ring-soft))] data-[placeholder]:text-foreground'
        : 'glass-field flex h-11 w-full items-center justify-between gap-3 rounded-md px-4 text-left text-sm text-foreground outline-none transition-[border-color,background-color,box-shadow] duration-150 focus-visible:ring-2 focus-visible:ring-[hsl(var(--ring-soft))] data-[placeholder]:text-[hsl(var(--input-placeholder))]',
      variant === 'filter' && active && 'border-[hsl(var(--primary)/0.54)] bg-[hsl(var(--primary)/0.08)] text-primary',
      className,
    )}
    {...props}
  >
    {variant === 'filter' ? (
      <>
        <CirclePlus className="h-4 w-4 shrink-0" strokeWidth={2} />
        <span className="min-w-0 truncate">{active ? children : filterLabel ?? children}</span>
      </>
    ) : (
      <>
        <span className="min-w-0 flex-1 truncate">{children}</span>
        <SelectPrimitive.Icon asChild>
          <ChevronDown className="text-muted-soft h-4 w-4 shrink-0" />
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
          ref={ref}
          position={position}
          sideOffset={8}
          onCloseAutoFocus={() => setSearch('')}
          // 阻止滚轮/触摸事件冒泡到 document，否则弹层在 Dialog/Sheet 内打开时滚动会被其滚动锁拦截
          onWheel={(event) => event.stopPropagation()}
          onTouchMove={(event) => event.stopPropagation()}
          className={cn(
            'glass-panel-strong relative z-[120] max-h-[min(var(--radix-select-content-available-height),32rem)] overflow-hidden rounded-xl',
            position === 'popper' && 'translate-y-1',
            className,
          )}
          style={{
            ...contentWidthStyle,
            ...style,
          }}
          {...props}
        >
          {searchable ? (
            <div className="p-2 pb-0">
              <div className="flex items-center gap-2 rounded-lg border border-[hsl(var(--glass-border-strong))] bg-[hsl(var(--surface-field))] px-3 py-2">
                <Search className="h-4 w-4 shrink-0 text-muted-soft" />
                <input
                  ref={inputRef}
                  type="text"
                  placeholder="Search..."
                  className="w-full bg-transparent text-base text-foreground outline-none placeholder:text-[hsl(var(--input-placeholder))] md:text-sm"
                  value={search}
                  onChange={(e) => setSearch(e.target.value)}
                  onKeyDown={(e) => e.stopPropagation()}
                />
              </div>
            </div>
          ) : null}
          <SelectPrimitive.ScrollUpButton className="text-muted-soft flex cursor-default items-center justify-center py-2">
            <ChevronUp className="h-4 w-4" />
          </SelectPrimitive.ScrollUpButton>
          <SelectPrimitive.Viewport className="p-2">{children}</SelectPrimitive.Viewport>
          <SelectPrimitive.ScrollDownButton className="text-muted-soft flex cursor-default items-center justify-center py-2">
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
        'text-muted-soft relative flex w-full cursor-default select-none items-center rounded-lg py-2.5 pr-3 text-sm outline-none transition-colors duration-150 data-[disabled]:pointer-events-none data-[disabled]:opacity-50 data-[highlighted]:bg-[hsl(var(--surface-subtle))] data-[highlighted]:text-foreground data-[state=checked]:bg-[hsl(var(--surface-selected))] data-[state=checked]:text-foreground',
        hideIndicator ? 'pl-3' : 'pl-9',
        !visible && 'hidden',
        className,
      )}
      textValue={text}
      {...props}
    >
      {hideIndicator ? null : (
        <span className="absolute left-3 flex h-4 w-4 items-center justify-center">
          <SelectPrimitive.ItemIndicator>
            <Check className="h-3.5 w-3.5 text-primary" />
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
