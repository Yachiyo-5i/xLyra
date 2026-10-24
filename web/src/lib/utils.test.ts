import { describe, expect, it } from 'vitest'
import { cn } from '@/lib/utils'

describe('cn with design tokens', () => {
  it('keeps a custom font size and a custom text color together', () => {
    expect(cn('text-label', 'text-body')).toBe('text-label text-body')
    expect(cn('text-headline', 'text-label-2')).toBe('text-headline text-label-2')
  })

  it('resolves conflicts within the same custom scale', () => {
    expect(cn('text-body', 'text-callout')).toBe('text-callout')
    expect(cn('text-body', 'text-xs')).toBe('text-xs')
    expect(cn('text-label', 'text-danger')).toBe('text-danger')
    expect(cn('rounded-full', 'rounded-card')).toBe('rounded-card')
    expect(cn('bg-fill-2', 'bg-tint')).toBe('bg-tint')
  })

  it('lets callers override control heights', () => {
    expect(cn('h-[var(--control-h)]', 'h-8')).toBe('h-8')
    expect(cn('size-[var(--control-h)]', 'size-10')).toBe('size-10')
  })
})
