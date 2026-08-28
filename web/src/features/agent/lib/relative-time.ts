const DIVISIONS: Array<{ limit: number; divisor: number; unit: Intl.RelativeTimeFormatUnit }> = [
  { limit: 60, divisor: 1, unit: 'second' },
  { limit: 3600, divisor: 60, unit: 'minute' },
  { limit: 86400, divisor: 3600, unit: 'hour' },
  { limit: 2629800, divisor: 86400, unit: 'day' },
  { limit: 31557600, divisor: 2629800, unit: 'month' },
  { limit: Infinity, divisor: 31557600, unit: 'year' },
]

export function formatRelativeTime(value: string | number | undefined, locale: string, now = Date.now()): string {
  if (value === undefined || value === null || value === '') return ''
  const timestamp = new Date(value).getTime()
  if (!Number.isFinite(timestamp)) return ''
  const diffSeconds = Math.round((timestamp - now) / 1000)
  const absSeconds = Math.abs(diffSeconds)
  const division = DIVISIONS.find((item) => absSeconds < item.limit) ?? DIVISIONS[DIVISIONS.length - 1]
  const language = locale.startsWith('ja') || locale.startsWith('jp') ? 'ja' : locale.startsWith('en') ? 'en' : 'zh'
  return new Intl.RelativeTimeFormat(language, { numeric: 'auto' }).format(Math.round(diffSeconds / division.divisor), division.unit)
}
