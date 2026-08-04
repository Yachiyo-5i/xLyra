import * as React from 'react'
import { ChevronLeft, ChevronRight, ChevronsLeft, ChevronsRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { cn } from '@/lib/utils'

type PaginationControlsProps = {
  page: number
  pageSize: number
  totalItems: number
  onPageChange: (page: number) => void
  onPageSizeChange?: (pageSize: number) => void
  pageSizeOptions?: number[]
  disabled?: boolean
  showPageSize?: boolean
  showDetails?: boolean
  className?: string
}

function clampPage(page: number, totalPages: number) {
  return Math.min(Math.max(page, 1), totalPages)
}

function PaginationIconButton({
  children,
  className,
  ...props
}: React.ComponentProps<typeof Button>) {
  return (
    <Button
      variant="ghost"
      size="icon"
      className={cn(
        'size-9 rounded-lg border border-[hsl(var(--glass-border-strong))] bg-[hsl(var(--surface-panel))] text-foreground shadow-[0_4px_14px_rgba(4,8,18,0.08)] hover:bg-[hsl(var(--surface-soft-hover))] disabled:border-[hsl(var(--glass-border))] disabled:bg-[hsl(var(--surface-subtle))] disabled:text-[hsl(var(--text-faint))]',
        className,
      )}
      {...props}
    >
      {children}
    </Button>
  )
}

export function PaginationControls({
  page,
  pageSize,
  totalItems,
  onPageChange,
  onPageSizeChange,
  pageSizeOptions = [10, 20, 50],
  disabled = false,
  showPageSize = true,
  showDetails = true,
  className,
}: PaginationControlsProps) {
  const { t } = useTranslation('components')
  const totalPages = Math.max(Math.ceil(totalItems / pageSize), 1)
  const currentPage = clampPage(page, totalPages)
  const [draftPage, setDraftPage] = React.useState(String(currentPage))
  const [isEditing, setIsEditing] = React.useState(false)
  const canPrevious = currentPage > 1
  const canNext = currentPage < totalPages

  const displayPage = isEditing ? draftPage : String(currentPage)

  function goToPage(nextPage: number) {
    const safePage = clampPage(nextPage, totalPages)
    setDraftPage(String(safePage))
    setIsEditing(false)
    onPageChange(safePage)
  }

  function commitDraftPage() {
    const parsedPage = Number.parseInt(draftPage, 10)

    if (!Number.isFinite(parsedPage)) {
      setDraftPage(String(currentPage))
      setIsEditing(false)
      return
    }

    goToPage(parsedPage)
  }

  return (
    <div className={cn('flex flex-wrap items-center gap-3', className)}>
      {showPageSize && onPageSizeChange ? (
        <Select
          value={String(pageSize)}
          disabled={disabled}
          onValueChange={(nextValue) => {
            onPageSizeChange(Number(nextValue))
            onPageChange(1)
            setDraftPage('1')
            setIsEditing(false)
          }}
        >
          <SelectTrigger className="h-10 w-[132px] rounded-xl">
            <SelectValue placeholder={t('pagination.perPagePlaceholder')} />
          </SelectTrigger>
          <SelectContent searchable={false}>
            {pageSizeOptions.map((option) => (
              <SelectItem key={option} value={String(option)} className="whitespace-nowrap">
                {option} / {t('pagination.perPage')}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      ) : null}

      <div className="flex flex-wrap items-center gap-3 px-1 py-1">
        <div className="flex items-center gap-2 text-sm font-medium text-foreground">
          <span>{t('pagination.pagePrefix')}</span>
          <input
            inputMode="numeric"
            pattern="[0-9]*"
            disabled={disabled}
            value={displayPage}
            onFocus={() => {
              setDraftPage(String(currentPage))
              setIsEditing(true)
            }}
            onChange={(event) => {
              setDraftPage(event.target.value.replace(/\D+/g, ''))
              setIsEditing(true)
            }}
            onBlur={commitDraftPage}
            onKeyDown={(event) => {
              if (event.key === 'Enter') {
                event.preventDefault()
                commitDraftPage()
              }

              if (event.key === 'Escape') {
                setDraftPage(String(currentPage))
                setIsEditing(false)
              }
            }}
            className="h-9 w-12 rounded-lg border border-[hsl(var(--glass-border-strong))] bg-[hsl(var(--surface-panel))] px-2 text-center text-base font-semibold text-foreground shadow-[0_4px_14px_rgba(4,8,18,0.08)] outline-none transition-[border-color,box-shadow] focus:border-[hsl(var(--field-border-focus))] focus:ring-2 focus:ring-[hsl(var(--ring-soft))] disabled:cursor-not-allowed disabled:opacity-50 md:text-sm"
            aria-label={t('pagination.label')}
          />
          <span>{t('pagination.pageSuffix')}</span>
          <span className="text-muted-soft font-medium">
            {t('pagination.totalPages', { count: totalPages })}
            {showDetails ? t('pagination.totalItems', { count: totalItems }) : ''}
          </span>
        </div>

        <div className="flex items-center gap-2">
          <PaginationIconButton
            aria-label={t('pagination.first')}
            disabled={disabled || !canPrevious}
            onClick={() => goToPage(1)}
          >
            <ChevronsLeft className="h-4 w-4" />
          </PaginationIconButton>
          <PaginationIconButton
            aria-label={t('pagination.previous')}
            disabled={disabled || !canPrevious}
            onClick={() => goToPage(currentPage - 1)}
          >
            <ChevronLeft className="h-4 w-4" />
          </PaginationIconButton>
          <PaginationIconButton
            aria-label={t('pagination.next')}
            disabled={disabled || !canNext}
            onClick={() => goToPage(currentPage + 1)}
          >
            <ChevronRight className="h-4 w-4" />
          </PaginationIconButton>
          <PaginationIconButton
            aria-label={t('pagination.last')}
            disabled={disabled || !canNext}
            onClick={() => goToPage(totalPages)}
          >
            <ChevronsRight className="h-4 w-4" />
          </PaginationIconButton>
        </div>
      </div>
    </div>
  )
}
