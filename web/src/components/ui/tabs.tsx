import * as React from 'react'
import * as TabsPrimitive from '@radix-ui/react-tabs'
import { cva, type VariantProps } from 'class-variance-authority'
import { cn } from '@/lib/utils'

type TabsVariant = 'segmented' | 'segmented-accent' | 'underline' | 'slash'

const TabsVariantContext = React.createContext<TabsVariant>('segmented')

type TabsProps = React.ComponentPropsWithoutRef<typeof TabsPrimitive.Root> & {
  variant?: TabsVariant
}

/**
 * Tabs
 * - segmented：分段控件，胶囊底 + 随选中项滑动的指示块（默认）
 * - segmented-accent：同上，指示块为强调色，用于需要突出当前模式的地方
 * - underline：页面级标签，选中项下方 2px 强调色线
 * - slash：卡片标题里的轻量切换，「今日 / 本周 / 本月」
 */
function Tabs({ variant = 'segmented', ...props }: TabsProps) {
  return (
    <TabsVariantContext.Provider value={variant}>
      <TabsPrimitive.Root {...props} />
    </TabsVariantContext.Provider>
  )
}

const tabsListVariants = cva('', {
  variants: {
    variant: {
      segmented: 'relative grid h-[var(--control-h)] w-full auto-cols-fr grid-flow-col items-center rounded-full bg-fill-3 p-0.5',
      'segmented-accent': 'relative grid h-[var(--control-h)] w-full auto-cols-fr grid-flow-col items-center rounded-full bg-fill-3 p-0.5',
      underline: 'relative inline-flex items-center gap-6 border-b border-separator',
      slash: 'inline-flex items-center',
    },
  },
  defaultVariants: {
    variant: 'segmented',
  },
})

const tabsTriggerVariants = cva(
  'relative z-10 inline-flex min-w-0 shrink-0 select-none items-center justify-center whitespace-nowrap outline-none transition-colors duration-200 focus-visible:ring-2 focus-visible:ring-tint/55 disabled:pointer-events-none disabled:opacity-40',
  {
    variants: {
      variant: {
        segmented: 'h-full rounded-full px-3 text-subhead text-label-2 hover:text-label data-[state=active]:text-label',
        'segmented-accent': 'h-full rounded-full px-3 text-subhead text-label-2 hover:text-label data-[state=active]:font-semibold data-[state=active]:text-tint-foreground',
        underline: 'rounded-none px-0 py-3 text-body text-label-2 hover:text-label data-[state=active]:text-label',
        slash: 'slash-tabs-trigger cursor-pointer px-0',
      },
    },
    defaultVariants: {
      variant: 'segmented',
    },
  },
)

/** 滑动指示块的外观：分段控件为浮起的胶囊，下划线为 2px 强调色线 */
const indicatorClass: Partial<Record<TabsVariant, string>> = {
  segmented: 'top-0.5 bottom-0.5 rounded-full bg-white/18 shadow-[0_1px_4px_rgb(0_0_0_/_0.35),inset_0_0.5px_0_rgb(255_255_255_/_0.12)]',
  'segmented-accent': 'top-0.5 bottom-0.5 rounded-full bg-tint shadow-[0_1px_4px_rgb(0_0_0_/_0.3)]',
  underline: '-bottom-px h-0.5 rounded-full bg-tint',
}

/** 跟踪当前选中的触发器位置，驱动指示块滑动（选中项、尺寸变化时都会更新） */
function useActiveIndicator(listRef: React.RefObject<HTMLElement | null>, enabled: boolean) {
  const [box, setBox] = React.useState<{ left: number; width: number } | null>(null)

  React.useLayoutEffect(() => {
    const list = listRef.current
    if (!enabled || !list) return

    const measure = () => {
      const active = list.querySelector<HTMLElement>('[role="tab"][data-state="active"]')
      setBox(active ? { left: active.offsetLeft, width: active.offsetWidth } : null)
    }

    measure()
    const mutationObserver = new MutationObserver(measure)
    mutationObserver.observe(list, { subtree: true, attributes: true, attributeFilter: ['data-state'] })
    const resizeObserver = new ResizeObserver(measure)
    resizeObserver.observe(list)

    return () => {
      mutationObserver.disconnect()
      resizeObserver.disconnect()
    }
  }, [enabled, listRef])

  return box
}

const TabsList = React.forwardRef<
  React.ElementRef<typeof TabsPrimitive.List>,
  React.ComponentPropsWithoutRef<typeof TabsPrimitive.List> & VariantProps<typeof tabsListVariants>
>(({ className, variant, children, ...props }, ref) => {
  const contextVariant = React.useContext(TabsVariantContext)
  const resolvedVariant = variant ?? contextVariant
  const listRef = React.useRef<HTMLDivElement>(null)
  React.useImperativeHandle(ref, () => listRef.current as HTMLDivElement)
  const indicator = indicatorClass[resolvedVariant]
  const box = useActiveIndicator(listRef, Boolean(indicator))

  return (
    <TabsPrimitive.List
      ref={listRef}
      className={cn(tabsListVariants({ variant: resolvedVariant }), className)}
      {...props}
    >
      {indicator && box ? (
        <span
          aria-hidden="true"
          className={cn(
            'pointer-events-none absolute left-0 transition-[transform,width] duration-300 ease-[cubic-bezier(0.34,1.25,0.64,1)] motion-reduce:transition-none',
            indicator,
          )}
          style={{ width: box.width, transform: `translateX(${box.left}px)` }}
        />
      ) : null}
      {children}
    </TabsPrimitive.List>
  )
})
TabsList.displayName = TabsPrimitive.List.displayName

const TabsTrigger = React.forwardRef<
  React.ElementRef<typeof TabsPrimitive.Trigger>,
  React.ComponentPropsWithoutRef<typeof TabsPrimitive.Trigger> & VariantProps<typeof tabsTriggerVariants>
>(({ className, variant, ...props }, ref) => {
  const contextVariant = React.useContext(TabsVariantContext)
  return (
    <TabsPrimitive.Trigger
      ref={ref}
      className={cn(tabsTriggerVariants({ variant: variant ?? contextVariant }), className)}
      {...props}
    />
  )
})
TabsTrigger.displayName = TabsPrimitive.Trigger.displayName

const TabsContent = React.forwardRef<
  React.ElementRef<typeof TabsPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof TabsPrimitive.Content>
>(({ className, ...props }, ref) => (
  <TabsPrimitive.Content ref={ref} className={cn('mt-4 outline-none', className)} {...props} />
))
TabsContent.displayName = TabsPrimitive.Content.displayName

export { Tabs, TabsContent, TabsList, TabsTrigger }
