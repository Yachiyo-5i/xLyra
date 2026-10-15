import { useCallback, useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react'
import { LiquidGlass } from '@/components/glass/liquid-glass'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

export type MorphToolbarAction = {
  key: string
  label: string
  icon: ReactNode
  /** 带 menu 的按钮点击后变形为菜单；没有则直接调用 onSelect */
  menu?: {
    /** 菜单宽度（px） */
    width: number
    render: (close: () => void) => ReactNode
  }
  onSelect?: () => void
  /** 按钮的样式微调，如头像按钮需要更小的内边距 */
  buttonClassName?: string
}

type Phase = 'closed' | 'opening' | 'open' | 'closing'

/** 按钮直径与间距，与 Button size-10、gap-2 对应 */
const BUTTON = 40
const GAP = 8
/** 变形途中的液滴直径 */
const DROPLET = 52
/** 菜单最终圆角：R-card 20，内边距 6 → 菜单项圆角 14（同心） */
const MENU_RADIUS = 20
const OPEN_MS = 380
const CLOSE_MS = 400
/** 每一段关键帧各自的缓动：整体时间线匀速推进，形态变化的节奏由分段缓动决定 */
const EASE_IN = 'cubic-bezier(0.4, 0, 1, 1)'
const EASE_BURST = 'cubic-bezier(0.2, 0.85, 0.3, 1)'
const EASE_SETTLE = 'cubic-bezier(0.45, 0, 0.55, 1)'

function px(value: number) {
  return `${Math.round(value * 10) / 10}px`
}

function frame(right: number, top: number, width: number, height: number, radius: number, extra: Keyframe = {}): Keyframe {
  return { right: px(right), top: px(top), width: px(width), height: px(height), borderRadius: px(radius), ...extra }
}

/**
 * 按钮的淡出 / 淡入逐个作用在按钮自身，而不是外层按钮组：父元素带 opacity < 1 或 filter 时
 * 会成为子元素的 backdrop root，玻璃按钮的背景模糊采样不到页面，动画结束时玻璃质感会跳一下。
 */
function animateButtons(buttons: HTMLElement[], keyframes: Keyframe[], options: KeyframeAnimationOptions) {
  return buttons.map((button) => button.animate(keyframes, options))
}

function cancelAnimations(elements: HTMLElement[]) {
  elements.forEach((element) => element.getAnimations().forEach((animation) => animation.cancel()))
}

function prefersReducedMotion() {
  return typeof window !== 'undefined' && window.matchMedia('(prefers-reduced-motion: reduce)').matches
}

/**
 * 工具栏按钮组 + 变形菜单（仿 iOS 原生工具栏菜单）。
 *
 * 打开：所点按钮缩成一颗玻璃液滴，随即弹性长成菜单（略微超出再回落），菜单右上角与按钮组右上角对齐、
 * 盖住整组按钮；按钮组同时模糊淡出，菜单内容从模糊中显现。
 * 关闭：菜单收缩成团、缩回所点按钮的位置；按钮组模糊淡入并回弹，
 * 一颗小液滴从按钮下方被吸回，按钮随之轻微挤压回弹。
 */
export function MorphToolbar({ actions, className }: { actions: MorphToolbarAction[]; className?: string }) {
  const [openKey, setOpenKey] = useState<string | null>(null)
  const [phase, setPhase] = useState<Phase>('closed')
  const surfaceRef = useRef<HTMLElement>(null)
  const contentRef = useRef<HTMLDivElement>(null)
  const dripRef = useRef<HTMLSpanElement>(null)
  const buttonRefs = useRef(new Map<string, HTMLButtonElement>())
  const menuSizeRef = useRef({ width: 0, height: 0 })

  const openIndex = actions.findIndex((action) => action.key === openKey)
  const openAction = openIndex >= 0 ? actions[openIndex] : undefined
  // 所点按钮距按钮组右边缘的距离（按钮从右往左排）
  const buttonRight = openIndex >= 0 ? (actions.length - 1 - openIndex) * (BUTTON + GAP) : 0

  // 变形动画进行中（约 0.4 秒）忽略关闭请求，避免动画中途被打断
  const close = useCallback(() => {
    if (phase === 'open') setPhase('closing')
  }, [phase])

  function handleButtonClick(action: MorphToolbarAction) {
    if (!action.menu) {
      action.onSelect?.()
      return
    }
    if (phase !== 'closed') return
    setOpenKey(action.key)
    setPhase('opening')
  }

  // 打开：按钮 → 液滴 → 菜单（超调后回落）
  useLayoutEffect(() => {
    if (phase !== 'opening' || !openAction?.menu) return
    const surface = surfaceRef.current
    const content = contentRef.current
    const buttons = [...buttonRefs.current.values()]
    if (!surface || !content) return

    const width = openAction.menu.width
    const height = content.offsetHeight + 2 // 玻璃 1px 透明边框
    menuSizeRef.current = { width, height }
    const center = buttonRight + BUTTON / 2
    const dropletRight = center - DROPLET / 2
    const dropletTop = BUTTON / 2 - DROPLET / 2
    const reduced = prefersReducedMotion()

    const surfaceAnimation = surface.animate(
      reduced
        ? [frame(0, 0, width, height, MENU_RADIUS, { opacity: 0 }), frame(0, 0, width, height, MENU_RADIUS, { opacity: 1 })]
        : [
            // 按钮先缩成液滴，再冲出超过最终尺寸，最后回落
            frame(buttonRight, 0, BUTTON, BUTTON, BUTTON / 2, { easing: EASE_IN }),
            frame(dropletRight, dropletTop, DROPLET, DROPLET, DROPLET / 2, { offset: 0.2, easing: EASE_BURST }),
            frame(-4, -4, width + 8, height * 1.06 + 4, MENU_RADIUS + 10, { offset: 0.72, easing: EASE_SETTLE }),
            frame(0, 0, width, height, MENU_RADIUS),
          ],
      { duration: reduced ? 160 : OPEN_MS, easing: 'linear', fill: 'forwards' },
    )
    content.animate(
      [
        { opacity: 0, filter: 'blur(12px)', transform: 'scale(0.9)' },
        { opacity: 1, filter: 'blur(0px)', transform: 'scale(1)' },
      ],
      // 液滴开始长大时内容就以重度模糊出现，随面板成形逐渐清晰
      { duration: reduced ? 1 : 290, delay: reduced ? 0 : 70, easing: 'ease-out', fill: 'both' },
    )
    // 按钮组模糊淡出；保持到关闭时由新的淡入动画接替（关闭开始时会先取消它）
    animateButtons(
      buttons,
      [
        { opacity: 1, filter: 'blur(0px)', transform: 'scale(1)' },
        { opacity: 0, filter: 'blur(6px)', transform: 'scale(0.86)' },
      ],
      { duration: reduced ? 1 : 140, easing: 'ease-in', fill: 'forwards' },
    )

    surfaceAnimation.finished
      .then(() => {
        content.querySelector<HTMLElement>('[role="menuitem"]')?.focus({ preventScroll: true })
        setPhase('open')
      })
      .catch(() => {})
  }, [buttonRight, openAction, phase])

  // 关闭：菜单 → 收缩成团 → 缩回按钮；按钮组淡入回弹；液滴被吸回按钮
  useLayoutEffect(() => {
    if (phase !== 'closing' || !openAction) return
    const surface = surfaceRef.current
    const content = contentRef.current
    const drip = dripRef.current
    const buttons = [...buttonRefs.current.values()]
    const button = buttonRefs.current.get(openAction.key)
    if (!surface || !content) return

    // 先取消打开时那段「保持透明」的淡出动画。否则关闭的淡入动画一旦播完，
    // 它会重新露出来，按钮在全部动画结束前又消失一下（闪烁）。
    // 新的淡入动画用 fill: both，在延迟期间保持透明，所以这里取消不会让按钮提前出现。
    cancelAnimations(buttons)

    const { width, height } = menuSizeRef.current
    const center = buttonRight + BUTTON / 2
    const reduced = prefersReducedMotion()
    const animations: Animation[] = []

    animations.push(
      surface.animate(
        reduced
          ? [frame(0, 0, width, height, MENU_RADIUS, { opacity: 1 }), frame(0, 0, width, height, MENU_RADIUS, { opacity: 0 })]
          : [
              // 先微缩，再收成一团，最后缩回按钮并淡出（按钮组同时淡入接替）
              frame(0, 0, width, height, MENU_RADIUS, { easing: EASE_SETTLE }),
              frame(4, 3, width * 0.96, height * 0.94, MENU_RADIUS + 2, { offset: 0.14, easing: EASE_IN }),
              frame(center - 30, -6, 60, 64, 30, { offset: 0.5, easing: EASE_BURST }),
              frame(buttonRight, 0, BUTTON, BUTTON, BUTTON / 2, { offset: 0.8, opacity: 1 }),
              frame(buttonRight, 0, BUTTON, BUTTON, BUTTON / 2, { opacity: 0 }),
            ],
        { duration: reduced ? 140 : CLOSE_MS, easing: 'linear', fill: 'forwards' },
      ),
    )
    animations.push(
      content.animate(
        [
          { opacity: 1, filter: 'blur(0px)', transform: 'scale(1)' },
          { opacity: 0, filter: 'blur(10px)', transform: 'scale(0.9)' },
        ],
        { duration: reduced ? 1 : 150, easing: 'ease-in', fill: 'forwards' },
      ),
    )
    animations.push(
      ...animateButtons(
        buttons,
        reduced
          ? [{ opacity: 0 }, { opacity: 1 }]
          : [
              { opacity: 0, filter: 'blur(6px)', transform: 'scale(0.86)' },
              { opacity: 1, filter: 'blur(0px)', transform: 'scale(1.03)', offset: 0.7 },
              // 结束值用 none：不留下 filter，玻璃按钮的背景模糊保持正常
              { opacity: 1, filter: 'none', transform: 'scale(1)' },
            ],
        { duration: reduced ? 140 : 260, delay: reduced ? 0 : CLOSE_MS * 0.55, easing: 'ease-out', fill: 'both' },
      ),
    )

    if (!reduced && drip && button) {
      // 液滴从按钮下方被吸回：上移并缩小，与按钮底边相接时最像一颗挂着的水滴
      animations.push(
        drip.animate(
          [
            { opacity: 0, transform: 'translateY(6px) scale(0.85)' },
            { opacity: 1, transform: 'translateY(4px) scale(1)', offset: 0.25 },
            { opacity: 1, transform: 'translateY(-6px) scale(0.55)', offset: 0.75 },
            { opacity: 0, transform: 'translateY(-9px) scale(0.3)' },
          ],
          { duration: 260, delay: CLOSE_MS * 0.58, easing: 'ease-in-out', fill: 'both' },
        ),
      )
      // 吸收液滴时按钮被轻轻挤压、回弹
      animations.push(
        button.animate(
          [
            { transform: 'scale(1)' },
            { transform: 'scale(1.1, 0.88)', offset: 0.35 },
            { transform: 'scale(0.96, 1.05)', offset: 0.7 },
            { transform: 'scale(1)' },
          ],
          { duration: 280, delay: CLOSE_MS * 0.72, easing: 'ease-out' },
        ),
      )
    }

    let cancelled = false
    Promise.all(animations.map((animation) => animation.finished))
      .then(() => {
        if (cancelled) return
        // 所有关闭动画的结束值都等于按钮的静止状态，取消后不会有任何跳变
        cancelAnimations(buttons)
        setOpenKey(null)
        setPhase('closed')
        button?.focus({ preventScroll: true })
      })
      .catch(() => {})
    return () => {
      cancelled = true
    }
  }, [buttonRight, openAction, phase])

  // 打开后：点菜单外或按 Esc 关闭
  useEffect(() => {
    if (phase !== 'open') return

    function handlePointerDown(event: PointerEvent) {
      if (surfaceRef.current?.contains(event.target as Node)) return
      close()
    }
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape') close()
    }

    document.addEventListener('pointerdown', handlePointerDown, true)
    document.addEventListener('keydown', handleKeyDown)
    return () => {
      document.removeEventListener('pointerdown', handlePointerDown, true)
      document.removeEventListener('keydown', handleKeyDown)
    }
  }, [close, phase])

  return (
    <div className={cn('relative', className)}>
      <div className={cn('flex items-center', phase !== 'closed' && 'pointer-events-none')} style={{ gap: GAP }}>
        {actions.map((action) => (
          <Button
            key={action.key}
            ref={(element) => {
              if (element) buttonRefs.current.set(action.key, element)
              else buttonRefs.current.delete(action.key)
            }}
            variant="glass"
            size="icon"
            className={cn('size-10', action.buttonClassName)}
            aria-label={action.label}
            aria-haspopup={action.menu ? 'menu' : undefined}
            aria-expanded={action.menu ? openKey === action.key : undefined}
            onClick={() => handleButtonClick(action)}
          >
            {action.icon}
          </Button>
        ))}
      </div>

      {openAction?.menu ? (
        <>
          <span
            ref={dripRef}
            aria-hidden="true"
            className="morph-drip pointer-events-none absolute size-[22px] rounded-full opacity-0"
            // 与按钮底边重叠约 6px，看起来是挂在按钮下方的一颗水滴
            style={{ right: buttonRight + BUTTON / 2 - 11, top: BUTTON - 16 }}
          />
          <LiquidGlass
            ref={surfaceRef}
            role="menu"
            aria-label={openAction.label}
            className="absolute z-10 overflow-hidden"
            style={{ right: buttonRight, top: 0, width: BUTTON, height: BUTTON, borderRadius: BUTTON / 2 }}
          >
            {/* 内容固定为菜单宽度、贴右上角，变形过程中由外层裁切，避免文字跟着挤压换行 */}
            <div ref={contentRef} className="absolute right-0 top-0 origin-top-right p-1.5" style={{ width: openAction.menu.width - 2 }}>
              {openAction.menu.render(close)}
            </div>
          </LiquidGlass>
        </>
      ) : null}
    </div>
  )
}
