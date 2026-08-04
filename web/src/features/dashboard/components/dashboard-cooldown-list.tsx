import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { StatusBadge } from '@/components/common/status-badge'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { cn } from '@/lib/utils'
import type { CooldownItem } from './dashboard-types'

type DashboardCooldownListProps = {
  items: CooldownItem[]
  action?: ReactNode
  onClearItem?: (item: CooldownItem) => void
  className?: string
  compact?: boolean
}

export function DashboardCooldownList({ items, action, onClearItem, className, compact = false }: DashboardCooldownListProps) {
  const { t } = useTranslation('dashboard')

  return (
    <Card className={cn('flex min-h-0 flex-col rounded-lg p-5', className)}>
      <div className="mb-4 flex shrink-0 items-center justify-between gap-3">
        <div className="space-y-1">
          <h3 className="text-base font-semibold text-foreground">{t('cooldowns.title')}</h3>
          <p className="text-sm text-muted-soft">{t('cooldowns.description')}</p>
        </div>
        {action}
      </div>
      {items.length ? (
        <div className="scrollbar-hidden min-h-0 flex-1 divide-y divide-[hsl(var(--glass-divider))] overflow-auto pr-1">
          {items.map((item) => compact ? (
            <CompactCooldownItem key={item.id} item={item} onClearItem={onClearItem} />
          ) : (
            <div
              key={item.id}
              className="grid gap-3 py-3 sm:grid-cols-[84px_minmax(0,1fr)_92px_auto] sm:items-center"
            >
              <CooldownScopeBadge item={item} />
              <CooldownText item={item} />
              <StatusBadge status={item.severity === 'error' ? 'error' : 'warning'} className="w-fit sm:justify-self-end">
                {item.remaining}
              </StatusBadge>
              {onClearItem ? <CooldownClearButton item={item} onClearItem={onClearItem} /> : null}
            </div>
          ))}
        </div>
      ) : (
        <div className="rounded-lg border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-subtle))] px-4 py-5 text-center text-sm text-muted-soft">
          {t('cooldowns.empty')}
        </div>
      )}
    </Card>
  )
}

function CompactCooldownItem({
  item,
  onClearItem,
}: {
  item: CooldownItem
  onClearItem?: (item: CooldownItem) => void
}) {
  return (
    <div className="grid min-w-0 grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-3 py-3">
      <CooldownScopeBadge item={item} />
      <CooldownText item={item} compact />
      <div className="flex shrink-0 items-center gap-2">
        <StatusBadge status={item.severity === 'error' ? 'error' : 'warning'} className="shrink-0">
          {item.remaining}
        </StatusBadge>
        {onClearItem ? <CooldownClearButton item={item} onClearItem={onClearItem} /> : null}
      </div>
    </div>
  )
}

function CooldownScopeBadge({ item }: { item: CooldownItem }) {
  const { t } = useTranslation('dashboard')

  return (
    <Badge variant={item.scope === 'model' ? 'secondary' : item.scope === 'credential' ? 'accent' : 'neutral'} className="w-fit shrink-0">
      {item.scope === 'model' ? t('cooldowns.scope.model') : item.scope === 'credential' ? t('cooldowns.scope.credential') : t('cooldowns.scope.site')}
    </Badge>
  )
}

function CooldownText({ item, compact = false }: { item: CooldownItem; compact?: boolean }) {
  return (
    <div className="min-w-0 space-y-1">
      <p className={cn('text-sm font-medium text-foreground', compact ? 'truncate' : 'break-words')}>
        {item.name}
        {item.target ? <span className="text-muted-soft"> / {item.target}</span> : null}
      </p>
      <p className={cn('text-xs text-muted-soft', compact ? 'truncate' : 'break-words')}>{item.reason}</p>
    </div>
  )
}

function CooldownClearButton({
  item,
  onClearItem,
}: {
  item: CooldownItem
  onClearItem: (item: CooldownItem) => void
}) {
  const { t } = useTranslation('dashboard')

  return (
    <Button
      variant="ghost"
      size="sm"
      className="h-8 shrink-0 px-2 text-muted-soft hover:text-foreground"
      onClick={() => onClearItem(item)}
    >
      {t('page.clear')}
    </Button>
  )
}
