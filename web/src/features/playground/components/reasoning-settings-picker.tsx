import { useTranslation } from 'react-i18next'
import { ModelSettingsPicker } from '@/features/playground/components/model-settings-picker'
import { normalizeReasoningEffort, reasoningEffortsForModel } from '@/features/playground/lib/reasoning'
import type { GatewayModel, ReasoningEffort } from '@/features/playground/lib/types'

type ReasoningSettingsPickerProps = {
  models: GatewayModel[]
  model: string | null
  onModelChange: (id: string) => void
  effort: ReasoningEffort
  onEffortChange: (effort: ReasoningEffort) => void
  disabled?: boolean
  triggerClassName?: string
}

/** 对话模式的模型 + 思考强度：档位取自所选模型（服务端声明或内置规则），切换模型后滑块档位随之替换 */
export function ReasoningSettingsPicker({ models, model, onModelChange, effort, onEffortChange, disabled, triggerClassName }: ReasoningSettingsPickerProps) {
  const { t } = useTranslation('playground')
  const selectedModel = models.find((item) => item.id === model)
  const availableEfforts = reasoningEffortsForModel(selectedModel)

  return (
    <ModelSettingsPicker
      models={models}
      model={model}
      onModelChange={onModelChange}
      parameter={{
        kind: 'slider',
        label: t('picker.reasoning'),
        value: normalizeReasoningEffort(selectedModel, effort),
        options: availableEfforts.map((value) => ({ value, label: t(`effort.${value}`) })),
        onChange: onEffortChange,
        defaultValue: normalizeReasoningEffort(selectedModel, 'medium'),
      }}
      disabled={disabled}
      triggerClassName={triggerClassName}
    />
  )
}
