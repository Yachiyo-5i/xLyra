import { isValidElement, useEffect, useId, useRef, useState, type ReactNode } from 'react'
import * as PopoverPrimitive from '@radix-ui/react-popover'
import { Slot } from '@radix-ui/react-slot'
import { activateHoverCard, releaseHoverCard } from '@/components/common/hover-details-registry'
import { useLiquidGlassRef } from '@/components/glass/use-liquid-glass'
import { popoverPanelClass } from '@/components/ui/field-styles'
import { useMobileLayout } from '@/hooks/use-media-query'
import { cn } from '@/lib/utils'

type PointerAnchor = {
  getBoundingClientRect: () => DOMRect
}

type HoverDetailsProps = {
  children: ReactNode
  content: ReactNode
  title: string
  description?: string
  accessibleDescription?: string
  disabled?: boolean
  asChild?: boolean
  className?: string
  contentClassName?: string
  titleClassName?: string
}

function emptyPointerAnchor(): PointerAnchor {
  return {
    getBoundingClientRect: () => new DOMRect(),
  }
}

function pointerAnchorAt(x: number, y: number): PointerAnchor {
  return {
    getBoundingClientRect: () => new DOMRect(x, y, 0, 0),
  }
}

export function HoverDetails(props: HoverDetailsProps) {
  // The open state lives in the inner component, so a trigger that becomes
  // disabled while its card is open (details loading or disappearing) drops that
  // state instead of bringing the card back when it is enabled again.
  if (props.disabled) return props.asChild ? props.children : <div className={props.className}>{props.children}</div>
  return <HoverDetailsCard {...props} />
}

function HoverDetailsCard({ children, content, title, description, accessibleDescription, asChild = false, className, contentClassName, titleClassName }: HoverDetailsProps) {
  const isMobile = useMobileLayout()
  const [open, setOpen] = useState(false)
  const [anchorToPointer, setAnchorToPointer] = useState(false)
  const closeTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const pointerAnchorRef = useRef<PointerAnchor>(emptyPointerAnchor())
  const descriptionId = useId()
  // 悬停卡片属于功能层：Liquid Glass 弹出面板；桌面跟随鼠标，移动端点击后锚定在点击元素旁
  const glassRef = useLiquidGlassRef<HTMLDivElement>('regular')

  useEffect(() => () => {
    if (closeTimer.current !== null) clearTimeout(closeTimer.current)
    releaseHoverCard(descriptionId)
  }, [descriptionId])

  function cancelClose() {
    if (closeTimer.current !== null) clearTimeout(closeTimer.current)
    closeTimer.current = null
  }

  // Closes at once; used when another hover card takes over.
  function hideNow() {
    cancelClose()
    setOpen(false)
    setAnchorToPointer(false)
  }

  function changeOpen(nextOpen: boolean) {
    cancelClose()
    setOpen(nextOpen)
    if (nextOpen) {
      // Only one card at a time: the previous one closes immediately instead of
      // lingering until its close timer fires.
      activateHoverCard({ id: descriptionId, close: hideNow })
      return
    }
    setAnchorToPointer(false)
    releaseHoverCard(descriptionId)
  }

  function scheduleClose() {
    cancelClose()
    closeTimer.current = setTimeout(() => changeOpen(false), 150)
  }

  function openAtPointer(clientX: number, clientY: number) {
    pointerAnchorRef.current = pointerAnchorAt(clientX, clientY)
    setAnchorToPointer(true)
    changeOpen(true)
  }

  const Trigger = asChild ? Slot : 'div'
  const childClassName = asChild && isValidElement<{ className?: string }>(children) ? children.props.className : undefined
  const hasOwnCursor = /(^|\s)cursor-/.test(`${childClassName ?? ''} ${className ?? ''}`)
  const trigger = (
    <Trigger
      // 可点击查看详情：默认手形光标；触发元素自己指定了 cursor-* 时保留它
      className={cn(!asChild && 'inline-flex min-w-0', !hasOwnCursor && 'cursor-pointer', className)}
      role="button"
      tabIndex={0}
      aria-label={description ? `${title}: ${description}` : title}
      aria-describedby={accessibleDescription ? descriptionId : undefined}
      onPointerEnter={(event) => {
        if (!isMobile && event.pointerType !== 'touch') {
          openAtPointer(event.clientX, event.clientY)
        }
      }}
      onPointerLeave={isMobile ? undefined : scheduleClose}
      onFocus={isMobile ? undefined : (event) => {
        // A mouse click also focuses the trigger; that must not re-anchor the card
        // to the trigger before the click anchors it to the pointer.
        if (!event.currentTarget.matches(':focus-visible')) return
        setAnchorToPointer(false)
        changeOpen(true)
      }}
      onBlur={isMobile ? undefined : scheduleClose}
      onClick={(event) => {
        event.preventDefault()
        event.stopPropagation()
        // 移动端：点击切换，卡片锚定在点击元素旁；桌面：跟随鼠标位置
        if (isMobile) {
          setAnchorToPointer(false)
          changeOpen(!open)
          return
        }
        openAtPointer(event.clientX, event.clientY)
      }}
      onKeyDown={(event) => {
        if (event.key === 'Enter' || event.key === ' ') {
          event.preventDefault()
          event.stopPropagation()
          setAnchorToPointer(false)
          changeOpen(true)
        }
      }}
    >
      {children}
    </Trigger>
  )

  return (
    <>
      <PopoverPrimitive.Root open={open} onOpenChange={changeOpen}>
        <PopoverPrimitive.Trigger asChild>{trigger}</PopoverPrimitive.Trigger>
        {anchorToPointer ? <PopoverPrimitive.Anchor virtualRef={pointerAnchorRef} /> : null}
        <PopoverPrimitive.Portal>
          <PopoverPrimitive.Content
            ref={glassRef}
            side="top"
            align={isMobile ? 'center' : 'start'}
            sideOffset={isMobile ? 8 : 12}
            collisionPadding={16}
            className={cn(
              popoverPanelClass,
              'z-[170] max-h-[min(70vh,520px,var(--radix-popover-content-available-height))] max-lg:max-h-[min(50dvh,var(--radix-popover-content-available-height))] w-[380px] max-w-[calc(100vw-32px)] origin-(--radix-popover-content-transform-origin) overflow-y-auto overscroll-contain px-3.5 py-3 text-footnote text-label-2 [overflow-wrap:anywhere]',
              contentClassName,
            )}
            aria-label={title}
            onOpenAutoFocus={(event) => event.preventDefault()}
            onCloseAutoFocus={(event) => event.preventDefault()}
            // 移动端靠点击外部关闭，触摸产生的 pointerleave/blur 不应触发关闭
            onPointerEnter={isMobile ? undefined : cancelClose}
            onPointerLeave={isMobile ? undefined : scheduleClose}
            onFocusCapture={isMobile ? undefined : cancelClose}
            onBlurCapture={isMobile ? undefined : scheduleClose}
            onClick={(event) => event.stopPropagation()}
          >
            <div className={cn('mb-1.5 text-subhead font-semibold text-label', titleClassName)}>{title}</div>
            {isMobile && description ? <div className="-mt-1 mb-1.5 break-words text-footnote text-label-3">{description}</div> : null}
            {content}
          </PopoverPrimitive.Content>
        </PopoverPrimitive.Portal>
      </PopoverPrimitive.Root>
      {accessibleDescription ? <span id={descriptionId} role="tooltip" className="sr-only">{accessibleDescription}</span> : null}
    </>
  )
}
