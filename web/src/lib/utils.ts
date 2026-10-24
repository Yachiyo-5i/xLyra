import { clsx, type ClassValue } from 'clsx'
import { extendTailwindMerge } from 'tailwind-merge'

/**
 * 让 tailwind-merge 认识 index.css @theme 中的自定义刻度。
 * 否则 `text-body` 这类自定义字号会被当成文字颜色，与 `text-label` 互相覆盖。
 */
const twMerge = extendTailwindMerge({
  extend: {
    theme: {
      text: ['large-title', 'title-1', 'title-2', 'title-3', 'headline', 'body', 'callout', 'subhead', 'footnote', 'caption-1', 'caption-2', 'kpi'],
      radius: ['shell', 'card', 'inner', 'control', 'small'],
      color: [
        'canvas', 'elevated-1', 'elevated-2', 'elevated-3',
        'label', 'label-2', 'label-3', 'label-4',
        'fill-1', 'fill-2', 'fill-3', 'fill-4',
        'separator', 'separator-opaque',
        'tint', 'tint-foreground', 'success', 'danger', 'warning', 'caution', 'info',
        'chart-1', 'chart-2', 'chart-3', 'chart-4', 'chart-5', 'chart-6', 'chart-7', 'chart-8',
      ],
    },
  },
})

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}
