import { Input } from '@/components/ui/input'
import type { APIKeyFormDraft } from '@/features/sites/components/site-api-key-form-data'

export function SiteAPIKeyFormFields({
  draft,
  onChange,
  showAPIKey,
  showCostMultiplier,
  t,
}: {
  draft: APIKeyFormDraft
  onChange: (value: APIKeyFormDraft) => void
  showAPIKey: boolean
  showCostMultiplier: boolean
  t: (key: string) => string
}) {
  return (
    <div className="space-y-4">
      <label className="block space-y-2">
        <span className="block text-sm font-medium">{t('apiKeys.name')}</span>
        <Input
          value={draft.name}
          maxLength={100}
          onChange={(event) => onChange({ ...draft, name: event.target.value })}
        />
      </label>
      <div
        className={showCostMultiplier ? 'grid grid-cols-2 gap-3' : undefined}
      >
        <label className="block space-y-2">
          <span className="block text-sm font-medium">
            {t('apiKeys.routingPriority')}
          </span>
          <Input
            type="number"
            min="1"
            max="5"
            step="0.1"
            value={draft.routingPriority}
            onChange={(event) =>
              onChange({ ...draft, routingPriority: event.target.value })
            }
          />
        </label>
        {showCostMultiplier ? (
          <label className="block space-y-2">
            <span className="block text-sm font-medium">
              {t('apiKeys.costMultiplier')}
            </span>
            <Input
              type="number"
              min="0.01"
              max="100"
              step="0.0001"
              value={draft.upstreamCostMultiplier}
              onChange={(event) =>
                onChange({
                  ...draft,
                  upstreamCostMultiplier: event.target.value,
                })
              }
            />
          </label>
        ) : null}
      </div>
      {showAPIKey ? (
        <label className="block space-y-2">
          <span className="block text-sm font-medium">ApiKey</span>
          <Input
            value={draft.apiKey}
            type="password"
            autoComplete="off"
            onChange={(event) =>
              onChange({ ...draft, apiKey: event.target.value })
            }
          />
        </label>
      ) : null}
    </div>
  )
}
