import { describe, expect, it } from 'vitest'
import { isChromiumEngine } from '@/components/glass/refraction'

const IPHONE_SAFARI =
  'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1'
const DESKTOP_CHROME =
  'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/152.0.0.0 Safari/537.36'
const IOS_CHROME =
  'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/152.0.0.0 Mobile/15E148 Safari/604.1'
const FIREFOX = 'Mozilla/5.0 (Macintosh; Intel Mac OS X 14.0; rv:140.0) Gecko/20100101 Firefox/140.0'

describe('isChromiumEngine', () => {
  it('detects Chromium even when DevTools device mode spoofs an iPhone Safari UA', () => {
    expect(isChromiumEngine({ userAgent: IPHONE_SAFARI, hasChromeObject: true, hasUserAgentData: true })).toBe(true)
  })

  it('detects Chromium over plain-http LAN, where userAgentData is unavailable', () => {
    expect(isChromiumEngine({ userAgent: DESKTOP_CHROME, hasChromeObject: true, hasUserAgentData: false })).toBe(true)
  })

  it('falls back to the UA string when no engine object is exposed', () => {
    expect(isChromiumEngine({ userAgent: DESKTOP_CHROME, hasChromeObject: false, hasUserAgentData: false })).toBe(true)
  })

  it('rejects real Safari and Firefox', () => {
    expect(isChromiumEngine({ userAgent: IPHONE_SAFARI, hasChromeObject: false, hasUserAgentData: false })).toBe(false)
    expect(isChromiumEngine({ userAgent: FIREFOX, hasChromeObject: false, hasUserAgentData: false })).toBe(false)
  })

  it('rejects WebKit-based browser shells on iOS', () => {
    expect(isChromiumEngine({ userAgent: IOS_CHROME, hasChromeObject: true, hasUserAgentData: false })).toBe(false)
  })
})
