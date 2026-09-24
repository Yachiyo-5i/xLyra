import { useEffect, useId, useRef, useState, type ReactNode } from 'react'
import * as PopoverPrimitive from '@radix-ui/react-popover'
import { Slot } from '@radix-ui/react-slot'
import { Draw, DrawBody, DrawContent, DrawDescription, DrawHeader, DrawTitle, DrawTrigger } from '@/components/ui/draw'
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

export function HoverDetails({ children, content, title, description, accessibleDescription, disabled = false, asChild = false, className, contentClassName, titleClassName }: HoverDetailsProps) {
  const isMobile = useMobileLayout()
  const [open, setOpen] = useState(false)
  const [anchorToPointer, setAnchorToPointer] = useState(false)
  const closeTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const pointerAnchorRef = useRef<PointerAnchor>(emptyPointerAnchor())
  const descriptionId = useId()

  useEffect(() => () => {
    if (closeTimer.current !== null) clearTimeout(closeTimer.current)
  }, [])

  function cancelClose() {
    if (closeTimer.current !== null) clearTimeout(closeTimer.current)
    closeTimer.current = null
  }

  function changeOpen(nextOpen: boolean) {
    cancelClose()
    setOpen(nextOpen)
    if (!nextOpen) setAnchorToPointer(false)
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
  if (disabled) return asChild ? children : <div className={className}>{children}</div>

  const trigger = (
    <Trigger
      className={cn(!asChild && 'inline-flex min-w-0', className)}
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
      onFocus={isMobile ? undefined : () => {
        setAnchorToPointer(false)
        changeOpen(true)
      }}
      onBlur={isMobile ? undefined : scheduleClose}
      onClick={(event) => {
        event.preventDefault()
        event.stopPropagation()
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
      {isMobile ? (
        <Draw open={open} onOpenChange={changeOpen}>
          <DrawTrigger asChild>{trigger}</DrawTrigger>
          <DrawContent>
            <DrawHeader>
              <DrawTitle className={titleClassName}>{title}</DrawTitle>
              <DrawDescription className={description ? 'break-words' : 'sr-only'}>{description || title}</DrawDescription>
            </DrawHeader>
            <DrawBody className="pb-[max(1.25rem,env(safe-area-inset-bottom))] text-sm [overflow-wrap:anywhere]">{content}</DrawBody>
          </DrawContent>
        </Draw>
      ) : (
        <PopoverPrimitive.Root open={open} onOpenChange={changeOpen}>
          <PopoverPrimitive.Trigger asChild>{trigger}</PopoverPrimitive.Trigger>
          {anchorToPointer ? <PopoverPrimitive.Anchor virtualRef={pointerAnchorRef} /> : null}
          <PopoverPrimitive.Portal>
            <PopoverPrimitive.Content
              side="top"
              align="start"
              sideOffset={12}
              collisionPadding={16}
              className={cn('glass-panel-strong z-[170] max-h-[min(70vh,520px,var(--radix-popover-content-available-height))] w-[380px] max-w-[calc(100vw-32px)] overflow-y-auto overscroll-contain rounded-lg px-3 py-2 text-xs leading-5 text-foreground shadow-lg [overflow-wrap:anywhere]', contentClassName)}
              aria-label={title}
              onOpenAutoFocus={(event) => event.preventDefault()}
              onCloseAutoFocus={(event) => event.preventDefault()}
              onPointerEnter={cancelClose}
              onPointerLeave={scheduleClose}
              onFocusCapture={cancelClose}
              onBlurCapture={scheduleClose}
              onClick={(event) => event.stopPropagation()}
            >
              <div className={cn('mb-1 font-medium', titleClassName)}>{title}</div>
              {content}
            </PopoverPrimitive.Content>
          </PopoverPrimitive.Portal>
        </PopoverPrimitive.Root>
      )}
      {accessibleDescription ? <span id={descriptionId} role="tooltip" className="sr-only">{accessibleDescription}</span> : null}
    </>
  )
}
