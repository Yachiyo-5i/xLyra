import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { FileText, LoaderCircle, MoveLeft, Pencil, Plus, Search, Sparkles, X } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog'
import { ConfirmDialog } from '@/components/common/confirm-dialog'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { TextArea } from '@/components/ui/textarea'
import {
  deleteWorkspaceFile,
  fetchAgentCapabilities,
  fetchAgentSkillDetail,
  fetchAgentSkillFile,
  fetchWorkspaceFile,
  listAgentSkills,
  putWorkspaceFile,
  updateAgentCapabilities,
  type AgentSkill,
} from '@/features/agent/api/agent'
import { toast } from '@/lib/toast'
import { cn } from '@/lib/utils'

const SKILL_NAME_PATTERN = /^[a-z0-9]+(-[a-z0-9]+)*$/
const AGENTS_MD_LIMIT = 32_000
const skillFilePath = (name: string) => `.agents/skills/${name}/SKILL.md`

const capabilitiesKey = ['agent', 'capabilities'] as const
const skillsKey = ['agent', 'skills'] as const
const agentsMdKey = ['agent', 'workspace-file', 'AGENTS.md'] as const

type SettingsTab = 'skills' | 'agentsMd'

/** In-dialog navigation for Skills: list / detail / edit (name null means creating). */
type SkillsNav =
  | { view: 'list' }
  | { view: 'detail'; name: string }
  | { view: 'edit'; name: string | null }

type AgentSettingsDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
}

/** Agent capabilities dialog: left nav (Skills / AGENTS.md) + right content; new settings sections can extend the tabs. */
export function AgentSettingsDialog({ open, onOpenChange }: AgentSettingsDialogProps) {
  const { t } = useTranslation('agent')
  const [tab, setTab] = useState<SettingsTab>('skills')
  const [skillsNav, setSkillsNav] = useState<SkillsNav>({ view: 'list' })

  const tabs: Array<{ key: SettingsTab; label: string; icon: typeof Sparkles }> = [
    { key: 'skills', label: t('settings.tabSkills'), icon: Sparkles },
    { key: 'agentsMd', label: 'AGENTS.md', icon: FileText },
  ]

  // Header back button: detail/edit views go one level up (editing an existing skill returns to detail, otherwise to the list).
  const headerBack = tab === 'skills' && skillsNav.view !== 'list'
    ? () => {
        if (skillsNav.view === 'edit' && skillsNav.name) setSkillsNav({ view: 'detail', name: skillsNav.name })
        else setSkillsNav({ view: 'list' })
      }
    : null

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      {/* DialogContent's default single auto track sizes to max-content, so nowrap
          text like skill descriptions would overflow the dialog; pinning the track
          with grid-cols-[minmax(0,1fr)] constrains it to the container width. */}
      <DialogContent className="w-[min(94vw,920px)] grid-cols-[minmax(0,1fr)] overflow-hidden" aria-describedby={undefined}>
        <DialogTitle className="sr-only">{t('settings.title')}</DialogTitle>
        <div className="flex h-[min(80vh,600px)]">
          <aside className="flex w-48 shrink-0 flex-col gap-0.5 border-r border-[hsl(var(--glass-divider))] bg-[hsl(var(--surface-subtle))]/40 p-3">
            <p className="px-2.5 pb-2 pt-1.5 text-[11px] font-semibold uppercase tracking-[0.18em] text-faint">
              {t('settings.title')}
            </p>
            {tabs.map((item) => (
              <button
                key={item.key}
                type="button"
                onClick={() => setTab(item.key)}
                className={cn(
                  'flex h-9 items-center gap-2.5 rounded-lg px-2.5 text-sm transition-colors',
                  tab === item.key
                    ? 'bg-[hsl(var(--surface-selected))] font-medium text-foreground'
                    : 'text-muted-soft hover:bg-[hsl(var(--surface-subtle))] hover:text-foreground',
                )}
              >
                <item.icon className="h-4 w-4 shrink-0" />
                {item.label}
              </button>
            ))}
          </aside>
          <div className="flex min-w-0 flex-1 flex-col">
            <div className="flex shrink-0 items-center justify-between border-b border-[hsl(var(--glass-divider))] py-3.5 pl-6 pr-4">
              {headerBack ? (
                <button
                  type="button"
                  onClick={headerBack}
                  className="flex items-center gap-1.5 text-sm font-medium text-muted-soft transition-colors hover:text-foreground"
                >
                  <MoveLeft className="h-4 w-4" />
                  {tabs.find((item) => item.key === tab)?.label}
                </button>
              ) : (
                <p className="text-[15px] font-semibold text-foreground">{tabs.find((item) => item.key === tab)?.label}</p>
              )}
              <button
                type="button"
                onClick={() => onOpenChange(false)}
                className="flex h-8 w-8 items-center justify-center rounded-lg text-muted-soft transition-colors hover:bg-[hsl(var(--surface-subtle))] hover:text-foreground"
                aria-label={t('settings.close')}
              >
                <X className="h-4 w-4" />
              </button>
            </div>
            <div className="min-h-0 flex-1 overflow-y-auto px-6 py-5">
              {tab === 'skills'
                ? <SkillsPane nav={skillsNav} onNavigate={setSkillsNav} />
                : <AgentsMdPane />}
            </div>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}

/** The global toggle and the enable/disable list both go through agent /config whole-save (credential fields are preserved server-side). */
function useCapabilitiesMutation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: updateAgentCapabilities,
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: capabilitiesKey })
      await queryClient.invalidateQueries({ queryKey: skillsKey })
    },
  })
}

/** Toggles a skill by maintaining the disabled_skills list in the config. */
function useSkillToggle() {
  const { t } = useTranslation('agent')
  const capabilitiesQuery = useQuery({ queryKey: capabilitiesKey, queryFn: fetchAgentCapabilities, retry: false })
  const capabilitiesMutation = useCapabilitiesMutation()
  const disabledSkills = useMemo(
    () => new Set(capabilitiesQuery.data?.disabled_skills ?? []),
    [capabilitiesQuery.data],
  )
  const toggleSkill = (skill: Pick<AgentSkill, 'name' | 'enabled'>) => {
    const next = new Set(disabledSkills)
    if (skill.enabled) next.add(skill.name)
    else next.delete(skill.name)
    capabilitiesMutation.mutate(
      { disabled_skills: [...next] },
      { onError: (error) => toast.error(t('settings.saveFailed'), { description: error.message }) },
    )
  }
  return { toggleSkill, pending: capabilitiesMutation.isPending }
}

function SkillsPane({ nav, onNavigate }: { nav: SkillsNav; onNavigate: (nav: SkillsNav) => void }) {
  const { t } = useTranslation('agent')
  const skillsQuery = useQuery({ queryKey: skillsKey, queryFn: listAgentSkills, retry: false })
  const capabilitiesQuery = useQuery({ queryKey: capabilitiesKey, queryFn: fetchAgentCapabilities, retry: false })
  const capabilitiesMutation = useCapabilitiesMutation()
  const { toggleSkill, pending: togglePending } = useSkillToggle()

  const [keyword, setKeyword] = useState('')

  const globalEnabled = skillsQuery.data?.enabled ?? capabilitiesQuery.data?.enable_skills !== false
  const skills = skillsQuery.data?.skills ?? []
  const filtered = skills.filter((skill) =>
    `${skill.name} ${skill.description}`.toLowerCase().includes(keyword.trim().toLowerCase()))

  if (nav.view === 'edit') {
    return (
      <SkillEditor
        name={nav.name}
        skills={skills}
        onBack={() => onNavigate(nav.name ? { view: 'detail', name: nav.name } : { view: 'list' })}
      />
    )
  }

  if (nav.view === 'detail') {
    return (
      <SkillDetail
        name={nav.name}
        fallback={skills.find((skill) => skill.name === nav.name)}
        onEdit={(name) => onNavigate({ view: 'edit', name })}
        onToggle={toggleSkill}
        togglePending={togglePending}
      />
    )
  }

  return (
    <div className="space-y-4">
      <Switch
        label={t('settings.skillsEnable')}
        description={t('settings.skillsEnableHint')}
        checked={globalEnabled}
        disabled={capabilitiesQuery.data === null || capabilitiesMutation.isPending}
        onCheckedChange={(checked) => capabilitiesMutation.mutate(
          { enable_skills: checked },
          { onError: (error) => toast.error(t('settings.saveFailed'), { description: error.message }) },
        )}
      />

      {skillsQuery.data === null ? (
        <p className="rounded-lg border border-[hsl(var(--glass-border))] px-4 py-8 text-center text-xs text-muted-soft">
          {t('settings.skillsUnavailable')}
        </p>
      ) : (
        <>
          <div className="flex items-center gap-2">
            <div className="relative flex-1">
              <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-foreground/40" />
              <Input
                value={keyword}
                onChange={(event) => setKeyword(event.target.value)}
                placeholder={t('settings.skillsSearch')}
                className="pl-9"
              />
            </div>
            <Button variant="secondary" onClick={() => onNavigate({ view: 'edit', name: null })}>
              <Plus className="h-4 w-4" />
              {t('settings.skillAdd')}
            </Button>
          </div>

          <div className="overflow-hidden rounded-xl border border-[hsl(var(--glass-border))]">
            {filtered.length === 0 ? (
              <p className="px-4 py-8 text-center text-xs text-muted-soft">{t('settings.skillsEmpty')}</p>
            ) : (
              filtered.map((skill) => {
                const external = skill.scope === 'user' || skill.scope === 'extra'
                return (
                  <div
                    key={`${skill.scope ?? 'project'}:${skill.name}`}
                    className="flex cursor-pointer items-center gap-3 border-t border-[hsl(var(--glass-divider))] px-4 py-3 transition-colors first:border-t-0 hover:bg-[hsl(var(--surface-subtle))]"
                    onClick={() => onNavigate({ view: 'detail', name: skill.name })}
                  >
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2">
                        <span className="truncate text-sm font-medium text-foreground">{skill.name}</span>
                        {external ? <Badge variant="neutral">{t('settings.skillScopeExternal')}</Badge> : null}
                      </div>
                      <p className="mt-0.5 truncate text-xs text-muted-soft" title={skill.description}>{skill.description}</p>
                    </div>
                    <div onClick={(event) => event.stopPropagation()}>
                      <Switch
                        checked={skill.enabled}
                        disabled={togglePending}
                        onCheckedChange={() => toggleSkill(skill)}
                      />
                    </div>
                  </div>
                )
              })
            )}
          </div>
          <p className="text-xs text-faint">{t('settings.effectiveHint')}</p>
        </>
      )}
    </div>
  )
}

/** Skill detail: header (name/toggle/edit) + Overview / Contents tabs; Contents defaults to SKILL.md. */
function SkillDetail({
  name,
  fallback,
  onEdit,
  onToggle,
  togglePending,
}: {
  name: string
  fallback?: AgentSkill
  onEdit: (name: string) => void
  onToggle: (skill: Pick<AgentSkill, 'name' | 'enabled'>) => void
  togglePending: boolean
}) {
  const { t } = useTranslation('agent')
  const detailQuery = useQuery({
    queryKey: ['agent', 'skill-detail', name],
    queryFn: () => fetchAgentSkillDetail(name),
    retry: false,
  })
  const detail = detailQuery.data
  const scope = detail?.scope ?? fallback?.scope
  const editable = !scope || scope === 'project'
  const enabled = detail?.enabled ?? fallback?.enabled ?? true
  const resources = useMemo(() => detail?.resources ?? [], [detail])
  const files = useMemo(() => ['SKILL.md', ...resources], [resources])

  const [selectedFile, setSelectedFile] = useState('SKILL.md')
  const fileQuery = useQuery({
    queryKey: ['agent', 'skill-file', name, selectedFile],
    queryFn: () => fetchAgentSkillFile(name, selectedFile),
    enabled: selectedFile !== 'SKILL.md',
    retry: false,
  })
  const viewingContent = selectedFile === 'SKILL.md' ? detail?.content : fileQuery.data?.content
  const viewingTruncated = selectedFile === 'SKILL.md' ? detail?.contentTruncated : fileQuery.data?.truncated

  const scopeLabel = scope === 'user'
    ? t('settings.detailScopeUser')
    : scope === 'extra'
      ? t('settings.detailScopeExtra')
      : t('settings.detailScopeProject')

  return (
    <div className="flex h-full flex-col">
      <div className="flex items-center justify-between gap-3">
        <p className="flex min-w-0 items-center gap-2 text-base font-semibold text-foreground">
          <span className="truncate">{name}</span>
          {!editable ? <Badge variant="neutral">{t('settings.skillScopeExternal')}</Badge> : null}
        </p>
        <div className="flex shrink-0 items-center gap-2">
          {editable ? (
            <Button size="sm" variant="outline" onClick={() => onEdit(name)}>
              <Pencil className="h-3.5 w-3.5" />
              {t('settings.detailEdit')}
            </Button>
          ) : null}
          <Switch
            checked={enabled}
            disabled={togglePending}
            onCheckedChange={() => onToggle({ name, enabled })}
          />
        </div>
      </div>

      {detailQuery.data === null && !detailQuery.isLoading ? (
        <p className="mt-6 rounded-lg border border-[hsl(var(--glass-border))] px-4 py-8 text-center text-xs text-muted-soft">
          {t('settings.detailUnavailable')}
        </p>
      ) : (
        <Tabs defaultValue="contents" className="mt-4 flex min-h-0 flex-1 flex-col">
          <TabsList className="h-9 w-60 shrink-0">
            <TabsTrigger value="overview">{t('settings.detailOverview')}</TabsTrigger>
            <TabsTrigger value="contents">{t('settings.detailContents', { count: files.length })}</TabsTrigger>
          </TabsList>

          <TabsContent value="overview">
            <p className="mt-4 whitespace-pre-wrap text-sm leading-6 text-foreground/90">
              {detail?.description ?? fallback?.description}
            </p>
            <div className="mt-4 rounded-xl border border-[hsl(var(--glass-border))]">
              <DetailRow label={t('settings.skillName')} value={name} mono />
              <DetailRow label={t('settings.detailScope')} value={scopeLabel} />
              <DetailRow label={t('settings.detailPath')} value={detail?.path ?? '—'} mono />
              {detail?.license ? <DetailRow label={t('settings.detailLicense')} value={detail.license} /> : null}
            </div>
          </TabsContent>
          <TabsContent value="contents" className="flex min-h-0 flex-1 flex-col data-[state=inactive]:hidden">
            <div className="mt-4 flex shrink-0 items-center gap-3">
              <Select value={selectedFile} onValueChange={setSelectedFile}>
                <SelectTrigger className="h-8 w-56 text-xs">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {files.map((file) => (
                    <SelectItem key={file} value={file} className="font-mono text-xs">{file}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <span className="text-xs text-faint">{t('settings.detailFiles', { count: files.length })}</span>
            </div>
            <div className="mt-3 min-h-0 flex-1 overflow-auto border-t border-[hsl(var(--glass-divider))]">
              {viewingContent === undefined || viewingContent === null ? (
                <p className="flex items-center gap-2 py-6 text-xs text-muted-soft">
                  <LoaderCircle className="h-3.5 w-3.5 animate-spin" />
                  {t('settings.detailLoading')}
                </p>
              ) : (
                <pre className="whitespace-pre-wrap break-all py-3 pr-2 font-mono text-xs leading-5 text-foreground/90">{viewingContent}</pre>
              )}
              {viewingTruncated ? (
                <p className="py-2 text-xs text-amber-500">{t('settings.detailTruncated')}</p>
              ) : null}
            </div>
          </TabsContent>
        </Tabs>
      )}
    </div>
  )
}

function DetailRow({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="flex items-center justify-between gap-4 border-t border-[hsl(var(--glass-divider))] px-4 py-2.5 first:border-t-0">
      <span className="shrink-0 text-xs text-muted-soft">{label}</span>
      <span className={cn('min-w-0 truncate text-xs text-foreground', mono && 'font-mono')} title={value}>{value}</span>
    </div>
  )
}

/** Skill editor: create (name editable) or edit the description and body of an existing project-scope skill. */
function SkillEditor({ name, skills, onBack }: { name: string | null; skills: AgentSkill[]; onBack: () => void }) {
  const { t } = useTranslation('agent')
  const queryClient = useQueryClient()
  const existing = name ? skills.find((skill) => skill.name === name) : undefined

  const [skillName, setSkillName] = useState(name ?? '')
  const [description, setDescription] = useState(existing?.description ?? '')
  const [bodyDraft, setBodyDraft] = useState<string | null>(null)
  const [confirmDelete, setConfirmDelete] = useState(false)

  const bodyQuery = useQuery({
    queryKey: ['agent', 'workspace-file', name],
    queryFn: () => fetchWorkspaceFile(skillFilePath(name!)),
    enabled: Boolean(name),
    retry: false,
  })

  // Existing skill: read the current SKILL.md and strip frontmatter for the body
  // (description is edited separately); a local draft wins over the fetched
  // content once the user starts editing.
  const loadedBody = useMemo(() => {
    if (!name || bodyQuery.data == null) return null
    const match = /^---\r?\n[\s\S]*?\r?\n---\r?\n?([\s\S]*)$/.exec(bodyQuery.data)
    return match ? match[1].trimStart() : bodyQuery.data
  }, [name, bodyQuery.data])
  const body = name ? (bodyDraft ?? loadedBody) : (bodyDraft ?? '')
  const bodyLoading = Boolean(name) && bodyQuery.isLoading

  const nameValid = SKILL_NAME_PATTERN.test(skillName)
  const canSave = nameValid && description.trim().length > 0 && body !== null

  const save = useMutation({
    mutationFn: () => putWorkspaceFile(
      skillFilePath(skillName),
      `---\nname: ${skillName}\ndescription: ${description.trim()}\n---\n\n${(body ?? '').trimEnd()}\n`,
    ),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: skillsKey })
      toast.success(t('settings.skillSaved'))
      onBack()
    },
    onError: (error) => toast.error(t('settings.saveFailed'), { description: error.message }),
  })

  const remove = useMutation({
    mutationFn: () => deleteWorkspaceFile(skillFilePath(name!)),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: skillsKey })
      toast.success(t('settings.skillDeleted'))
      onBack()
    },
    onError: (error) => toast.error(t('settings.saveFailed'), { description: error.message }),
  })

  return (
    <div className="flex h-full flex-col space-y-4">
      <div className="grid gap-3 sm:grid-cols-2">
        <label className="space-y-1.5">
          <span className="text-xs font-medium text-muted-soft">{t('settings.skillName')}</span>
          <Input
            value={skillName}
            onChange={(event) => setSkillName(event.target.value)}
            placeholder="my-skill"
            disabled={Boolean(name)}
            className="font-mono text-xs"
          />
          {!name && skillName && !nameValid ? (
            <span className="text-xs text-red-500">{t('settings.skillNameInvalid')}</span>
          ) : null}
        </label>
        <label className="space-y-1.5">
          <span className="text-xs font-medium text-muted-soft">{t('settings.skillDescription')}</span>
          <Input value={description} onChange={(event) => setDescription(event.target.value)} />
        </label>
      </div>

      <label className="flex min-h-0 flex-1 flex-col space-y-1.5">
        <span className="text-xs font-medium text-muted-soft">{t('settings.skillBody')}</span>
        <TextArea
          value={body ?? ''}
          onChange={(event) => setBodyDraft(event.target.value)}
          placeholder={t('settings.skillBodyPlaceholder')}
          className="min-h-0 flex-1 resize-none font-mono text-xs leading-5"
          disabled={bodyLoading}
        />
      </label>

      <div className="flex items-center gap-2">
        <Button onClick={() => save.mutate()} disabled={!canSave || save.isPending}>
          {save.isPending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : null}
          {t('settings.save')}
        </Button>
        {name ? (
          <Button variant="ghost" className="text-destructive" onClick={() => setConfirmDelete(true)}>
            {t('settings.skillDelete')}
          </Button>
        ) : null}
      </div>

      <ConfirmDialog
        open={confirmDelete}
        title={t('settings.skillDelete')}
        description={t('settings.skillDeleteConfirm')}
        confirmLabel={t('settings.skillDelete')}
        destructive
        onCancel={() => setConfirmDelete(false)}
        onConfirm={() => {
          setConfirmDelete(false)
          remove.mutate()
        }}
      />
    </div>
  )
}

function AgentsMdPane() {
  const { t } = useTranslation('agent')
  const queryClient = useQueryClient()
  const capabilitiesQuery = useQuery({ queryKey: capabilitiesKey, queryFn: fetchAgentCapabilities, retry: false })
  const capabilitiesMutation = useCapabilitiesMutation()
  const fileQuery = useQuery({ queryKey: agentsMdKey, queryFn: () => fetchWorkspaceFile('AGENTS.md'), retry: false })

  const [draft, setDraft] = useState<string | null>(null)
  const content = draft ?? fileQuery.data ?? ''

  const globalEnabled = capabilitiesQuery.data?.enable_agents_md !== false
  const bytes = new TextEncoder().encode(content).length
  const overLimit = bytes > AGENTS_MD_LIMIT

  const save = useMutation({
    mutationFn: () => putWorkspaceFile('AGENTS.md', content),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: agentsMdKey })
      toast.success(t('settings.agentsMdSaved'))
    },
    onError: (error) => toast.error(t('settings.saveFailed'), { description: error.message }),
  })

  const remove = useMutation({
    mutationFn: () => deleteWorkspaceFile('AGENTS.md'),
    onSuccess: async () => {
      setDraft('')
      await queryClient.invalidateQueries({ queryKey: agentsMdKey })
      toast.success(t('settings.agentsMdDeleted'))
    },
    onError: (error) => toast.error(t('settings.saveFailed'), { description: error.message }),
  })

  return (
    <div className="flex h-full flex-col space-y-4">
      <Switch
        label={t('settings.agentsMdEnable')}
        description={t('settings.agentsMdEnableHint')}
        checked={globalEnabled}
        disabled={capabilitiesQuery.data === null || capabilitiesMutation.isPending}
        onCheckedChange={(checked) => capabilitiesMutation.mutate(
          { enable_agents_md: checked },
          { onError: (error) => toast.error(t('settings.saveFailed'), { description: error.message }) },
        )}
      />

      <TextArea
        value={content}
        onChange={(event) => setDraft(event.target.value)}
        placeholder={t('settings.agentsMdPlaceholder')}
        disabled={fileQuery.isLoading}
        className="min-h-0 flex-1 resize-none font-mono text-xs leading-5"
      />

      <div className="flex items-center justify-between gap-3">
        <span className={cn('text-xs', overLimit ? 'text-red-500' : 'text-faint')}>
          {t('settings.agentsMdBytes', { bytes: bytes.toLocaleString(), limit: AGENTS_MD_LIMIT.toLocaleString() })}
        </span>
        <div className="flex items-center gap-2">
          {fileQuery.data !== null && fileQuery.data !== undefined ? (
            <Button variant="ghost" className="text-destructive" onClick={() => remove.mutate()} disabled={remove.isPending}>
              {t('settings.agentsMdDelete')}
            </Button>
          ) : null}
          <Button onClick={() => save.mutate()} disabled={overLimit || save.isPending || fileQuery.isLoading}>
            {save.isPending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : null}
            {t('settings.save')}
          </Button>
        </div>
      </div>
      <p className="text-xs text-faint">{t('settings.effectiveHint')}</p>
    </div>
  )
}
