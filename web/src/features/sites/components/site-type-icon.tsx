import { useState } from 'react'
import { resolveSiteTypeIcon } from '@/features/sites/lib/site-utils'
import { cn } from '@/lib/utils'

/**
 * Site-type mark. `icon_url` (or a built-in icon path) is an `<img>`.
 * A load error falls back to the same letter tile the create wall uses when no icon is set.
 * `empty="hidden"` keeps a missing url blank; a failed image still falls back to the letter.
 * `muted` grays both forms when the provider plugin is unavailable: `opacity-60` is the
 * dim already used for disabled content, and `grayscale` desaturates the bitmap.
 */
export function SiteTypeIcon({
  src,
  name,
  className,
  imageClassName,
  empty = 'letter',
  muted = false,
}: {
  src?: string | null
  name: string
  className?: string
  imageClassName?: string
  empty?: 'letter' | 'hidden'
  muted?: boolean
}) {
  const [failedSrc, setFailedSrc] = useState<string | null>(null)
  const trimmed = src?.trim() ?? ''
  const failed = trimmed !== '' && failedSrc === trimmed
  const state = resolveSiteTypeIcon(src, failed, empty, name)
  const tone = muted ? 'grayscale opacity-60' : undefined
  if (state.kind === 'hidden') return null
  if (state.kind === 'letter') {
    return (
      <div className={cn('flex items-center justify-center bg-fill-3 text-body font-medium text-label-2', tone, className)}>
        {state.text}
      </div>
    )
  }
  return (
    <img
      src={state.src}
      alt=""
      className={cn('object-contain', tone, className, imageClassName)}
      onError={() => setFailedSrc(state.src)}
    />
  )
}
