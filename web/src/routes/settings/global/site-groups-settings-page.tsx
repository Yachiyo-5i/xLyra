import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { LoaderCircle, PencilLine, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { ErrorState } from '@/components/common/error-state'
import { toSlug } from '@/lib/slug'
import { TableToolbar } from '@/components/common/table-toolbar'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog, DialogBody, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Draw, DrawBody, DrawContent, DrawFooter, DrawHeader, DrawTitle } from '@/components/ui/draw'
import { FormField } from '@/components/ui/form-field'
import { Input } from '@/components/ui/input'
import { MultiSelect } from '@/components/ui/multi-select'
import { Switch } from '@/components/ui/switch'
import { toast } from '@/lib/toast'
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
    return <p className="text-body text-label-2">{t('siteGroups.loading')}</p>
  }

  if (groupsQuery.isError) {
    return <ErrorState title={t('siteGroups.loadFailed')} description={groupsQuery.error.message} />
  }

  return (
    <div className="max-w-3xl space-y-6">
      <div className="space-y-1.5">
        <h3 className="text-title-3 text-label">{t('siteGroups.title')}</h3>
        <p className="text-body text-label-2">{t('siteGroups.description')}</p>
      </div>

      <TableToolbar
        searchValue={search}
        onSearchChange={(event) => setSearch(event.target.value)}
        searchPlaceholder={t('siteGroups.search')}
        searchClassName="lg:max-w-sm"
        actions={
          <Button onClick={startCreate}>
            <Plus className="h-4 w-4" />
            {t('siteGroups.actions.create')}
          </Button>
        }
      />

      {filteredGroups.length ? (
        <div className="mat-thick divide-y divide-separator rounded-card px-5">
          {filteredGroups.map((group) => {
            const groupSites = sortSitesByName(group.sites.map((item) => sitesById.get(item.site_id)).filter(Boolean) as Site[])
            return (
              <div key={group.id} className="grid gap-3 py-4 md:grid-cols-[minmax(0,1fr)_auto]">
                <div className="min-w-0 space-y-2">
                  <div className="flex items-start justify-between gap-3">
                    <div className="min-w-0 flex flex-wrap items-center gap-2 pr-1">
                      <span className="min-w-0 truncate font-medium text-label">{group.name}</span>
                      <Badge variant={group.enabled ? 'success' : 'neutral'} className="shrink-0">
                        {group.enabled ? t('siteGroups.enabled') : t('siteGroups.disabled')}
                      </Badge>
                    </div>
                    <div className="flex shrink-0 items-start gap-1 md:hidden">
                      <Button size="icon" variant="ghost" className="h-8 w-8" onClick={() => startEdit(group)} title={t('siteGroups.actions.edit')}>
                        <PencilLine className="h-4 w-4" />
                      </Button>
                      <Button size="icon" variant="ghost" className="h-8 w-8 text-danger hover:bg-danger/10 hover:text-danger" onClick={() => setDeleteTarget(group)} title={t('siteGroups.actions.delete')}>
                        <Trash2 className="h-4 w-4" />
                      </Button>
                    </div>
                  </div>
                  {group.description ? <p className="text-body text-label-2">{group.description}</p> : null}
                  <div className="flex flex-wrap gap-1.5">
                    {groupSites.length ? groupSites.slice(0, 6).map((site) => (
                      <span key={site.id} className="rounded-control bg-fill-3 px-2 py-1 text-footnote text-label-2">{site.name}</span>
                    )) : <span className="text-footnote text-label-2">{t('siteGroups.noSites')}</span>}
                    {groupSites.length > 6 ? <span className="rounded-control bg-fill-3 px-2 py-1 text-footnote text-label-2">+{groupSites.length - 6}</span> : null}
                  </div>
                </div>
                <div className="hidden items-start gap-1 md:flex">
                  <Button size="icon" variant="ghost" className="h-8 w-8" onClick={() => startEdit(group)} title={t('siteGroups.actions.edit')}>
                    <PencilLine className="h-4 w-4" />
                  </Button>
                  <Button size="icon" variant="ghost" className="h-8 w-8 text-danger hover:bg-danger/10 hover:text-danger" onClick={() => setDeleteTarget(group)} title={t('siteGroups.actions.delete')}>
                    <Trash2 className="h-4 w-4" />
                  </Button>
                </div>
              </div>
            )
          })}
        </div>
      ) : null}

      <Draw open={formOpen} onOpenChange={(open) => { if (!open && !saveMutation.isPending) closeForm() }}>
        <DrawContent side="right" onOpenAutoFocus={(event) => event.preventDefault()}>
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
          <DialogFooter
            className="border-t-0 pt-2"
            cancel={<Button variant="secondary" onClick={() => setDeleteTarget(null)} disabled={deleteMutation.isPending}>{t('siteGroups.actions.cancel')}</Button>}
            confirm={(
              <Button variant="destructive" onClick={() => { if (deleteTarget) deleteMutation.mutate(deleteTarget.id) }} disabled={deleteMutation.isPending}>
                {deleteMutation.isPending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : null}
                {t('siteGroups.actions.delete')}
              </Button>
            )}
          />
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
