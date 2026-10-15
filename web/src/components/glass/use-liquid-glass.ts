import { useCallback, useId, useLayoutEffect, useRef, type Ref, type RefCallback, type RefObject } from 'react'
import {
  refractionProfiles,
  removeRefractionFilter,
  supportsGlassRefraction,
  upsertRefractionFilter,
  type RefractionVariant,
} from '@/components/glass/refraction'

/** 给元素挂上折射，返回卸载函数。元素尺寸变化时在下一帧重建位移贴图。 */
function attachLiquidGlass(element: HTMLElement, variant: RefractionVariant, id: string) {
  const profile = refractionProfiles[variant]
  let frame = 0
  let lastKey = ''

  const update = () => {
    frame = 0
    const width = element.offsetWidth
    const height = element.offsetHeight
    const radius = parseFloat(getComputedStyle(element).borderTopLeftRadius) || 0
    const key = `${width}x${height}r${radius}`
    if (!width || !height || key === lastKey) return
    lastKey = key
    const value = upsertRefractionFilter(id, { width, height, radius }, profile)
    if (!value) return
    element.style.backdropFilter = value
    element.dataset.refraction = variant
  }

  update()
  const observer = new ResizeObserver(() => {
    if (!frame) frame = requestAnimationFrame(update)
  })
  observer.observe(element)

  return () => {
    observer.disconnect()
    if (frame) cancelAnimationFrame(frame)
    element.style.backdropFilter = ''
    delete element.dataset.refraction
    removeRefractionFilter(id)
  }
}

function useFilterId(variant: RefractionVariant) {
  const reactId = useId()
  return `lg-${variant}-${reactId.replace(/[^a-zA-Z0-9_-]/g, '')}`
}

/**
 * 给元素挂上 Liquid Glass 折射。元素本身仍需带 `.lg-regular` / `.lg-clear` 类：
 * 不支持折射的浏览器上，它们就是最终外观；支持时由这里覆盖 backdrop-filter。
 */
export function useLiquidGlass(
  ref: RefObject<HTMLElement | null>,
  variant: RefractionVariant,
  /** 为 false 时不挂载折射，便于在组件里按条件启用而不违反 hooks 规则 */
  enabled = true,
) {
  const id = useFilterId(variant)

  useLayoutEffect(() => {
    const element = ref.current
    if (!enabled || !element || !supportsGlassRefraction()) return
    return attachLiquidGlass(element, variant, id)
  }, [enabled, id, ref, variant])
}

/**
 * 回调 ref 版本：适合 Portal 中按需挂载的弹层（Select、Popover 内容）。
 * 元素出现时挂上折射，消失时自动卸载；可与组件转发的 ref 合并。
 */
export function useLiquidGlassRef<T extends HTMLElement>(variant: RefractionVariant, forwardedRef?: Ref<T>): RefCallback<T> {
  const id = useFilterId(variant)
  const cleanupRef = useRef<(() => void) | null>(null)
  const forwardedRefRef = useRef(forwardedRef)
  useLayoutEffect(() => {
    forwardedRefRef.current = forwardedRef
  })

  return useCallback(
    (element: T | null) => {
      cleanupRef.current?.()
      cleanupRef.current = element && supportsGlassRefraction() ? attachLiquidGlass(element, variant, id) : null

      const target = forwardedRefRef.current
      if (typeof target === 'function') target(element)
      else if (target) target.current = element
    },
    [id, variant],
  )
}
