import type { ReactNode } from 'react'
import { cn } from '@/lib/utils'

// Phone layouts for the developer page. The tables there have five or more
// columns, which on a narrow screen end up with one character per line, so on
// mobile each row becomes a card instead (the same approach as the OAuth and
// API key pages).

const CARD_CLASS =
  'rounded-lg border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-elevated))] p-3 shadow-[var(--button-secondary-shadow)]'

export function MobileCardList({ children, className }: { children: ReactNode; className?: string }) {
  return <div className={cn('space-y-3', className)}>{children}</div>
}

// Inside dialogs, rows are separated by thin lines rather than wrapped in cards.
export function MobileRowList({ children, className }: { children: ReactNode; className?: string }) {
  return <div className={cn('divide-y divide-[hsl(var(--glass-divider))]', className)}>{children}</div>
}

function CardActions({ children }: { children: ReactNode }) {
  return (
    <div className="mt-3 flex flex-wrap gap-2 border-t border-[hsl(var(--glass-divider))] pt-3 [&>button]:min-w-[7rem] [&>button]:flex-1">
      {children}
    </div>
  )
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="min-w-0">
      <div className="text-[11px] text-muted-soft">{label}</div>
      <div className="mt-0.5 min-w-0 break-words text-sm text-foreground">{children}</div>
    </div>
  )
}

type MobileUploadedCardProps = {
  name: string
  id: string
  kind: string
  /** Badge or dash for the enabled version. */
  enabledVersion: ReactNode
  metrics: string
  labels: { kind: string; enabledVersion: string; metrics: string }
  actions: ReactNode
}

export function MobileUploadedCard({ name, id, kind, enabledVersion, metrics, labels, actions }: MobileUploadedCardProps) {
  return (
    <article className={CARD_CLASS}>
      <h3 className="break-words text-base font-semibold text-foreground">{name}</h3>
      <div className="mt-0.5 break-all font-mono text-xs text-muted-soft">{id}</div>
      <div className="mt-3 grid grid-cols-2 gap-3">
        <Field label={labels.kind}>{kind}</Field>
        <Field label={labels.enabledVersion}>{enabledVersion}</Field>
        <div className="col-span-2">
          <Field label={labels.metrics}>
            <span className="tabular-nums text-muted-soft">{metrics}</span>
          </Field>
        </div>
      </div>
      <CardActions>{actions}</CardActions>
    </article>
  )
}

type MobileVersionCardProps = {
  version: string
  name?: string
  kind?: string
  status: ReactNode
  trust: ReactNode
  sha: string
  selftest: string
  /** Result of the last trial run, if there was one. */
  trial?: ReactNode
  labels: { package: string; selftest: string; trial: string }
  actions: ReactNode
}

export function MobileVersionCard({ version, name, kind, status, trust, sha, selftest, trial, labels, actions }: MobileVersionCardProps) {
  return (
    <article className="py-3 first:pt-0 last:pb-0">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="break-all font-mono text-base font-semibold text-foreground">{version}</div>
          {name || kind ? (
            <div className="mt-0.5 break-words text-xs text-muted-soft">{[name, kind].filter(Boolean).join(' · ')}</div>
          ) : null}
        </div>
        <div className="shrink-0">{status}</div>
      </div>
      <div className="mt-3 grid grid-cols-1 gap-3">
        <Field label={labels.package}>
          <div className="flex flex-wrap items-center gap-2">
            {trust}
            <span className="break-all font-mono text-xs text-muted-soft">{sha}</span>
          </div>
        </Field>
        <Field label={labels.selftest}>
          <span className="text-muted-soft">{selftest}</span>
        </Field>
        {trial ? <Field label={labels.trial}>{trial}</Field> : null}
      </div>
      <div className="mt-3 flex flex-wrap gap-2 [&>button]:min-w-[6rem] [&>button]:flex-1">{actions}</div>
    </article>
  )
}
