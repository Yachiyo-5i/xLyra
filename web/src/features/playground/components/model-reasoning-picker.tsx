import { useTranslation } from 'react-i18next'
import { ModelParameterPicker } from '@/features/playground/components/model-parameter-picker'
import { normalizeReasoningEffort, reasoningEffortsForModel } from '@/features/playground/lib/reasoning'
import type { GatewayModel, ReasoningEffort } from '@/features/playground/lib/types'

type ModelReasoningPickerProps = {
  apiKeys?: Array<{ id: string; name: string }>
  apiKeyId?: string | null
  onAPIKeyChange?: (id: string) => void
  models: GatewayModel[]
  model: string | null
  onModelChange: (id: string) => void
  effort: ReasoningEffort
  onEffortChange: (effort: ReasoningEffort) => void
  disabled?: boolean
  triggerClassName?: string
  panelClassName?: string
  subPanelClassName?: string
  panelRenderer?: (children: React.ReactNode, kind: 'panel' | 'subpanel') => React.ReactNode
}

export function ModelReasoningPicker({
  apiKeys,
  apiKeyId,
  onAPIKeyChange,
  models,
  model,
  onModelChange,
  effort,
  onEffortChange,
  disabled,
  triggerClassName,
  panelClassName,
  subPanelClassName,
  panelRenderer,
}: ModelReasoningPickerProps) {
  const { t } = useTranslation('playground')
  const selectedModel = models.find((item) => item.id === model)
  const availableEfforts = reasoningEffortsForModel(selectedModel)
  const effectiveEffort = normalizeReasoningEffort(selectedModel, effort)

  return (
    <ModelParameterPicker
      apiKeys={apiKeys}
      apiKeyId={apiKeyId}
      onAPIKeyChange={onAPIKeyChange}
      models={models}
      model={model}
      onModelChange={onModelChange}
      parameterLabel={t('picker.reasoning')}
      parameterValue={effectiveEffort}
      parameterOptions={availableEfforts.map((value) => ({ value, label: t(`effort.${value}`) }))}
      onParameterChange={onEffortChange}
      disabled={disabled}
      triggerClassName={triggerClassName}
      panelClassName={panelClassName}
      subPanelClassName={subPanelClassName}
      panelRenderer={panelRenderer}
    />
  )
}
