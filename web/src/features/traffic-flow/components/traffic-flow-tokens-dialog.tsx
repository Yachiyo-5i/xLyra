import { useLayoutEffect, useMemo, useState, type CSSProperties, type RefObject } from 'react'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import type { TrafficFlowUsageCell } from '@/features/traffic-flow/api/traffic-flow'
import { formatCompactTokens } from '@/features/traffic-flow/lib/traffic-flow-format'
import {
  deriveTokenUsage,
  emptyTokenUsageFilter,
  isTokenUsageFilterActive,
  toggleTokenUsageFilter,
  type TokenUsageFilter,
  type TokenUsageRank,
} from '@/features/traffic-flow/lib/token-usage-filter'
import { OTHER_BRAND_LABEL, providerCatalog } from '@/lib/brands'
import { sameNode, type TrafficFlowNodeRef } from '@/features/traffic-flow/lib/wing-layout'

type Translate = (key: string, options?: Record<string, string | number>) => string

type TrafficFlowTokensDialogProps = {
  open: boolean
  cells: TrafficFlowUsageCell[]
  selectedNode: TrafficFlowNodeRef | null
  container?: HTMLElement | null
  kpisRef: RefObject<HTMLElement | null>
  tokensButtonRef: RefObject<HTMLButtonElement | null>
  t: Translate
  onOpenChange: (open: boolean) => void
  highlightNode: (node: TrafficFlowNodeRef | null) => void
}

export function TrafficFlowTokensDialog({
  open,
  cells,
  selectedNode,
  container,
  kpisRef,
  tokensButtonRef,
  t,
  onOpenChange,
  highlightNode,
}: TrafficFlowTokensDialogProps) {
  const [filter, setFilter] = useState<TokenUsageFilter>(emptyTokenUsageFilter)
  const [lastNodeClick, setLastNodeClick] = useState<{ kind: 'key' | 'site'; id: string } | null>(null)
  const [panelStyle, setPanelStyle] = useState<CSSProperties>({})
  const [openSnapshot, setOpenSnapshot] = useState(open)
  if (open !== openSnapshot) {
    setOpenSnapshot(open)
    if (!open) {
      setFilter(emptyTokenUsageFilter())
      setLastNodeClick(null)
    }
  }
  const breakdown = useMemo(() => deriveTokenUsage(cells, filter), [cells, filter])
  const active = isTokenUsageFilterActive(filter)

  useLayoutEffect(() => {
    if (!open) return
    const update = () => setPanelStyle(measurePanelStyle(container, kpisRef.current, tokensButtonRef.current))
    update()
    window.addEventListener('resize', update)
    return () => window.removeEventListener('resize', update)
  }, [container, kpisRef, open, tokensButtonRef])

  useLayoutEffect(() => {
    if (!open) return
    const target = highlightTarget(filter, lastNodeClick)
    if (target) {
      if (!sameNode(selectedNode, target)) highlightNode(target)
      return
    }
    if (!selectedNode) return
    const visible = selectedNode.kind === 'downstream'
      ? breakdown.keys.some((item) => item.id === selectedNode.id)
      : breakdown.sites.some((item) => item.id === selectedNode.id)
    if (!visible) highlightNode(null)
  }, [breakdown.keys, breakdown.sites, filter, highlightNode, lastNodeClick, open, selectedNode])

  const toggle = (field: keyof TokenUsageFilter, value: string) => {
    if (field === 'keyIds' || field === 'siteIds') setLastNodeClick({ kind: field === 'keyIds' ? 'key' : 'site', id: value })
    setFilter((current) => toggleTokenUsageFilter(current, field, value))
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        container={container}
        overlayClassName="traffic-flow-tokens-overlay absolute inset-0 bg-black/28 backdrop-blur-[6px]"
        className="traffic-flow-tokens-dialog absolute left-auto top-auto w-auto max-w-none translate-x-0 translate-y-0 overflow-hidden rounded-[var(--flow-radius-lg)] border-white/20 bg-[#080808]/[0.92] p-0 shadow-none"
        style={panelStyle}
      >
        <DialogHeader className="traffic-flow-tokens-header">
          <div>
            <DialogTitle>{t('tokensDialog.title')}</DialogTitle>
            <DialogDescription>{t('tokensDialog.description')}</DialogDescription>
          </div>
          <dl className="traffic-flow-tokens-split">
            <div>
              <dt>{t('tokensDialog.input')}</dt>
              <dd>{formatCompactTokens(breakdown.input_tokens)}</dd>
            </div>
            <div>
              <dt>{t('tokensDialog.output')}</dt>
              <dd>{formatCompactTokens(breakdown.output_tokens)}</dd>
            </div>
            <div>
              <dt>{t('tokensDialog.cached')}</dt>
              <dd>{formatCompactTokens(breakdown.cached_tokens)}</dd>
            </div>
          </dl>
        </DialogHeader>
        <div className="traffic-flow-tokens-body">
          <div className="traffic-flow-tokens-toolbar">
            <div className="traffic-flow-tokens-vendors" aria-label={t('tokensDialog.vendors')}>
              <button type="button" className={filter.vendors.length === 0 ? 'is-active' : undefined} onClick={() => setFilter((current) => ({ ...current, vendors: [] }))}>
                {t('tokensDialog.allVendors')}
              </button>
              {breakdown.vendors.map((vendor) => {
                const iconPath = vendorIconPath(vendor)
                return (
                  <button
                    key={vendor.id || 'other'}
                    type="button"
                    className={filter.vendors.includes(vendor.id) ? 'is-active' : undefined}
                    aria-pressed={filter.vendors.includes(vendor.id)}
                    onClick={() => toggle('vendors', vendor.id)}
                  >
                    {iconPath ? <img src={iconPath} alt="" /> : <b>{vendorGlyph(vendor, t)}</b>}
                    {vendorName(vendor, t)}
                  </button>
                )
              })}
            </div>
            {active ? (
              <button type="button" className="traffic-flow-tokens-clear" onClick={() => { setFilter(emptyTokenUsageFilter()); setLastNodeClick(null) }}>
                {t('tokensDialog.clear')}
              </button>
            ) : null}
          </div>
          <div className="traffic-flow-tokens-columns">
            <TokenRankColumn
              title={t('tokensDialog.keys')}
              items={breakdown.keys}
              selectedIds={filter.keyIds}
              empty={emptyLabel(cells.length, t)}
              captionOf={(item) => t('tokensDialog.keyCaption', { sites: item.siteCount, models: item.modelCount })}
              nameOf={(item) => item.name || item.id}
              t={t}
              onSelect={(id) => toggle('keyIds', id)}
            />
            <TokenRankColumn
              title={t('tokensDialog.sites')}
              items={breakdown.sites}
              selectedIds={filter.siteIds}
              empty={emptyLabel(cells.length, t)}
              captionOf={(item) => t('tokensDialog.siteCaption', { keys: item.keyCount, models: item.modelCount })}
              nameOf={(item) => item.id ? item.name || item.id : t('tokensDialog.unknownSite')}
              t={t}
              onSelect={(id) => toggle('siteIds', id)}
            />
            <TokenRankColumn
              title={t('tokensDialog.models')}
              items={breakdown.models}
              selectedIds={filter.modelKeys}
              empty={emptyLabel(cells.length, t)}
              captionOf={(item) => t('tokensDialog.modelCaption', { vendor: vendorName(item, t), keys: item.keyCount, sites: item.siteCount })}
              nameOf={(item) => item.id ? item.name : t('inspector.unknownModel')}
              t={t}
              onSelect={(id) => toggle('modelKeys', id)}
            />
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}

function TokenRankColumn({
  title,
  items,
  selectedIds,
  empty,
  captionOf,
  nameOf,
  t,
  onSelect,
}: {
  title: string
  items: TokenUsageRank[]
  selectedIds: string[]
  empty: string
  captionOf: (item: TokenUsageRank) => string
  nameOf: (item: TokenUsageRank) => string
  t: Translate
  onSelect: (id: string) => void
}) {
  return (
    <section className="traffic-flow-tokens-column">
      <h3>{title}</h3>
      {items.length === 0 ? <p>{empty}</p> : (
        <ul>
          {items.map((item) => {
            const selected = selectedIds.includes(item.id)
            return (
              <li key={item.id || 'empty'}>
                <button type="button" className={selected ? 'traffic-flow-tokens-row is-active' : 'traffic-flow-tokens-row'} aria-pressed={selected} onClick={() => onSelect(item.id)}>
                  <span className="traffic-flow-tokens-row-head">
                    <b>{nameOf(item)}</b>
                    <strong>{formatCompactTokens(item.total_tokens)}</strong>
                  </span>
                  <span className="traffic-flow-tokens-row-meta">
                    <em>{captionOf(item)}</em>
                    <small title={`${t('tokensDialog.input')} / ${t('tokensDialog.output')} / ${t('tokensDialog.cached')}`}>
                      {formatCompactTokens(item.input_tokens)}
                      <i>/</i>
                      {formatCompactTokens(item.output_tokens)}
                      <i>/</i>
                      {formatCompactTokens(item.cached_tokens)}
                    </small>
                  </span>
                  <span className="traffic-flow-tokens-row-track" aria-hidden="true">
                    <span className="traffic-flow-tokens-row-bar" style={{ width: `${Math.min(100, item.share * 100)}%`, background: item.color }} />
                  </span>
                </button>
              </li>
            )
          })}
        </ul>
      )}
    </section>
  )
}

function measurePanelStyle(container: HTMLElement | null | undefined, kpis: HTMLElement | null, tokensButton: HTMLButtonElement | null): CSSProperties {
  if (!kpis) return {}
  const origin = container?.getBoundingClientRect()
  const kpiRect = kpis.getBoundingClientRect()
  const tokenRect = tokensButton?.getBoundingClientRect()
  const topOffset = origin?.top ?? 0
  const leftOffset = origin?.left ?? 0
  const top = kpiRect.bottom - topOffset + 8
  const left = kpiRect.left - leftOffset
  const width = kpiRect.width
  const originX = tokenRect && width > 0 ? ((tokenRect.left + tokenRect.width / 2 - kpiRect.left) / width) * 100 : 100
  const maxHeight = Math.max(280, (origin?.bottom ?? window.innerHeight) - (kpiRect.bottom + 20))
  return {
    top,
    left,
    width,
    maxHeight,
    transformOrigin: `${originX}% 0%`,
  }
}

function highlightTarget(filter: TokenUsageFilter, lastNodeClick: { kind: 'key' | 'site'; id: string } | null): TrafficFlowNodeRef | null {
  if (lastNodeClick?.kind === 'key' && filter.keyIds.includes(lastNodeClick.id)) return { kind: 'downstream', id: lastNodeClick.id }
  if (lastNodeClick?.kind === 'site' && lastNodeClick.id && filter.siteIds.includes(lastNodeClick.id)) return { kind: 'upstream', id: lastNodeClick.id }
  if (filter.keyIds.length === 1) return { kind: 'downstream', id: filter.keyIds[0] }
  if (filter.siteIds.length === 1 && filter.siteIds[0]) return { kind: 'upstream', id: filter.siteIds[0] }
  return null
}

function vendorName(vendor: Pick<TokenUsageRank, 'id' | 'name' | 'vendor'>, t: Translate) {
  const id = vendor.vendor || vendor.id
  return id === OTHER_BRAND_LABEL || !id ? t('tokensDialog.otherVendors') : vendor.vendor || vendor.name
}

function vendorIconPath(vendor: Pick<TokenUsageRank, 'id' | 'vendor'>) {
  const name = (vendor.vendor || vendor.id).trim().toLowerCase()
  if (!name || name === OTHER_BRAND_LABEL.toLowerCase()) return undefined
  return providerCatalog.find((entry) => entry.name.toLowerCase() === name)?.iconPath
}

function vendorGlyph(vendor: Pick<TokenUsageRank, 'id' | 'name' | 'vendor'>, t: Translate) {
  return vendorName(vendor, t).trim().slice(0, 1).toUpperCase()
}

function emptyLabel(cellCount: number, t: Translate) {
  return cellCount === 0 ? t('tokensDialog.empty') : t('tokensDialog.emptyFilter')
}
