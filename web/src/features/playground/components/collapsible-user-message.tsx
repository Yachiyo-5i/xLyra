import { useLayoutEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'

type CollapsibleUserMessageProps = {
  content: string
}

export function CollapsibleUserMessage({ content }: CollapsibleUserMessageProps) {
  const { t } = useTranslation('playground')
  const [expandedContent, setExpandedContent] = useState<string | null>(null)
  const [overflowing, setOverflowing] = useState(false)
  const contentRef = useRef<HTMLDivElement>(null)
  const expanded = expandedContent === content

  useLayoutEffect(() => {
    const element = contentRef.current
    if (!element) {
      setOverflowing(false)
      return
    }
    const measure = () => {
      const lineHeight = Number.parseFloat(window.getComputedStyle(element).lineHeight)
      setOverflowing(Number.isFinite(lineHeight) && element.scrollHeight > lineHeight * 5 + 1)
    }
    measure()
    if (typeof ResizeObserver === 'undefined') {
      window.addEventListener('resize', measure)
      return () => window.removeEventListener('resize', measure)
    }
    const observer = new ResizeObserver(measure)
    observer.observe(element)
    return () => observer.disconnect()
  }, [content, expanded])

  return (
    <div className="max-w-[80%] rounded-3xl border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-selected))] px-4 py-2.5 text-[15px] leading-6 text-foreground">
      <div
        ref={contentRef}
        className={cn('whitespace-pre-wrap break-words', !expanded && 'line-clamp-5')}
      >
        {content}
      </div>
      {overflowing ? (
        <button
          type="button"
          className="ml-auto mt-1 block text-xs font-medium text-muted-soft transition-colors hover:text-foreground"
          aria-expanded={expanded}
          onClick={() => setExpandedContent(expanded ? null : content)}
        >
          {expanded ? t('actions.collapse') : t('actions.expandMore')}
        </button>
      ) : null}
    </div>
  )
}
