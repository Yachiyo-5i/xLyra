import { toast } from '@/lib/toast'

export async function copyToClipboard(text: string, label: string, failureLabel: string | null = 'Copy failed') {
  if (!text) return false
  try {
    await navigator.clipboard.writeText(text)
    toast.success(label)
    return true
  } catch {
    const ta = document.createElement('textarea')
    ta.value = text
    ta.style.position = 'fixed'
    ta.style.left = '-9999px'
    document.body.appendChild(ta)
    ta.select()
    try {
      document.execCommand('copy')
      toast.success(label)
      return true
    } catch {
      if (failureLabel) toast.error(failureLabel)
      return false
    } finally {
      document.body.removeChild(ta)
    }
  }
}
