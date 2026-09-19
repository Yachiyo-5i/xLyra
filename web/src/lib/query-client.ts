import { focusManager, QueryClient } from '@tanstack/react-query'

focusManager.setEventListener((setFocused) => {
  if (typeof window === 'undefined') return

  const onFocus = () => setFocused(document.visibilityState !== 'hidden')
  const onBlur = () => setFocused(false)
  window.addEventListener('focus', onFocus)
  window.addEventListener('blur', onBlur)
  document.addEventListener('visibilitychange', onFocus)

  return () => {
    window.removeEventListener('focus', onFocus)
    window.removeEventListener('blur', onBlur)
    document.removeEventListener('visibilitychange', onFocus)
  }
})

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      refetchOnWindowFocus: false,
      retry: 1,
      staleTime: 30_000,
    },
    mutations: {
      retry: 0,
    },
  },
})
