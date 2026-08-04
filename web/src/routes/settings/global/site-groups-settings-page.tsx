import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { LoaderCircle, PencilLine, Plus, Search, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { ErrorState } from '@/components/common/error-state'
import { toSlug } from '@/lib/slug'
import { Button } from '@/components/ui/button'
import { Dialog, DialogBody, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Draw, DrawBody, DrawContent, DrawFooter, DrawHeader, DrawTitle } from '@/components/ui/draw'
import { FormField } from '@/components/ui/form-field'
import { Input } from '@/components/ui/input'
import { MultiSelect } from '@/components/ui/multi-select'
import { Switch } from '@/components/ui/switch'
import { toast } from '@/lib/toast'
import { cn } from '@/lib/utils'
import { downstreamAPIKeyQueryKeys } from '@/features/api-keys/api/api-keys'
import { listSitesWithOAuth, sitesQueryKeys, type Site } from '@/features/sites/api/sites'
import { sortSitesByName } from '@/features/sites/lib/site-utils'
import {
  createSiteGroup,
  deleteSiteGroup,
  listSiteGroups,
  siteGroupQueryKeys,
  updateSiteGroup,
  type SiteGroup,
} from '@/features/settings/api/site-groups'

const EMPTY_GROUPS: SiteGroup[] = []
const EMPTY_SITES: Site[] = []

type SiteGroupFormValues = {
  name: string
  description: string
  enabled: boolean
  sortOrder: string
  siteIds: string[]
}

const emptyForm: SiteGroupFormValues = {
  name: '',
  description: '',
  enabled: true,
  sortOrder: '0',
  siteIds: [],
}

export function SiteGroupsSettingsPage() {
  const { t } = useTranslation('settings')
  const queryClient = useQueryClient()
  const [search, setSearch] = useState('')
  const [formOpen, setFormOpen] = useState(false)
  const [editingGroup, setEditingGroup] = useState<SiteGroup | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<SiteGroup | null>(null)
  const [form, setForm] = useState<SiteGroupFormValues>(emptyForm)

  const groupsQuery = useQuery({ queryKey: siteGroupQueryKeys.list(), queryFn: listSiteGroups })
  const sitesQuery = useQuery({ queryKey: sitesQueryKeys.listWithOAuth(), queryFn: listSitesWithOAuth })
  const groups = groupsQuery.data?.items ?? EMPTY_GROUPS
  const sites = useMemo(() => sortSitesByName(sitesQuery.data?.items ?? EMPTY_SITES), [sitesQuery.data?.items])

  const siteOptions = useMemo(() => sites.map((site) => ({
    value: site.id,
    label: site.name,
    description: `${site.site_type}${site.oauth_account?.email ? ` · ${site.oauth_account.email}` : ''}`,
  })), [sites])
  const sitesById = useMemo(() => new Map(sites.map((site) => [site.id, site])), [sites])
  const filteredGroups = useMemo(() => {
    const keyword = search.trim().toLowerCase()
    if (!keyword) return groups
    return groups.filter((group) => (
      `${group.name} ${group.description}`.toLowerCase().includes(keyword)
    ))
  }, [groups, search])

  const saveMutation = useMutation({
    mutationFn: async () => {
      const name = form.name.trim()
      if (!name) throw new Error(t('siteGroups.validation.nameRequired'))
      const sortOrder = Number(form.sortOrder || 0)
      if (!Number.isInteger(sortOrder)) throw new Error(t('siteGroups.validation.sortOrderInvalid'))
      const input = {
        name,
        slug: editingGroup?.slug || newGroupSlug(name),
        description: form.description.trim(),
        enabled: form.enabled,
        sortOrder,
        siteIds: form.siteIds,
      }
      return editingGroup ? updateSiteGroup(editingGroup.id, input) : createSiteGroup(input)
    },
    onSuccess: async () => {
      closeForm()
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: siteGroupQueryKeys.list() }),
        queryClient.invalidateQueries({ queryKey: sitesQueryKeys.all }),
        queryClient.invalidateQueries({ queryKey: downstreamAPIKeyQueryKeys.list() }),
      ])
      toast.success(t('siteGroups.toast.saved'))
    },
    onError: (error) => toast.error(t('siteGroups.toast.saveFailed'), { description: error.message }),
  })

  const deleteMutation = useMutation({
    mutationFn: (groupId: string) => deleteSiteGroup(groupId),
    onSuccess: async () => {
      setDeleteTarget(null)
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: siteGroupQueryKeys.list() }),
        queryClient.invalidateQueries({ queryKey: downstreamAPIKeyQueryKeys.list() }),
      ])
      toast.success(t('siteGroups.toast.deleted'))
    },
    onError: (error) => toast.error(t('siteGroups.toast.deleteFailed'), { description: error.message }),
  })

  function startCreate() {
    setEditingGroup(null)
    setForm(emptyForm)
    setFormOpen(true)
  }

  function startEdit(group: SiteGroup) {
    setEditingGroup(group)
    setForm({
      name: group.name,
      description: group.description,
      enabled: group.enabled,
      sortOrder: String(group.sort_order),
      siteIds: group.sites.map((site) => site.site_id),
    })
    setFormOpen(true)
  }

  function closeForm() {
    setFormOpen(false)
    setEditingGroup(null)
    setForm(emptyForm)
  }

  if (groupsQuery.isLoading) {
    return <p className="text-sm text-muted-soft">{t('siteGroups.loading')}</p>
  }

  if (groupsQuery.isError) {
    return <ErrorState title={t('siteGroups.loadFailed')} description={groupsQuery.error.message} />
  }

  return (
    <div className="max-w-3xl space-y-6">
      <div className="space-y-1.5">
        <h3 className="text-lg font-semibold text-foreground">{t('siteGroups.title')}</h3>
        <p className="text-sm text-muted-soft">{t('siteGroups.description')}</p>
      </div>

      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="relative min-w-0 sm:max-w-sm sm:flex-1">
          <Search className="pointer-events-none absolute left-3 top-1/2 z-10 h-4 w-4 -translate-y-1/2 text-muted-soft" />
          <Input value={search} onChange={(event) => setSearch(event.target.value)} placeholder={t('siteGroups.search')} className="pl-9" />
        </div>
        <Button onClick={startCreate}>
          <Plus className="h-4 w-4" />
          {t('siteGroups.actions.create')}
        </Button>
      </div>

      {filteredGroups.length ? (
        <div className="divide-y divide-[hsl(var(--divider))] border-y border-[hsl(var(--divider))]">
          {filteredGroups.map((group) => {
            const groupSites = sortSitesByName(group.sites.map((item) => sitesById.get(item.site_id)).filter(Boolean) as Site[])
            return (
              <div key={group.id} className="grid gap-3 py-4 md:grid-cols-[minmax(0,1fr)_auto]">
                <div className="min-w-0 space-y-2">
                  <div className="flex items-start justify-between gap-3">
                    <div className="min-w-0 flex flex-wrap items-center gap-2 pr-1">
                      <span className="min-w-0 truncate font-medium text-foreground">{group.name}</span>
                      <span className={cn('shrink-0 rounded-full px-2 py-0.5 text-xs', group.enabled ? 'bg-emerald-500/12 text-emerald-300' : 'bg-[hsl(var(--surface-subtle))] text-muted-soft')}>
                        {group.enabled ? t('siteGroups.enabled') : t('siteGroups.disabled')}
                      </span>
                    </div>
                    <div className="flex shrink-0 items-start gap-1 md:hidden">
                      <Button size="icon" variant="ghost" className="h-8 w-8" onClick={() => startEdit(group)} title={t('siteGroups.actions.edit')}>
                        <PencilLine className="h-4 w-4" />
                      </Button>
                      <Button size="icon" variant="ghost" className="h-8 w-8 text-red-500 hover:bg-red-500/10 hover:text-red-400" onClick={() => setDeleteTarget(group)} title={t('siteGroups.actions.delete')}>
                        <Trash2 className="h-4 w-4" />
                      </Button>
                    </div>
                  </div>
                  {group.description ? <p className="text-sm text-muted-soft">{group.description}</p> : null}
                  <div className="flex flex-wrap gap-1.5">
                    {groupSites.length ? groupSites.slice(0, 6).map((site) => (
                      <span key={site.id} className="rounded-md bg-[hsl(var(--surface-subtle))] px-2 py-1 text-xs text-muted-soft">{site.name}</span>
                    )) : <span className="text-xs text-muted-soft">{t('siteGroups.noSites')}</span>}
                    {groupSites.length > 6 ? <span className="rounded-md bg-[hsl(var(--surface-subtle))] px-2 py-1 text-xs text-muted-soft">+{groupSites.length - 6}</span> : null}
                  </div>
                </div>
                <div className="hidden items-start gap-1 md:flex">
                  <Button size="icon" variant="ghost" className="h-8 w-8" onClick={() => startEdit(group)} title={t('siteGroups.actions.edit')}>
                    <PencilLine className="h-4 w-4" />
                  </Button>
                  <Button size="icon" variant="ghost" className="h-8 w-8 text-red-500 hover:bg-red-500/10 hover:text-red-400" onClick={() => setDeleteTarget(group)} title={t('siteGroups.actions.delete')}>
                    <Trash2 className="h-4 w-4" />
                  </Button>
                </div>
              </div>
            )
          })}
        </div>
      ) : null}

      <Draw open={formOpen} onOpenChange={(open) => { if (!open && !saveMutation.isPending) closeForm() }}>
        <DrawContent side="right" size="wide" onOpenAutoFocus={(event) => event.preventDefault()}>
          <DrawHeader>
            <DrawTitle>{editingGroup ? t('siteGroups.form.editTitle') : t('siteGroups.form.createTitle')}</DrawTitle>
          </DrawHeader>
          <DrawBody className="space-y-4">
            <FormField label={t('siteGroups.form.name')} required>
              <Input value={form.name} onChange={(event) => setForm((current) => ({ ...current, name: event.target.value }))} />
            </FormField>
            <FormField label={t('siteGroups.form.description')}>
              <Input value={form.description} onChange={(event) => setForm((current) => ({ ...current, description: event.target.value }))} />
            </FormField>
            <FormField label={t('siteGroups.form.sortOrder')}>
              <Input inputMode="numeric" value={form.sortOrder} onChange={(event) => setForm((current) => ({ ...current, sortOrder: event.target.value }))} />
            </FormField>
            <FormField label={t('siteGroups.form.sites')}>
              <MultiSelect
                value={form.siteIds}
                options={siteOptions}
                placeholder={t('siteGroups.form.sitesPlaceholder')}
                searchPlaceholder={t('siteGroups.form.siteSearch')}
                emptyText={t('siteGroups.noSites')}
                disabled={sitesQuery.isLoading}
                onChange={(siteIds) => setForm((current) => ({ ...current, siteIds }))}
              />
            </FormField>
            <Switch
              checked={form.enabled}
              onCheckedChange={(enabled) => setForm((current) => ({ ...current, enabled }))}
              label={t('siteGroups.form.enabled')}
            />
          </DrawBody>
          <DrawFooter>
            <Button onClick={() => saveMutation.mutate()} disabled={saveMutation.isPending}>
              {saveMutation.isPending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : null}
              {editingGroup ? t('siteGroups.actions.save') : t('siteGroups.actions.create')}
            </Button>
            <Button variant="ghost" onClick={closeForm} disabled={saveMutation.isPending}>{t('siteGroups.actions.cancel')}</Button>
          </DrawFooter>
        </DrawContent>
      </Draw>

      <Dialog open={Boolean(deleteTarget)} onOpenChange={(open) => { if (!open && !deleteMutation.isPending) setDeleteTarget(null) }}>
        <DialogContent className="w-[min(92vw,520px)] overflow-hidden">
          <DialogHeader className="border-b-0 pb-2">
            <DialogTitle>{t('siteGroups.deleteDialog.title')}</DialogTitle>
          </DialogHeader>
          <DialogBody className="pt-0">
            <DialogDescription className="mt-0">
              {deleteTarget ? t('siteGroups.deleteDialog.confirm', { name: deleteTarget.name }) : t('siteGroups.deleteDialog.confirmDefault')}
            </DialogDescription>
          </DialogBody>
          <DialogFooter className="border-t-0 pt-2">
            <Button variant="outline" onClick={() => setDeleteTarget(null)} disabled={deleteMutation.isPending}>{t('siteGroups.actions.cancel')}</Button>
            <Button variant="destructive" onClick={() => { if (deleteTarget) deleteMutation.mutate(deleteTarget.id) }} disabled={deleteMutation.isPending}>
              {deleteMutation.isPending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : null}
              {t('siteGroups.actions.delete')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

function newGroupSlug(name: string) {
  const suffix = typeof crypto !== 'undefined' && 'randomUUID' in crypto
    ? crypto.randomUUID().slice(0, 8)
    : Date.now().toString(36)
  return `${toSlug(name) || 'site-group'}-${suffix}`
}
