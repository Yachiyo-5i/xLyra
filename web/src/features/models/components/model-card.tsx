import { ChevronDown, ChevronRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { Card } from '@/components/ui/card'
import { BrandMark } from '@/components/common/brand-mark'
import { buildModelGlyph } from '@/components/common/brand-utils'
import { copyToClipboard } from '@/components/common/copy-to-clipboard'
import { OTHER_BRAND_KEY } from '@/lib/brands'
import { formatEndpointTypeLabel, isNewAPISite } from '../lib/model-helpers'
import {
  formatGroupRatio,
  formatPricingValue,
  type MarketplaceModel,
  type MarketplacePricingRow,
} from '../api/models'

type TFunction = (key: string, options?: Record<string, unknown>) => string

function cacheWriteTooltip(
  pricing: MarketplacePricingRow,
  t: TFunction,
): string | undefined {
  if (pricing.createCache1hInputValue == null) return undefined
  const fmt = (v: number | null | undefined) =>
    formatPricingValue(v, pricing.billingType, pricing.currency, t)
  return `5min: ${fmt(pricing.createCacheInputValue)}\n1h: ${fmt(pricing.createCache1hInputValue)}`
}

type ModelCardProps = {
  model: MarketplaceModel
  expanded: boolean
  onToggle: () => void
}

export function ModelCard({ model, expanded, onToggle }: ModelCardProps) {
  const { t } = useTranslation('models')
  const Chevron = expanded ? ChevronDown : ChevronRight

  return (
    <Card className="overflow-hidden p-0">
      <button
        type="button"
        onClick={onToggle}
        className="flex w-full items-start gap-4 px-5 py-4 text-left"
      >
        <BrandMark
          iconPath={model.iconPath}
          label={model.brand}
          fallback={model.name}
          fallbackText={
            model.providerId === OTHER_BRAND_KEY
              ? buildModelGlyph(model.name)
              : undefined
          }
          highlightedFallback={model.providerId === OTHER_BRAND_KEY}
        />

        <div className="min-w-0 flex-1 space-y-3">
          <div className="flex items-start justify-between gap-4">
            <div className="min-w-0 space-y-1">
              <div
                className="truncate text-headline text-label cursor-pointer hover:opacity-80 transition-opacity"
                onClick={(e) => {
                  e.stopPropagation()
                  copyToClipboard(model.model_key, t('card.copied'))
                }}
              >
                {model.model_key}
              </div>
              <div className="text-label-2 text-body">{model.brand}</div>
            </div>
            <Chevron className="mt-1 h-4 w-4 shrink-0 text-label-2" />
          </div>

          <div className="flex flex-wrap gap-2">
            {model.supportedSites.map((site) => (
              <span
                key={`${model.id}-${site.siteId}`}
                className="inline-flex items-center rounded-full bg-fill-3 px-2.5 py-1 text-footnote font-medium text-label-2"
              >
                {site.siteName}
              </span>
            ))}
          </div>
        </div>
      </button>

      {expanded ? (
        <div className="border-t border-separator px-5 py-4">
          <div className="overflow-hidden rounded-control border border-separator">
            <table className="min-w-full table-fixed border-collapse text-left text-body">
              <thead className="bg-fill-3 text-footnote font-semibold text-label-2">
                <tr>
                  <th className="w-[10%] px-4 py-3 font-medium">
                    {t('card.headers.site')}
                  </th>
                  <th className="w-[12%] px-4 py-3 font-medium">
                    {t('card.headers.upstreamModel')}
                  </th>
                  <th className="w-[9%] px-4 py-3 font-medium">
                    {t('card.headers.group')}
                  </th>
                  <th className="w-[10%] px-4 py-3 text-center font-medium">
                    {t('card.headers.apiKeys')}
                  </th>
                  <th className="w-[12%] px-4 py-3 font-medium">
                    {t('card.headers.protocol')}
                  </th>
                  <th className="w-[12%] px-4 py-3 font-medium">
                    {t('card.headers.inputPrice')}
                  </th>
                  <th className="w-[12%] px-4 py-3 font-medium">
                    {t('card.headers.outputPrice')}
                  </th>
                  <th className="w-[9%] px-4 py-3 font-medium">
                    {t('card.headers.cachePrice')}
                  </th>
                  <th className="w-[9%] px-4 py-3 font-medium">
                    {t('card.headers.cacheWritePrice')}
                  </th>
                  <th className="w-[9%] px-4 py-3 font-medium">
                    {t('card.headers.groupRatio')}
                  </th>
                </tr>
              </thead>
              <tbody>
                {model.supportedSites.flatMap((site) => {
                  const rows = site.pricingRows.length
                    ? site.pricingRows
                    : [
                        {
                          id: `${site.siteId}-empty`,
                          modelName: site.upstreamName,
                          groupName: '',
                          keyGroupStatus: undefined,
                          billingType: '',
                          currency: '',
                          available: false,
                        },
                      ]

                  return rows.map((pricing) => (
                    <tr
                      key={`${model.id}-${site.siteId}-${pricing.id}`}
                      className="border-t border-separator"
                    >
                      <td className="px-4 py-3">
                        <div className="truncate font-medium">
                          {site.siteName}
                        </div>
                      </td>
                      <td className="px-4 py-3 text-label-2">
                        <div
                          className="truncate"
                          title={pricing.modelName || site.upstreamName}
                        >
                          {pricing.modelName || site.upstreamName || '--'}
                        </div>
                      </td>
                      <td className="px-4 py-3 text-label-2">
                        {isNewAPISite(site.siteType)
                          ? pricing.groupName || '--'
                          : '-'}
                      </td>
                      <td className="px-4 py-3 text-center">
                        <APIKeyCell pricing={pricing} t={t} />
                      </td>
                      <td className="px-4 py-3 text-label-2">
                        <div className="space-y-1">
                          {(pricing.supportedEndpointTypes ?? site.supportedEndpointTypes).length
                            ? (pricing.supportedEndpointTypes ?? site.supportedEndpointTypes).map(
                                (endpointType, index, protocols) => (
                                  <span key={`${site.modelId}-${endpointType}`}>
                                    {formatEndpointTypeLabel(endpointType)}{index < protocols.length - 1 ? ' / ' : ''}
                                  </span>
                                ),
                              )
                            : '--'}
                        </div>
                      </td>
                      <td className="px-4 py-3 text-label-2">
                        {formatPricingValue(
                          pricing.inputValue ?? pricing.perRequestValue,
                          pricing.billingType,
                          pricing.currency,
                          t,
                        )}
                      </td>
                      <td className="px-4 py-3 text-label-2">
                        {formatPricingValue(
                          pricing.outputValue ?? pricing.audioOutputValue,
                          pricing.billingType,
                          pricing.currency,
                          t,
                        )}
                      </td>
                      <td className="px-4 py-3 text-label-2">
                        {formatPricingValue(
                          pricing.cacheInputValue,
                          pricing.billingType,
                          pricing.currency,
                          t,
                        )}
                      </td>
                      <td
                        className="px-4 py-3 text-label-2"
                        title={cacheWriteTooltip(pricing, t)}
                      >
                        {formatPricingValue(
                          pricing.createCacheInputValue,
                          pricing.billingType,
                          pricing.currency,
                          t,
                        )}
                      </td>
                      <td className="px-4 py-3 text-label-2">
                        {formatGroupRatio(pricing.groupRatio)}
                      </td>
                    </tr>
                  ))
                })}
              </tbody>
            </table>
          </div>
        </div>
      ) : null}
    </Card>
  )
}

function APIKeyCell({
  pricing,
  t,
}: {
  pricing: MarketplacePricingRow
  t: TFunction
}) {
  if (pricing.apiKeyName) {
    const enabled = pricing.apiKeyEnabled && pricing.apiKeyModelEnabled
    return (
      <div className="flex min-w-0 items-center">
        <Badge
          variant={enabled ? 'success' : 'warning'}
          className="max-w-full border-transparent px-2 py-0.5 text-footnote tracking-normal"
          title={pricing.apiKeyName}
        >
          <span className="truncate">{pricing.apiKeyName}</span>
        </Badge>
      </div>
    )
  }
  return <KeyGroupStatusBadge status={pricing.keyGroupStatus} t={t} />
}

function KeyGroupStatusBadge({
  status,
  t,
}: {
  status?: {
    enabled: number
    total: number
    source?: 'api_keys' | 'codex_accounts'
  }
  t: TFunction
}) {
  if (!status) {
    return <span className="text-label-2">--</span>
  }

  const variant =
    status.enabled > 0 ? 'success' : status.total > 0 ? 'warning' : 'neutral'
  const tooltipKey =
    status.source === 'codex_accounts'
      ? 'card.keyGroup.codexTooltip'
      : 'card.keyGroup.keyTooltip'

  return (
    <Badge
      variant={variant}
      className="justify-center border-transparent px-2 py-0.5 text-footnote tabular-nums tracking-normal"
      title={t(tooltipKey, { enabled: status.enabled, total: status.total })}
    >
      {status.enabled}/{status.total}
    </Badge>
  )
}
