import {
  useLayoutEffect,
  useRef,
  useState,
  type ComponentType,
  type CSSProperties,
  type MouseEvent,
  type ReactNode,
  type RefObject,
} from 'react'
import { LiquidGlass } from '@/components/glass/liquid-glass'
import { cn } from '@/lib/utils'

export type LiquidTabBarIcon = ComponentType<{ className?: string; style?: CSSProperties; strokeWidth?: number }>

export type LiquidTabBarItem = {
  key: string
  label: string
  icon: LiquidTabBarIcon
}

/** 交给调用方渲染每一项（NavLink、<a>、<button>……）时需要带上的属性 */
export type LiquidTabBarSlotProps = {
  'data-tab-key': string
  'aria-label': string
  'aria-current': 'page' | undefined
  title: string
  className: string
  children: ReactNode
}

type LiquidTabBarProps = {
  items: LiquidTabBarItem[]
  activeKey: string
  /** 向下滚动时收起：只保留当前项，且不显示选中块 */
  collapsed?: boolean
  onExpand?: () => void
  renderItem: (item: LiquidTabBarItem, slotProps: LiquidTabBarSlotProps) => ReactNode
  className?: string
}

/** Tab Bar 高 62：去掉 1px 玻璃边框与 6px 内边距 → 选中块高 48；宽最多 60，比圆形略向左右拉长 */
const INDICATOR_HEIGHT = 48
const INDICATOR_MAX_WIDTH = 60
const SPRING = 'cubic-bezier(0.34,1.4,0.64,1)'
const EASE = 'ease-[cubic-bezier(0.32,0.72,0,1)]'

/**
 * 胶囊 Tab Bar 的外观与动效，供主应用的 MobileTabbar 使用。
 * - Liquid Glass · clear，高 62，只放图标。
 * - 选中块：偏黑胶囊（高 48、宽 48–60），切换时沿弹簧曲线滑过去，途中横向拉伸再回弹；新选中的图标弹跳一次。
 * - 收起：只保留当前项、隐藏选中块，变成左侧一个圆形图标；收起状态下点击只负责展开。
 * 定位（固定在底部、左右内缩）由调用方的外层容器负责。
 */
export function LiquidTabBar({ items, activeKey, collapsed = false, onExpand, renderItem, className }: LiquidTabBarProps) {
  const barRef = useRef<HTMLElement>(null)
  const indicator = useIndicatorBox(barRef, activeKey)

  function handleCollapsedClick(event: MouseEvent) {
    if (!collapsed) return
    event.preventDefault()
    event.stopPropagation()
    onExpand?.()
  }

  return (
    <LiquidGlass
      ref={barRef}
      variant="clear"
      onClickCapture={handleCollapsedClick}
      className={cn(
        'pointer-events-auto relative flex h-[62px] items-stretch rounded-full p-1.5 transition-[max-width] duration-300',
        EASE,
        collapsed ? 'max-w-[62px]' : 'max-w-[480px]',
        className,
      )}
    >
      {/* 选中块：外层负责滑动，内层在每次切换时播放一次拉伸回弹；收起时淡出 */}
      {indicator ? (
        <span
          aria-hidden="true"
          className={cn('tabbar-indicator pointer-events-none absolute left-0 top-1.5', collapsed ? 'opacity-0' : 'opacity-100')}
          style={{
            width: indicator.width,
            height: INDICATOR_HEIGHT,
            transform: `translateX(${indicator.x}px)`,
            transition: `transform 420ms ${SPRING}, width 300ms ease, opacity 200ms ease`,
          }}
        >
          <span
            key={activeKey}
            className="block size-full rounded-full bg-black/40 shadow-[inset_0_1px_2px_rgb(0_0_0_/_0.35),inset_0_-0.5px_0_rgb(255_255_255_/_0.12)]"
            style={{ animation: 'tabbar-indicator-stretch 420ms ease-out' }}
          />
        </span>
      ) : null}

      {items.map((item) => {
        const active = item.key === activeKey
        const Icon = item.icon
        return renderItem(item, {
          'data-tab-key': item.key,
          'aria-label': item.label,
          'aria-current': active ? 'page' : undefined,
          title: item.label,
          className: cn(
            'relative z-10 grid min-w-0 flex-1 basis-0 place-items-center overflow-hidden rounded-full transition-[max-width,opacity] duration-300',
            EASE,
            active ? 'text-tint' : 'text-label',
            collapsed && !active ? 'pointer-events-none max-w-0 opacity-0' : 'max-w-[120px] opacity-100',
          ),
          children: (
            <Icon
              // 变为选中时重新挂载，播放一次弹跳
              key={active ? 'active' : 'idle'}
              className={cn('size-6', active && 'tabbar-icon-bounce')}
              style={active ? { animation: `tabbar-icon-bounce 420ms ${SPRING}` } : undefined}
              strokeWidth={1.75}
            />
          ),
        })
      })}
    </LiquidGlass>
  )
}

/**
 * 计算选中块的位置和宽度：居中于当前项，宽度在 48–60 之间随项宽变化（项多时不会盖住相邻图标）。
 * 监听尺寸变化与过渡结束，收起 / 展开动画过程中也能跟上。
 */
function useIndicatorBox(barRef: RefObject<HTMLElement | null>, activeKey: string) {
  const [box, setBox] = useState<{ x: number; width: number } | null>(null)

  useLayoutEffect(() => {
    const bar = barRef.current
    if (!bar) return

    const measure = () => {
      const slot = bar.querySelector<HTMLElement>(`[data-tab-key="${CSS.escape(activeKey)}"]`)
      if (!slot) return
      const width = Math.min(INDICATOR_MAX_WIDTH, Math.max(INDICATOR_HEIGHT, slot.offsetWidth))
      setBox({ x: slot.offsetLeft + (slot.offsetWidth - width) / 2, width })
    }

    measure()
    const observer = new ResizeObserver(measure)
    observer.observe(bar)
    bar.querySelectorAll('[data-tab-key]').forEach((slot) => observer.observe(slot))
    // 其他项收缩时当前项只移动不变尺寸，ResizeObserver 不会触发；过渡结束时再量一次
    bar.addEventListener('transitionend', measure)
    return () => {
      observer.disconnect()
      bar.removeEventListener('transitionend', measure)
    }
  }, [activeKey, barRef])

  return box
}
