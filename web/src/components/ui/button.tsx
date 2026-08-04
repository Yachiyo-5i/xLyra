import * as React from 'react'
import { Slot } from '@radix-ui/react-slot'
import { cva, type VariantProps } from 'class-variance-authority'
import { cn } from '@/lib/utils'

const buttonVariants = cva(
  'inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-md text-sm font-medium transition-[transform,box-shadow,background-color,border-color,filter] duration-200 ease-out focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[hsl(var(--ring-strong))] hover:[box-shadow:var(--button-hover-shadow)] hover:-translate-y-[1px] active:scale-[0.97] active:translate-y-0 disabled:pointer-events-none disabled:opacity-50 disabled:hover:translate-y-0 disabled:hover:shadow-none',
  {
    variants: {
      variant: {
        default:
          'border bg-primary text-primary-foreground hover:brightness-105 [border-color:hsl(var(--button-soft-border))] [box-shadow:var(--button-soft-shadow),inset_0_1px_0_rgba(255,255,255,0.15)]',
        secondary:
          'border bg-[hsl(var(--surface-subtle))] text-foreground hover:bg-[hsl(var(--surface-soft-hover))] [border-color:hsl(var(--button-secondary-border))] [box-shadow:var(--button-secondary-shadow),inset_0_1px_0_rgba(255,255,255,0.05)]',
        ghost: 'bg-transparent text-foreground hover:bg-[hsl(var(--surface-subtle))]',
        outline:
          'border bg-transparent text-foreground hover:bg-[hsl(var(--surface-subtle))] [border-color:hsl(var(--button-secondary-border))] [box-shadow:var(--button-secondary-shadow)]',
        destructive:
          'border bg-red-500 text-white hover:bg-red-600 [border-color:hsl(var(--button-soft-border))] [box-shadow:var(--button-soft-shadow),inset_0_1px_0_rgba(255,255,255,0.15)]',
      },
      size: {
        default: 'h-10 px-4 py-2',
        sm: 'h-9 px-3',
        lg: 'h-11 px-5 text-[15px]',
        icon: 'size-10',
      },
    },
    defaultVariants: {
      variant: 'default',
      size: 'default',
    },
  },
)

interface ButtonProps
  extends React.ButtonHTMLAttributes<HTMLButtonElement>,
    VariantProps<typeof buttonVariants> {
  asChild?: boolean
}

const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant, size, asChild = false, type = 'button', ...props }, ref) => {
    const Comp = asChild ? Slot : 'button'

    return (
      <Comp
        className={cn(buttonVariants({ variant, size, className }))}
        ref={ref}
        type={asChild ? undefined : type}
        {...props}
      />
    )
  },
)
Button.displayName = 'Button'

export { Button }
