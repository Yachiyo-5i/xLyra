import { focusManager, QueryObserver } from '@tanstack/react-query'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { queryClient } from '@/lib/query-client'

describe('window focus refresh', () => {
  let browser: EventTarget
  let page: EventTarget & { visibilityState: string }
  let unsubscribe: () => void

  beforeEach(() => {
    browser = new EventTarget()
    page = Object.assign(new EventTarget(), { visibilityState: 'visible' })
    vi.stubGlobal('window', browser)
    vi.stubGlobal('document', page)
    focusManager.setFocused(true)
    queryClient.mount()
    unsubscribe = () => {}
  })

  afterEach(() => {
    unsubscribe()
    queryClient.unmount()
    queryClient.clear()
    focusManager.setFocused(undefined)
    vi.unstubAllGlobals()
  })

  function observe(refetchOnWindowFocus?: 'always') {
    const fetch = vi.fn().mockResolvedValue('updated')
    queryClient.setQueryData(['focus-test'], 'cached')
    const observer = new QueryObserver(queryClient, {
      queryKey: ['focus-test'],
      queryFn: fetch,
      staleTime: Infinity,
      ...(refetchOnWindowFocus ? { refetchOnWindowFocus } : {}),
    })
    unsubscribe = observer.subscribe(() => {})
    return fetch
  }

  it('refreshes fresh data when the browser window regains focus', async () => {
    const fetch = observe('always')
    browser.dispatchEvent(new Event('blur'))
    browser.dispatchEvent(new Event('focus'))
    await vi.waitFor(() => expect(fetch).toHaveBeenCalledTimes(1))
  })

  it('refreshes once for visibility and focus events from the same return', async () => {
    const fetch = observe('always')
    page.visibilityState = 'hidden'
    page.dispatchEvent(new Event('visibilitychange'))
    browser.dispatchEvent(new Event('focus'))
    expect(fetch).not.toHaveBeenCalled()
    page.visibilityState = 'visible'
    page.dispatchEvent(new Event('visibilitychange'))
    await vi.waitFor(() => expect(fetch).toHaveBeenCalledTimes(1))
    browser.dispatchEvent(new Event('focus'))
    await Promise.resolve()
    await Promise.resolve()
    expect(fetch).toHaveBeenCalledTimes(1)
  })

  it('keeps queries without focus refresh disabled', async () => {
    const fetch = observe()
    browser.dispatchEvent(new Event('blur'))
    browser.dispatchEvent(new Event('focus'))
    await Promise.resolve()
    await Promise.resolve()
    expect(fetch).not.toHaveBeenCalled()
  })

  it('does not refresh queries after their observer is removed', async () => {
    const fetch = observe('always')
    unsubscribe()
    browser.dispatchEvent(new Event('blur'))
    browser.dispatchEvent(new Event('focus'))
    await Promise.resolve()
    await Promise.resolve()
    expect(fetch).not.toHaveBeenCalled()
  })
})
