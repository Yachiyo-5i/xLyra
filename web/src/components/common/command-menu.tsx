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
      {/* 覆盖层：Liquid Glass thick，位于屏幕上部三分之一，类似 Spotlight */}
      <DialogContent
        className="lg-thick top-[14dvh] flex max-h-[min(72dvh,560px)] w-[min(92vw,640px)] translate-y-0 flex-col overflow-hidden rounded-shell"
        overlayClassName="bg-black/40 backdrop-blur-0"
        onKeyDown={handleKeyDown}
        onOpenAutoFocus={(e) => {
          e.preventDefault()
          inputRef.current?.focus()
        }}
      >
        {/* 搜索框：大号、无边框 */}
        <div className="flex shrink-0 items-center gap-3 border-b border-separator px-5">
          <Search className="size-5 shrink-0 text-label-3" />
          <input
            ref={inputRef}
            value={query}
            onChange={(e) => {
              setQuery(e.target.value)
              setActiveIndex(0)
            }}
            placeholder={t('actions.search') + '…'}
            className="h-14 flex-1 bg-transparent text-title-3 font-normal text-label outline-none placeholder:text-label-3"
          />
          <kbd className="hidden h-6 items-center rounded-full bg-fill-3 px-2 font-mono text-caption-1 text-label-2 sm:inline-flex">
            esc
          </kbd>
        </div>

        {/* 结果：按分组列出，键盘上下选择 */}
        <div ref={listRef} className="scrollbar-hidden min-h-0 flex-1 overflow-auto p-2">
          {flatItems.length === 0 ? (
            <div className="px-4 py-10 text-center text-callout text-label-3">{t('status.noData')}</div>
          ) : (
            (() => {
              let flatIdx = -1
              return [...grouped.entries()].map(([section, items]) => (
                <div key={section}>
                  <div className="px-3 pb-1 pt-3 text-caption-1 text-label-3 first:pt-1">{section}</div>
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
                          // 面板圆角 24、内边距 8 → 行圆角 16（同心）
                          'flex min-h-11 w-full items-center gap-3 rounded-2xl px-3 py-2 text-left transition-colors duration-100',
                          isActive ? 'bg-fill-3' : 'hover:bg-fill-4',
                        )}
                        onMouseEnter={() => setActiveIndex(idx)}
                        onClick={() => handleSelect(item)}
                      >
                        <span className={cn('grid size-8 shrink-0 place-items-center rounded-[10px] bg-fill-3', isActive ? 'text-tint' : 'text-label-2')}>
                          <Icon className="size-4" />
                        </span>
                        <div className="min-w-0 flex-1">
                          <div className="truncate text-body text-label">{item.label}</div>
                          {item.description ? (
                            <div className="truncate text-footnote text-label-3">{item.description}</div>
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

        {/* 底部快捷键提示 */}
        <div className="hidden shrink-0 items-center gap-4 border-t border-separator px-5 py-2.5 text-caption-1 text-label-3 sm:flex">
          {[
            ['↑↓', 'navigate'],
            ['↵', 'open'],
            ['esc', 'close'],
          ].map(([key, label]) => (
            <span key={key} className="inline-flex items-center gap-1.5">
              <kbd className="inline-flex h-5 min-w-5 items-center justify-center rounded-small bg-fill-3 px-1 font-mono text-label-2">{key}</kbd>
              {label}
            </span>
          ))}
        </div>
      </DialogContent>
    </Dialog>
  )
}
