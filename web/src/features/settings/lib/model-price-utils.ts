import type {
  ModelPriceBillingType,
  ModelPriceInput,
  ModelPriceItem,
} from '@/features/settings/api/model-prices'

export type ModelPriceGroup = {
  id: string
  canonicalModelId?: string | null
  modelKey: string
  displayName: string
  provider?: string | null
  category?: string | null
  rows: ModelPriceItem[]
  editableSiteModelIds: string[]
  uniqueSiteCount: number
  editableCount: number
  missingCount: number
  manualCount: number
  syncedCount: number
}

export type PriceDraft = {
  groupName: string
  billingType: ModelPriceBillingType
  currency: string
  inputValue: string
  outputValue: string
  audioOutputValue: string
  cacheInputValue: string
  createCacheInputValue: string
  createCache1hInputValue: string
  perRequestValue: string
  groupRatio: string
  manualNote: string
}

export type ParsedPriceDraft =
  | { ok: true; input: ModelPriceInput }
  | { ok: false; message: string }

export const DEFAULT_PRICE_DRAFT: PriceDraft = {
  groupName: '',
  billingType: 'tokens',
  currency: 'USD',
  inputValue: '',
  outputValue: '',
  audioOutputValue: '',
  cacheInputValue: '',
  createCacheInputValue: '',
  createCache1hInputValue: '',
  perRequestValue: '',
  groupRatio: '',
  manualNote: '',
}

export function buildModelPriceGroups(items: ModelPriceItem[]) {
  const map = new Map<string, ModelPriceGroup>()

  for (const item of items) {
    const canonicalId = item.model.canonical_model_id
    const modelKey = item.model.canonical_model_key || item.model.upstream_model_name || item.model.display_name
    const id = canonicalId ? `canonical:${canonicalId}` : `upstream:${modelKey.toLowerCase()}`
    const group = map.get(id) ?? {
      id,
      canonicalModelId: canonicalId,
      modelKey,
      displayName: item.model.canonical_display_name || modelKey,
      provider: item.model.provider,
      category: item.model.category,
      rows: [],
      editableSiteModelIds: [],
      uniqueSiteCount: 0,
      editableCount: 0,
      missingCount: 0,
      manualCount: 0,
      syncedCount: 0,
    }

    group.rows.push(item)
    map.set(id, group)
  }

  return [...map.values()]
    .map((group) => {
      const siteIds = new Set<string>()
      const editableSiteModelIds = new Set<string>()
      let missingCount = 0
      let manualCount = 0
      let syncedCount = 0

      for (const row of group.rows) {
        siteIds.add(row.site.id)
        if (row.editable) editableSiteModelIds.add(row.site_model_id)
        switch (row.pricing_status) {
          case 'missing':
            missingCount += 1
            break
          case 'manual':
            manualCount += 1
            break
          case 'synced':
            syncedCount += 1
            break
        }
      }

      return {
        ...group,
        rows: sortModelPriceRows(group.rows),
        editableSiteModelIds: [...editableSiteModelIds],
        uniqueSiteCount: siteIds.size,
        editableCount: editableSiteModelIds.size,
        missingCount,
        manualCount,
        syncedCount,
      }
    })
    .sort((a, b) => a.modelKey.localeCompare(b.modelKey))
}

export type SiteModelRow = ModelPriceItem & {
  displayKey: string
}

export type SiteModelGroup = {
  siteId: string
  siteName: string
  siteType: string
  rows: SiteModelRow[]
  manualCount: number
  totalCount: number
}

export function buildSiteModelGroup(items: ModelPriceItem[], siteId: string): SiteModelGroup | null {
  const siteItems = items.filter((item) => item.site.id === siteId)
  if (siteItems.length === 0) return null

  const first = siteItems[0]
  let manualCount = 0
  const rows: SiteModelRow[] = siteItems
    .map((item) => {
      if (item.pricing_status === 'manual') manualCount++
      const displayKey = item.model.canonical_model_key || item.model.upstream_model_name
      return { ...item, displayKey }
    })
    .sort((a, b) => a.displayKey.localeCompare(b.displayKey))

  return {
    siteId: first.site.id,
    siteName: first.site.name,
    siteType: first.site.type,
    rows,
    manualCount,
    totalCount: rows.length,
  }
}

export function extractSiteOptions(items: ModelPriceItem[]): { id: string; name: string }[] {
  const map = new Map<string, string>()
  for (const item of items) {
    if (!map.has(item.site.id)) {
      map.set(item.site.id, item.site.name)
    }
  }
  return [...map.entries()]
    .map(([id, name]) => ({ id, name }))
    .sort((a, b) => a.name.localeCompare(b.name))
}

export function createDraftFromPricing(row?: ModelPriceItem | null): PriceDraft {
  const pricing = row?.pricing
  if (!pricing) return DEFAULT_PRICE_DRAFT
  const billingType = normalizeBillingType(pricing.billing_type)
  return {
    groupName: isNewAPISite(row?.site.type) ? pricing.group_name || '' : '',
    billingType,
    currency: pricing.currency || 'USD',
    inputValue: numberToDraft(pricing.input_value),
    outputValue: numberToDraft(pricing.output_value),
    audioOutputValue: numberToDraft(pricing.audio_output_value),
    cacheInputValue: numberToDraft(pricing.cache_input_value),
    createCacheInputValue: numberToDraft(pricing.create_cache_input_value),
    createCache1hInputValue: numberToDraft(pricing.create_cache_1h_input_value),
    perRequestValue: numberToDraft(pricing.per_request_value),
    groupRatio: numberToDraft(pricing.group_ratio),
    manualNote: pricing.manual_note || '',
  }
}

export function parsePriceDraft(draft: PriceDraft, t?: TFunction): ParsedPriceDraft {
  const groupName = draft.groupName.trim()
  const currency = draft.currency.trim().toUpperCase() || 'USD'
  const inputValue = parsePriceNumber(draft.inputValue, t ? t('modelsPrice.validation.inputPrice') : '输入价格', t)
  const outputValue = parsePriceNumber(draft.outputValue, t ? t('modelsPrice.validation.outputPrice') : '输出价格', t)
  const audioOutputValue = parsePriceNumber(draft.audioOutputValue, t ? t('modelsPrice.validation.audioOutputPrice') : '音频输出价', t)
  const cacheInputValue = parsePriceNumber(draft.cacheInputValue, t ? t('modelsPrice.validation.cachePrice') : '缓存价格', t)
  const createCacheInputValue = parsePriceNumber(draft.createCacheInputValue, t ? t('modelsPrice.validation.cacheWritePrice') : '缓存写入价格', t)
  const createCache1hInputValue = parsePriceNumber(draft.createCache1hInputValue, t ? t('modelsPrice.validation.cacheWrite1hPrice') : '缓存写入(1h)价格', t)
  const perRequestValue = parsePriceNumber(draft.perRequestValue, t ? t('modelsPrice.validation.perRequestPrice') : '按次价格', t)
  const groupRatioValue = parsePriceNumber(draft.groupRatio, t ? t('modelsPrice.validation.groupRatio') : '组倍率', t)

  if (!inputValue.ok) return inputValue
  if (!outputValue.ok) return outputValue
  if (!audioOutputValue.ok) return audioOutputValue
  if (!cacheInputValue.ok) return cacheInputValue
  if (!createCacheInputValue.ok) return createCacheInputValue
  if (!createCache1hInputValue.ok) return createCache1hInputValue
  if (!perRequestValue.ok) return perRequestValue
  if (!groupRatioValue.ok) return groupRatioValue

  if (draft.billingType === 'tokens') {
    if (inputValue.value == null && outputValue.value == null) {
      return { ok: false, message: t ? t('modelsPrice.validation.tokenRequiresInputOrOutput') : '按 token 计费至少填写输入价格或输出价格' }
    }
    if (audioOutputValue.value != null && (inputValue.value == null || inputValue.value <= 0)) {
      return { ok: false, message: t ? t('modelsPrice.validation.audioRequiresInput') : '设置音频输出价需填写正数输入价格' }
    }
    return {
      ok: true,
      input: {
        group_name: groupName || undefined,
        billing_type: 'tokens',
        currency,
        input_value: roundPriceValue(inputValue.value),
        output_value: roundPriceValue(outputValue.value),
        audio_output_value: roundPriceValue(audioOutputValue.value),
        cache_input_value: roundPriceValue(cacheInputValue.value),
        create_cache_input_value: roundPriceValue(createCacheInputValue.value),
        create_cache_1h_input_value: roundPriceValue(createCache1hInputValue.value),
        per_request_value: null,
        group_ratio: roundPriceValue(groupRatioValue.value),
        manual_note: draft.manualNote.trim(),
      },
    }
  }

  if (perRequestValue.value == null) {
    return { ok: false, message: t ? t('modelsPrice.validation.perRequestRequiresValue') : '按次计费需要填写按次价格' }
  }
  return {
    ok: true,
    input: {
      group_name: groupName || undefined,
      billing_type: 'per_request',
      currency,
      input_value: null,
      output_value: null,
      audio_output_value: null,
      cache_input_value: null,
      create_cache_input_value: null,
      create_cache_1h_input_value: null,
      per_request_value: roundPriceValue(perRequestValue.value),
      group_ratio: roundPriceValue(groupRatioValue.value),
      manual_note: draft.manualNote.trim(),
    },
  }
}

export function isNewAPISite(siteType?: string | null) {
  return siteType?.trim().toLowerCase() === 'newapi'
}

export function tokenPriceHeaderLabel(label: string) {
  return `${label} ($/1M)`
}

export function perRequestPriceHeaderLabel(label: string, t?: TFunction) {
  const suffix = t ? t('modelsPrice.perRequestSuffix') : '/call'
  return `${label} ($${suffix})`
}

function formatPriceValue(value?: number | null, currency = 'USD') {
  if (typeof value !== 'number') return '-'
  const prefix = currency.toUpperCase() === 'USD' ? '$' : `${currency} `
  return `${prefix}${compactNumber(value)}`
}

function formatTokenPrice(value?: number | null, currency = 'USD') {
  const formatted = formatPriceValue(value, currency)
  return formatted === '-' ? '-' : `${formatted}/1M`
}

function formatPerRequestPrice(value?: number | null, currency = 'USD', suffix = '/call') {
  const formatted = formatPriceValue(value, currency)
  return formatted === '-' ? '-' : `${formatted}${suffix}`
}

type TFunction = (key: string, options?: Record<string, unknown>) => string

export function pricingSummary(group: ModelPriceGroup, t?: TFunction) {
  const pricedRow = group.rows.find((row) => row.pricing)
  if (!pricedRow?.pricing) return t ? t('modelsPrice.subtitle.noPrice') : '暂无价格'
  const pricing = pricedRow.pricing
  if (normalizeBillingType(pricing.billing_type) === 'per_request') {
    const price = formatPerRequestPrice(pricing.per_request_value, pricing.currency, '')
    return t
      ? t('modelsPrice.subtitle.perRequest', { price })
      : `按次 ${formatPerRequestPrice(pricing.per_request_value, pricing.currency)}`
  }
  const input = formatTokenPrice(pricing.input_value, pricing.currency)
  const output = formatTokenPrice(pricing.output_value, pricing.currency)
  return t
    ? t('modelsPrice.subtitle.tokenPrice', { input, output })
    : `输入 ${input} / 输出 ${output}`
}

export function statusBadgeVariant(status: string) {
  switch (status) {
    case 'missing':
      return 'warning'
    case 'manual':
      return 'secondary'
    case 'synced':
      return 'success'
    default:
      return 'neutral'
  }
}

export function statusLabel(status: string, t?: TFunction) {
  if (t) {
    const key = `modelsPrice.statusLabels.${status}`
    const translated = t(key)
    return translated !== key ? translated : (status || '-')
  }
  switch (status) {
    case 'missing':
      return '缺失'
    case 'manual':
      return '手动'
    case 'synced':
      return '同步'
    case 'priced':
      return '已定价'
    default:
      return status || '-'
  }
}

function normalizeBillingType(value?: string | null): ModelPriceBillingType {
  return value === 'per_request' || value === 'fixed' ? 'per_request' : 'tokens'
}

function sortModelPriceRows(rows: ModelPriceItem[]) {
  return [...rows].sort((a, b) => {
    const siteCompare = a.site.name.localeCompare(b.site.name)
    if (siteCompare !== 0) return siteCompare
    const modelCompare = a.model.upstream_model_name.localeCompare(b.model.upstream_model_name)
    if (modelCompare !== 0) return modelCompare
    return (a.pricing?.group_name || '').localeCompare(b.pricing?.group_name || '')
  })
}

function parsePriceNumber(value: string, label: string, t?: TFunction): { ok: true; value: number | null } | { ok: false; message: string } {
  const trimmed = value.trim()
  if (!trimmed) return { ok: true, value: null }
  const parsed = Number(trimmed)
  if (!Number.isFinite(parsed) || parsed < 0) {
    const msg = t
      ? t('modelsPrice.validation.nonNegativeNumber', { label })
      : `${label}必须是大于等于 0 的数字`
    return { ok: false, message: msg }
  }
  return { ok: true, value: parsed }
}

function numberToDraft(value?: number | null) {
  return typeof value === 'number' ? String(roundPriceValue(value)) : ''
}

function compactNumber(value: number) {
  return new Intl.NumberFormat('en-US', {
    maximumFractionDigits: 6,
  }).format(value)
}

function roundPriceValue(value: number | null) {
  return typeof value === 'number' ? Number(value.toFixed(6)) : null
}
