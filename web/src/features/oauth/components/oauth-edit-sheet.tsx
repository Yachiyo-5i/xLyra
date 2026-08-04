import { useMemo, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { LoaderCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Draw, DrawBody, DrawContent, DrawFooter, DrawHeader, DrawTitle } from '@/components/ui/draw'
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
import { cn } from '@/lib/utils'
import { toast } from '@/lib/toast'
import { updateSite, type Site } from '@/features/sites/api/sites'
import { listSiteGroups, siteGroupQueryKeys, updateSiteGroupMembershipForSite, type SiteGroup } from '@/features/settings/api/site-groups'
import { fetchSystemProxyConfig } from '@/features/settings/api/settings'

const EMPTY_SITE_GROUPS: SiteGroup[] = []

function FieldHint({ children }: { children?: string }) {
  if (!children) return null
  return <p className="text-muted-soft text-xs leading-5">{children}</p>
}

function FieldError({ children }: { children?: string }) {
  if (!children) return null
  return <p className="text-xs leading-5 text-red-300">{children}</p>
}

function FormField({
  label,
  hint,
  error,
  children,
}: {
  label: string
  hint?: string
  error?: string
  children: React.ReactNode
}) {
  return (
    <div className="space-y-2">
      <span className="block text-sm font-medium">{label}</span>
      {children}
      {error ? <FieldError>{error}</FieldError> : hint ? <FieldHint>{hint}</FieldHint> : null}
    </div>
  )
}

function FormSection({
  title,
  divided,
  children,
}: {
  title: React.ReactNode
  divided?: boolean
  children: React.ReactNode
}) {
  return (
    <section className={cn('space-y-4 py-5', divided ? 'border-t border-[hsl(var(--glass-divider))]' : 'pt-0')}>
      {typeof title === 'string' ? <h3 className="text-sm font-semibold text-foreground">{title}</h3> : title}
      <div className="space-y-4">{children}</div>
    </section>
  )
}

export function OAuthEditSheet({
  open,
  site,
  email,
  onOpenChange,
  onSaved,
}: {
  open: boolean
  site: Site | null
  email?: string | null
  onOpenChange: (open: boolean) => void
  onSaved: () => void
}) {
  const { t } = useTranslation('oauth')

  return (
    <Draw open={open} onOpenChange={onOpenChange}>
      <DrawContent side="right" size="wide" onOpenAutoFocus={(event) => event.preventDefault()}>
        <DrawHeader>
          <DrawTitle>{t('edit.title')}</DrawTitle>
        </DrawHeader>
        {site ? (
          <OAuthEditForm
            key={site.id}
            site={site}
            email={email}
            onOpenChange={onOpenChange}
            onSaved={onSaved}
          />
        ) : null}
      </DrawContent>
    </Draw>
  )
}

function OAuthEditForm({
  site,
  email,
  onOpenChange,
  onSaved,
}: {
  site: Site
  email?: string | null
  onOpenChange: (open: boolean) => void
  onSaved: () => void
}) {
  const { t } = useTranslation('oauth')
  const queryClient = useQueryClient()
  const [routingPriority, setRoutingPriority] = useState(site.routing_priority != null ? site.routing_priority.toFixed(1) : '1.0')
  const [enabled, setEnabled] = useState(site.enabled ?? true)
  const [proxyId, setProxyId] = useState(site.proxy_id || 'direct')
  const [saving, setSaving] = useState(false)
  const [priorityError, setPriorityError] = useState<string>()
  const groupsQuery = useQuery({ queryKey: siteGroupQueryKeys.list(), queryFn: listSiteGroups })
  const systemProxyQuery = useQuery({ queryKey: ['settings', 'system-proxy'], queryFn: fetchSystemProxyConfig })
  const groups = groupsQuery.data?.items ?? EMPTY_SITE_GROUPS
  const proxies = systemProxyQuery.data?.proxies ?? []
  const initialSiteGroupIds = useMemo(() => groups
    .filter((group) => group.sites.some((item) => item.site_id === site.id))
    .map((group) => group.id), [groups, site.id])
  const [editedSiteGroupIds, setEditedSiteGroupIds] = useState<string[] | null>(null)
  const siteGroupIds = editedSiteGroupIds ?? initialSiteGroupIds
  const groupOptions = useMemo(() => groups.filter((group) => group.enabled).map((group) => ({
    value: group.id,
    label: group.name,
    description: t('edit.fields.groupSiteCount', { count: group.sites.length }),
  })), [groups, t])

  const handleSiteGroupIdsChange = (nextIds: string[]) => {
    setEditedSiteGroupIds(nextIds)
  }

  function validate(): boolean {
    const priority = parseFloat(routingPriority)
    if (!routingPriority.trim()) {
      setPriorityError(t('edit.validation.priorityRequired'))
      return false
    }
    if (!/^\d+(\.\d)?$/.test(routingPriority.trim())) {
      setPriorityError(t('edit.validation.priorityFormat'))
      return false
    }
    if (!Number.isFinite(priority) || priority < 1 || priority > 5) {
      setPriorityError(t('edit.validation.priorityRange'))
      return false
    }
    setPriorityError(undefined)
    return true
  }

  const handleSave = async () => {
    if (!validate()) return
    setSaving(true)
    try {
      await updateSite(site.id, {
        name: site.name,
        slug: site.slug,
        siteType: site.site_type,
        baseUrl: site.base_url,
        enabled,
        routingPriority: parseFloat(routingPriority),
        proxyId: proxyId === 'direct' ? null : proxyId,
        skipRefresh: true,
      })
      await updateSiteGroupMembershipForSite(groups, site.id, siteGroupIds)
      await queryClient.invalidateQueries({ queryKey: siteGroupQueryKeys.list() })
      toast.success(t('edit.toast.saved'))
      onSaved()
      onOpenChange(false)
    } catch (error) {
      toast.error(t('edit.toast.saveFailed'), { description: error instanceof Error ? error.message : String(error) })
    } finally {
      setSaving(false)
    }
  }

  return (
    <>
      <DrawBody className="space-y-0">
        <FormSection title={t('edit.sections.basic')}>
          <FormField label={t('edit.fields.siteName')}>
            <Input value={site.name} disabled />
          </FormField>

          <Switch
            checked={enabled}
            onCheckedChange={setEnabled}
            label={t('edit.fields.enableRouting')}
            description={t('edit.fields.enableRoutingDesc')}
          />

          <FormField label={t('edit.fields.siteType')}>
            <Input value={site.site_type} disabled />
          </FormField>

          <FormField label={t('edit.fields.account')}>
            <Input value={email ?? ''} disabled />
          </FormField>
        </FormSection>

        <FormSection title={t('edit.sections.groups')} divided>
          <FormField label={t('edit.fields.siteGroups')} hint={t('edit.fields.siteGroupsHint')}>
            <MultiSelect
              value={siteGroupIds}
              options={groupOptions}
              placeholder={t('edit.fields.siteGroupsPlaceholder')}
              searchPlaceholder={t('edit.fields.siteGroupsSearch')}
              emptyText={t('edit.fields.noSiteGroups')}
              disabled={groupsQuery.isLoading}
              onChange={handleSiteGroupIdsChange}
            />
          </FormField>
        </FormSection>

        <FormSection title={t('edit.sections.routing')} divided>
          <FormField
            label={t('edit.fields.routingPriority')}
            hint={t('edit.fields.routingPriorityHint')}
            error={priorityError}
          >
            <Input
              inputMode="decimal"
              min="1"
              max="5"
              step="0.1"
              value={routingPriority}
              onChange={(e) => setRoutingPriority(e.target.value)}
              placeholder="1.0"
            />
          </FormField>

          <FormField label={t('edit.fields.proxy')} hint={t('edit.fields.proxyDesc')}>
            <Select value={proxyId || 'direct'} onValueChange={setProxyId}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent searchable={false}>
                <SelectItem value="direct">{t('edit.fields.proxyDirect')}</SelectItem>
                {proxies.map((proxy) => (
                  <SelectItem key={proxy.id} value={proxy.id}>
                    {proxy.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </FormField>
        </FormSection>
      </DrawBody>
      <DrawFooter>
        <Button onClick={handleSave} disabled={saving}>
          {saving ? <LoaderCircle className="h-4 w-4 animate-spin" /> : null}
          {t('edit.actions.save')}
        </Button>
        <Button variant="ghost" onClick={() => onOpenChange(false)} disabled={saving}>
          {t('edit.actions.cancel')}
        </Button>
      </DrawFooter>
    </>
  )
}
