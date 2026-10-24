export type ParsedUserAgent = {
  browser: string
  os: string
  device: '手机' | '平板' | '桌面'
}

const FALLBACK_UA_LENGTH = 32

export function parseUserAgent(ua: string): ParsedUserAgent {
  const raw = ua.trim()
  if (!raw) return { browser: '-', os: '', device: '桌面' }

  const device = detectDevice(raw)
  if (isNonBrowserClient(raw)) return { browser: truncateUserAgent(raw), os: '', device }

  const browser = detectBrowser(raw)
  if (!browser) return { browser: truncateUserAgent(raw), os: '', device }
  return { browser, os: detectOS(raw), device }
}

function isNonBrowserClient(ua: string) {
  return /\b(?:curl|scriptable)\b/i.test(ua)
}

function detectBrowser(ua: string) {
  const wechat = major(ua, /MicroMessenger\/(\d+)/)
  if (wechat || /MicroMessenger/.test(ua)) return withVersion('微信', wechat)

  const edge = major(ua, /Edg(?:e|iOS|A)?\/(\d+)/)
  if (edge || /Edg(?:e|iOS|A)?\//.test(ua)) return withVersion('Edge', edge)

  const opera = major(ua, /(?:OPR|Opera|OPiOS)\/(\d+)/)
  if (opera || /(?:OPR|Opera|OPiOS)\//.test(ua)) return withVersion('Opera', opera)

  const firefox = major(ua, /(?:Firefox|FxiOS)\/(\d+)/)
  if (firefox || /(?:Firefox|FxiOS)\//.test(ua)) return withVersion('Firefox', firefox)

  const chrome = major(ua, /(?:Chrome|CriOS)\/(\d+)/)
  if (chrome || /(?:Chrome|CriOS)\//.test(ua)) return withVersion('Chrome', chrome)

  if (/Safari\//.test(ua)) return withVersion('Safari', major(ua, /Version\/(\d+)/))
  return ''
}

function detectOS(ua: string) {
  if (/(?:iPhone|iPad|iPod)/.test(ua)) {
    return withVersion('iOS', major(ua, /(?:CPU (?:iPhone |iPad )?OS|CPU OS) (\d+)[_.]/))
  }
  if (/Android/.test(ua)) return withVersion('Android', major(ua, /Android (\d+)/))
  if (/CrOS/.test(ua)) return 'ChromeOS'
  if (/Windows/.test(ua)) return withVersion('Windows', windowsVersion(major(ua, /Windows NT (\d+\.\d+)/)))
  if (/Mac OS X|Macintosh/.test(ua)) return withVersion('macOS', major(ua, /Mac OS X (\d+)[_.]/))
  if (/Linux/.test(ua)) return 'Linux'
  return ''
}

function detectDevice(ua: string): ParsedUserAgent['device'] {
  if (/iPad|\bTablet\b/.test(ua)) return '平板'
  if (/Android/.test(ua) && !/Mobile/.test(ua)) return '平板'
  if (/iPhone|iPod|Android|Mobile/.test(ua)) return '手机'
  return '桌面'
}

function windowsVersion(nt: string | null) {
  switch (nt) {
    case '10.0':
      return '10'
    case '6.3':
      return '8.1'
    case '6.2':
      return '8'
    case '6.1':
      return '7'
    case '6.0':
      return '6'
    case '5.1':
    case '5.2':
      return '5'
    default:
      return null
  }
}

function major(ua: string, pattern: RegExp) {
  return pattern.exec(ua)?.[1] ?? null
}

function withVersion(name: string, version: string | null) {
  return version ? `${name} ${version}` : name
}

function truncateUserAgent(ua: string) {
  if (ua.length <= FALLBACK_UA_LENGTH) return ua
  return `${ua.slice(0, FALLBACK_UA_LENGTH)}…`
}
