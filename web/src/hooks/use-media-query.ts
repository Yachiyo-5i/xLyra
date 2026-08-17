import { useEffect, useState } from 'react'

const MOBILE_LAYOUT_MAX_WIDTH = 1023
const MOBILE_LAYOUT_QUERY = `(max-width: ${MOBILE_LAYOUT_MAX_WIDTH}px)`
const COMPACT_DASHBOARD_LAYOUT_QUERY = '(min-width: 1024px) and (max-width: 1366px)'
const MOBILE_DEVICE_PATTERN = /Android|Mobi|iPhone|iPad|iPod|IEMobile|Opera Mini/i

export type MobileDeviceSignals = {
  userAgent: string
  userAgentDataMobile?: boolean
  platform?: string
  maxTouchPoints?: number
  coarsePointer?: boolean
  screenWidth?: number
  screenHeight?: number
}

export function detectMobileDevice(signals: MobileDeviceSignals) {
  if (signals.userAgentDataMobile === true) return true
  if (MOBILE_DEVICE_PATTERN.test(signals.userAgent)) return true
  if (signals.platform === 'MacIntel' && (signals.maxTouchPoints ?? 0) > 1) return true

  const shortestScreenEdge = Math.min(signals.screenWidth ?? 0, signals.screenHeight ?? 0)
  const longestScreenEdge = Math.max(signals.screenWidth ?? 0, signals.screenHeight ?? 0)
  return Boolean(signals.coarsePointer && (signals.maxTouchPoints ?? 0) > 0 && shortestScreenEdge > 0 && shortestScreenEdge <= 820 && longestScreenEdge <= 1024)
}

function useMediaQuery(query: string) {
  const [matches, setMatches] = useState(() => {
    if (typeof window === 'undefined') return false
    return window.matchMedia(query).matches
  })

  useEffect(() => {
    const mediaQuery = window.matchMedia(query)
    const handleChange = () => setMatches(mediaQuery.matches)

    handleChange()
    mediaQuery.addEventListener('change', handleChange)

    return () => mediaQuery.removeEventListener('change', handleChange)
  }, [query])

  return matches
}

export function useMobileLayout() {
  return useMediaQuery(MOBILE_LAYOUT_QUERY)
}

export function shouldUseCompactDashboardLayout(compactViewport: boolean) {
  return compactViewport
}

export function useCompactDashboardLayout() {
  return shouldUseCompactDashboardLayout(useMediaQuery(COMPACT_DASHBOARD_LAYOUT_QUERY))
}

export function useMobileDevice() {
  const [mobileDevice] = useState(() => {
    if (typeof window === 'undefined') return false
    const navigatorWithUAData = window.navigator as Navigator & { userAgentData?: { mobile?: boolean } }
    return detectMobileDevice({
      userAgent: window.navigator.userAgent,
      userAgentDataMobile: navigatorWithUAData.userAgentData?.mobile,
      platform: window.navigator.platform,
      maxTouchPoints: window.navigator.maxTouchPoints,
      coarsePointer: window.matchMedia('(hover: none) and (pointer: coarse)').matches,
      screenWidth: window.screen.width,
      screenHeight: window.screen.height,
    })
  })

  return mobileDevice
}
