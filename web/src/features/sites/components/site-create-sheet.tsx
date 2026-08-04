import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { zodResolver } from '@hookform/resolvers/zod'
import { ChevronDown, ChevronLeft, KeyRound, LoaderCircle, PencilLine, Plus, Trash2 } from 'lucide-react'
import { Controller, useFieldArray, useForm, useWatch } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { z } from 'zod'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Draw,
  DrawBody,
  DrawContent,
  DrawFooter,
  DrawHeader,
  DrawTitle,
} from '@/components/ui/draw'
import { Input } from '@/components/ui/input'
import { MultiSelect } from '@/components/ui/multi-select'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { siteTypeIconPath } from '@/components/common/brand-utils'
import { cn } from '@/lib/utils'
import { useResolvedTheme } from '@/hooks/theme-context'
import {
  type CreateSiteInput,
  type Site,
  type SiteTypeInfo,
} from '@/features/sites/api/sites'
import type { SiteGroup } from '@/features/settings/api/site-groups'
import { type ProxyConfig } from '@/features/settings/api/settings'
import { siteTypeIconClassName } from '@/features/sites/lib/site-utils'
import {
  SiteAPIKeyFormFields,
} from '@/features/sites/components/site-api-key-form'
import {
  DEFAULT_API_KEY_FORM_DRAFT,
  parseSiteAPIKeyForm,
  type APIKeyFormDraft,
} from '@/features/sites/components/site-api-key-form-data'

function credentialTypeOf(siteType: string, siteTypes: SiteTypeInfo[] | undefined): string | undefined {
  return siteTypes?.find(st => st.site_type === siteType)?.credential_type
}

function requiresBaseURL(siteType: string, siteTypes: SiteTypeInfo[] | undefined): boolean {
  const info = siteTypes?.find(st => st.site_type === siteType)
  if (!info) return true
  return info.requires_base_url
}

function usesDomesticBuiltinBaseURL(siteType: string): boolean {
  return ['deepseek', 'minimax', 'xiaomi_mimo', 'zhipu', 'glm_code'].includes(siteType)
}

function createSiteSchema(mode: SiteSheetMode, siteTypes: SiteTypeInfo[] | undefined, t: (key: string) => string, initialSite?: Site | null) {
  const validTypes = (siteTypes ?? []).filter(st => st.show_in_create_dialog).map(st => st.site_type)

  return z
    .object({
      name: z.string().trim().min(1, t('form.validation.nameRequired')),
      baseUrl: z.string().trim(),
      siteType: z.string().refine(v => validTypes.includes(v), t('form.validation.typeRequired')),
      apiKey: z.string().trim(),
      apiKeys: z.array(z.object({
        apiKey: z.string().trim().min(1),
        name: z.string(),
        routingPriority: z.number(),
        upstreamCostMultiplier: z.number().optional(),
      })),
      newapiAccessToken: z.string().trim(),
      newapiUserId: z.string().trim(),
      xlyraAuthMode: z.enum(['access_token', 'api_key']),
      xlyraAccessToken: z.string().trim(),
      routingPriority: z.string().trim(),
      enabled: z.boolean(),
      proxyId: z.string(),
      responsesToolPolicy: z.enum(['passthrough', 'compatibility']),
      disableResponsesImageGenerationTool: z.boolean(),
      quotaProbe: z.enum(['none', 'sub2api', 'newapi', 'xlyra']),
      impersonateCodexClient: z.boolean(),
      impersonateClaudeCodeClient: z.boolean(),
      siteGroupIds: z.array(z.string()),
      requestHeaders: z.array(z.object({
        key: z.string().trim().min(1, t('form.validation.headerNameRequired')),
        value: z.string().trim(),
      })),
    })
    .superRefine((value, ctx) => {
      if (value.baseUrl) {
        try { new URL(value.baseUrl) } catch {
          ctx.addIssue({ code: 'custom', path: ['baseUrl'], message: t('form.validation.urlInvalid') })
        }
      } else if (value.siteType !== 'grok' && requiresBaseURL(value.siteType, siteTypes)) {
        ctx.addIssue({ code: 'custom', path: ['baseUrl'], message: t('form.validation.urlRequired') })
      }

      const credType = credentialTypeOf(value.siteType, siteTypes)

      if (credType === 'system_token') {
        if (!value.newapiAccessToken) {
          ctx.addIssue({
            code: 'custom',
            path: ['newapiAccessToken'],
            message: t('form.validation.tokenRequired'),
          })
        }

        if (!value.newapiUserId) {
          ctx.addIssue({
            code: 'custom',
            path: ['newapiUserId'],
            message: t('form.validation.userIdRequired'),
          })
        } else if (!/^\d+$/.test(value.newapiUserId)) {
          ctx.addIssue({
            code: 'custom',
            path: ['newapiUserId'],
            message: t('form.validation.userIdNumeric'),
          })
        }
      }

      if (credType === 'xlyra') {
        if (value.xlyraAuthMode === 'access_token' && !value.xlyraAccessToken) {
          ctx.addIssue({
            code: 'custom',
            path: ['xlyraAccessToken'],
            message: t('form.validation.tokenRequired'),
          })
        }

        const initialXLyraMode = initialSite?.auth_config?.xlyra?.auth_mode ?? 'api_key'
        if (value.xlyraAuthMode === 'api_key' && (mode === 'create' || initialXLyraMode !== 'api_key') && !value.apiKey) {
          ctx.addIssue({
            code: 'custom',
            path: ['apiKey'],
            message: t('form.validation.apiKeyRequired'),
          })
        }
      }

      if (value.siteType !== 'grok' && credType !== 'system_token' && credType !== 'oauth' && credType !== 'xlyra' && mode === 'create' && value.apiKeys.length === 0) {
        ctx.addIssue({
          code: 'custom',
          path: ['apiKeys'],
          message: t('form.validation.apiKeyRequired'),
        })
      }

      if (value.siteType !== 'grok' && value.responsesToolPolicy === 'compatibility' && !value.disableResponsesImageGenerationTool) {
        ctx.addIssue({
          code: 'custom',
          path: ['disableResponsesImageGenerationTool'],
          message: t('form.validation.responsesToolRequired'),
        })
      }

      if (!value.routingPriority) {
        ctx.addIssue({
          code: 'custom',
          path: ['routingPriority'],
          message: t('form.validation.priorityRequired'),
        })
        return
      }

      if (!/^\d+(\.\d)?$/.test(value.routingPriority)) {
        ctx.addIssue({
          code: 'custom',
          path: ['routingPriority'],
          message: t('form.validation.priorityFormat'),
        })
        return
      }

      const routingPriority = Number(value.routingPriority)
      if (!Number.isFinite(routingPriority) || routingPriority < 1 || routingPriority > 5) {
        ctx.addIssue({
          code: 'custom',
          path: ['routingPriority'],
          message: t('form.validation.priorityRange'),
        })
      }
    })
}

type SiteSheetMode = 'create' | 'edit'
type CreateSiteFormValues = z.infer<ReturnType<typeof createSiteSchema>>
export type SiteCreateSubmitInput = CreateSiteInput & { siteGroupIds?: string[] }

type SiteCreateSheetProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  mode?: SiteSheetMode
  initialSite?: Site | null
  isPending: boolean
  siteTypes?: SiteTypeInfo[]
  siteGroups?: SiteGroup[]
  proxies?: ProxyConfig[]
  siteGroupsLoading?: boolean
  onSubmit: (input: SiteCreateSubmitInput) => Promise<void>
}

const defaultValues: CreateSiteFormValues = {
  name: '',
  baseUrl: '',
  siteType: '',
  apiKey: '',
  apiKeys: [],
  newapiAccessToken: '',
  newapiUserId: '',
  xlyraAuthMode: 'api_key',
  xlyraAccessToken: '',
  routingPriority: '1.0',
  enabled: true,
  proxyId: 'direct',
  responsesToolPolicy: 'passthrough',
  disableResponsesImageGenerationTool: false,
  quotaProbe: 'none',
  impersonateCodexClient: false,
  impersonateClaudeCodeClient: false,
  siteGroupIds: [],
  requestHeaders: [],
}

function formatRoutingPriorityInput(value?: number) {
  const normalized = typeof value === 'number' && Number.isFinite(value) ? value : 1
  return normalized.toFixed(1)
}

function readRequestHeadersFromMeta(meta: Record<string, unknown>): { key: string; value: string }[] {
  const raw = meta?.request_headers
  if (!Array.isArray(raw)) return []
  return raw
    .filter((item): item is { key: unknown; value: unknown } => item && typeof item === 'object')
    .filter((item) => typeof item.key === 'string' && typeof item.value === 'string')
    .map((item) => ({ key: item.key as string, value: item.value as string }))
}


function responsesToolConfigFromSite(site: Site) {
  const legacyPolicy = site.gateway_config?.responses_image_generation_policy
  const disableImageGenerationTool =
    site.gateway_config?.disabled_responses_tools?.includes('image_generation') ||
    legacyPolicy === 'strip_auto_tool' ||
    legacyPolicy === 'disabled'
  const responsesToolPolicy =
    site.gateway_config?.responses_tool_policy ??
    (disableImageGenerationTool ? 'compatibility' : 'passthrough')

  return {
    responsesToolPolicy,
    disableResponsesImageGenerationTool: disableImageGenerationTool,
  }
}

function quotaProbeFromSite(site: Site): CreateSiteFormValues['quotaProbe'] {
  const value = site.gateway_config?.quota_probe
  if (value === 'sub2api' || value === 'newapi' || value === 'xlyra') return value
  return 'none'
}

function formValuesFromSite(site?: Site | null): CreateSiteFormValues {
  if (!site) {
    return defaultValues
  }

  return {
    ...defaultValues,
    name: site.name,
    baseUrl: site.base_url,
    siteType: site.site_type,
    newapiAccessToken: site.auth_config?.newapi?.access_token ?? '',
    newapiUserId: site.auth_config?.newapi?.user_id ? String(site.auth_config.newapi.user_id) : '',
    xlyraAuthMode: site.auth_config?.xlyra?.auth_mode ?? 'api_key',
    xlyraAccessToken: site.auth_config?.xlyra?.access_token ?? '',
    routingPriority: formatRoutingPriorityInput(site.routing_priority),
    enabled: site.enabled,
    proxyId: site.proxy_id || 'direct',
    ...responsesToolConfigFromSite(site),
    quotaProbe: quotaProbeFromSite(site),
    impersonateCodexClient: site.gateway_config?.impersonate_codex_client ?? false,
    impersonateClaudeCodeClient: site.gateway_config?.impersonate_claude_code_client ?? false,
    siteGroupIds: [],
    requestHeaders: readRequestHeadersFromMeta(site.meta),
  }
}

function siteGroupIdsForSite(site: Site | null | undefined, groups: SiteGroup[]) {
  if (!site) return []
  return groups
    .filter((group) => group.sites.some((item) => item.site_id === site.id))
    .map((group) => group.id)
}

function normalizeComparableURL(value: string) {
  return value.trim().replace(/\/+$/, '')
}

function shouldSkipRefreshOnUpdate(values: CreateSiteFormValues, initialSite?: Site | null, siteTypes?: SiteTypeInfo[]) {
  if (!initialSite) return false

  const credType = credentialTypeOf(values.siteType, siteTypes)
  const siteTypeUnchanged = values.siteType === initialSite.site_type
  const baseURLUnchanged = normalizeComparableURL(values.baseUrl) === normalizeComparableURL(initialSite.base_url)

  if (!siteTypeUnchanged || !baseURLUnchanged) return false

  if (credType === 'system_token') {
    const currentUserID = initialSite.auth_config?.newapi?.user_id
    return (
      values.newapiAccessToken.trim() === (initialSite.auth_config?.newapi?.access_token ?? '') &&
      values.newapiUserId.trim() === (currentUserID ? String(currentUserID) : '')
    )
  }

  if (credType === 'api_key') {
    return values.apiKey.trim() === ''
  }

  if (credType === 'xlyra') {
    const initialMode = initialSite.auth_config?.xlyra?.auth_mode ?? 'api_key'
    if (values.xlyraAuthMode !== initialMode) return false
    if (values.xlyraAuthMode === 'access_token') {
      return values.xlyraAccessToken.trim() === (initialSite.auth_config?.xlyra?.access_token ?? '')
    }
    return values.apiKey.trim() === ''
  }

  return true
}

function toSlug(input: string) {
  const normalized = input
    .normalize('NFKC')
    .toLowerCase()
    .trim()
    .replace(/[^\p{L}\p{N}]+/gu, '')
  return Array.from(normalized)
    .slice(0, 48)
    .join('')
}

function FieldHint({ children }: { children?: string }) {
  if (!children) return null
  return <p className="text-muted-soft text-xs leading-5">{children}</p>
}

function FieldError({ children }: { children?: string }) {
  if (!children) return null
  return <p className="text-xs leading-5 text-red-300">{children}</p>
}

function FieldWarning({ children }: { children?: string }) {
  if (!children) return null
  return <p className="text-xs leading-5 text-red-400">{children}</p>
}

function FormField({
  label,
  action,
  hint,
  warning,
  error,
  children,
}: {
  label: string
  action?: ReactNode
  hint?: string
  warning?: string
  error?: string
  children: React.ReactNode
}) {
  return (
    <div className="space-y-2">
      <div
        className={cn(
          'flex items-center justify-between gap-3',
          action && 'min-h-8',
        )}
      >
        <span className="block text-sm font-medium">{label}</span>
        {action}
      </div>
      {children}
      {error ? <FieldError>{error}</FieldError> : warning ? <FieldWarning>{warning}</FieldWarning> : hint ? <FieldHint>{hint}</FieldHint> : null}
    </div>
  )
}

function FormSection({
  title,
  divided,
  children,
}: {
  title: ReactNode
  divided?: boolean
  children: ReactNode
}) {
  return (
    <section className={cn('space-y-4 py-5', divided ? 'border-t border-[hsl(var(--glass-divider))]' : 'pt-0')}>
      {typeof title === 'string' ? <h3 className="text-sm font-semibold text-foreground">{title}</h3> : title}
      <div className="space-y-4">{children}</div>
    </section>
  )
}

function ProviderWall({ siteTypes, onSelect, language, t }: { siteTypes: SiteTypeInfo[]; onSelect: (siteType: string) => void; language: string; t: (key: string) => string }) {
  const resolvedMode = useResolvedTheme()
  const groups = providerGroups(siteTypes.filter((st) => st.show_in_create_dialog), t)
  return (
    <div className="space-y-5">
      {groups.map((group) => (
        <section key={group.title} className="space-y-2.5 border-t border-[hsl(var(--glass-divider))] pt-5 first:border-t-0 first:pt-0">
          <div className="text-muted-soft text-xs font-medium">{group.title}</div>
          <div className="space-y-2">
            {group.items.map((st) => {
              const icon = siteTypeIconPath(st.site_type, resolvedMode) ?? st.icon_url
              const subtitle = providerSubtitle(st, language, t)
              return (
                <button
                  key={st.site_type}
                  type="button"
                  className="flex w-full items-center gap-3 rounded-lg border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-panel))] px-4 py-3 text-left hover:bg-[hsl(var(--surface-subtle))] transition-colors cursor-pointer"
                  onClick={() => onSelect(st.site_type)}
                >
                  {icon ? (
                    <img
                      src={icon}
                      alt=""
                      className={cn('h-8 w-8 shrink-0 rounded-md object-contain', siteTypeIconClassName(st.site_type))}
                    />
                  ) : (
                    <div className="h-8 w-8 shrink-0 rounded-md bg-[hsl(var(--surface-soft))] flex items-center justify-center text-sm font-medium text-muted-soft">
                      {st.display_name.charAt(0)}
                    </div>
                  )}
                  <div className="min-w-0 flex-1">
                    <div className="text-sm font-semibold text-foreground">{st.display_name}</div>
                    {subtitle ? (
                      <div className="truncate text-xs text-muted-soft">{subtitle}</div>
                    ) : null}
                  </div>
                </button>
              )
            })}
          </div>
        </section>
      ))}
    </div>
  )
}

function providerSubtitle(siteType: SiteTypeInfo, language: string, t: (key: string) => string) {
  const key = `form.providerSubtitles.${siteType.site_type}`
  const translated = t(key)
  if (translated !== key) return translated
  if (!language.startsWith('zh') && /[\u4e00-\u9fff]/.test(siteType.subtitle)) return undefined
  return siteType.subtitle
}

function providerGroups(providers: SiteTypeInfo[], t: (key: string) => string) {
  const official = providers.filter((st) => st.credential_type === 'api_key' || st.site_type === 'grok')
  const relay = providers
    .filter((st) => st.site_type === 'xlyra' || st.site_type === 'newapi' || st.credential_type === 'system_token')
    .sort((a, b) => relayProviderRank(a.site_type) - relayProviderRank(b.site_type))
  const rest = providers.filter((st) => !official.includes(st) && !relay.includes(st) && st.credential_type !== 'oauth')

  return [
    { title: t('form.categories.official'), items: official },
    { title: t('form.categories.relay'), items: relay },
    { title: t('form.categories.other'), items: rest },
  ].filter((group) => group.items.length > 0)
}

function relayProviderRank(siteType: string) {
  if (siteType === 'xlyra') return 0
  if (siteType === 'newapi') return 1
  return 2
}

export function SiteCreateSheet({
  open,
  onOpenChange,
  mode = 'create',
  initialSite,
  isPending,
  siteTypes,
  siteGroups = [],
  proxies = [],
  siteGroupsLoading = false,
  onSubmit,
}: SiteCreateSheetProps) {
  const { t, i18n } = useTranslation('sites')
  const resolver = useMemo(() => zodResolver(createSiteSchema(mode, siteTypes, t, initialSite)), [initialSite, mode, siteTypes, t])
  const lastFormResetKeyRef = useRef<string | null>(null)

  const {
    control,
    register,
    handleSubmit,
    reset,
    setValue,
    formState: { errors },
  } = useForm<CreateSiteFormValues>({
    resolver,
    defaultValues: {
      ...formValuesFromSite(initialSite),
      siteGroupIds: siteGroupIdsForSite(initialSite, siteGroups),
    },
  })

  const { fields, append, remove } = useFieldArray({ control, name: 'requestHeaders' })
  const [headersExpanded, setHeadersExpanded] = useState(false)
  const [apiKeyEditorOpen, setAPIKeyEditorOpen] = useState(false)
  const [editingAPIKeyIndex, setEditingAPIKeyIndex] = useState<number | null>(null)
  const [apiKeyDraft, setAPIKeyDraft] = useState<APIKeyFormDraft>(
    DEFAULT_API_KEY_FORM_DRAFT,
  )

  const siteType = useWatch({
    control,
    name: 'siteType',
  })
  const baseUrl = useWatch({
    control,
    name: 'baseUrl',
  })
  const responsesToolPolicy = useWatch({
    control,
    name: 'responsesToolPolicy',
  })
  const apiKeys = useWatch({ control, name: 'apiKeys' }) ?? []

  const nameField = register('name')
  const baseUrlField = register('baseUrl')
  const apiKeyField = register('apiKey')
  const newapiAccessTokenField = register('newapiAccessToken')
  const newapiUserIdField = register('newapiUserId')
  const xlyraAccessTokenField = register('xlyraAccessToken')
  const routingPriorityField = register('routingPriority')
  const showV1Hint = (baseUrl ?? '').trim().length > 0 && /\/v1\/?$/.test((baseUrl ?? '').trim())

  const currentCredType = credentialTypeOf(siteType, siteTypes)
  const showSystemTokenFields = currentCredType === 'system_token'
  const showXLyraFields = currentCredType === 'xlyra'
  const xlyraAuthMode = useWatch({ control, name: 'xlyraAuthMode' })
  const isGrok = siteType === 'grok'
  const canConfigureQuotaProbe = siteType === 'openai'
  const showAPIKeyField = !isGrok && currentCredType !== 'system_token' && currentCredType !== 'oauth' && currentCredType !== 'xlyra' && !!siteType && mode === 'create'
  const supportsAPIKeyCostMultiplier = siteType.trim().toLowerCase() === 'openai'
  const groupOptions = useMemo(() => siteGroups.filter((group) => group.enabled).map((group) => ({
    value: group.id,
    label: group.name,
    description: t('form.fields.groupSiteCount', { count: group.sites.length }),
  })), [siteGroups, t])

  const resolvedMode = useResolvedTheme()
  const selectedProvider = siteType || null
  const selectedProviderInfo = siteTypes?.find(st => st.site_type === selectedProvider)
  const selectedProviderIcon = selectedProvider
    ? siteTypeIconPath(selectedProvider, resolvedMode) ?? selectedProviderInfo?.icon_url
    : null
  const showBuiltinHint = !requiresBaseURL(siteType, siteTypes)
  const builtinBaseURL = selectedProviderInfo?.official_base_url?.trim()
  const baseURLHint = showBuiltinHint
    ? usesDomesticBuiltinBaseURL(siteType) && builtinBaseURL
      ? t('form.fields.baseUrlBuiltinDomesticHint', { url: builtinBaseURL })
      : builtinBaseURL
        ? t('form.fields.baseUrlBuiltinHint', { url: builtinBaseURL })
        : t('form.fields.baseUrlOptionalHint')
    : undefined
  const baseURLPlaceholder = showBuiltinHint && builtinBaseURL
    ? builtinBaseURL
    : t('form.fields.baseUrlPlaceholder')

  useEffect(() => {
    if (responsesToolPolicy === 'passthrough') {
      setValue('disableResponsesImageGenerationTool', false, { shouldDirty: true })
    }
  }, [responsesToolPolicy, setValue])

  useEffect(() => {
    if (!open) {
      lastFormResetKeyRef.current = null
      reset(defaultValues)
      return
    }

    if (mode === 'edit' && initialSite) {
      const resetKey = [
        mode,
        initialSite.id ?? 'new',
        initialSite.auth_config?.newapi?.access_token ?? '',
        initialSite.auth_config?.newapi?.user_id ?? '',
        initialSite.auth_config?.xlyra?.auth_mode ?? '',
        initialSite.auth_config?.xlyra?.access_token ?? '',
      ].join(':')
      if (lastFormResetKeyRef.current !== resetKey) {
        reset({
          ...formValuesFromSite(initialSite),
          siteGroupIds: siteGroupIdsForSite(initialSite, siteGroups),
        })
        lastFormResetKeyRef.current = resetKey
      }
    } else if (mode === 'create') {
      const resetKey = 'create'
      if (lastFormResetKeyRef.current !== resetKey) {
        reset(defaultValues)
        lastFormResetKeyRef.current = resetKey
      }
    }
  }, [initialSite, mode, open, reset, siteGroups])

  function handleSelectProvider(siteType: string) {
    setValue('siteType', siteType, { shouldDirty: true })
    setValue('apiKeys', [], { shouldDirty: false })
    if (siteType === 'grok' || !requiresBaseURL(siteType, siteTypes)) {
      setValue('baseUrl', '')
    }
  }

  function handleBackToProviders() {
    setValue('siteType', '', { shouldDirty: false })
    setValue('name', '', { shouldDirty: false })
    setValue('baseUrl', '', { shouldDirty: false })
    setValue('apiKey', '', { shouldDirty: false })
    setValue('apiKeys', [], { shouldDirty: false })
    setValue('newapiAccessToken', '', { shouldDirty: false })
    setValue('newapiUserId', '', { shouldDirty: false })
    setValue('xlyraAuthMode', 'api_key', { shouldDirty: false })
    setValue('xlyraAccessToken', '', { shouldDirty: false })
  }

  function openNewAPIKeyForm() {
    setEditingAPIKeyIndex(null)
    setAPIKeyDraft(DEFAULT_API_KEY_FORM_DRAFT)
    setAPIKeyEditorOpen(true)
  }

  function openExistingAPIKeyForm(index: number) {
    const apiKey = apiKeys[index]
    if (!apiKey) return
    setEditingAPIKeyIndex(index)
    setAPIKeyDraft({
      name: apiKey.name,
      apiKey: apiKey.apiKey,
      routingPriority: String(apiKey.routingPriority),
      upstreamCostMultiplier: String(apiKey.upstreamCostMultiplier ?? 1),
    })
    setAPIKeyEditorOpen(true)
  }

  function saveAPIKeyDraft() {
    const parsed = parseSiteAPIKeyForm(apiKeyDraft, {
      includeCostMultiplier: supportsAPIKeyCostMultiplier,
      requireAPIKey: true,
    })
    if (!parsed?.apiKey) return
    const nextAPIKey = {
      apiKey: parsed.apiKey,
      name: parsed.name,
      routingPriority: parsed.routingPriority,
      upstreamCostMultiplier: parsed.upstreamCostMultiplier,
    }
    const next = [...apiKeys]
    if (editingAPIKeyIndex == null) {
      next.push(nextAPIKey)
    } else {
      next[editingAPIKeyIndex] = nextAPIKey
    }
    setValue('apiKeys', next, {
      shouldDirty: true,
      shouldValidate: true,
    })
    setAPIKeyEditorOpen(false)
    setEditingAPIKeyIndex(null)
    setAPIKeyDraft(DEFAULT_API_KEY_FORM_DRAFT)
  }

  async function handleFormSubmit(values: CreateSiteFormValues) {
    const slug = initialSite?.slug ?? (toSlug(values.name) || toSlug(values.baseUrl) || 'site')
    const credType = credentialTypeOf(values.siteType, siteTypes)
    const selectedProxyID = values.proxyId

    await onSubmit({
      name: values.name.trim(),
      slug,
      baseUrl: values.baseUrl.trim(),
      siteType: values.siteType as CreateSiteInput['siteType'],
      enabled: values.enabled,
      routingPriority: Number(values.routingPriority),
      proxyId: selectedProxyID === 'direct' ? null : selectedProxyID,
      apiKeys:
        mode === 'create' && credType === 'api_key'
          ? values.apiKeys
          : undefined,
      newapi:
        credType === 'system_token'
          ? {
              accessToken: values.newapiAccessToken.trim(),
              userId: Number(values.newapiUserId),
            }
          : undefined,
      xlyra:
        credType === 'xlyra'
          ? {
              authMode: values.xlyraAuthMode,
              accessToken: values.xlyraAuthMode === 'access_token' ? values.xlyraAccessToken.trim() : undefined,
              apiKey: values.xlyraAuthMode === 'api_key' ? values.apiKey.trim() || undefined : undefined,
            }
          : undefined,
      requestHeaders: values.requestHeaders.filter((h) => h.key.trim()),
      gateway: values.siteType === 'grok'
        ? undefined
        : {
            responsesToolPolicy: values.responsesToolPolicy,
            disabledResponsesTools:
              values.responsesToolPolicy === 'compatibility' && values.disableResponsesImageGenerationTool
                ? ['image_generation']
                : [],
            impersonateCodexClient: values.impersonateCodexClient,
            impersonateClaudeCodeClient: values.impersonateClaudeCodeClient,
            quotaProbe: values.siteType === 'openai' ? values.quotaProbe : 'none',
          },
      siteGroupIds: values.siteGroupIds,
      skipRefresh: mode === 'edit' && shouldSkipRefreshOnUpdate(values, initialSite, siteTypes),
    })
  }

  const showProviderWall = mode === 'create' && !selectedProvider

  return (
    <>
      <Draw open={open} onOpenChange={onOpenChange}>
      <DrawContent side="right" size="wide" onOpenAutoFocus={(event) => event.preventDefault()}>
        <DrawHeader>
          {selectedProvider && mode === 'create' ? (
            <div className="flex items-center gap-2">
              <Button
                variant="ghost"
                size="icon"
                className="h-7 w-7 -ml-1"
                onClick={handleBackToProviders}
              >
                <ChevronLeft className="h-4 w-4" />
              </Button>
              {selectedProviderIcon ? (
                <img
                  src={selectedProviderIcon}
                  alt=""
                  className={cn('h-6 w-6 shrink-0 rounded-md object-contain', siteTypeIconClassName(selectedProvider))}
                />
              ) : null}
              <DrawTitle>
                {selectedProviderInfo?.display_name ?? t('form.createTitle')}
              </DrawTitle>
            </div>
          ) : (
            <DrawTitle>{mode === 'edit' ? t('form.editTitle') : t('form.createTitle')}</DrawTitle>
          )}
        </DrawHeader>

        <DrawBody className="space-y-0">
          {showProviderWall ? (
            <ProviderWall siteTypes={siteTypes ?? []} onSelect={handleSelectProvider} language={i18n.language} t={t} />
          ) : (
            <>
              <FormSection title={t('form.sections.basic')}>
                <FormField label={t('form.fields.name')} error={errors.name?.message}>
                  <Input
                    placeholder={t('form.fields.namePlaceholder')}
                    {...nameField}
                  />
                </FormField>

                <Controller
                  control={control}
                  name="enabled"
                  render={({ field }) => (
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                      label={t('form.fields.enableRouting')}
                      description={t('form.fields.enableRoutingDesc')}
                    />
                  )}
                />
              </FormSection>

              {!isGrok ? <FormSection title={t('form.sections.connection')} divided>
                <FormField
                  label={t('form.fields.baseUrl')}
                  warning={
                    showV1Hint
                      ? t('form.validation.urlContainsV1')
                      : undefined
                  }
                  hint={
                    baseURLHint
                  }
                  error={errors.baseUrl?.message}
                >
                  <Input
                    placeholder={baseURLPlaceholder}
                    {...baseUrlField}
                  />
                </FormField>
              </FormSection> : null}

              {isGrok && mode === 'create' ? (
                <FormSection title={t('form.sections.grokAccounts')} divided>
                  <p className="text-sm text-muted-soft">{t('form.grokDeviceHint')}</p>
                </FormSection>
              ) : null}

              {(showSystemTokenFields || showAPIKeyField || showXLyraFields) ? (
                <FormSection title={t('form.sections.auth')} divided>
                  {showSystemTokenFields ? (
                    <>
                      <FormField label={t('form.fields.systemToken')} error={errors.newapiAccessToken?.message}>
                        <Input
                          autoComplete="off"
                          placeholder={t('form.fields.systemTokenPlaceholder')}
                          {...newapiAccessTokenField}
                        />
                      </FormField>

                      <FormField label={t('form.fields.userId')} error={errors.newapiUserId?.message}>
                        <Input
                          inputMode="numeric"
                          placeholder={t('form.fields.userIdPlaceholder')}
                          {...newapiUserIdField}
                        />
                      </FormField>
                    </>
                  ) : null}

                  {showXLyraFields ? (
                    <>
                      <Controller
                        control={control}
                        name="xlyraAuthMode"
                        render={({ field }) => (
                          <FormField label={t('form.fields.xlyraAuthMode')}>
                            <Select value={field.value} onValueChange={field.onChange}>
                              <SelectTrigger>
                                <SelectValue />
                              </SelectTrigger>
                              <SelectContent searchable={false}>
                                <SelectItem value="access_token">{t('form.fields.xlyraAccessTokenMode')}</SelectItem>
                                <SelectItem value="api_key">{t('form.fields.xlyraApiKeyMode')}</SelectItem>
                              </SelectContent>
                            </Select>
                          </FormField>
                        )}
                      />

                      {xlyraAuthMode === 'access_token' ? (
                        <FormField
                          label={t('form.fields.xlyraAccessToken')}
                          hint={t('form.fields.xlyraAccessTokenHint')}
                          error={errors.xlyraAccessToken?.message}
                        >
                          <Input
                            type="password"
                            autoComplete="off"
                            placeholder={t('form.fields.xlyraAccessTokenPlaceholder')}
                            {...xlyraAccessTokenField}
                          />
                        </FormField>
                      ) : (
                        <FormField
                          label={t('form.fields.xlyraApiKey')}
                          hint={mode === 'edit' ? t('form.fields.apiKeyEditHint') : t('form.fields.xlyraApiKeyHint')}
                          error={errors.apiKey?.message}
                        >
                          <Input
                            type="password"
                            autoComplete="off"
                            placeholder={mode === 'edit' ? t('form.fields.apiKeyPlaceholder') : 'sk-your-api-key'}
                            {...apiKeyField}
                          />
                        </FormField>
                      )}
                    </>
                  ) : null}

                  {showAPIKeyField ? (
                    <FormField
                      label={t('form.fields.apiKey')}
                      error={errors.apiKeys?.message}
                      action={
                        apiKeys.length ? (
                          <Button
                            type="button"
                            size="sm"
                            variant="outline"
                            className="h-8"
                            onClick={openNewAPIKeyForm}
                          >
                            <Plus className="h-3.5 w-3.5" />
                            {t('apiKeys.add')}
                          </Button>
                        ) : null
                      }
                    >
                      {apiKeys.length ? (
                        <div className="space-y-2">
                          {apiKeys.map((apiKey, index) => {
                            const keyName =
                              apiKey.name || t('apiKeys.defaultKey')
                            return (
                              <div
                                key={`${index}:${apiKey.name}`}
                                className="group flex min-h-14 items-center justify-between gap-3 rounded-md border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-panel))] px-3 py-2.5 transition-colors hover:bg-[hsl(var(--surface-subtle))]"
                              >
                                <div className="flex min-w-0 items-center gap-3">
                                  <div className="flex size-8 shrink-0 items-center justify-center rounded-md bg-[hsl(var(--surface-subtle))] text-muted-soft">
                                    <KeyRound className="h-4 w-4" />
                                  </div>
                                  <div className="min-w-0">
                                    <div className="truncate text-sm font-medium text-foreground">
                                      {keyName}
                                    </div>
                                    <div className="mt-0.5 flex flex-wrap items-center gap-x-2 text-xs text-muted-soft">
                                      <span>
                                        {t('apiKeys.routingPriority')} {apiKey.routingPriority}
                                      </span>
                                      {supportsAPIKeyCostMultiplier ? (
                                        <span>
                                          {t('apiKeys.costMultiplier')} {apiKey.upstreamCostMultiplier ?? 1}x
                                        </span>
                                      ) : null}
                                    </div>
                                  </div>
                                </div>
                                <div className="flex shrink-0 items-center gap-1">
                                  <Button
                                    type="button"
                                    size="icon"
                                    variant="ghost"
                                    className="h-8 w-8 text-muted-soft hover:text-foreground"
                                    title={t('apiKeys.editConfig')}
                                    aria-label={t('apiKeys.editConfigLabel', {
                                      name: keyName,
                                    })}
                                    onClick={() =>
                                      openExistingAPIKeyForm(index)
                                    }
                                  >
                                    <PencilLine className="h-3.5 w-3.5" />
                                  </Button>
                                  <Button
                                    type="button"
                                    size="icon"
                                    variant="ghost"
                                    className="h-8 w-8 text-muted-soft hover:bg-red-500/10 hover:text-red-400"
                                    title={t('apiKeys.delete')}
                                    aria-label={t('apiKeys.deleteLabel', {
                                      name: keyName,
                                    })}
                                    onClick={() => {
                                      setValue(
                                        'apiKeys',
                                        apiKeys.filter(
                                          (_item, itemIndex) =>
                                            itemIndex !== index,
                                        ),
                                        {
                                          shouldDirty: true,
                                          shouldValidate: true,
                                        },
                                      )
                                    }}
                                  >
                                    <Trash2 className="h-3.5 w-3.5" />
                                  </Button>
                                </div>
                              </div>
                            )
                          })}
                        </div>
                      ) : (
                        <Button
                          type="button"
                          variant="outline"
                          className="h-12 w-full border-dashed text-muted-soft hover:text-foreground"
                          onClick={openNewAPIKeyForm}
                        >
                          <Plus className="h-4 w-4" />
                          {t('apiKeys.add')}
                        </Button>
                      )}
                    </FormField>
                  ) : null}
                </FormSection>
              ) : null}

              <FormSection title={t('form.sections.groups')} divided>
                <Controller
                  control={control}
                  name="siteGroupIds"
                  render={({ field }) => (
                    <FormField label={t('form.fields.siteGroups')} hint={t('form.fields.siteGroupsHint')}>
                      <MultiSelect
                        value={field.value}
                        options={groupOptions}
                        placeholder={t('form.fields.siteGroupsPlaceholder')}
                        searchPlaceholder={t('form.fields.siteGroupsSearch')}
                        emptyText={t('form.fields.noSiteGroups')}
                        disabled={siteGroupsLoading}
                        onChange={field.onChange}
                      />
                    </FormField>
                  )}
                />
              </FormSection>

              <FormSection title={t('form.sections.routing')} divided>
                <FormField
                  label={t('form.fields.routingPriority')}
                  hint={t('form.fields.routingPriorityHint')}
                  error={errors.routingPriority?.message}
                >
                  <Input
                    inputMode="decimal"
                    min="1"
                    max="5"
                    step="0.1"
                    placeholder="1.0"
                    {...routingPriorityField}
                  />
                </FormField>

                {(
                  <Controller
                    control={control}
                    name="proxyId"
                    render={({ field }) => (
                      <FormField label={t('form.fields.proxy')} hint={t('form.fields.proxyDesc')}>
                        <Select value={field.value || 'direct'} onValueChange={field.onChange}>
                          <SelectTrigger>
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent searchable={false}>
                            <SelectItem value="direct">{t('form.fields.proxyDirect')}</SelectItem>
                            {proxies.map((proxy) => (
                              <SelectItem key={proxy.id} value={proxy.id}>
                                {proxy.name}
                              </SelectItem>
                            ))}
                          </SelectContent>
                        </Select>
                      </FormField>
                    )}
                  />
                )}
              </FormSection>

              <FormSection
                title={(
                  <button
                    type="button"
                    className="flex w-full items-center gap-1.5 text-left text-sm font-semibold text-foreground transition-colors hover:text-foreground"
                    onClick={() => setHeadersExpanded((v) => !v)}
                  >
                    <ChevronDown className={cn('h-4 w-4 transition-transform', headersExpanded ? 'rotate-0' : '-rotate-90')} />
                    {t('form.actions.advanced')}
                  </button>
                )}
                divided
              >

                {headersExpanded ? (
                  <div className="space-y-4">
                    {!isGrok ? (
                      <>
                        {canConfigureQuotaProbe ? (
                          <Controller
                            control={control}
                            name="quotaProbe"
                            render={({ field }) => (
                              <FormField
                                label={t('form.fields.quotaProbe')}
                                hint={t('form.fields.quotaProbeDesc')}
                              >
                                <Select value={field.value} onValueChange={field.onChange}>
                                  <SelectTrigger>
                                    <SelectValue />
                                  </SelectTrigger>
                                  <SelectContent searchable={false}>
                                    <SelectItem value="none">{t('form.fields.quotaProbeNone')}</SelectItem>
                                    <SelectItem value="xlyra">xLyra</SelectItem>
                                    <SelectItem value="sub2api">Sub2API</SelectItem>
                                    <SelectItem value="newapi">NewAPI / OneAPI</SelectItem>
                                  </SelectContent>
                                </Select>
                              </FormField>
                            )}
                          />
                        ) : null}

                        <Controller
                          control={control}
                          name="responsesToolPolicy"
                          render={({ field }) => (
                            <FormField
                              label={t('form.fields.responsesToolPolicy')}
                              hint={t('form.fields.responsesToolPolicyDesc')}
                            >
                              <Select value={field.value} onValueChange={field.onChange}>
                                <SelectTrigger>
                                  <SelectValue />
                                </SelectTrigger>
                                <SelectContent searchable={false}>
                                  <SelectItem value="passthrough">{t('form.fields.responsesToolPolicyPassthrough')}</SelectItem>
                                  <SelectItem value="compatibility">{t('form.fields.responsesToolPolicyCompatibility')}</SelectItem>
                                </SelectContent>
                              </Select>
                            </FormField>
                          )}
                        />

                        {responsesToolPolicy === 'compatibility' ? (
                          <div className="space-y-3 rounded-lg border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-subtle))] px-4 py-3">
                            <div>
                              <span className="block text-sm font-medium">{t('form.fields.disabledResponsesTools')}</span>
                              <p className="text-muted-soft mt-1 text-xs">{t('form.fields.disabledResponsesToolsDesc')}</p>
                            </div>
                            <Controller
                              control={control}
                              name="disableResponsesImageGenerationTool"
                              render={({ field }) => (
                                <Checkbox
                                  checked={field.value}
                                  onCheckedChange={field.onChange}
                                  label={t('form.fields.disableImageGenerationTool')}
                                  description={t('form.fields.disableImageGenerationToolDesc')}
                                />
                              )}
                            />
                            <FieldError>{errors.disableResponsesImageGenerationTool?.message}</FieldError>
                          </div>
                        ) : null}

                        <div className="space-y-3">
                          <div>
                            <span className="block text-sm font-medium">{t('form.fields.clientImpersonation')}</span>
                            <p className="text-muted-soft mt-1 text-xs">{t('form.fields.clientImpersonationDesc')}</p>
                          </div>
                          <div className="space-y-2">
                            <Controller
                              control={control}
                              name="impersonateCodexClient"
                              render={({ field }) => (
                                <Checkbox
                                  checked={field.value}
                                  onCheckedChange={field.onChange}
                                  label={t('form.fields.impersonateCodexClient')}
                                  description={t('form.fields.impersonateCodexClientDesc')}
                                />
                              )}
                            />
                            <Controller
                              control={control}
                              name="impersonateClaudeCodeClient"
                              render={({ field }) => (
                                <Checkbox
                                  checked={field.value}
                                  onCheckedChange={field.onChange}
                                  label={t('form.fields.impersonateClaudeCodeClient')}
                                  description={t('form.fields.impersonateClaudeCodeClientDesc')}
                                />
                              )}
                            />
                          </div>
                        </div>
                      </>
                    ) : null}

                    <div className="space-y-3">
                      <div>
                        <span className="block text-sm font-medium">{t('form.fields.requestHeaders')}</span>
                        <p className="text-muted-soft mt-1 text-xs">{t('form.fields.requestHeadersDesc')}</p>
                      </div>
                      <div className="rounded-lg border border-[hsl(var(--glass-border))]">
                        {fields.length ? (
                          <div className="p-3 space-y-2">
                            {fields.map((field, index) => (
                              <div key={field.id} className="flex items-center gap-2">
                                <Input
                                  placeholder={t('form.fields.headerKeyPlaceholder')}
                                  className="flex-1"
                                  {...register(`requestHeaders.${index}.key`)}
                                />
                                <Input
                                  placeholder={t('form.fields.headerValuePlaceholder')}
                                  className="flex-1"
                                  {...register(`requestHeaders.${index}.value`)}
                                />
                                <Button
                                  size="icon"
                                  variant="ghost"
                                  className="h-9 w-9 shrink-0 text-muted-soft hover:text-red-400"
                                  aria-label={t('form.actions.removeHeader')}
                                  onClick={() => remove(index)}
                                >
                                  <Trash2 className="h-4 w-4" />
                                </Button>
                              </div>
                            ))}
                            <Button
                              size="sm"
                              variant="outline"
                              onClick={() => append({ key: '', value: '' })}
                            >
                              <Plus className="h-3.5 w-3.5 mr-1" />{t('form.actions.addHeader')}
                            </Button>
                          </div>
                        ) : (
                          <div className="flex items-center justify-center h-24">
                            <Button
                              size="sm"
                              variant="outline"
                              onClick={() => append({ key: '', value: '' })}
                            >
                              <Plus className="h-3.5 w-3.5 mr-1" />{t('form.actions.addHeader')}
                            </Button>
                          </div>
                        )}
                      </div>
                    </div>
                  </div>
                ) : null}
              </FormSection>
            </>
          )}
        </DrawBody>

        {showProviderWall ? null : (
          <DrawFooter>
            <Button onClick={handleSubmit(handleFormSubmit)} disabled={isPending}>
              {isPending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : null}
              {mode === 'edit' ? t('form.actions.save') : t('form.actions.create')}
            </Button>
            <Button variant="ghost" onClick={() => onOpenChange(false)} disabled={isPending}>
              {t('form.actions.cancel')}
            </Button>
          </DrawFooter>
        )}
      </DrawContent>
      </Draw>

      <Draw
        open={apiKeyEditorOpen}
        onOpenChange={(next) => {
          setAPIKeyEditorOpen(next)
          if (!next) {
            setEditingAPIKeyIndex(null)
            setAPIKeyDraft(DEFAULT_API_KEY_FORM_DRAFT)
          }
        }}
      >
        <DrawContent side="right" size="wide">
          <DrawHeader>
            <div className="flex items-center gap-2">
              <Button
                type="button"
                variant="ghost"
                size="icon"
                className="-ml-1 h-7 w-7"
                title={t('apiKeys.back')}
                aria-label={t('apiKeys.back')}
                onClick={() => setAPIKeyEditorOpen(false)}
              >
                <ChevronLeft className="h-4 w-4" />
              </Button>
              <DrawTitle>
                {editingAPIKeyIndex == null
                  ? t('apiKeys.addTitle')
                  : t('apiKeys.editConfig')}
              </DrawTitle>
            </div>
          </DrawHeader>
          <DrawBody>
            <SiteAPIKeyFormFields
              draft={apiKeyDraft}
              onChange={setAPIKeyDraft}
              showAPIKey
              showCostMultiplier={supportsAPIKeyCostMultiplier}
              t={t}
            />
          </DrawBody>
          <DrawFooter>
            <Button
              type="button"
              onClick={saveAPIKeyDraft}
              disabled={
                !parseSiteAPIKeyForm(apiKeyDraft, {
                  includeCostMultiplier: supportsAPIKeyCostMultiplier,
                  requireAPIKey: true,
                })
              }
            >
              {t('apiKeys.save')}
            </Button>
            <Button
              type="button"
              variant="ghost"
              onClick={() => setAPIKeyEditorOpen(false)}
            >
              {t('apiKeys.cancel')}
            </Button>
          </DrawFooter>
        </DrawContent>
      </Draw>
    </>
  )
}
