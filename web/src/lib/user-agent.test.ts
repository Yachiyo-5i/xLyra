import { describe, expect, it } from 'vitest'
import { parseUserAgent } from './user-agent'

const chromeWindows = 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36'
const edgeWindows = 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Edg/120.0.0.0'
const operaWindows = 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 OPR/106.0.0.0'
const firefoxLinux = 'Mozilla/5.0 (X11; Linux x86_64; rv:121.0) Gecko/20100101 Firefox/121.0'
const safariMac = 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.2 Safari/605.1.15'
const safariIphone = 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_2 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.2 Mobile/15E148 Safari/604.1'
const safariIpad = 'Mozilla/5.0 (iPad; CPU OS 17_2 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.2 Mobile/15E148 Safari/604.1'
const chromeAndroidTablet = 'Mozilla/5.0 (Linux; Android 13; SM-X900) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36'
const wechatAndroid = 'Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/120.0.6099.210 Mobile Safari/537.36 MicroMessenger/8.0.47.2560(0x28002F35) WeChat/arm64'
const chromeOS = 'Mozilla/5.0 (X11; CrOS x86_64 14541.0.0) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36'
const scriptable = 'Scriptable/163 (iPhone; iOS 16.1; Scale/3.00)'

describe('parseUserAgent', () => {
  it.each([
    [chromeWindows, { browser: 'Chrome 120', os: 'Windows 10', device: '桌面' }],
    [edgeWindows, { browser: 'Edge 120', os: 'Windows 10', device: '桌面' }],
    [operaWindows, { browser: 'Opera 106', os: 'Windows 10', device: '桌面' }],
    [firefoxLinux, { browser: 'Firefox 121', os: 'Linux', device: '桌面' }],
    [safariMac, { browser: 'Safari 17', os: 'macOS 10', device: '桌面' }],
    [safariIphone, { browser: 'Safari 17', os: 'iOS 17', device: '手机' }],
    [safariIpad, { browser: 'Safari 17', os: 'iOS 17', device: '平板' }],
    [chromeAndroidTablet, { browser: 'Chrome 120', os: 'Android 13', device: '平板' }],
    [wechatAndroid, { browser: '微信 8', os: 'Android 14', device: '手机' }],
    [chromeOS, { browser: 'Chrome 120', os: 'ChromeOS', device: '桌面' }],
  ])('parses %s', (ua, expected) => {
    expect(parseUserAgent(ua)).toEqual(expected)
  })

  it('prefers Edge over Chrome and Chrome over Safari', () => {
    expect(parseUserAgent(edgeWindows).browser).toBe('Edge 120')
    expect(parseUserAgent(chromeWindows).browser).toBe('Chrome 120')
    expect(parseUserAgent(operaWindows).browser).toBe('Opera 106')
    expect(parseUserAgent(wechatAndroid).browser).toBe('微信 8')
  })

  it('falls back to a truncated raw UA for non-browser clients', () => {
    expect(parseUserAgent('curl/8.4.0')).toEqual({ browser: 'curl/8.4.0', os: '', device: '桌面' })
    expect(parseUserAgent(scriptable)).toEqual({
      browser: `${scriptable.slice(0, 32)}…`,
      os: '',
      device: '手机',
    })
  })

  it('shows a dash for an empty UA', () => {
    expect(parseUserAgent('')).toEqual({ browser: '-', os: '', device: '桌面' })
    expect(parseUserAgent('   ')).toEqual({ browser: '-', os: '', device: '桌面' })
  })
})
