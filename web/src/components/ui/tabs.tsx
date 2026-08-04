import * as React from 'react'
import * as TabsPrimitive from '@radix-ui/react-tabs'
import { cva, type VariantProps } from 'class-variance-authority'
import { cn } from '@/lib/utils'

type TabsVariant = 'segmented' | 'segmented-accent' | 'underline' | 'slash'

const TabsVariantContext = React.createContext<TabsVariant>('segmented')

type TabsProps = React.ComponentPropsWithoutRef<typeof TabsPrimitive.Root> & {
  variant?: TabsVariant
}

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
      segmented: 'grid h-11 w-full auto-cols-fr grid-flow-col items-center rounded-lg bg-[hsl(var(--surface-subtle))] p-1',
      'segmented-accent': 'grid h-11 w-full auto-cols-fr grid-flow-col items-center rounded-lg bg-[hsl(var(--surface-subtle))] p-1',
      underline: 'inline-flex items-center gap-5 border-b border-[hsl(var(--divider))]',
      slash: 'inline-flex items-center',
    },
  },
  defaultVariants: {
    variant: 'segmented',
  },
})

const tabsTriggerVariants = cva(
  'inline-flex min-w-0 shrink-0 whitespace-nowrap items-center justify-center text-sm font-medium transition-colors duration-150 outline-none focus-visible:ring-2 focus-visible:ring-[hsl(var(--ring-soft))] focus-visible:ring-offset-0',
  {
    variants: {
      variant: {
        segmented:
          'h-full rounded-md px-3 leading-none text-muted-soft data-[state=active]:bg-[hsl(var(--surface-panel))] data-[state=active]:text-foreground data-[state=active]:shadow-[0_1px_2px_rgba(15,23,42,0.08)]',
        'segmented-accent':
          'h-full rounded-md px-3 leading-none text-muted-soft data-[state=active]:bg-primary data-[state=active]:text-primary-foreground data-[state=active]:shadow-[var(--button-soft-shadow)]',
        underline:
          'relative rounded-none px-0 py-3 text-muted-soft after:absolute after:bottom-0 after:left-0 after:h-0.5 after:w-full after:origin-left after:scale-x-0 after:rounded-full after:bg-[hsl(var(--primary))] after:transition-transform after:duration-150 data-[state=active]:text-foreground data-[state=active]:after:scale-x-100',
        slash: 'slash-tabs-trigger relative cursor-pointer px-0',
      },
    },
    defaultVariants: {
      variant: 'segmented',
    },
  },
)

const TabsList = React.forwardRef<
  React.ElementRef<typeof TabsPrimitive.List>,
  React.ComponentPropsWithoutRef<typeof TabsPrimitive.List> & VariantProps<typeof tabsListVariants>
>(({ className, variant, ...props }, ref) => {
  const contextVariant = React.useContext(TabsVariantContext)
  return (
    <TabsPrimitive.List
      ref={ref}
      className={cn(tabsListVariants({ variant: variant ?? contextVariant }), className)}
      {...props}
    />
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
