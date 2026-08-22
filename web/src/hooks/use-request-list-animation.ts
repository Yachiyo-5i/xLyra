import { useEffect, useLayoutEffect, useRef, type RefObject } from 'react'

type RequestListPosition = {
  top: number
  parentID: string
  live: boolean
  handoff: boolean
}

const requestListItemSelector = '[data-request-list-item]'
const requestListAnimationOptions: KeyframeAnimationOptions = {
  duration: 260,
  easing: 'cubic-bezier(.2, .8, .2, 1)',
  fill: 'both',
}

export function useRequestListAnimation<T extends HTMLElement>(
  containerRef: RefObject<T | null>,
  itemKeys: readonly string[],
) {
  const previousPositionsRef = useRef<Map<string, RequestListPosition>>(new Map())
  const previousItemKeysRef = useRef<readonly string[] | null>(null)
  const animationsRef = useRef<Animation[]>([])

  useLayoutEffect(() => {
    const container = containerRef.current
    const previousItemKeys = previousItemKeysRef.current
    const keysChanged = previousItemKeys === null || !sameItemKeys(previousItemKeys, itemKeys)

    if (keysChanged) {
      for (const animation of animationsRef.current) animation.cancel()
      animationsRef.current = []
    }

    if (!container) {
      previousPositionsRef.current = new Map()
      previousItemKeysRef.current = [...itemKeys]
      return
    }

    const elements = Array.from(container.querySelectorAll<HTMLElement>(requestListItemSelector))
    const previousPositions = previousPositionsRef.current
    const currentPositions = new Map<string, RequestListPosition>()
    const usedPreviousKeys = new Set<string>()
    const animations: Animation[] = []

    for (const element of elements) {
      const key = element.dataset.requestListItem
      if (!key) continue

      currentPositions.set(key, {
        top: element.getBoundingClientRect().top,
        parentID: element.dataset.requestListParent ?? key,
        live: element.dataset.requestListLive === 'true',
        handoff: element.dataset.requestListHandoff === 'true',
      })
    }

    const reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches
    if (keysChanged && !reducedMotion) {
      for (const element of elements) {
        const key = element.dataset.requestListItem
        if (!key) continue

        const currentPosition = currentPositions.get(key)
        if (!currentPosition) continue

        const previousKey = previousPositions.has(key)
          ? key
          : findPreviousHandoffKey(previousPositions, currentPosition.parentID, usedPreviousKeys)
        const previousPosition = previousKey ? previousPositions.get(previousKey) : undefined

        if (previousPosition && previousKey) {
          usedPreviousKeys.add(previousKey)
          const deltaY = previousPosition.top - currentPosition.top
          if (Math.abs(deltaY) > 0.5 && typeof element.animate === 'function') {
            animations.push(element.animate([
              { transform: `translateY(${deltaY}px)` },
              { transform: 'translateY(0)' },
            ], requestListAnimationOptions))
          }
          continue
        }

        if (previousPositions.size > 0 && currentPosition.live === false && currentPosition.handoff === false && typeof element.animate === 'function') {
          animations.push(element.animate([
            { opacity: 0, transform: 'translateY(-8px)' },
            { opacity: 1, transform: 'translateY(0)' },
          ], requestListAnimationOptions))
        }
      }
    }

    previousPositionsRef.current = currentPositions
    previousItemKeysRef.current = [...itemKeys]
    if (keysChanged) animationsRef.current = animations
  }, [containerRef, itemKeys])

  useEffect(() => () => {
    for (const animation of animationsRef.current) animation.cancel()
    animationsRef.current = []
  }, [])
}

function sameItemKeys(previousKeys: readonly string[], currentKeys: readonly string[]) {
  if (previousKeys.length !== currentKeys.length) return false
  return previousKeys.every((key, index) => key === currentKeys[index])
}

function findPreviousHandoffKey(
  previousPositions: ReadonlyMap<string, RequestListPosition>,
  parentID: string,
  usedPreviousKeys: ReadonlySet<string>,
) {
  for (const [key, position] of previousPositions) {
    if (usedPreviousKeys.has(key)) continue
    if (position.parentID === parentID && (position.live || position.handoff)) return key
  }
  return null
}
