import { useEffect, useState } from 'react'
import * as QRCodeLib from 'qrcode'
import { cn } from '@/lib/utils'

type QRCodeProps = {
  value: string
  size?: number
  className?: string
  imageClassName?: string
}

export function QRCode({ value, size = 192, className, imageClassName }: QRCodeProps) {
  const [dataURL, setDataURL] = useState('')

  useEffect(() => {
    let cancelled = false

    Promise.resolve()
      .then(() => {
        if (!value) return ''

        return QRCodeLib.toDataURL(value, {
          width: size,
          margin: 1,
          errorCorrectionLevel: 'M',
          color: {
            dark: '#111827',
            light: '#ffffff',
          },
        })
      })
      .then((nextDataURL) => {
        if (!cancelled) setDataURL(nextDataURL)
      })
      .catch(() => {
        if (!cancelled) setDataURL('')
      })

    return () => {
      cancelled = true
    }
  }, [size, value])

  return (
    <div
      className={cn(
        'flex items-center justify-center rounded-lg border border-[hsl(var(--glass-border))] bg-white p-3',
        className,
      )}
      style={{ width: size + 24, height: size + 24 }}
    >
      {dataURL ? (
        <img
          src={dataURL}
          alt=""
          className={cn('block size-full object-contain', imageClassName)}
          draggable={false}
        />
      ) : (
        <div className="h-full w-full animate-pulse rounded bg-slate-100" />
      )}
    </div>
  )
}
