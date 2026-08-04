import { useRef, type ChangeEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { Upload } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { toast } from '@/lib/toast'

type AvatarUploadProps = {
  value: string
  fallback?: string
  maxSize?: number
  onChange: (value: string) => void
}

export function AvatarUpload({
  value,
  fallback = '',
  maxSize = 1024 * 1024,
  onChange,
}: AvatarUploadProps) {
  const { t } = useTranslation('settings')
  const inputRef = useRef<HTMLInputElement>(null)
  const initials = getInitials(fallback)

  function handleFileChange(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0]
    event.target.value = ''
    if (!file) return

    if (!file.type.startsWith('image/')) {
      toast.error(t('profile.adminInfo.avatarImageRequired'))
      return
    }

    if (file.size > maxSize) {
      toast.error(t('profile.adminInfo.avatarTooLarge', { size: formatFileSize(maxSize) }))
      return
    }

    const reader = new FileReader()
    reader.onload = () => {
      if (typeof reader.result === 'string') {
        onChange(reader.result)
      }
    }
    reader.onerror = () => toast.error(t('profile.adminInfo.avatarReadFailed'))
    reader.readAsDataURL(file)
  }

  return (
    <div className="flex items-center gap-4">
      <div className="flex size-16 shrink-0 items-center justify-center overflow-hidden rounded-lg border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-subtle))] text-sm font-semibold text-foreground">
        {value ? <img src={value} alt="" className="h-full w-full object-cover" /> : initials}
      </div>
      <div className="flex flex-wrap gap-2">
        <input ref={inputRef} type="file" accept="image/*" className="hidden" onChange={handleFileChange} />
        <Button type="button" variant="secondary" onClick={() => inputRef.current?.click()}>
          <Upload className="h-4 w-4" />
          {t('profile.adminInfo.uploadAvatar')}
        </Button>
        {value ? (
          <Button type="button" variant="secondary" onClick={() => onChange('')}>
            {t('profile.adminInfo.clearAvatar')}
          </Button>
        ) : null}
      </div>
    </div>
  )
}

function getInitials(value: string) {
  const trimmed = value.trim()
  if (!trimmed) return 'A'
  return trimmed.slice(0, 2).toUpperCase()
}

function formatFileSize(value: number) {
  if (value >= 1024 * 1024) {
    return `${Math.round(value / 1024 / 1024)}MB`
  }

  if (value >= 1024) {
    return `${Math.round(value / 1024)}KB`
  }

  return `${value}B`
}
