import { type ReactNode, useState } from 'react'
import { ChevronLeft, Search } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import {
  Draw,
  DrawBody,
  DrawContent,
  DrawHeader,
  DrawTitle,
} from '@/components/ui/draw'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { BrandMark } from '@/components/common/brand-mark'
import { copyToClipboard } from '@/components/common/copy-to-clipboard'

export type ModelsDrawItem = {
  id: string
  displayName: string
  upstreamName?: string
  enabled: boolean
  toggleDisabled?: boolean
  icon?: {
    iconPath?: string
    label: string
    fallback: string
    fallbackText?: string
  }
  leadingAction?: ReactNode
}

export function ModelsDraw({ open, title, items, loading, pendingItemId, bulkPending, toolbarAction, children, backLabel, onBack, onToggleItem, onBulkToggleItems, onOpenChange }: {
  open: boolean; title: string; items: ModelsDrawItem[]; loading?: boolean; pendingItemId?: string; bulkPending?: boolean
  toolbarAction?: ReactNode; children?: ReactNode; backLabel?: string; onBack?: () => void
  onToggleItem: (item: ModelsDrawItem, enabled: boolean) => void; onBulkToggleItems?: (items: ModelsDrawItem[], enabled: boolean) => void; onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation('components')
  const [search, setSearch] = useState('')
  const keyword = search.trim().toLowerCase()
  const filtered = keyword ? items.filter((i) => [i.displayName, i.upstreamName ?? ''].some((v) => v.toLowerCase().includes(keyword))) : items
  const bulkItems = filtered.filter((item) => !item.toggleDisabled)
  const bulkDisabled = Boolean(loading || bulkPending || pendingItemId || !bulkItems.length)
  const canBulkEnable = bulkItems.some((item) => !item.enabled)
  const canBulkDisable = bulkItems.some((item) => item.enabled)

  function handleOpenChange(nextOpen: boolean) {
    onOpenChange(nextOpen)
    if (!nextOpen) setSearch('')
  }

  return (
    <Draw open={open} onOpenChange={handleOpenChange}>
      <DrawContent side="right" size="wide" onOpenAutoFocus={(event) => event.preventDefault()}>
        <DrawHeader>
          <div className="flex items-center gap-2">
            {onBack ? (
              <Button
                variant="ghost"
                size="icon"
                className="-ml-1 h-7 w-7"
                title={backLabel}
                aria-label={backLabel}
                onClick={onBack}
              >
                <ChevronLeft className="h-4 w-4" />
              </Button>
            ) : null}
            <DrawTitle>{title}</DrawTitle>
          </div>
        </DrawHeader>
        <DrawBody className="space-y-4">
          <div className="space-y-3">
            <div className="relative min-w-0">
              <Search className="text-foreground/40 absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 pointer-events-none z-10" />
              <Input value={search} onChange={(e) => setSearch(e.target.value)} placeholder={t('modelsDraw.searchPlaceholder')} className="pl-10" />
            </div>
            <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
              {onBulkToggleItems ? (
                <div className="grid grid-cols-2 gap-2 sm:flex">
                  <Button size="sm" variant="secondary" disabled={bulkDisabled || !canBulkEnable} onClick={() => onBulkToggleItems(bulkItems, true)}>
                    {t('modelsDraw.enableAll')}
                  </Button>
                  <Button size="sm" variant="secondary" disabled={bulkDisabled || !canBulkDisable} onClick={() => onBulkToggleItems(bulkItems, false)}>
                    {t('modelsDraw.disableAll')}
                  </Button>
                </div>
              ) : <span />}
              {toolbarAction ? <div className="sm:shrink-0">{toolbarAction}</div> : null}
            </div>
          </div>
          {children}
          <div className="scrollbar-hidden overflow-auto rounded-lg border border-[hsl(var(--glass-border))]">
            <table className="w-full table-fixed border-collapse text-left text-sm">
              <thead className="bg-[hsl(var(--surface-subtle))] text-faint text-xs uppercase tracking-[0.16em]">
                <tr>
                  <th className="w-[65%] px-4 py-3 font-medium">{t('modelsDraw.headers.model')}</th>
                  <th className="w-[35%] px-4 py-3 font-medium text-right">{t('modelsDraw.headers.enabled')}</th>
                </tr>
              </thead>
              <tbody>
                {loading ? (
                  <tr><td colSpan={2} className="px-4 py-10 text-center text-sm text-muted-soft">{t('modelsDraw.loading')}</td></tr>
                ) : filtered.length ? (
                  filtered.map((item) => {
                    const subtitle = item.upstreamName && item.upstreamName !== item.displayName ? item.upstreamName : undefined
                    return (
                      <tr key={item.id} className="border-t border-[hsl(var(--glass-divider))]">
                        <td className="min-w-0 px-4 py-3">
                          <div className={subtitle ? 'flex min-w-0 items-start gap-2' : 'flex min-w-0 items-center gap-2'}>
                            {item.leadingAction ? <div className={subtitle ? 'mt-0.5 shrink-0' : 'shrink-0'}>{item.leadingAction}</div> : null}
                            {item.icon ? (
                              <BrandMark
                                iconPath={item.icon.iconPath}
                                label={item.icon.label}
                                fallback={item.icon.fallback}
                                fallbackText={item.icon.fallbackText}
                                size="sm"
                              />
                            ) : null}
                            <div className="min-w-0">
                              <button type="button" className="block max-w-full cursor-pointer truncate text-left font-medium text-foreground" title={t('modelsDraw.copyName')} onClick={() => copyToClipboard(item.displayName, t('modelsDraw.copied'), t('modelsDraw.copyFailed'))}>
                                {item.displayName}
                              </button>
                              {subtitle ? <div className="text-muted-soft truncate text-xs">{subtitle}</div> : null}
                            </div>
                          </div>
                        </td>
                        <td className="px-4 py-3 text-right">
                          <Switch checked={item.enabled} disabled={bulkPending || pendingItemId === item.id || item.toggleDisabled} aria-label={t('modelsDraw.toggleLabel', { name: item.displayName })} onCheckedChange={(checked) => onToggleItem(item, checked)} />
                        </td>
                      </tr>
                    )
                  })
                ) : (
                  <tr><td colSpan={2} className="px-4 py-10 text-center text-sm text-muted-soft">{t('modelsDraw.noModels')}</td></tr>
                )}
              </tbody>
            </table>
          </div>
        </DrawBody>
      </DrawContent>
    </Draw>
  )
}
