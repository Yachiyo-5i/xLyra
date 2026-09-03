import * as React from 'react'
import { cn } from '@/lib/utils'

type SliderProps = Omit<React.ComponentProps<'input'>, 'type' | 'value' | 'defaultValue' | 'onChange'> & {
  value: number
  min?: number
  max?: number
  step?: number
  onValueChange: (value: number) => void
}

const Slider = React.forwardRef<HTMLInputElement, SliderProps>(
  ({ className, value, min = 0, max = 100, step = 1, onValueChange, ...props }, ref) => {
    const progress = max === min ? 0 : ((value - min) / (max - min)) * 100
    return (
      <input
        ref={ref}
        type="range"
        min={min}
        max={max}
        step={step}
        value={value}
        onChange={(event) => onValueChange(Number(event.target.value))}
        className={cn('h-1.5 w-full cursor-pointer appearance-none rounded-full bg-[linear-gradient(to_right,hsl(var(--foreground))_0%,hsl(var(--foreground))_var(--slider-progress),hsl(var(--glass-border-strong))_var(--slider-progress),hsl(var(--glass-border-strong))_100%)] outline-none disabled:cursor-not-allowed disabled:opacity-50 [&::-webkit-slider-thumb]:size-4 [&::-webkit-slider-thumb]:appearance-none [&::-webkit-slider-thumb]:rounded-full [&::-webkit-slider-thumb]:border [&::-webkit-slider-thumb]:border-white/60 [&::-webkit-slider-thumb]:bg-[hsl(var(--card))] [&::-webkit-slider-thumb]:shadow-[0_2px_8px_rgb(0_0_0_/_0.24)] [&::-moz-range-thumb]:size-4 [&::-moz-range-thumb]:rounded-full [&::-moz-range-thumb]:border [&::-moz-range-thumb]:border-white/60 [&::-moz-range-thumb]:bg-[hsl(var(--card))] [&::-moz-range-thumb]:shadow-[0_2px_8px_rgb(0_0_0_/_0.24)]', className)}
        style={{ '--slider-progress': `${progress}%` } as React.CSSProperties}
        {...props}
      />
    )
  },
)
Slider.displayName = 'Slider'

export { Slider }
