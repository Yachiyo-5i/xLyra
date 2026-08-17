import { describe, expect, it } from 'vitest'
import { detectMobileDevice, shouldUseCompactDashboardLayout } from './use-media-query'

describe('detectMobileDevice', () => {
  it('detects user agent client hints', () => {
    expect(detectMobileDevice({ userAgent: '', userAgentDataMobile: true })).toBe(true)
  })

  it('detects mobile and tablet user agents', () => {
    expect(detectMobileDevice({ userAgent: 'Mozilla/5.0 (Linux; Android 15; Pixel 9)' })).toBe(true)
    expect(detectMobileDevice({ userAgent: 'Mozilla/5.0 (iPad; CPU OS 18_0 like Mac OS X)' })).toBe(true)
  })

  it('detects iPadOS desktop user agents', () => {
    expect(detectMobileDevice({ userAgent: 'Mozilla/5.0 (Macintosh)', platform: 'MacIntel', maxTouchPoints: 5 })).toBe(true)
  })

  it('uses touch capability only for compact coarse-pointer screens', () => {
    expect(detectMobileDevice({ userAgent: '', maxTouchPoints: 5, coarsePointer: true, screenWidth: 800, screenHeight: 1024 })).toBe(true)
    expect(detectMobileDevice({ userAgent: '', maxTouchPoints: 10, coarsePointer: true, screenWidth: 1366, screenHeight: 768 })).toBe(false)
    expect(detectMobileDevice({ userAgent: '', maxTouchPoints: 10, coarsePointer: true, screenWidth: 1920, screenHeight: 1080 })).toBe(false)
  })

  it('does not classify a narrow desktop user agent as a mobile device', () => {
    expect(detectMobileDevice({ userAgent: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)', platform: 'MacIntel', maxTouchPoints: 0 })).toBe(false)
  })
})

describe('shouldUseCompactDashboardLayout', () => {
  it('follows the compact dashboard viewport range regardless of device UA', () => {
    expect(shouldUseCompactDashboardLayout(true)).toBe(true)
    expect(shouldUseCompactDashboardLayout(false)).toBe(false)
  })
})
