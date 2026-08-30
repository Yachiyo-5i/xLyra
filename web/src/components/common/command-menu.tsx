import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { Search } from 'lucide-react'
import { cn } from '@/lib/utils'
import { getAppNavSections, type AppNavItem } from '@/lib/navigation'
import { useMobileDevice, useMobileLayout } from '@/hooks/use-media-query'
import { useAgentAvailability } from '@/features/agent/lib/use-agent-availability'
import {
  Dialog,
  DialogContent,
} from '@/components/ui/dialog'

type CommandItem = AppNavItem & { section: string }

export type CommandMenuProps = {
  open?: boolean
  onOpenChange?: (open: boolean) => void
}

export function CommandMenu({ open: openProp, onOpenChange: onOpenChangeProp }: CommandMenuProps = {}) {
  const { t } = useTranslation('common')
  const isMobileLayout = useMobileLayout()
  const isMobileDevice = useMobileDevice()
  const agentAvailable = useAgentAvailability()
  const [openState, setOpenState] = useState(false)
  const open = openProp ?? openState
  const setOpen = onOpenChangeProp ?? setOpenState
  const [query, setQuery] = useState('')
  const [activeIndex, setActiveIndex] = useState(0)
  const navigate = useNavigate()
  const inputRef = useRef<HTMLInputElement>(null)
  const listRef = useRef<HTMLDivElement>(null)

  const allCommandItems = useMemo<CommandItem[]>(() => {
    const sections = getAppNavSections(t, { agent: agentAvailable })
    return sections.flatMap((section) =>
      section.items.filter((item) => !item.desktopOnly || (!isMobileLayout && !isMobileDevice)).flatMap((item) => {
        const self: CommandItem = { ...item, section: section.label }
        const kids = item.children
          ? item.children.map((c) => ({ ...c, section: section.label }))
          : []
        return [self, ...kids]
      }),
    )
  }, [isMobileDevice, isMobileLayout, t, agentAvailable])

  const handleOpenChange = useCallback(
    (nextOpen: boolean) => {
      setOpen(nextOpen)
      if (!nextOpen) {
        setQuery('')
        setActiveIndex(0)
      }
    },
    [setOpen],
  )

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return allCommandItems
    return allCommandItems.filter(
      (item) =>
        item.label.toLowerCase().includes(q) ||
        item.description.toLowerCase().includes(q),
    )
  }, [query, allCommandItems])

  // Group by section
  const grouped = useMemo(() => {
    const map = new Map<string, CommandItem[]>()
    for (const item of filtered) {
      const arr = map.get(item.section) ?? []
      arr.push(item)
      map.set(item.section, arr)
    }
    return map
  }, [filtered])

  // Flat list for keyboard navigation
  const flatItems = useMemo(() => [...grouped.values()].flat(), [grouped])

  // Scroll active item into view
  useEffect(() => {
    const el = listRef.current?.querySelector('[data-active="true"]')
    el?.scrollIntoView({ block: 'nearest' })
  }, [activeIndex])

  const handleSelect = useCallback(
    (item: CommandItem) => {
      navigate(item.to)
      handleOpenChange(false)
    },
    [handleOpenChange, navigate],
  )

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (e.key === 'ArrowDown') {
        e.preventDefault()
        setActiveIndex((i) => Math.min(i + 1, flatItems.length - 1))
      } else if (e.key === 'ArrowUp') {
        e.preventDefault()
        setActiveIndex((i) => Math.max(i - 1, 0))
      } else if (e.key === 'Enter') {
        e.preventDefault()
        const item = flatItems[activeIndex]
        if (item) handleSelect(item)
      }
    },
    [flatItems, activeIndex, handleSelect],
  )

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent
        className="w-[min(92vw,560px)] overflow-hidden p-0 gap-0"
        overlayClassName="bg-black/40"
        onKeyDown={handleKeyDown}
        onOpenAutoFocus={(e) => {
          e.preventDefault()
          inputRef.current?.focus()
        }}
      >
        {/* Search input */}
        <div className="flex items-center gap-3 border-b border-[hsl(var(--glass-divider))] px-4 py-3">
          <Search className="h-4 w-4 shrink-0 text-foreground/40" />
          <input
            ref={inputRef}
            value={query}
            onChange={(e) => {
              setQuery(e.target.value)
              setActiveIndex(0)
            }}
            placeholder={t('actions.search') + '...'}
            className="flex-1 bg-transparent text-base text-foreground outline-none placeholder:text-[hsl(var(--input-placeholder))] md:text-sm"
          />
          <kbd className="hidden sm:inline-flex items-center rounded border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-subtle))] px-1.5 py-0.5 text-[10px] font-medium text-muted-soft">
            ESC
          </kbd>
        </div>

        {/* Results */}
        <div ref={listRef} className="scrollbar-hidden max-h-[50vh] overflow-auto py-2">
          {flatItems.length === 0 ? (
            <div className="px-4 py-8 text-center text-sm text-muted-soft">
              {t('status.noData')}
            </div>
          ) : (
            (() => {
              let flatIdx = -1
              return [...grouped.entries()].map(([section, items]) => (
                <div key={section}>
                  <div className="px-4 pt-3 pb-1 text-[10px] font-medium uppercase tracking-[0.2em] text-muted-soft">
                    {section}
                  </div>
                  {items.map((item) => {
                    flatIdx++
                    const idx = flatIdx
                    const isActive = idx === activeIndex
                    const Icon = item.icon
                    return (
                      <button
                        key={item.to}
                        data-active={isActive}
                        type="button"
                        className={cn(
                          'flex w-full items-center gap-3 px-4 py-2.5 text-left text-sm transition-colors',
                          isActive
                            ? 'bg-[hsl(var(--primary)/0.1)] text-foreground'
                            : 'text-foreground/80 hover:bg-[hsl(var(--surface-subtle))]',
                        )}
                        onMouseEnter={() => setActiveIndex(idx)}
                        onClick={() => handleSelect(item)}
                      >
                        <Icon className="h-4 w-4 shrink-0 text-foreground/50" />
                        <div className="min-w-0 flex-1">
                          <div className="font-medium truncate">{item.label}</div>
                          {item.description ? (
                            <div className="text-xs text-muted-soft truncate">
                              {item.description}
                            </div>
                          ) : null}
                        </div>
                      </button>
                    )
                  })}
                </div>
              ))
            })()
          )}
        </div>

        {/* Footer hint */}
        <div className="flex items-center gap-4 border-t border-[hsl(var(--glass-divider))] px-4 py-2 text-[10px] text-muted-soft">
          <span className="inline-flex items-center gap-1">
            <kbd className="rounded border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-subtle))] px-1 py-0.5">↑↓</kbd>
            navigate
          </span>
          <span className="inline-flex items-center gap-1">
            <kbd className="rounded border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-subtle))] px-1 py-0.5">↵</kbd>
            open
          </span>
          <span className="inline-flex items-center gap-1">
            <kbd className="rounded border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-subtle))] px-1 py-0.5">esc</kbd>
            close
          </span>
        </div>
      </DialogContent>
    </Dialog>
  )
}
