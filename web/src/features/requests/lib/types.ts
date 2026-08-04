import type { RequestLogListFilters } from '@/features/requests/api/requests'

export type SuccessFilter = 'all' | 'success' | 'failed'

export type RequestFilterState = {
  search: string
  requestId: string
  modelKey: string
  errorType: string
  success: SuccessFilter
  siteId: string
  apiKeyId: string
  hideWithoutSite: boolean
  createdFromDate?: Date
  createdFromTime?: string
  createdToDate?: Date
  createdToTime?: string
}

const REQUEST_FILTER_EMPTY: RequestFilterState = {
  search: '',
  requestId: '',
  modelKey: '',
  errorType: '',
  success: 'all',
  siteId: 'all',
  apiKeyId: 'all',
  hideWithoutSite: false,
}

export const REQUESTS_AUTO_REFRESH_INTERVAL_MS = 10_000
export const REQUESTS_PAGE_SIZE_OPTIONS = [50, 100, 200]

export function getEmptyRequestFilters(): RequestFilterState {
  return { ...REQUEST_FILTER_EMPTY }
}

export function getInitialRequestFilters(now = new Date()): RequestFilterState {
  const from = new Date(now)
  from.setHours(0, 0, 0, 0)

  const to = new Date(now)
  to.setHours(to.getHours() + 1)

  return {
    ...REQUEST_FILTER_EMPTY,
    hideWithoutSite: true,
    createdFromDate: from,
    createdFromTime: formatTimeFilter(from),
    createdToDate: to,
    createdToTime: formatTimeFilter(to),
  }
}

export function requestFiltersToListFilters(filters: RequestFilterState): RequestLogListFilters {
  return {
    success: filters.success === 'all' ? null : filters.success === 'success',
    siteId: filters.siteId === 'all' ? undefined : filters.siteId,
    apiKeyId: filters.apiKeyId === 'all' ? undefined : filters.apiKeyId,
    search: filters.search.trim() || undefined,
    requestId: filters.requestId.trim() || undefined,
    modelKey: filters.modelKey.trim() || undefined,
    errorType: filters.errorType.trim() || undefined,
    hideWithoutSite: filters.hideWithoutSite,
    createdFrom: dateTimeFilterToRFC3339(filters.createdFromDate, filters.createdFromTime, 'start'),
    createdTo: dateTimeFilterToRFC3339(filters.createdToDate, filters.createdToTime, 'end'),
  }
}

export function hasRequestFilters(filters: RequestFilterState) {
  return filters.search.trim() !== '' ||
    filters.requestId.trim() !== '' ||
    filters.modelKey.trim() !== '' ||
    filters.errorType.trim() !== '' ||
    filters.success !== 'all' ||
    filters.siteId !== 'all' ||
    filters.apiKeyId !== 'all' ||
    Boolean(filters.createdFromDate) ||
    Boolean(filters.createdToDate)
}

export function requestFiltersFromSearchParams(params: URLSearchParams): RequestFilterState {
  const createdFrom = dateTimeFilterFromParam(params.get('created_from'))
  const createdTo = dateTimeFilterFromParam(params.get('created_to'))
  const initial = getInitialRequestFilters()

  return {
    ...initial,
    search: params.get('search')?.trim() ?? '',
    requestId: params.get('request_id')?.trim() ?? '',
    modelKey: params.get('model_key')?.trim() ?? '',
    errorType: params.get('error_type')?.trim() ?? '',
    success: successFilterFromParam(params.get('success')),
    siteId: params.get('site_id')?.trim() || 'all',
    apiKeyId: params.get('api_key_id')?.trim() || 'all',
    hideWithoutSite: boolFilterFromParam(params.get('hide_without_site'), initial.hideWithoutSite),
    createdFromDate: createdFrom.date ?? initial.createdFromDate,
    createdFromTime: createdFrom.time ?? initial.createdFromTime,
    createdToDate: createdTo.date ?? initial.createdToDate,
    createdToTime: createdTo.time ?? initial.createdToTime,
  }
}

function successFilterFromParam(value: string | null): SuccessFilter {
  if (value === 'true') return 'success'
  if (value === 'false') return 'failed'
  return 'all'
}

function boolFilterFromParam(value: string | null, fallback: boolean) {
  if (value === 'true') return true
  if (value === 'false') return false
  return fallback
}

function dateTimeFilterFromParam(value: string | null) {
  if (!value) return { date: undefined, time: undefined }
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) return { date: undefined, time: undefined }

  return {
    date: parsed,
    time: `${String(parsed.getHours()).padStart(2, '0')}:${String(parsed.getMinutes()).padStart(2, '0')}`,
  }
}

function dateTimeFilterToRFC3339(date: Date | undefined, time: string | undefined, mode: 'start' | 'end') {
  if (!date) return undefined

  const [hour, minute] = (time || (mode === 'start' ? '00:00' : '23:59')).split(':').map(Number)
  const nextDate = new Date(date)
  nextDate.setHours(Number.isFinite(hour) ? hour : 0)
  nextDate.setMinutes(Number.isFinite(minute) ? minute : 0)
  nextDate.setSeconds(mode === 'start' ? 0 : 59)
  nextDate.setMilliseconds(mode === 'start' ? 0 : 999)
  return nextDate.toISOString()
}

function formatTimeFilter(value: Date) {
  return `${String(value.getHours()).padStart(2, '0')}:${String(value.getMinutes()).padStart(2, '0')}`
}
