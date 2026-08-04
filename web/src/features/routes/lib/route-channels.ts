import type { RouteCandidateItem, RouteCooldown } from '@/features/routes/api/routes'
import type { CanonicalModelMatrixRow, Site, SiteAPIKey, SiteAPIKeyModel } from '@/features/sites/api/sites'
import { isNewAPISite } from '@/features/routes/lib/route-format'
import type { PendingChannelState, RouteChannelRow } from '@/features/routes/lib/types'

const routeChannelNameCollator = new Intl.Collator('zh-CN', { numeric: true, sensitivity: 'base' })

export function routeChannelRowsFromMatrix(
  matrixRows: CanonicalModelMatrixRow[],
  sites: Site[],
  apiKeysMap: Record<string, SiteAPIKey[]>,
  selected: RouteCandidateItem | undefined,
  candidates: RouteCandidateItem[],
  cooldowns: RouteCooldown[],
) {
  const siteMap = new Map(sites.map((site) => [site.id, site]))
  const candidateMap = new Map(candidates.map((candidate) => [candidate.model.site_model_id, candidate]))
  const rows: RouteChannelRow[] = []

  for (const row of matrixRows) {
    const site = siteMap.get(row.site_id) ?? matrixRowSite(row)
    const candidate = candidateMap.get(row.site_model_id)
    const current = selected?.model.site_model_id === row.site_model_id
    const currentCredentialId = selected?.credential.id ?? undefined
    const cooling = routeModelCooling(cooldowns, row.site_id, row.site_model_id)

    rows.push(...buildRouteChannelRows({
      site,
      siteModelId: row.site_model_id,
      siteModelStatus: row.model_status,
      upstreamName: row.upstream_model_name || row.display_name,
      displayName: row.display_name,
      apiKeys: apiKeysMap[row.site_id] ?? [],
      current,
      currentCredentialId,
      cooling,
      candidate,
      matrixRow: row,
    }))
  }

  return rows.sort(compareRouteChannelRows)
}

function compareRouteChannelRows(a: RouteChannelRow, b: RouteChannelRow) {
  const siteDiff = routeChannelNameCollator.compare(a.siteName, b.siteName)
  if (siteDiff !== 0) return siteDiff

  const upstreamDiff = routeChannelNameCollator.compare(a.upstreamName, b.upstreamName)
  if (upstreamDiff !== 0) return upstreamDiff

  const groupDiff = routeChannelNameCollator.compare(a.groupName ?? '', b.groupName ?? '')
  if (groupDiff !== 0) return groupDiff

  const apiKeyDiff = routeChannelNameCollator.compare(a.apiKeyName ?? '', b.apiKeyName ?? '')
  if (apiKeyDiff !== 0) return apiKeyDiff

  return a.id.localeCompare(b.id)
}

function buildRouteChannelRows(input: {
  site: Site
  siteModelId: string
  siteModelStatus: string
  upstreamName: string
  displayName?: string | null
  apiKeys: SiteAPIKey[]
  current?: boolean
  currentCredentialId?: string
  cooling?: boolean
  candidate?: RouteCandidateItem
  matrixRow?: CanonicalModelMatrixRow
}): RouteChannelRow[] {
  const siteModelEnabled = isRouteModelEnabled(input.siteModelStatus)
  if (input.siteModelStatus === 'unavailable') return []

  if (!input.site.supports_multiple_api_keys && !isNewAPISite(input.site.site_type)) {
    const enabled = input.site.enabled && siteModelEnabled
    return [{
      id: `site-model:${input.site.id}:${input.siteModelId}`,
      kind: 'site_model' as const,
      siteId: input.site.id,
      siteName: input.site.name,
      siteType: input.site.site_type,
      siteEnabled: input.site.enabled,
      siteModelId: input.siteModelId,
      siteModelEnabled,
      upstreamName: input.upstreamName,
      current: input.current,
      cooling: input.cooling,
      candidate: input.candidate,
      pricing: input.candidate?.pricing ?? (input.matrixRow ? pickMatrixPricing(input.matrixRow) : undefined),
      enabled,
      routable: enabled && !input.cooling,
      toggleDisabled: !input.site.enabled,
    }]
  }

  const names = buildModelNameSet(input.upstreamName, input.displayName)
  const rows: RouteChannelRow[] = []
  const supportsCostMultiplier =
    input.site.supports_api_key_cost_multiplier === true

  for (const apiKey of input.apiKeys) {
    const apiKeyModel = findAPIKeyModel(apiKey, names)
    if (!apiKeyModel) continue

    const apiKeyEnabled = apiKey.enabled && apiKey.status !== 'disabled'
    const apiKeySecretMissing = apiKey.secret_missing === true
    const apiKeyModelEnabled = apiKeyModel.enabled !== false
    const groupName = normalizeGroupName(apiKey.group)
    const enabled = input.site.enabled && siteModelEnabled && apiKeyEnabled && apiKeyModelEnabled

    rows.push({
      id: `api-key:${input.site.id}:${input.siteModelId}:${apiKey.id}:${apiKeyModel.name}`,
      kind: 'api_key',
      siteId: input.site.id,
      siteName: input.site.name,
      siteType: input.site.site_type,
      siteEnabled: input.site.enabled,
      siteModelId: input.siteModelId,
      siteModelEnabled,
      upstreamName: input.upstreamName,
      apiKeyId: apiKey.id,
      apiKeyName: apiKey.name,
      apiKeyEnabled,
      apiKeyRoutingPriority: apiKey.routing_priority ?? 1,
      apiKeyUpstreamCostMultiplier: supportsCostMultiplier
        ? (apiKey.upstream_cost_multiplier ?? 1)
        : undefined,
      apiKeySecretMissing,
      apiKeyModelName: apiKeyModel.name,
      apiKeyModelEnabled,
      groupName,
      current: input.current && (!input.currentCredentialId || input.currentCredentialId === apiKey.id),
      cooling: input.cooling,
      candidate: input.candidate,
      pricing: input.candidate?.credential.id === apiKey.id
        ? input.candidate.pricing
        : input.matrixRow
          ? effectivePricingForAPIKey(
              pickPricingForGroup(input.matrixRow, groupName),
              supportsCostMultiplier
                ? (apiKey.upstream_cost_multiplier ?? 1)
                : 1,
            )
          : undefined,
      enabled,
      routable: enabled && !apiKeySecretMissing && !input.cooling,
      toggleDisabled: !input.site.enabled || !apiKeyEnabled,
    })
  }

  return rows
}

function matrixRowSite(row: CanonicalModelMatrixRow): Site {
  return {
    id: row.site_id,
    name: row.site_name,
    slug: row.site_slug,
    site_type: row.site_type as Site['site_type'],
    base_url: '',
    status: row.site_enabled ? 'active' : 'disabled',
    enabled: row.site_enabled,
    routing_priority: 0,
    meta: {},
    created_at: row.created_at,
    updated_at: row.updated_at,
  }
}

function buildModelNameSet(...names: Array<string | null | undefined>) {
  return new Set(names.map(normalizeModelName).filter(Boolean))
}

function normalizeModelName(name?: string | null) {
  return name?.trim().toLowerCase() ?? ''
}

function normalizeGroupName(groupName?: string | null) {
  return groupName?.trim() || ''
}

function findAPIKeyModel(apiKey: SiteAPIKey, names: Set<string>) {
  return apiKeyModels(apiKey).find((model) => names.has(normalizeModelName(model.name)))
}

function apiKeyModels(apiKey: SiteAPIKey): SiteAPIKeyModel[] {
  if (apiKey.model_items?.length) return apiKey.model_items
  return apiKey.models.map((name) => ({ name, enabled: true }))
}

function pickMatrixPricing(row: CanonicalModelMatrixRow) {
  return row.pricing.find((pricing) => pricing.available) ?? row.pricing[0]
}

function pickPricingForGroup(row: CanonicalModelMatrixRow, groupName: string) {
  const normalizedGroup = normalizeGroupName(groupName)
  const sameGroup = row.pricing.filter((pricing) => normalizeGroupName(pricing.group_name) === normalizedGroup)
  return sameGroup.find((pricing) => pricing.available) ?? sameGroup[0] ?? pickMatrixPricing(row)
}

function effectivePricingForAPIKey(
  pricing: CanonicalModelMatrixRow['pricing'][number] | undefined,
  multiplier: number,
) {
  if (!pricing) return undefined
  const normalizedMultiplier = Number.isFinite(multiplier) && multiplier > 0 ? multiplier : 1
  return {
    ...pricing,
    base_input_value: pricing.input_value,
    base_output_value: pricing.output_value,
    base_per_request_value: pricing.per_request_value,
    input_value: multiplyOptionalPrice(pricing.input_value, normalizedMultiplier),
    output_value: multiplyOptionalPrice(pricing.output_value, normalizedMultiplier),
    per_request_value: multiplyOptionalPrice(pricing.per_request_value, normalizedMultiplier),
    upstream_cost_multiplier: normalizedMultiplier,
  }
}

function multiplyOptionalPrice(value: number | null | undefined, multiplier: number) {
  return typeof value === 'number' ? value * multiplier : value
}

export function findCurrentRouteChannel(rows: RouteChannelRow[], selected?: RouteCandidateItem) {
  if (!selected) return undefined

  const matchedRows = rows.filter((row) =>
    row.siteId === selected.site.id && row.siteModelId === selected.model.site_model_id)
  const credentialRow = selected.credential.id
    ? matchedRows.find((row) => row.apiKeyId === selected.credential.id)
    : undefined
  if (credentialRow) return { ...credentialRow, candidate: credentialRow.candidate ?? selected }
  const selectedGroup = normalizeGroupName(selected.pricing.group_name)
  const sameGroupRows = selectedGroup
    ? matchedRows.filter((row) => normalizeGroupName(row.groupName) === selectedGroup)
    : matchedRows

  const row = (
    sameGroupRows.find((row) => row.routable)
    ?? matchedRows.find((row) => row.routable)
    ?? sameGroupRows[0]
    ?? matchedRows[0]
  )
  return row ? { ...row, candidate: row.candidate ?? selected } : undefined
}

function isRouteModelEnabled(status: string) {
  return status === 'active' || status === 'enabled'
}

type TFunction = (key: string, options?: Record<string, unknown>) => string

export function routeChannelStatus(row: RouteChannelRow, t?: TFunction) {
  if (row.routable) {
    return {
      label: row.candidate
        ? (t ? t('channels.routableRank', { rank: row.candidate.rank }) : `可路由 #${row.candidate.rank}`)
        : (t ? t('channels.routable') : '可路由'),
      badgeVariant: 'neutral' as const,
    }
  }

  if (row.cooling) {
    return {
      label: t ? t('channels.cooling') : '冷却中',
      badgeVariant: 'warning' as const,
    }
  }

  if (!row.siteEnabled) {
    return {
      label: t ? t('channels.siteDisabled') : '站点停用',
      badgeVariant: 'warning' as const,
    }
  }

  if (!row.siteModelEnabled) {
    return {
      label: t ? t('channels.modelDisabled') : '模型停用',
      badgeVariant: 'warning' as const,
    }
  }

  if (row.kind === 'api_key') {
    if (!row.apiKeyEnabled) {
      return {
        label: t ? t('channels.keyDisabled') : 'Key 停用',
        badgeVariant: 'warning' as const,
      }
    }

    if (row.apiKeySecretMissing) {
      return {
        label: t ? t('channels.keyIncomplete') : 'Key 未补全',
        badgeVariant: 'warning' as const,
      }
    }

    if (!row.apiKeyModelEnabled) {
      return {
        label: t ? t('channels.modelDisabled') : '模型停用',
        badgeVariant: 'warning' as const,
      }
    }
  }

  return {
    label: t ? t('channels.notCandidate') : '未进入候选',
    badgeVariant: 'neutral' as const,
  }
}

export function routeChannelSwitchDisabledReason(row: RouteChannelRow, pending?: PendingChannelState, t?: TFunction) {
  if (pending) return t ? t('channels.updating') : '正在更新'
  if (!row.siteEnabled) return t ? t('channels.siteDisabledHint') : '站点已停用，不能切换模型'
  if (row.kind === 'api_key' && !row.apiKeyEnabled) return t ? t('channels.keyDisabledHint') : 'Key 已停用，不能切换模型'
  return undefined
}

function routeModelCooling(cooldowns: RouteCooldown[], siteId: string, siteModelId: string) {
  return cooldowns.some((cooldown) => {
    if (cooldown.site_model_id) {
      return cooldown.site_model_id === siteModelId
    }
    return cooldown.site_id === siteId && !cooldown.site_credential_id
  })
}
