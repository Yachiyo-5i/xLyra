import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Check, ChevronRight, FileText, ImagePlus, LoaderCircle, MoveLeft, Palette, Pencil, Plus, Search, Sparkles, X } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog'
import { ConfirmDialog } from '@/components/common/confirm-dialog'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { TextArea } from '@/components/ui/textarea'
import { Slider } from '@/components/ui/slider'
import {
  deleteWorkspaceFile,
  fetchAgentCapabilities,
  fetchAgentRuntimeSettings,
  fetchAgentSkillDetail,
  fetchAgentSkillFile,
  fetchWorkspaceFile,
  listAgentSkills,
  putWorkspaceFile,
  updateAgentCapabilities,
  updateAgentAppearanceSettings,
  type AgentAppearanceSettings,
  defaultAgentAppearance,
  type AgentSkill,
} from '@/features/agent/api/agent'
import { toast } from '@/lib/toast'
import { cn } from '@/lib/utils'
import { useMobileLayout } from '@/hooks/use-media-query'
import { AgentLiquidGlassPanel } from '@/features/agent/components/liquid-glass/agent-liquid-glass'

const SKILL_NAME_PATTERN = /^[a-z0-9]+(-[a-z0-9]+)*$/
const AGENTS_MD_LIMIT = 32_000
const skillFilePath = (name: string) => `.agents/skills/${name}/SKILL.md`

const capabilitiesKey = ['agent', 'capabilities'] as const
const skillsKey = ['agent', 'skills'] as const
const agentsMdKey = ['agent', 'workspace-file', 'AGENTS.md'] as const

type SettingsTab = 'skills' | 'agentsMd' | 'appearance'
type MobileSettingsView = 'menu' | 'detail'
type SettingsActions = {
  save: () => void
  canSave: boolean
  pending: boolean
}

/** In-dialog navigation for Skills: list / detail / edit (name null means creating). */
type SkillsNav =
  | { view: 'list' }
  | { view: 'detail'; name: string }
  | { view: 'edit'; name: string | null }

type AgentSettingsDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  backgroundImage?: string
}

/** Agent capabilities dialog: left nav (Skills / AGENTS.md) + right content; new settings sections can extend the tabs. */
export function AgentSettingsDialog({ open, onOpenChange, backgroundImage = '/agent-backdrop.png' }: AgentSettingsDialogProps) {
  const { t } = useTranslation('agent')
  const mobileLayout = useMobileLayout()
  const [tab, setTab] = useState<SettingsTab>('appearance')
  const [mobileView, setMobileView] = useState<MobileSettingsView>('menu')
  const [paneActions, setPaneActions] = useState<SettingsActions | null>(null)
  const [skillsNav, setSkillsNav] = useState<SkillsNav>({ view: 'list' })
  const runtime = useQuery({ queryKey: ['settings', 'agent', 'runtime'], queryFn: fetchAgentRuntimeSettings, retry: false })
  const queryClient = useQueryClient()
  const [appearanceDraftOverride, setAppearanceDraftOverride] = useState<AgentAppearanceSettings | null>(null)
  const appearanceDraft = appearanceDraftOverride ?? runtime.data?.appearance ?? defaultAgentAppearance
  const appearanceSave = useMutation({
    mutationFn: () => updateAgentAppearanceSettings(appearanceDraft),
    onSuccess: async () => {
      setAppearanceDraftOverride(null)
      await queryClient.invalidateQueries({ queryKey: ['settings', 'agent', 'runtime'] })
      toast.success(t('settings.appearance.saved'))
    },
    onError: (error) => toast.error(t('settings.saveFailed'), { description: error.message }),
  })
  const darkBackground = backgroundImage.includes('plain')

  function resetDialogState() {
    setTab('appearance')
    setMobileView('menu')
    setSkillsNav({ view: 'list' })
    setAppearanceDraftOverride(null)
    setPaneActions(null)
  }

  function closeDialog() {
    resetDialogState()
    onOpenChange(false)
  }

  function resetAppearance() {
    setAppearanceDraftOverride({
      ...defaultAgentAppearance,
      custom_background_images: appearanceDraft.custom_background_images,
    })
  }

  const tabs: Array<{ key: SettingsTab; label: string; icon: typeof Sparkles }> = [
    { key: 'appearance', label: t('settings.tabAppearance'), icon: Palette },
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

  const dialogSidebarClass = cn(
    'flex shrink-0 gap-0.5 p-3',
    mobileLayout
      ? 'w-full flex-row flex-nowrap items-center overflow-x-auto border-b border-[hsl(var(--glass-divider))] bg-[hsl(var(--surface-subtle))]/40 px-2 py-1.5'
      : 'w-48 flex-col agent-liquid-dialog__sidebar',
  )

  const tabContent = tab === 'skills'
    ? <SkillsPane nav={skillsNav} onNavigate={setSkillsNav} onActionsChange={setPaneActions} />
    : tab === 'agentsMd'
      ? <AgentsMdPane onActionsChange={setPaneActions} />
      : <AppearancePane draft={appearanceDraft} onChange={setAppearanceDraftOverride} />

  const dialogBody = (
    <div className={cn('flex min-h-0', mobileLayout ? 'h-full flex-col' : 'h-[min(80svh,600px)]')}>
      <aside className={dialogSidebarClass}>
        <p className={cn(
          'px-2.5 pb-2 pt-1.5 text-[11px] font-semibold uppercase tracking-[0.18em] text-faint',
          mobileLayout && 'sr-only',
        )}>
          {t('settings.title')}
        </p>
        {tabs.map((item) => (
          <button
            key={item.key}
            type="button"
            onClick={() => {
              setPaneActions(null)
              setTab(item.key)
            }}
            className={cn(
              'agent-liquid-dialog__nav flex h-9 items-center gap-2.5 rounded-lg px-2.5 text-sm transition-colors',
              mobileLayout && 'min-w-[4.75rem] flex-none justify-center px-2 text-xs',
              tab === item.key
                ? 'agent-liquid-dialog__nav--active font-medium text-foreground'
                : 'text-muted-soft hover:bg-[hsl(var(--surface-subtle))] hover:text-foreground',
            )}
          >
            <item.icon className="h-4 w-4 shrink-0" />
            {item.label}
          </button>
        ))}
      </aside>
      <div className="flex min-h-0 min-w-0 flex-1 flex-col">
        <div className={cn(
          'flex shrink-0 items-center justify-between border-b border-[hsl(var(--glass-divider))] py-3.5',
          mobileLayout ? 'px-4' : 'pl-6 pr-4',
        )}>
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
            onClick={closeDialog}
            className="flex h-8 w-8 items-center justify-center rounded-lg text-muted-soft transition-colors hover:bg-[hsl(var(--surface-subtle))] hover:text-foreground"
            aria-label={t('settings.close')}
          >
            <X className="h-4 w-4" />
          </button>
        </div>
        <div className={cn(
          'agent-liquid-dialog__body min-h-0 flex-1 py-5',
          mobileLayout ? 'px-4' : 'px-6',
          tab === 'agentsMd' || (tab === 'skills' && skillsNav.view === 'edit') ? 'overflow-hidden' : 'overflow-y-auto',
        )}>
          {tabContent}
        </div>
        <div className={cn(
          'flex shrink-0 items-center justify-end gap-2 border-t border-[hsl(var(--glass-divider))] py-3',
          mobileLayout ? 'px-4' : 'px-6',
        )}>
          {tab === 'appearance' ? <Button variant="ghost" size="sm" onClick={resetAppearance} disabled={appearanceSave.isPending}>{t('settings.appearance.reset')}</Button> : null}
          <Button variant="outline" size="sm" onClick={closeDialog} disabled={appearanceSave.isPending || paneActions?.pending}>{t('settings.appearance.cancel')}</Button>
          {tab === 'appearance' ? (
            <Button size="sm" onClick={() => appearanceSave.mutate()} disabled={appearanceSave.isPending || runtime.isLoading}>{t('settings.save')}</Button>
          ) : paneActions ? (
            <Button size="sm" onClick={paneActions.save} disabled={!paneActions.canSave || paneActions.pending}>{t('settings.save')}</Button>
          ) : null}
        </div>
      </div>
    </div>
  )

  const mobileDialogBody = mobileView === 'menu' ? (
    <div className="agent-mobile-settings__layout agent-mobile-settings__layout--menu">
      <div className="agent-mobile-settings__header">
        <p className="agent-mobile-settings__title">{t('settings.title')}</p>
      </div>
      <div className="agent-mobile-settings__menu">
        {tabs.map((item) => (
          <button
            key={item.key}
            type="button"
            onClick={() => {
              setPaneActions(null)
              setTab(item.key)
              setMobileView('detail')
            }}
            className="agent-mobile-settings__menu-item"
          >
            <item.icon className="h-4 w-4 shrink-0" />
            <span className="min-w-0 flex-1 truncate text-left">{item.label}</span>
            <ChevronRight className="h-4 w-4 shrink-0 text-muted-soft" />
          </button>
        ))}
      </div>
    </div>
  ) : (
    <div className="agent-mobile-settings__layout">
      <div className="agent-mobile-settings__header">
        <button type="button" onClick={() => { setPaneActions(null); setMobileView('menu') }} className="agent-mobile-settings__back">
          <MoveLeft className="h-4 w-4" />
          {t('header.back')}
        </button>
        <p className="agent-mobile-settings__title">{tabs.find((item) => item.key === tab)?.label}</p>
      </div>
      <div className={cn(
        'agent-mobile-settings__content',
        tab === 'agentsMd' || (tab === 'skills' && skillsNav.view === 'edit') ? 'overflow-hidden' : 'overflow-y-auto',
      )}>
        {tabContent}
      </div>
      <div className="agent-mobile-settings__footer">
        {tab === 'appearance' ? <Button variant="ghost" size="sm" onClick={resetAppearance} disabled={appearanceSave.isPending}>{t('settings.appearance.reset')}</Button> : null}
        <Button variant="outline" size="sm" onClick={closeDialog} disabled={appearanceSave.isPending || paneActions?.pending}>{t('settings.appearance.cancel')}</Button>
        {tab === 'appearance' ? (
          <Button size="sm" onClick={() => appearanceSave.mutate()} disabled={appearanceSave.isPending || runtime.isLoading}>{t('settings.save')}</Button>
        ) : paneActions ? (
          <Button size="sm" onClick={paneActions.save} disabled={!paneActions.canSave || paneActions.pending}>{t('settings.save')}</Button>
        ) : null}
      </div>
    </div>
  )

  return (
    <Dialog open={open} onOpenChange={(nextOpen) => {
      if (nextOpen && mobileLayout) setMobileView('menu')
      if (!nextOpen) resetDialogState()
      onOpenChange(nextOpen)
    }}>
      {/* DialogContent's default single auto track sizes to max-content, so nowrap
          text like skill descriptions would overflow the dialog; pinning the track
          with grid-cols-[minmax(0,1fr)] constrains it to the container width. */}
      <DialogContent
        className={cn(
          'agent-liquid-dialog-host min-h-0 grid-rows-[minmax(0,1fr)] grid-cols-[minmax(0,1fr)] overflow-hidden',
          mobileLayout
            ? cn(
                'w-[calc(100vw-2rem)] max-w-none rounded-[24px]',
                mobileView === 'menu' ? 'h-auto max-h-[min(52svh,420px)]' : 'h-[min(78svh,680px)]',
              )
            : 'w-[min(94vw,920px)]',
        )}
        aria-describedby={undefined}
      >
        <DialogTitle className="sr-only">{t('settings.title')}</DialogTitle>
        <AgentLiquidGlassPanel
          backgroundImage={backgroundImage}
          variant={darkBackground ? 'dark' : 'frosted'}
          sampleBackground={darkBackground ? false : 0.72}
          className="agent-liquid-dialog"
          contentClassName="agent-liquid-dialog__content"
          settings={{
            blur: darkBackground ? 0.18 : 0.34,
            refraction: darkBackground ? 0.72 : 0.38,
            chromaticAberration: darkBackground ? 0.045 : 0.025,
            distortion: darkBackground ? 0.015 : 0.012,
            darkTint: darkBackground ? 0.18 : 0.24,
            tintStrength: darkBackground ? 0.06 : 0.1,
            edgeHighlight: darkBackground ? 0.08 : 0.1,
            specular: darkBackground ? 0.14 : 0.12,
            fresnel: darkBackground ? 1.08 : 0.9,
            shadow: darkBackground ? 0.12 : 0.18,
            bevel: 0,
            depth: 32,
            radius: 28,
            opacity: darkBackground ? 1 : 0.96,
          }}
        >
          {mobileLayout ? mobileDialogBody : dialogBody}
        </AgentLiquidGlassPanel>
      </DialogContent>
    </Dialog>
  )
}

type AppearanceSliderKey = 'side_transparency' | 'side_brightness' | 'side_thickness' | 'backdrop_blur' | 'backdrop_dim'

const appearanceSliderFields: Array<{ key: AppearanceSliderKey; labelKey: string; minLabelKey: string; maxLabelKey: string; unit: string }> = [
  { key: 'side_transparency', labelKey: 'settings.appearance.sideTransparency', minLabelKey: 'settings.appearance.standard', maxLabelKey: 'settings.appearance.full', unit: '%' },
  { key: 'side_brightness', labelKey: 'settings.appearance.sideBrightness', minLabelKey: 'settings.appearance.dark', maxLabelKey: 'settings.appearance.bright', unit: '%' },
  { key: 'side_thickness', labelKey: 'settings.appearance.sideThickness', minLabelKey: 'settings.appearance.thin', maxLabelKey: 'settings.appearance.thick', unit: '' },
  { key: 'backdrop_blur', labelKey: 'settings.appearance.backdropBlur', minLabelKey: 'settings.appearance.clear', maxLabelKey: 'settings.appearance.blurry', unit: '' },
  { key: 'backdrop_dim', labelKey: 'settings.appearance.backdropDim', minLabelKey: 'settings.appearance.transparent', maxLabelKey: 'settings.appearance.full', unit: '%' },
]

function readFileAsDataURL(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(String(reader.result))
    reader.onerror = () => reject(reader.error ?? new Error('failed to read image'))
    reader.readAsDataURL(file)
  })
}

function AppearancePane({ draft, onChange }: { draft: AgentAppearanceSettings; onChange: (next: AgentAppearanceSettings) => void }) {
  const { t } = useTranslation('agent')
  const fileInputRef = useRef<HTMLInputElement>(null)
  const customImages = draft.custom_background_images
  const selectedImage = draft.background_image || defaultAgentAppearance.background_image

  function selectBackground(value: string) {
    onChange({ ...draft, background_image: value })
  }

  function removeCustomBackground(image: string) {
    const remaining = customImages.filter((item) => item !== image)
    onChange({
      ...draft,
      background_image: draft.background_image === image ? defaultAgentAppearance.background_image : draft.background_image,
      custom_background_images: remaining,
    })
  }

  async function uploadBackground(file: File | undefined) {
    if (!file || !file.type.startsWith('image/')) return
    if (file.size > 8 * 1024 * 1024) {
      toast.error(t('settings.appearance.imageTooLarge'))
      return
    }
    try {
      const dataURL = await readFileAsDataURL(file)
      const nextImages = [...customImages.filter((item) => item !== dataURL), dataURL]
      onChange({ ...draft, background_image: dataURL, custom_background_images: nextImages })
    } catch (error) {
      toast.error(t('settings.appearance.imageReadFailed'), { description: error instanceof Error ? error.message : String(error) })
    }
  }

  return (
    <div className="space-y-6">
      <section className="space-y-3">
        <div>
          <h3 className="text-sm font-semibold text-foreground">{t('settings.appearance.backgroundTitle')}</h3>
          <p className="mt-1 text-xs leading-5 text-muted-soft">{t('settings.appearance.backgroundHint')}</p>
        </div>
        <div className="appearance-background-preview group relative overflow-hidden rounded-xl border border-[hsl(var(--glass-border))]">
          <img src={selectedImage} alt="" className="aspect-[16/9] w-full object-cover object-center" />
          <div className="absolute inset-0 flex items-center justify-center bg-black/0 transition-colors group-hover:bg-black/55">
            <button
              type="button"
              onClick={() => fileInputRef.current?.click()}
              className="appearance-background-change rounded-lg bg-primary px-3 py-2 text-xs font-medium text-primary-foreground opacity-0 shadow-lg transition-[opacity,filter] hover:brightness-105 group-hover:opacity-100"
            >
              {t('settings.appearance.changeBackground')}
            </button>
          </div>
        </div>
        <div className="appearance-background-list grid grid-cols-2 items-stretch gap-3 sm:grid-cols-3">
          {[
            { value: '/agent-backdrop.png', label: t('settings.appearance.defaultBackground') },
          ].map((item) => (
            <button
              key={item.value}
              type="button"
              onClick={() => selectBackground(item.value)}
              className={cn('appearance-background-card group relative overflow-hidden rounded-xl border text-left transition-colors', draft.background_image === item.value ? 'border-foreground' : 'border-[hsl(var(--glass-border))]')}
            >
              <img src={item.value} alt="" className="aspect-[16/9] w-full object-cover object-center" />
              <span className="block px-3 py-2 text-xs text-muted-soft">{item.label}</span>
              {draft.background_image === item.value ? <Check className="absolute right-2 top-2 h-4 w-4 rounded-full bg-foreground p-0.5 text-background" /> : null}
            </button>
          ))}
          {customImages.map((image, index) => (
            <div key={image} className={cn('appearance-background-card relative overflow-hidden rounded-xl border text-left transition-colors', draft.background_image === image ? 'border-foreground' : 'border-[hsl(var(--glass-border))]')}>
              <button type="button" onClick={() => selectBackground(image)} className="block w-full text-left">
                <img src={image} alt="" className="aspect-[16/9] w-full object-cover object-center" />
                <span className="block px-3 py-2 text-xs text-muted-soft">{t('settings.appearance.customBackground', { index: index + 1 })}</span>
              </button>
              {draft.background_image === image ? <Check className="absolute right-2 top-2 h-4 w-4 rounded-full bg-foreground p-0.5 text-background" /> : null}
              <button
                type="button"
                onClick={(event) => { event.stopPropagation(); removeCustomBackground(image) }}
                className="absolute left-2 top-2 flex h-6 w-6 items-center justify-center rounded-full bg-black/65 text-white transition-colors hover:bg-black/85"
                aria-label={t('settings.appearance.removeBackground')}
              >
                <X className="h-3.5 w-3.5" />
              </button>
            </div>
          ))}
          <div className="appearance-background-card relative h-full min-h-0">
            <button
              type="button"
              onClick={() => fileInputRef.current?.click()}
              className="flex h-[calc(100%_-_4px)] min-h-[110px] w-full flex-col items-center justify-center gap-2 rounded-xl border border-dashed border-[hsl(var(--glass-border))] text-xs text-muted-soft transition-colors hover:border-foreground hover:text-foreground"
            >
              <ImagePlus className="h-5 w-5" /><span>{t('settings.appearance.uploadBackground')}</span>
            </button>
          </div>
          <input ref={fileInputRef} type="file" accept="image/*" className="hidden" onChange={(event) => { void uploadBackground(event.target.files?.[0]); event.target.value = '' }} />
        </div>
      </section>

      <section className="space-y-4">
        <div>
          <h3 className="text-sm font-semibold text-foreground">{t('settings.appearance.materialTitle')}</h3>
          <p className="mt-1 text-xs leading-5 text-muted-soft">{t('settings.appearance.materialHint')}</p>
        </div>
        {appearanceSliderFields.map((field) => (
          <label key={field.key} className="block space-y-2">
            <span className="flex items-center justify-between gap-3 text-sm">
              <span className="font-medium text-foreground">{t(field.labelKey)}</span>
              <span className="text-xs tabular-nums text-muted-soft">{draft[field.key]}{field.unit}</span>
            </span>
            <Slider
              value={draft[field.key]}
              onValueChange={(value) => onChange({ ...draft, [field.key]: value })}
              aria-label={t(field.labelKey)}
            />
            <span className="flex justify-between text-[11px] text-faint"><span>{t(field.minLabelKey)}</span><span>{t(field.maxLabelKey)}</span></span>
          </label>
        ))}
        <p className="pt-1 text-xs text-muted-soft">{t('settings.appearance.savedHint')}</p>
      </section>
    </div>
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

function SkillsPane({ nav, onNavigate, onActionsChange }: { nav: SkillsNav; onNavigate: (nav: SkillsNav) => void; onActionsChange: (actions: SettingsActions | null) => void }) {
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
        onActionsChange={onActionsChange}
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
function SkillEditor({ name, skills, onBack, onActionsChange }: { name: string | null; skills: AgentSkill[]; onBack: () => void; onActionsChange: (actions: SettingsActions | null) => void }) {
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

  const { mutate: saveMutate, isPending: savePending } = save
  const saveAction = useCallback(() => saveMutate(), [saveMutate])

  useEffect(() => {
    onActionsChange({ save: saveAction, canSave, pending: savePending })
    return () => onActionsChange(null)
  }, [canSave, onActionsChange, saveAction, savePending])

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
          className="h-0 min-h-0 flex-1 resize-none font-mono text-xs leading-5 focus:ring-inset"
          disabled={bodyLoading}
        />
      </label>

      <div className="flex items-center gap-2">
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

function AgentsMdPane({ onActionsChange }: { onActionsChange: (actions: SettingsActions | null) => void }) {
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

  const { mutate: saveMutate, isPending: savePending } = save
  const saveAction = useCallback(() => saveMutate(), [saveMutate])

  useEffect(() => {
    onActionsChange({ save: saveAction, canSave: !overLimit && !fileQuery.isLoading, pending: savePending })
    return () => onActionsChange(null)
  }, [fileQuery.isLoading, onActionsChange, overLimit, saveAction, savePending])

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
        className="h-0 min-h-0 flex-1 resize-none font-mono text-xs leading-5 focus:ring-inset"
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
        </div>
      </div>
      <p className="text-xs text-faint">{t('settings.effectiveHint')}</p>
    </div>
  )
}
