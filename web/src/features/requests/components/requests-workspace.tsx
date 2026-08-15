import { useMemo, useRef, useState } from 'react'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { ErrorState } from '@/components/common/error-state'
import { PageHeader } from '@/components/common/page-header'
import type { TokenUsageBreakdown } from '@/components/common/token-usage-hover-card'
import { PaginationControls } from '@/components/ui/pagination'
import { listDownstreamAPIKeys, downstreamAPIKeyQueryKeys } from '@/features/api-keys/api/api-keys'
import { sortAPIKeysForDisplay } from '@/features/api-keys/lib/api-key-utils'
import { getRequestLogSummary, listRequestLogs, requestQueryKeys, type RequestLogItem } from '@/features/requests/api/requests'
import { RequestsFilterBar } from '@/features/requests/components/requests-filter-bar'
import { RequestsMobileFilterBar } from '@/features/requests/components/requests-mobile-filter-bar'
import { RequestsMobileList } from '@/features/requests/components/requests-mobile-list'
import { RequestsTable } from '@/features/requests/components/requests-table'
import { RequestsWorkspaceSkeleton } from '@/features/requests/components/requests-workspace-skeleton'
import {
  REQUESTS_AUTO_REFRESH_INTERVAL_MS,
  REQUESTS_PAGE_SIZE_OPTIONS,
  requestFiltersFromSearchParams,
  requestFiltersToListFilters,
  type RequestFilterState,
} from '@/features/requests/lib/types'
import { readRequestsAutoRefreshPreference, writeRequestsAutoRefreshPreference } from '@/features/requests/lib/request-preferences'
import { listSites, sitesQueryKeys } from '@/features/sites/api/sites'
import { sortSitesForDisplay } from '@/features/sites/lib/site-utils'
import { useMobileLayout } from '@/hooks/use-media-query'

const EMPTY_REQUESTS: RequestLogItem[] = []
const EMPTY_SITES: Awaited<ReturnType<typeof listSites>>['items'] = []
const EMPTY_API_KEYS: Awaited<ReturnType<typeof listDownstreamAPIKeys>>['items'] = []

export function RequestsWorkspace({ initialSearch = '' }: { initialSearch?: string }) {
  const { t } = useTranslation('requests')
  const isMobile = useMobileLayout()
  const initialSearchParams = useMemo(() => new URLSearchParams(initialSearch), [initialSearch])
  const [filters, setFilters] = useState<RequestFilterState>(() => requestFiltersFromSearchParams(initialSearchParams))
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [autoRefresh, setAutoRefresh] = useState(readRequestsAutoRefreshPreference)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(50)
  const tableScrollRef = useRef<HTMLDivElement | null>(null)

  const requestListFilters = useMemo(() => requestFiltersToListFilters(filters), [filters])
  const requestListInput = useMemo(
    () => ({ ...requestListFilters, page, pageSize }),
    [page, pageSize, requestListFilters],
  )
  const costUnsupportedByFilters = Boolean(requestListFilters.search || requestListFilters.requestId)
  const requestListQueryKey = useMemo(
    () => requestQueryKeys.list(requestListInput),
    [requestListInput],
  )

  const requestsQuery = useQuery({
    queryKey: requestListQueryKey,
    queryFn: () => listRequestLogs(requestListInput),
    placeholderData: keepPreviousData,
    refetchInterval: autoRefresh ? REQUESTS_AUTO_REFRESH_INTERVAL_MS : false,
  })
  const requestSummaryQuery = useQuery({
    queryKey: requestQueryKeys.summary(requestListFilters),
    queryFn: () => getRequestLogSummary(requestListFilters),
    enabled: !costUnsupportedByFilters,
    placeholderData: keepPreviousData,
    refetchInterval: autoRefresh && !costUnsupportedByFilters ? REQUESTS_AUTO_REFRESH_INTERVAL_MS : false,
  })
  const sitesQuery = useQuery({
    queryKey: sitesQueryKeys.list('all'),
    queryFn: () => listSites({ oauth: 'all' }),
  })
  const apiKeysQuery = useQuery({
    queryKey: downstreamAPIKeyQueryKeys.list(),
    queryFn: listDownstreamAPIKeys,
  })

  const sites = useMemo(
    () => sortSitesForDisplay(sitesQuery.data?.items ?? EMPTY_SITES),
    [sitesQuery.data?.items],
  )
  const apiKeys = useMemo(
    () => sortAPIKeysForDisplay(apiKeysQuery.data?.items ?? EMPTY_API_KEYS),
    [apiKeysQuery.data?.items],
  )
  const items = requestsQuery.data?.items ?? EMPTY_REQUESTS
  const totalItems = requestsQuery.data?.meta?.total ?? items.length
  const currentPage = page
  const rateLimitUsage = requestsQuery.data?.meta?.rate_usage ?? undefined
  const requestSummary = requestSummaryQuery.data?.summary
  const totalCostSupported = !costUnsupportedByFilters && !requestSummaryQuery.isError && requestSummary?.supported !== false
  const totalCost = totalCostSupported ? requestSummary?.total_cost : null
  const tokenUsage: TokenUsageBreakdown | null = totalCostSupported
    ? {
        total: requestSummary?.total_tokens ?? 0,
        input: requestSummary?.prompt_tokens ?? 0,
        output: requestSummary?.completion_tokens ?? 0,
        cached: requestSummary?.cached_tokens ?? 0,
      }
    : null
  const totalCostLoading = !costUnsupportedByFilters && requestSummaryQuery.isFetching && !requestSummaryQuery.data
  const showPagination = totalItems > 0

  function scrollTableTop() {
    window.requestAnimationFrame(() => {
      let node: HTMLElement | null = tableScrollRef.current
      while (node) {
        if (node.scrollTop > 0) {
          node.scrollTo({ top: 0, behavior: 'smooth' })
          return
        }
        node = node.parentElement
      }
    })
  }

  function handleFiltersChange(nextFilters: RequestFilterState) {
    setExpandedId(null)
    setPage(1)
    setFilters(nextFilters)
  }

  function handlePageChange(nextPage: number) {
    setExpandedId(null)
    setPage(nextPage)
    scrollTableTop()
  }

  function handlePageSizeChange(nextPageSize: number) {
    setExpandedId(null)
    setPage(1)
    setPageSize(nextPageSize)
    scrollTableTop()
  }

  function refreshRequests() {
    void requestsQuery.refetch()
    if (!costUnsupportedByFilters) {
      void requestSummaryQuery.refetch()
    }
  }

  function handleAutoRefreshChange(enabled: boolean) {
    setAutoRefresh(enabled)
    writeRequestsAutoRefreshPreference(enabled)
  }

  if (requestsQuery.isLoading) {
    return <RequestsWorkspaceSkeleton />
  }

  if (requestsQuery.isError) {
    return (
      <ErrorState
        title={t('page.loadFailed')}
        description={requestsQuery.error.message}
      />
    )
  }

  const pagination = (
    <PaginationControls
      page={currentPage}
      pageSize={pageSize}
      totalItems={totalItems}
      onPageChange={handlePageChange}
      onPageSizeChange={handlePageSizeChange}
      pageSizeOptions={REQUESTS_PAGE_SIZE_OPTIONS}
      disabled={requestsQuery.isFetching}
      showPageSize={!isMobile}
      showDetails={!isMobile}
      className={isMobile ? 'mt-3 justify-end border-t border-[hsl(var(--glass-divider))] pb-2 pt-3' : 'mt-auto flex-none justify-end border-t border-[hsl(var(--glass-divider))] pt-3'}
    />
  )

  if (isMobile) {
    return (
      <div className="relative space-y-4 pb-2">
        <div>
          <PageHeader
            eyebrow={t('page.eyebrow')}
            title={t('page.title')}
            description={t('page.description')}
          />
        </div>

        <div className="sticky top-0 z-20 -mx-1">
          <div className="rounded-2xl border border-[hsl(var(--glass-border))] bg-[hsl(var(--card)/0.22)] p-2 shadow-[0_18px_38px_rgba(0,0,0,0.10)] backdrop-blur-[32px] backdrop-saturate-150">
            <RequestsMobileFilterBar
              filters={filters}
              sites={sites}
              apiKeys={apiKeys}
              autoRefresh={autoRefresh}
              isFetching={requestsQuery.isFetching || requestSummaryQuery.isFetching}
              totalCost={totalCost}
              tokenUsage={tokenUsage}
              totalCostSupported={totalCostSupported}
              totalCostLoading={totalCostLoading}
              currency={requestSummary?.currency}
              rateLimitUsage={rateLimitUsage}
              onFiltersChange={handleFiltersChange}
              onAutoRefreshChange={handleAutoRefreshChange}
              onRefresh={refreshRequests}
            />
          </div>
        </div>

        <div ref={tableScrollRef}>
          <RequestsMobileList
            items={items}
            expandedId={expandedId}
            onExpandedIdChange={setExpandedId}
          />
          {showPagination ? pagination : null}
        </div>
      </div>
    )
  }

  return (
    <div className="flex min-h-full flex-col">
      <div className="flex-none space-y-6 pb-5">
        <PageHeader
          eyebrow={t('page.eyebrow')}
          title={t('page.title')}
          description={t('page.description')}
        />

        <RequestsFilterBar
          filters={filters}
          sites={sites}
          apiKeys={apiKeys}
          autoRefresh={autoRefresh}
          isFetching={requestsQuery.isFetching || requestSummaryQuery.isFetching}
          totalCost={totalCost}
          tokenUsage={tokenUsage}
          totalCostSupported={totalCostSupported}
          totalCostLoading={totalCostLoading}
          currency={requestSummary?.currency}
          rateLimitUsage={rateLimitUsage}
          onFiltersChange={handleFiltersChange}
          onAutoRefreshChange={handleAutoRefreshChange}
          onRefresh={refreshRequests}
        />
      </div>

      <RequestsTable
        items={items}
        expandedId={expandedId}
        onExpandedIdChange={setExpandedId}
        scrollContainerRef={tableScrollRef}
      />

      {showPagination ? pagination : null}
    </div>
  )
}
