import { forwardRef, useImperativeHandle, useRef, type ComponentPropsWithoutRef, type RefObject } from 'react'
import { cn } from '@/lib/utils'
import type { RefractionVariant } from '@/components/glass/refraction'
import { useLiquidGlass } from '@/components/glass/use-liquid-glass'

type LiquidGlassProps = ComponentPropsWithoutRef<'div'> & {
  variant?: RefractionVariant
  as?: 'div' | 'nav' | 'aside' | 'section' | 'header' | 'span'
}

/** Liquid Glass 容器：`.lg-{variant}` 外观 + Chromium 上的真实折射 */
export const LiquidGlass = forwardRef<HTMLElement, LiquidGlassProps>(function LiquidGlass(
  { variant = 'regular', as: Tag = 'div', className, ...props },
  forwardedRef,
) {
  const innerRef = useRef<HTMLElement>(null)
  useImperativeHandle(forwardedRef, () => innerRef.current as HTMLElement)
  useLiquidGlass(innerRef, variant)

  return <Tag ref={innerRef as RefObject<never>} className={cn(`lg-${variant}`, className)} {...props} />
})
