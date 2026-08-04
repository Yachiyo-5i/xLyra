import type { ModelsDrawItem } from '@/components/common/models-draw'
import type { CanonicalModelItem, Site, SiteAPIKey, SiteModel } from '@/features/sites/api/sites'
import { modelNameIconInfo, siteModelIconInfo } from '@/features/sites/lib/model-icon'
import { apiKeyModels, isNewAPISite } from '@/features/sites/lib/site-utils'

export function buildDisplayModelItems(
  site: Site | null,
  models: SiteModel[],
  apiKeys: SiteAPIKey[],
  canonicalModels: CanonicalModelItem[] = [],
): ModelsDrawItem[] {
  if (!site) return []
  const canonicalMap = new Map(canonicalModels.map((model) => [model.id, model]))

  if (!isNewAPISite(site)) {
    return models.map((model) => ({
      id: model.id,
      displayName: model.upstream_model_name || model.id,
      enabled: model.status === 'active',
      icon: siteModelIconInfo(model, canonicalMap, site),
    }))
  }

  const modelByName = new Map<string, SiteModel>()
  for (const model of models) {
    const keys = [model.upstream_model_name, model.display_name].map((value) => value?.trim()).filter(Boolean) as string[]
    for (const key of keys) {
      if (!modelByName.has(key)) modelByName.set(key, model)
    }
  }

  const deduped = new Map<string, ModelsDrawItem>()
  for (const apiKey of apiKeys) {
    if (!apiKey.enabled) continue
    for (const model of apiKeyModels(apiKey)) {
      if (!model.enabled) continue
      const name = model.name.trim()
      if (!name || deduped.has(name)) continue
      const matched = modelByName.get(name)
      deduped.set(name, {
        id: matched?.id ?? `unmapped:${name}`,
        displayName: name,
        enabled: matched ? matched.status === 'active' : true,
        toggleDisabled: !matched,
        icon: matched ? siteModelIconInfo(matched, canonicalMap, site) : modelNameIconInfo(name),
      })
    }
  }

  return [...deduped.values()].sort((a, b) => a.displayName.localeCompare(b.displayName, 'zh-CN'))
}
