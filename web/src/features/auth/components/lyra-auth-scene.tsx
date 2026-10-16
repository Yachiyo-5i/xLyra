import { useEffect, useLayoutEffect, useRef, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { LyraSky } from '@/features/auth/components/lyra-sky'
import { cn } from '@/lib/utils'

type LyraAuthSceneProps = {
  /** 入口按钮的文字（「登录」「创建管理员」）；不传时卡片始终显示、没有关闭按钮（加载 / 出错的状态页） */
  entryLabel?: string
  /** 表单卡片；`close` 把卡片收回成入口按钮 */
  children: (close: () => void) => ReactNode
  /** 织女星亮起（注册成功） */
  vegaLit?: boolean
  /** 初始就展开表单（如登录失效后跳回登录页） */
  defaultOpen?: boolean
  className?: string
}

const EASE = 'duration-700 ease-(--ease-sheet)'
const WIDE = 900
/** 窄屏展开时字标与卡片之间的间距 */
const NARROW_GAP = 24
/** 窄屏展开时字标相对收起位置上移的距离（卡片放不下时会再往上） */
const NARROW_LIFT = 12

/**
 * 登录 / 注册页的场景：天琴座夜空 + 字标与标语 + 入口按钮。
 * 点击入口按钮后表单卡片升起，卡片右上角的关闭按钮把它收回成按钮。
 * 用容器查询布局，放在任意尺寸的容器里都成立：
 * - 宽（≥ 900）：星座在右侧，左侧一栏垂直居中，卡片升起时字标被推上去；
 * - 窄：收起时字标在画面中央、入口按钮在下方、星座在上方；
 *   展开时卡片从底部升起，字标只比收起时略微上移、停在卡片上方，星座仍留在字标上方的空间里。
 * 宽屏的布局靠三段弹性空白（上 / 中 / 下）的 flex-grow 过渡完成；窄屏由脚本算出上、中两段空白的高度并过渡，
 * 这样展开前后字标的位置可以精确控制。
 */
export function LyraAuthScene({ entryLabel, children, vegaLit = false, defaultOpen = false, className }: LyraAuthSceneProps) {
  const { t } = useTranslation('auth')
  const dismissible = entryLabel !== undefined
  const [open, setOpen] = useState(defaultOpen || !dismissible)
  // 入口按钮在星座开始描出后再淡入
  const [entryReady, setEntryReady] = useState(defaultOpen)
  const [wide, setWide] = useState(true)
  const [focus, setFocus] = useState<{ top: number; bottom: number } | null>(null)
  // 窄屏上、中两段空白的高度；未测量前为 null，先用 flex-grow 布局
  const [spacers, setSpacers] = useState<{ top: number; middle: number } | null>(null)
  const sceneRef = useRef<HTMLDivElement>(null)
  const columnRef = useRef<HTMLDivElement>(null)
  const headerRef = useRef<HTMLElement>(null)
  const entryRef = useRef<HTMLDivElement>(null)
  const cardRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const reduceMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches
    const timer = window.setTimeout(() => setEntryReady(true), reduceMotion ? 0 : 1200)
    return () => window.clearTimeout(timer)
  }, [])

  // 窄屏：按动画结束后的位置算出上、中两段空白，字标在展开时只略微上移；星座始终放在字标上方的空间里
  useLayoutEffect(() => {
    const scene = sceneRef.current
    const column = columnRef.current
    const header = headerRef.current
    const card = cardRef.current
    if (!scene || !column || !header || !card) return

    const measure = () => {
      const isWide = scene.clientWidth >= WIDE
      setWide(isWide)
      if (isWide) {
        setFocus(null)
        setSpacers(null)
        return
      }
      const safeTop = Number.parseFloat(getComputedStyle(scene).getPropertyValue('--auth-safe-top')) || 0
      const style = getComputedStyle(column)
      const padTop = Number.parseFloat(style.paddingTop) || 0
      const padBottom = Number.parseFloat(style.paddingBottom) || 0
      const inner = column.clientHeight - padTop - padBottom
      const headerHeight = header.offsetHeight
      // 收起时：字标与入口按钮之间至少 24，剩余空间上下平分，字标居中
      const entryHeight = dismissible && entryReady ? (entryRef.current?.scrollHeight ?? 0) : 0
      const closedFree = inner - headerHeight - entryHeight
      const closedMiddle = Math.max(NARROW_GAP, closedFree / 2)
      const closedTop = Math.max(0, closedFree - closedMiddle)
      let top = closedTop
      let middle = closedMiddle
      if (open) {
        // 展开时：比收起时略高一点；卡片放不下时字标继续上移，直到贴近顶部（再不够就整栏滚动）
        const stackedTop = inner - headerHeight - NARROW_GAP - card.offsetHeight
        top = Math.max(0, Math.min(closedTop - NARROW_LIFT, stackedTop))
        middle = NARROW_GAP
      }
      setSpacers({ top, middle })
      setFocus({ top: safeTop + 20, bottom: padTop + top - 20 })
    }

    measure()
    const observer = new ResizeObserver(measure)
    observer.observe(scene)
    observer.observe(card)
    observer.observe(header)
    return () => observer.disconnect()
  }, [open, entryReady, dismissible])

  const close = () => setOpen(false)

  // 三段空白：宽屏始终上下等分（居中）；窄屏上、中两段用测量出的高度（测量前退回 flex-grow），下段填满余下空间
  const narrowSpacers = !wide && spacers
  const topGrow = narrowSpacers ? 'grow-0' : 'grow'
  const middleGrow = narrowSpacers || wide ? 'grow-0' : 'grow'
  const bottomGrow = wide || narrowSpacers ? 'grow' : 'grow-0'

  return (
    <div
      ref={sceneRef}
      className={cn('@container/auth relative size-full overflow-hidden bg-canvas [--auth-safe-top:env(safe-area-inset-top,0px)]', className)}
    >
      <LyraSky vegaLit={vegaLit} focus={focus} />

      <div
        ref={columnRef}
        className={cn(
          // 内容放不下（横屏手机、很矮的窗口）时整栏可滚动，空白段先收缩到 0
          'relative flex h-full flex-col items-center overflow-y-auto overscroll-contain px-2 pb-[calc(env(safe-area-inset-bottom,0px)+8px)] [scrollbar-width:none]',
          'pt-[calc(var(--auth-safe-top)+56px)]',
          '@min-[900px]/auth:items-start @min-[900px]/auth:px-0 @min-[900px]/auth:pb-0 @min-[900px]/auth:pl-[10%] @min-[900px]/auth:pt-0',
        )}
      >
        <div
          className={cn('shrink-0 transition-[flex-grow,height]', EASE, topGrow)}
          style={narrowSpacers ? { height: narrowSpacers.top } : undefined}
        />

        <header ref={headerRef} className="text-center @min-[900px]/auth:mb-10 @min-[900px]/auth:text-left">
          <h1 className="font-serif text-[44px] font-medium leading-none tracking-tight text-label @min-[900px]/auth:text-[60px]">
            x<span className="italic">Lyra</span>
          </h1>
          <div className="mx-auto mt-5 h-px w-10 bg-label-4 @min-[900px]/auth:mx-0" />
          <p className="mt-5 font-serif text-title-3 font-normal text-label">{t('shell.slogan')}</p>
          <p className="mx-auto mt-1.5 max-w-[320px] text-subhead text-label-2 @min-[900px]/auth:mx-0">{t('shell.subtitle')}</p>
        </header>

        <div
          className={cn('shrink-0 transition-[flex-grow,height]', !narrowSpacers && 'min-h-6', EASE, middleGrow)}
          style={narrowSpacers ? { height: narrowSpacers.middle } : undefined}
        />

        {/* 入口按钮：表单展开时收起 */}
        {dismissible ? (
          <div
            className={cn(
              'grid w-full max-w-[400px] transition-[grid-template-rows,opacity]',
              EASE,
              !open && entryReady ? 'grid-rows-[1fr] opacity-100' : 'grid-rows-[0fr] opacity-0',
            )}
            aria-hidden={open}
          >
            <div ref={entryRef} className="flex min-h-0 justify-center overflow-hidden @min-[900px]/auth:justify-start">
              <Button variant="glass" size="lg" tabIndex={open ? -1 : 0} className="mb-10 mt-1 min-w-[180px] @min-[900px]/auth:mb-1" onClick={() => setOpen(true)}>
                {entryLabel}
              </Button>
            </div>
          </div>
        ) : null}

        {/* 表单卡片：网格行从 0fr 过渡到 1fr。收起时裁掉卡片：行高为 0 时卡片仍会溢出到下方，
            把整栏的可滚动高度撑大，页面就能被往上推 */}
        <div className={cn('grid w-full max-w-[400px] transition-[grid-template-rows]', EASE, open ? 'grid-rows-[1fr]' : 'grid-rows-[0fr]')}>
          <div className={cn('min-h-0', !open && 'overflow-hidden')}>
            <div
              ref={cardRef}
              className={cn(
                'relative transition-[opacity,transform]',
                EASE,
                open ? 'translate-y-0 opacity-100 delay-100' : 'pointer-events-none translate-y-10 opacity-0',
              )}
              inert={!open}
            >
              {children(close)}
              {dismissible ? (
                <button
                  type="button"
                  aria-label={t('shell.closeForm')}
                  onClick={close}
                  className="absolute right-4 top-4 flex size-8 items-center justify-center rounded-full bg-fill-3 text-label-2 transition-colors hover:bg-fill-2 hover:text-label"
                >
                  <X className="size-4" />
                </button>
              ) : null}
            </div>
          </div>
        </div>

        <div className={cn('transition-[flex-grow]', EASE, bottomGrow)} />
      </div>
    </div>
  )
}
