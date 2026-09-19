import { useEffect, useId, useRef, useState, type ReactNode } from 'react'
import * as PopoverPrimitive from '@radix-ui/react-popover'
import { Slot } from '@radix-ui/react-slot'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Draw, DrawBody, DrawContent, DrawDescription, DrawFooter, DrawHeader, DrawTitle, DrawTrigger } from '@/components/ui/draw'
import { useMobileLayout } from '@/hooks/use-media-query'
import { cn } from '@/lib/utils'

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

export function HoverDetails({ children, content, title, description, accessibleDescription, disabled = false, asChild = false, className, contentClassName, titleClassName }: HoverDetailsProps) {
  const { t } = useTranslation('common')
  const isMobile = useMobileLayout()
  const [open, setOpen] = useState(false)
  const closeTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
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
  }

  function scheduleClose() {
    cancelClose()
    closeTimer.current = setTimeout(() => setOpen(false), 150)
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
        if (!isMobile && event.pointerType !== 'touch') changeOpen(true)
      }}
      onPointerLeave={isMobile ? undefined : scheduleClose}
      onFocus={isMobile ? undefined : () => changeOpen(true)}
      onBlur={isMobile ? undefined : scheduleClose}
      onClick={(event) => {
        event.preventDefault()
        event.stopPropagation()
        changeOpen(true)
      }}
      onKeyDown={(event) => {
        if (event.key === 'Enter' || event.key === ' ') {
          event.preventDefault()
          event.stopPropagation()
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
            <DrawBody className="text-sm [overflow-wrap:anywhere]">{content}</DrawBody>
            <DrawFooter>
              <Button variant="outline" className="w-full" onClick={() => changeOpen(false)}>{t('actions.close')}</Button>
            </DrawFooter>
          </DrawContent>
        </Draw>
      ) : (
        <PopoverPrimitive.Root open={open} onOpenChange={changeOpen}>
          <PopoverPrimitive.Trigger asChild>{trigger}</PopoverPrimitive.Trigger>
          <PopoverPrimitive.Portal>
            <PopoverPrimitive.Content
              sideOffset={8}
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
