import { useRef, useState, type KeyboardEvent, type PointerEvent } from 'react'
import { cn } from '@/lib/utils'

type EffortSliderProps<T extends string> = {
  options: Array<{ value: T; label: string }>
  value: T
  onChange: (value: T) => void
  /** 已选部分的颜色（随档位变化） */
  color: string
  ariaLabel: string
  disabled?: boolean
}

const THUMB = 30
const TRACK = 34
/** 档位圆点与轨道两端的距离：滑块在第一档 / 最后一档时刚好贴住轨道内侧 */
const INSET = TRACK / 2

/**
 * 可拖动的档位滑块（仿 Codex 的思考强度）：一条胶囊轨道，每个档位一个小圆点，
 * 白色圆形滑块可以拖动或点击轨道，松手后吸附到最近的档位；左侧已选部分用档位颜色填充。
 * 支持方向键、Home / End。
 */
export function EffortSlider<T extends string>({ options, value, onChange, color, ariaLabel, disabled = false }: EffortSliderProps<T>) {
  const trackRef = useRef<HTMLDivElement>(null)
  const [dragging, setDragging] = useState(false)
  const count = options.length
  const index = Math.max(0, options.findIndex((option) => option.value === value))
  const fraction = count > 1 ? index / (count - 1) : 0
  // 圆点 / 滑块中心的位置：在 [INSET, 宽度 - INSET] 之间均分
  const position = (ratio: number) => `calc(${INSET}px + (100% - ${INSET * 2}px) * ${ratio})`

  function indexFromPointer(clientX: number) {
    const rect = trackRef.current?.getBoundingClientRect()
    if (!rect || count < 2) return index
    const ratio = (clientX - rect.left - INSET) / Math.max(rect.width - INSET * 2, 1)
    return Math.round(Math.min(Math.max(ratio, 0), 1) * (count - 1))
  }

  function select(nextIndex: number) {
    const next = options[Math.min(Math.max(nextIndex, 0), count - 1)]
    if (next && next.value !== value) onChange(next.value)
  }

  function handlePointerDown(event: PointerEvent<HTMLDivElement>) {
    if (disabled) return
    event.currentTarget.setPointerCapture(event.pointerId)
    setDragging(true)
    select(indexFromPointer(event.clientX))
  }

  function handlePointerMove(event: PointerEvent<HTMLDivElement>) {
    if (!dragging) return
    select(indexFromPointer(event.clientX))
  }

  function handlePointerUp(event: PointerEvent<HTMLDivElement>) {
    if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId)
    setDragging(false)
  }

  function handleKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    const keys: Record<string, number> = { ArrowLeft: index - 1, ArrowDown: index - 1, ArrowRight: index + 1, ArrowUp: index + 1, Home: 0, End: count - 1 }
    if (!(event.key in keys)) return
    event.preventDefault()
    select(keys[event.key])
  }

  const motion = dragging ? 'duration-75' : 'duration-200'

  return (
    <div
      ref={trackRef}
      role="slider"
      tabIndex={disabled ? -1 : 0}
      aria-label={ariaLabel}
      aria-valuemin={0}
      aria-valuemax={Math.max(count - 1, 0)}
      aria-valuenow={index}
      aria-valuetext={options[index]?.label}
      aria-disabled={disabled}
      onPointerDown={handlePointerDown}
      onPointerMove={handlePointerMove}
      onPointerUp={handlePointerUp}
      onPointerCancel={handlePointerUp}
      onKeyDown={handleKeyDown}
      className={cn(
        'relative h-[34px] w-full touch-none select-none rounded-full bg-fill-3 shadow-[inset_0_1px_2px_rgb(0_0_0/0.35)] outline-none focus-visible:ring-2 focus-visible:ring-tint/50',
        disabled ? 'cursor-not-allowed opacity-50' : 'cursor-pointer',
      )}
    >
      {/* 已选部分：从左端填充到滑块右缘 */}
      <div
        className={cn('absolute inset-y-0 left-0 rounded-full transition-[width,background-color] ease-(--ease-standard)', motion)}
        style={{ width: `calc(${position(fraction)} + ${THUMB / 2}px)`, backgroundColor: color }}
      />
      {/* 档位圆点 */}
      {options.map((option, optionIndex) => (
        <span
          key={option.value}
          aria-hidden="true"
          className={cn('absolute top-1/2 size-[5px] -translate-x-1/2 -translate-y-1/2 rounded-full', optionIndex <= index ? 'bg-white/45' : 'bg-white/25')}
          style={{ left: position(count > 1 ? optionIndex / (count - 1) : 0) }}
        />
      ))}
      {/* 滑块 */}
      <span
        aria-hidden="true"
        className={cn(
          'absolute top-1/2 -translate-x-1/2 -translate-y-1/2 rounded-full bg-white shadow-[0_2px_8px_rgb(0_0_0/0.45)] transition-[left,transform] ease-(--ease-standard)',
          motion,
          dragging && 'scale-105',
        )}
        style={{ left: position(fraction), width: THUMB, height: THUMB }}
      />
    </div>
  )
}
