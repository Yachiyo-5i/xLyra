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
        className={cn(
          // 输入框本身与滑块等高（扩大点击区域），4px 的轨道用居中的背景条绘制：已填充部分为强调色
          'h-6 w-full cursor-pointer appearance-none bg-[linear-gradient(to_right,var(--tint)_0%,var(--tint)_var(--slider-progress),var(--fill-primary)_var(--slider-progress),var(--fill-primary)_100%)] bg-[length:100%_4px] bg-center bg-no-repeat outline-none disabled:cursor-not-allowed disabled:opacity-40',
          // 滑块：白色圆形；按住时放大并变为半透明玻璃（HIG：交互中的滑块呈现 Liquid Glass 外观）
          '[&::-webkit-slider-thumb]:size-6 [&::-webkit-slider-thumb]:appearance-none [&::-webkit-slider-thumb]:rounded-full [&::-webkit-slider-thumb]:bg-white [&::-webkit-slider-thumb]:shadow-[0_2px_6px_rgb(0_0_0_/_0.3),0_0_0_0.5px_rgb(0_0_0_/_0.08)] [&::-webkit-slider-thumb]:transition-[transform,background-color] [&::-webkit-slider-thumb]:duration-150',
          'active:[&::-webkit-slider-thumb]:scale-[1.18] active:[&::-webkit-slider-thumb]:bg-white/75 active:[&::-webkit-slider-thumb]:shadow-[inset_0_0_0_0.5px_rgb(255_255_255_/_0.7),0_4px_12px_rgb(0_0_0_/_0.32)]',
          '[&::-moz-range-thumb]:size-6 [&::-moz-range-thumb]:rounded-full [&::-moz-range-thumb]:border-0 [&::-moz-range-thumb]:bg-white [&::-moz-range-thumb]:shadow-[0_2px_6px_rgb(0_0_0_/_0.3)]',
          'focus-visible:[&::-webkit-slider-thumb]:ring-2 focus-visible:[&::-webkit-slider-thumb]:ring-tint/55',
          className,
        )}
        style={{ '--slider-progress': `${progress}%` } as React.CSSProperties}
        {...props}
      />
    )
  },
)
Slider.displayName = 'Slider'

export { Slider }
