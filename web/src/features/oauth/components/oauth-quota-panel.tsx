import { useCallback, useState, type CSSProperties } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Ticket } from 'lucide-react'
import { Skeleton } from '@/components/ui/skeleton'
import { Progress, type ProgressThreshold } from '@/components/ui/progress'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Draw, DrawBody, DrawContent, DrawHeader, DrawTitle } from '@/components/ui/draw'
import { useTranslation } from 'react-i18next'
import { useMobileLayout } from '@/hooks/use-media-query'
import { toast } from '@/lib/toast'
import {
  consumeOAuthConnectionResetCredit,
  listOAuthConnectionResetCredits,
  oauthQueryKeys,
  type OAuthConnectionDetail,
  type OAuthResetCredit,
  type OAuthResetCreditsList,
} from '@/features/oauth/api/oauth'
import {
  clampPercent,
  formatPercent,
  formatQuotaResetTime,
  getPrimaryAntigravityQuotaModelKey,
  isAntigravityProvider,
  parseQuotaResetAt,
  PRIMARY_ANTIGRAVITY_QUOTA_MODEL_KEYS,
} from '@/features/oauth/lib/oauth-utils'
import { cn } from '@/lib/utils'
import type { OAuthQuotaWindowLike } from '@/features/oauth/lib/types'

const QUOTA_PROGRESS_THRESHOLDS: ProgressThreshold[] = [
  { max: 20, variant: 'danger' },
  { max: 50, variant: 'warning' },
  { max: 100, variant: 'success' },
]

const ALL_MODELS_TOOLTIP_WIDTH = 420
const ALL_MODELS_TOOLTIP_OFFSET = 14
const ALL_MODELS_TOOLTIP_MARGIN = 12

export function QuotaPanel({
  connectionId,
  provider,
  quota,
  loading,
  resetCreditsData,
}: {
  connectionId?: string
  provider: string
  quota?: { type?: string | null; five_hour?: OAuthQuotaWindowLike; weekly?: OAuthQuotaWindowLike; models?: OAuthQuotaWindowLike[]; reset_credits?: { available_count?: number | null } }
  loading: boolean
  resetCreditsData?: OAuthResetCreditsList | null
}) {
  const { t, i18n } = useTranslation('oauth')
  const queryClient = useQueryClient()
  const [mousePos, setMousePos] = useState<{ x: number; y: number } | null>(null)
  const [mobileModelsOpen, setMobileModelsOpen] = useState(false)
  const [resetCreditsOpen, setResetCreditsOpen] = useState(false)
  const [confirmResetOpen, setConfirmResetOpen] = useState(false)
  const [desktopTooltipHeight, setDesktopTooltipHeight] = useState<number | null>(null)
  const isMobile = useMobileLayout()
  const isAntigravity = isAntigravityProvider(provider)
  const isCodex = provider === 'codex'
  const isClaudeCode = provider === 'claude_code'
  const modelQuotas = Array.isArray(quota?.models) ? quota.models : []
  const summaryResetCount = resetCreditAvailableCount(quota)
  const visibleModelQuotas = getVisibleAntigravityQuotaModels(modelQuotas)
  const claudeCodeFiveHour = isClaudeCode
    ? quota?.five_hour ?? modelQuotas.find((item) => isClaudeCodeFiveHourWindowName(item.name))
    : quota?.five_hour
  const hoverQuotaEntries: QuotaEntry[] = isAntigravity
    ? modelQuotas.map((item) => ({ label: quotaModelKeyLabel(item, t), window: item }))
    : isClaudeCode
      ? getClaudeCodeQuotaEntries(claudeCodeFiveHour, quota?.weekly, modelQuotas, t)
      : []
  const hoverTooltipTitle = isClaudeCode ? t('quota.allQuotas') : t('quota.allModels')
  const canOpenMobileModels = isMobile && hoverQuotaEntries.length > 0
  const desktopTooltipLayout = mousePos ? getAllModelsTooltipLayout(mousePos, hoverQuotaEntries.length, desktopTooltipHeight) : null
  const resetCreditsQuery = useQuery({
    queryKey: oauthQueryKeys.resetCredits(connectionId ?? ''),
    queryFn: () => listOAuthConnectionResetCredits(connectionId ?? ''),
    enabled: isCodex && resetCreditsOpen && Boolean(connectionId),
    staleTime: 30_000,
  })
  const effectiveResetCredits = resetCreditsQuery.data ?? resetCreditsData
  const resetCredits = effectiveResetCredits?.credits ?? []
  const availableResetCredits = sortResetCreditsByExpiry(
    resetCredits.filter((credit) => isAvailableResetCredit(credit)),
  )
  const resetCreditCount = effectiveResetCredits?.available_count ?? summaryResetCount
  const [selectedCreditId, setSelectedCreditId] = useState<string | null>(null)
  const effectiveSelectedCreditId = selectedCreditId && availableResetCredits.some((credit) => credit.id === selectedCreditId)
    ? selectedCreditId
    : availableResetCredits[0]?.id
  const consumeResetMutation = useMutation({
    mutationFn: ({ idempotencyKey, creditId }: { idempotencyKey: string; creditId?: string }) => {
      if (!connectionId) throw new Error(t('quota.resetCredits.missingConnection'))
      return consumeOAuthConnectionResetCredit(connectionId, idempotencyKey, creditId)
    },
  })

  const measureDesktopTooltip = useCallback((node: HTMLDivElement | null) => {
    const nextHeight = node?.offsetHeight ?? null
    setDesktopTooltipHeight((current) => current === nextHeight ? current : nextHeight)
  }, [])

  async function handleConfirmReset() {
    try {
      const result = await consumeResetMutation.mutateAsync({
        idempotencyKey: newIdempotencyKey(),
        creditId: effectiveSelectedCreditId,
      })
      if (connectionId) {
        queryClient.setQueryData<{ connection: OAuthConnectionDetail }>(
          oauthQueryKeys.detail(connectionId),
          { connection: result.connection },
        )
        await Promise.all([
          queryClient.invalidateQueries({ queryKey: oauthQueryKeys.connections() }),
          queryClient.invalidateQueries({ queryKey: oauthQueryKeys.detail(connectionId) }),
          queryClient.invalidateQueries({ queryKey: oauthQueryKeys.resetCredits(connectionId) }),
        ])
      }
      if (result.result.outcome === 'reset' || result.result.outcome === 'alreadyRedeemed') {
        toast.success(t('quota.resetCredits.toast.success'))
        setConfirmResetOpen(false)
        setResetCreditsOpen(false)
        return
      }
      toast.error(resetOutcomeLabel(result.result.outcome, t))
      setConfirmResetOpen(false)
    } catch (error) {
      toast.error(t('quota.resetCredits.toast.failed'), {
        description: error instanceof Error ? error.message : undefined,
      })
    }
  }

  return (
    <div
      className={cn(
        'py-1',
        canOpenMobileModels ? 'cursor-pointer rounded-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[hsl(var(--ring-strong))]' : null,
      )}
      role={canOpenMobileModels ? 'button' : undefined}
      tabIndex={canOpenMobileModels ? 0 : undefined}
      aria-label={canOpenMobileModels ? hoverTooltipTitle : undefined}
      onClick={() => {
        if (canOpenMobileModels) setMobileModelsOpen(true)
      }}
      onKeyDown={(event) => {
        if (!canOpenMobileModels) return
        if (event.key === 'Enter' || event.key === ' ') {
          event.preventDefault()
          setMobileModelsOpen(true)
        }
      }}
      onMouseMove={(event) => {
        if (!isMobile && hoverQuotaEntries.length > 0) {
          setMousePos({ x: event.clientX, y: event.clientY })
        }
      }}
      onMouseEnter={(event) => {
        if (!isMobile && hoverQuotaEntries.length > 0) {
          setMousePos({ x: event.clientX, y: event.clientY })
        }
      }}
      onMouseLeave={() => {
        setMousePos(null)
        setDesktopTooltipHeight(null)
      }}
    >
      <div className="mb-2 flex items-center justify-between gap-3">
        <div className="text-sm font-medium text-foreground">{t('quota.title')}</div>
        {isCodex ? (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="h-7 shrink-0 gap-1.5 px-2 text-xs text-muted-soft hover:text-foreground"
            disabled={!connectionId || loading}
            onClick={(event) => {
              event.stopPropagation()
              setResetCreditsOpen(true)
            }}
          >
            <Ticket className="h-3.5 w-3.5" />
            {t('quota.resetCredits.count', { count: resetCreditCount })}
          </Button>
        ) : null}
      </div>
      {loading ? (
        <div className="space-y-3">
          <Skeleton className="h-9 w-full" />
          <Skeleton className="h-9 w-full" />
        </div>
      ) : isAntigravity && modelQuotas.length > 0 ? (
        <div className="space-y-3">
          {visibleModelQuotas.length ? visibleModelQuotas.map((item, index) => (
            <QuotaProgress key={`${item.name ?? item.display_name ?? 'model'}-${index}`} label={formatQuotaCardModelLabel(item, t)} window={item} t={t} language={i18n.language} />
          )) : (
            <QuotaProgress label={t('quota.modelQuota')} t={t} language={i18n.language} />
          )}
        </div>
      ) : (
        <div className="space-y-3">
          <QuotaProgress label={t('quota.fiveHour')} window={claudeCodeFiveHour} t={t} language={i18n.language} />
          <QuotaProgress label={t('quota.weekly')} window={quota?.weekly} t={t} language={i18n.language} />
        </div>
      )}

      {desktopTooltipLayout && !isMobile && hoverQuotaEntries.length > 0 ? (
        <div
          ref={measureDesktopTooltip}
          className="pointer-events-none fixed z-50 rounded-lg border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-panel))] p-3 shadow-lg"
          style={desktopTooltipLayout}
        >
          <div className="mb-2 text-xs font-medium text-muted-soft">{hoverTooltipTitle}</div>
          <div className="space-y-2.5">
            {hoverQuotaEntries.map((entry, index) => (
              <QuotaModelRow key={`${entry.label}-${index}`} item={entry.window} label={entry.label} t={t} language={i18n.language} />
            ))}
          </div>
        </div>
      ) : null}

      <Draw open={mobileModelsOpen} onOpenChange={setMobileModelsOpen}>
        <DrawContent side="right" size="wide">
          <DrawHeader>
            <DrawTitle>{hoverTooltipTitle}</DrawTitle>
          </DrawHeader>
          <DrawBody>
            <div className="space-y-3">
              {hoverQuotaEntries.map((entry, index) => (
                <QuotaModelRow key={`${entry.label}-${index}`} item={entry.window} label={entry.label} t={t} language={i18n.language} />
              ))}
            </div>
          </DrawBody>
        </DrawContent>
      </Draw>

      {isCodex ? (
        <Dialog open={resetCreditsOpen} onOpenChange={(open) => { if (!consumeResetMutation.isPending) setResetCreditsOpen(open) }}>
          <DialogContent className="w-[min(96vw,640px)] overflow-hidden">
            <DialogHeader className="border-b-0 pb-2">
              <DialogTitle>{t('quota.resetCredits.title')}</DialogTitle>
            </DialogHeader>
            <DialogBody className="pt-0">
              <DialogDescription className="mt-0">{t('quota.resetCredits.description')}</DialogDescription>
              <div className="mt-3">
                <ResetCreditTable
                  credits={availableResetCredits}
                  loading={loading || resetCreditsQuery.isLoading}
                  selectedId={effectiveSelectedCreditId}
                  onSelect={setSelectedCreditId}
                  t={t}
                  language={i18n.language}
                />
              </div>
              <p className="mt-3 text-xs text-muted-soft">{t('quota.resetCredits.priorityHint')}</p>
            </DialogBody>
            <DialogFooter className="border-t-0 pt-2">
              <Button
                variant="outline"
                disabled={consumeResetMutation.isPending}
                onClick={() => setResetCreditsOpen(false)}
              >
                {t('quota.resetCredits.close')}
              </Button>
              <Button
                disabled={consumeResetMutation.isPending || availableResetCredits.length === 0}
                onClick={() => setConfirmResetOpen(true)}
              >
                {t('quota.resetCredits.useOne')}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      ) : null}

      {isCodex ? (
        <Dialog open={confirmResetOpen} onOpenChange={(open) => { if (!open && !consumeResetMutation.isPending) setConfirmResetOpen(false) }}>
        <DialogContent className="w-[min(92vw,460px)] overflow-hidden">
          <DialogHeader className="border-b-0 pb-2">
            <DialogTitle>{t('quota.resetCredits.confirmTitle')}</DialogTitle>
          </DialogHeader>
          <DialogBody className="pt-0">
            <DialogDescription className="mt-0">
              {t('quota.resetCredits.confirmDescription')}
            </DialogDescription>
          </DialogBody>
          <DialogFooter className="border-t-0 pt-2">
            <Button variant="outline" disabled={consumeResetMutation.isPending} onClick={() => setConfirmResetOpen(false)}>
              {t('quota.resetCredits.cancel')}
            </Button>
            <Button disabled={consumeResetMutation.isPending} onClick={handleConfirmReset}>
              {t('quota.resetCredits.confirmUse')}
            </Button>
          </DialogFooter>
        </DialogContent>
        </Dialog>
      ) : null}
    </div>
  )
}

function getAllModelsTooltipLayout(mousePos: { x: number; y: number }, modelCount: number, measuredHeight: number | null): CSSProperties {
  if (typeof window === 'undefined') {
    return {
      left: mousePos.x + ALL_MODELS_TOOLTIP_OFFSET,
      top: mousePos.y + ALL_MODELS_TOOLTIP_OFFSET,
      width: ALL_MODELS_TOOLTIP_WIDTH,
    }
  }

  const viewportWidth = window.innerWidth
  const viewportHeight = window.innerHeight
  const width = Math.min(ALL_MODELS_TOOLTIP_WIDTH, Math.max(240, viewportWidth - ALL_MODELS_TOOLTIP_MARGIN * 2))
  const preferredLeft = mousePos.x + ALL_MODELS_TOOLTIP_OFFSET
  const preferredTop = mousePos.y + ALL_MODELS_TOOLTIP_OFFSET
  const tooltipHeight = measuredHeight ?? 44 + 36 * Math.max(1, modelCount)
  const left = Math.max(
    ALL_MODELS_TOOLTIP_MARGIN,
    Math.min(preferredLeft, viewportWidth - width - ALL_MODELS_TOOLTIP_MARGIN),
  )
  const top = preferredTop + tooltipHeight > viewportHeight - ALL_MODELS_TOOLTIP_MARGIN
    ? Math.max(ALL_MODELS_TOOLTIP_MARGIN, mousePos.y - tooltipHeight - ALL_MODELS_TOOLTIP_OFFSET)
    : preferredTop

  return {
    left,
    top,
    width,
  }
}

function getVisibleAntigravityQuotaModels(items: OAuthQuotaWindowLike[]) {
  const byKey = new Map<string, OAuthQuotaWindowLike>()

  for (const item of items) {
    const key = getPrimaryAntigravityQuotaModelKey(item)
    if (key && !byKey.has(key)) {
      byKey.set(key, item)
    }
  }

  return PRIMARY_ANTIGRAVITY_QUOTA_MODEL_KEYS
    .map((key) => byKey.get(key))
    .filter((item): item is OAuthQuotaWindowLike => Boolean(item))
}

function formatQuotaCardModelLabel(item: OAuthQuotaWindowLike, t: (key: string) => string) {
  const raw = quotaModelKeyLabel(item, t)
  return raw.replace(/\s*\((High|Thinking)\)\s*$/i, ' $1')
}

function quotaModelKeyLabel(item: OAuthQuotaWindowLike, t: (key: string) => string) {
  return item.name ?? item.display_name ?? t('quota.modelQuota')
}

type QuotaEntry = { label: string; window: OAuthQuotaWindowLike }

function isClaudeCodeFiveHourWindowName(name?: string | null) {
  const canonical = (name ?? '').trim().toLowerCase()
  return canonical === '5h' || canonical === 'session' || canonical === 'five_hour'
}

function isClaudeCodeWeeklyWindowName(name?: string | null) {
  const canonical = (name ?? '').trim().toLowerCase()
  return canonical === 'weekly' || canonical === '7d' || canonical === 'seven_day'
}

function getClaudeCodeQuotaEntries(
  fiveHour: OAuthQuotaWindowLike | undefined,
  weekly: OAuthQuotaWindowLike | undefined,
  models: OAuthQuotaWindowLike[],
  t: (key: string) => string,
): QuotaEntry[] {
  const entries: QuotaEntry[] = []
  if (fiveHour) entries.push({ label: t('quota.fiveHour'), window: fiveHour })
  if (weekly) entries.push({ label: t('quota.weekly'), window: weekly })
  for (const item of models) {
    // The session limit duplicates the dedicated five_hour window and the
    // account-wide weekly limit duplicates the dedicated weekly window above;
    // only model-scoped windows (e.g. Fable) add information here.
    if (isClaudeCodeFiveHourWindowName(item.name)) continue
    if (isClaudeCodeWeeklyWindowName(item.name) && !isClaudeCodeModelScopedWindow(item)) continue
    entries.push({ label: claudeCodeQuotaEntryLabel(item, t), window: item })
  }
  return entries
}

function isClaudeCodeModelScopedWindow(item: OAuthQuotaWindowLike) {
  const display = item.display_name?.trim() ?? ''
  return Boolean(display) && display.toLowerCase() !== (item.name ?? '').trim().toLowerCase()
}

function claudeCodeQuotaEntryLabel(item: OAuthQuotaWindowLike, t: (key: string) => string) {
  const windowLabel = isClaudeCodeWeeklyWindowName(item.name)
    ? t('quota.weekly')
    : isClaudeCodeFiveHourWindowName(item.name)
      ? t('quota.fiveHour')
      : null
  const display = item.display_name?.trim() ?? ''
  if (windowLabel && isClaudeCodeModelScopedWindow(item)) return `${display} · ${windowLabel}`
  if (windowLabel) return windowLabel
  return display || item.name || t('quota.modelQuota')
}

function QuotaModelRow({ item, label, t, language }: { item: OAuthQuotaWindowLike; label?: string; t: (key: string, vars?: Record<string, unknown>) => string; language?: string }) {
  const remaining = clampPercent(item.remaining_percent)
  const resetLabel = formatQuotaResetTime(parseQuotaResetAt(item.reset_at), language)

  return (
    <div>
      <div className="flex items-center justify-between gap-3 text-xs">
        <span className="text-muted-soft truncate">{label ?? quotaModelKeyLabel(item, t)}</span>
        <div className="flex items-center gap-2 shrink-0">
          <span className="text-foreground tabular-nums">{formatPercent(item.remaining_percent, language)}</span>
          {resetLabel !== '-' ? <span className="text-muted-soft tabular-nums">{resetLabel}</span> : null}
        </div>
      </div>
      <Progress value={remaining ?? 0} variant={remaining === null ? 'muted' : 'auto'} thresholds={QUOTA_PROGRESS_THRESHOLDS} className="h-1.5 mt-1 bg-[hsl(var(--surface-panel))]" />
    </div>
  )
}

function QuotaProgress({ label, window, t, language }: { label: string; window?: OAuthQuotaWindowLike; t: (key: string, vars?: Record<string, unknown>) => string; language?: string }) {
  if (!window) {
    return (
      <div className="space-y-1.5">
        <div className="flex items-center justify-between gap-3 text-xs">
          <span className="text-muted-soft">{label}</span>
          <span className="text-foreground">-</span>
        </div>
        <Progress value={0} variant="muted" className="h-2.5 bg-[hsl(var(--surface-panel))]" />
      </div>
    )
  }

  const remaining = clampPercent(window.remaining_percent)
  const resetLabel = formatQuotaResetTime(parseQuotaResetAt(window.reset_at), language)

  return (
    <div className="space-y-1.5">
      <div className="flex items-center justify-between gap-3 text-xs">
        <span className="text-muted-soft">{label}</span>
        <span className="text-foreground">
          {t('quota.remaining', { percent: formatPercent(window.remaining_percent, language) })}
          {resetLabel !== '-' ? <span className="text-muted-soft ml-2">{resetLabel}</span> : null}
        </span>
      </div>
      <Progress
        value={remaining ?? 0}
        variant={remaining === null ? 'muted' : 'auto'}
        thresholds={QUOTA_PROGRESS_THRESHOLDS}
        className="h-2.5 bg-[hsl(var(--surface-panel))]"
      />
    </div>
  )
}

function ResetCreditTable({
  credits,
  loading,
  selectedId,
  onSelect,
  t,
  language,
}: {
  credits: OAuthResetCredit[]
  loading: boolean
  selectedId?: string
  onSelect: (creditId: string) => void
  t: (key: string, vars?: Record<string, unknown>) => string
  language?: string
}) {
  if (loading) {
    return (
      <div className="space-y-2">
        <Skeleton className="h-9 w-full" />
        <Skeleton className="h-9 w-full" />
      </div>
    )
  }
  if (!credits.length) {
    return (
      <div className="rounded-md border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-subtle)/0.5)] px-3 py-4 text-sm text-muted-soft">
        {t('quota.resetCredits.empty')}
      </div>
    )
  }
  return (
    <table className="w-full table-fixed border-collapse text-left text-sm">
      <thead>
        <tr className="text-faint text-xs uppercase tracking-[0.16em]">
          <th className="w-[8%] px-3 py-3 font-medium md:px-4">#</th>
          <th className="w-[46%] px-3 py-3 font-medium md:px-4">{t('quota.resetCredits.tableHeaders.granted')}</th>
          <th className="w-[46%] px-3 py-3 font-medium md:px-4">{t('quota.resetCredits.tableHeaders.expires')}</th>
        </tr>
      </thead>
      <tbody>
        {credits.map((credit, index) => {
          const selected = credit.id === selectedId
          return (
            <tr
              key={credit.id}
              role="radio"
              aria-checked={selected}
              tabIndex={0}
              className={cn(
                'cursor-pointer border-t border-[hsl(var(--glass-divider))] text-foreground transition-colors',
                selected ? 'bg-[hsl(var(--surface-subtle))]' : 'hover:bg-[hsl(var(--surface-subtle)/0.5)]',
              )}
              onClick={() => onSelect(credit.id)}
              onKeyDown={(event) => {
                if (event.key === 'Enter' || event.key === ' ') {
                  event.preventDefault()
                  onSelect(credit.id)
                }
              }}
            >
              <td className="px-3 py-3 align-middle md:px-4">
                <span className="flex items-center gap-2">
                  <span
                    className={cn(
                      'inline-block size-3 shrink-0 rounded-full border transition-colors',
                      selected
                        ? 'border-4 border-[hsl(var(--primary))]'
                        : 'border-[hsl(var(--glass-divider))]',
                    )}
                  />
                  <span className="tabular-nums text-muted-soft">{index + 1}</span>
                </span>
              </td>
              <td className="px-3 py-3 align-middle md:px-4">{formatResetCreditDate(credit.granted_at, language) || '-'}</td>
              <td className="px-3 py-3 align-middle md:px-4">{formatResetCreditDate(credit.expires_at, language) || '-'}</td>
            </tr>
          )
        })}
      </tbody>
    </table>
  )
}

function sortResetCreditsByExpiry(credits: OAuthResetCredit[]) {
  return [...credits].sort((a, b) => {
    const aTs = a.expires_at ? Date.parse(a.expires_at) : Number.POSITIVE_INFINITY
    const bTs = b.expires_at ? Date.parse(b.expires_at) : Number.POSITIVE_INFINITY
    return aTs - bTs
  })
}

function isAvailableResetCredit(credit: OAuthResetCredit) {
  const status = (credit.status ?? '').toLowerCase()
  if (status && status !== 'available') return false
  if (credit.redeemed_at || credit.redeem_started_at) return false
  return true
}

function formatResetCreditDate(value: string | null | undefined, language?: string) {
  if (!value) return ''
  const ts = Date.parse(value)
  if (!Number.isFinite(ts)) return ''
  try {
    return new Intl.DateTimeFormat(language || undefined, {
      year: 'numeric',
      month: 'short',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
    }).format(ts)
  } catch {
    return new Date(ts).toISOString().slice(0, 16).replace('T', ' ')
  }
}

function resetCreditAvailableCount(quota: { reset_credits?: { available_count?: number | null } } | undefined) {
  const count = quota?.reset_credits?.available_count
  const numericCount = typeof count === 'number' ? count : Number(count)
  return Number.isFinite(numericCount) && numericCount > 0 ? Math.floor(numericCount) : 0
}

function newIdempotencyKey() {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID()
  }
  return `reset-${Date.now()}-${Math.random().toString(36).slice(2)}`
}

function resetOutcomeLabel(outcome: string, t: (key: string) => string) {
  switch (outcome) {
    case 'nothingToReset':
      return t('quota.resetCredits.toast.nothingToReset')
    case 'noCredit':
      return t('quota.resetCredits.toast.noCredit')
    default:
      return t('quota.resetCredits.toast.failed')
  }
}
