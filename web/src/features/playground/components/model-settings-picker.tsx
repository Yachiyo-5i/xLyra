import { useState } from 'react'
import * as PopoverPrimitive from '@radix-ui/react-popover'
import { useTranslation } from 'react-i18next'
import { Check, ChevronDown, ChevronLeft, ChevronRight, RotateCcw } from 'lucide-react'
import { useLiquidGlassRef } from '@/components/glass/use-liquid-glass'
import { EffortSlider } from '@/features/playground/components/effort-slider'
import { PlaygroundModelIcon } from '@/features/playground/components/playground-model-icon'
import type { GatewayModel } from '@/features/playground/lib/types'
import { cn } from '@/lib/utils'

type ParameterOption<T extends string> = { value: T; label: string }

type ModelSettingsPickerProps<T extends string> = {
  models: GatewayModel[]
  model: string | null
  onModelChange: (id: string) => void
  parameter: {
    label: string
    value: T
    options: ParameterOption<T>[]
    onChange: (value: T) => void
    /** 「恢复默认」回到的值；不传则不显示重置按钮 */
    defaultValue?: T
    /** slider：有先后顺序的档位（思考强度）；chips：并列选项（图片尺寸） */
    kind: 'slider' | 'chips'
  }
  disabled?: boolean
  triggerClassName?: string
}

/** 思考强度的颜色：档位越高越「热」（系统色），与 Codex 的高档位紫色一致 */
const EFFORT_COLORS: Record<string, string> = {
  none: '#8e8e93',
  minimal: '#40c8e0',
  low: '#40c8e0',
  medium: '#0a84ff',
  high: '#5e5ce6',
  xhigh: '#9b5cf0',
  max: '#bf5af2',
  ultra: '#ff375f',
}

/**
 * 模型与参数选择（仿 Codex）：输入框里的触发器显示「图标 · 模型 · 参数」；
 * 点开后是一张浮起的卡片——顶部是当前参数（思考强度用对应颜色）和模型名，
 * 下面是可拖动的档位滑块（或尺寸选项）；点模型名进入模型列表，选完回到参数页，
 * 新模型支持的档位会自动替换滑块的档位（外部用 normalizeReasoningEffort 把当前值吸附到新档位）。
 */
export function ModelSettingsPicker<T extends string>({ models, model, onModelChange, parameter, disabled, triggerClassName }: ModelSettingsPickerProps<T>) {
  const { t } = useTranslation('playground')
  const [open, setOpen] = useState(false)
  const [view, setView] = useState<'settings' | 'models'>('settings')
  const glassRef = useLiquidGlassRef<HTMLDivElement>('regular')
  const selectedModel = models.find((item) => item.id === model)
  const selectedOption = parameter.options.find((option) => option.value === parameter.value)
  const modelLabel = selectedModel?.displayName || model || t('credential.modelPlaceholder')
  const color = parameter.kind === 'slider' ? (EFFORT_COLORS[parameter.value] ?? '#0a84ff') : 'var(--tint)'
  const canReset = parameter.defaultValue !== undefined && parameter.defaultValue !== parameter.value
    && parameter.options.some((option) => option.value === parameter.defaultValue)

  return (
    <PopoverPrimitive.Root
      open={open}
      onOpenChange={(next) => {
        setOpen(next)
        if (!next) setView('settings')
      }}
    >
      <PopoverPrimitive.Trigger asChild>
        <button
          type="button"
          disabled={disabled}
          className={cn(
            'inline-flex h-8 max-w-[15rem] items-center gap-1.5 rounded-full px-2.5 text-footnote font-medium text-label-2 outline-none transition-colors hover:bg-fill-3 hover:text-label focus-visible:ring-2 focus-visible:ring-tint/40 disabled:opacity-50 data-[state=open]:bg-fill-3',
            triggerClassName,
          )}
        >
          {selectedModel ? <PlaygroundModelIcon modelId={selectedModel.id} displayName={selectedModel.displayName} ownedBy={selectedModel.ownedBy} /> : null}
          <span className="min-w-0 truncate text-label">{modelLabel}</span>
          <span className="shrink-0" style={parameter.kind === 'slider' ? { color } : undefined}>
            · {selectedOption?.label ?? parameter.value}
          </span>
          <ChevronDown className="size-3.5 shrink-0 opacity-60" />
        </button>
      </PopoverPrimitive.Trigger>
      <PopoverPrimitive.Portal>
        <PopoverPrimitive.Content
          ref={glassRef}
          side="top"
          align="end"
          sideOffset={10}
          collisionPadding={12}
          className="lg-regular z-[130] w-[min(calc(100vw-24px),280px)] origin-(--radix-popover-content-transform-origin) rounded-[20px] p-3 data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=closed]:zoom-out-95 data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[state=open]:zoom-in-95"
          onOpenAutoFocus={(event) => event.preventDefault()}
        >
          {view === 'settings' ? (
            <div className="space-y-3">
              <div className="grid grid-cols-[28px_minmax(0,1fr)_28px] items-center">
                <span aria-hidden="true" />
                {/* 参数与模型两行整体是一个按钮：点哪里都进入模型列表 */}
                <button
                  type="button"
                  onClick={() => setView('models')}
                  className="min-w-0 rounded-[14px] px-2 py-1 text-center transition-colors hover:bg-fill-3"
                >
                  <span className="block text-subhead font-semibold" style={{ color: parameter.kind === 'slider' ? color : 'var(--label-primary)' }}>
                    {selectedOption?.label ?? parameter.value}
                  </span>
                  <span className="mt-0.5 flex max-w-full items-center justify-center gap-1 text-footnote text-label-2">
                    {selectedModel ? <PlaygroundModelIcon modelId={selectedModel.id} displayName={selectedModel.displayName} ownedBy={selectedModel.ownedBy} /> : null}
                    <span className="truncate">{modelLabel}</span>
                    <ChevronRight className="size-3 shrink-0 opacity-60" />
                  </span>
                </button>
                {canReset ? (
                  <button
                    type="button"
                    aria-label={t('picker.reset')}
                    title={t('picker.reset')}
                    onClick={() => parameter.defaultValue !== undefined && parameter.onChange(parameter.defaultValue)}
                    className="flex size-7 items-center justify-center justify-self-end rounded-full text-label-3 transition-colors hover:bg-fill-3 hover:text-label"
                  >
                    <RotateCcw className="size-3.5" />
                  </button>
                ) : (
                  <span />
                )}
              </div>

              {parameter.options.length === 0 ? null : parameter.kind === 'slider' ? (
                <EffortSlider
                  options={parameter.options}
                  value={parameter.value}
                  onChange={parameter.onChange}
                  color={color}
                  ariaLabel={parameter.label}
                />
              ) : (
                <div className="flex flex-wrap gap-2">
                  {parameter.options.map((option) => (
                    <button
                      key={option.value}
                      type="button"
                      onClick={() => parameter.onChange(option.value)}
                      className={cn(
                        'h-8 rounded-full px-3 font-mono text-footnote transition-colors',
                        option.value === parameter.value ? 'bg-tint text-tint-foreground' : 'bg-fill-3 text-label-2 hover:bg-fill-2 hover:text-label',
                      )}
                    >
                      {option.label}
                    </button>
                  ))}
                </div>
              )}
            </div>
          ) : (
            <div className="-mx-1 -my-1">
              <div className="flex items-center gap-1 px-1 pb-1">
                <button
                  type="button"
                  aria-label={t('picker.back')}
                  onClick={() => setView('settings')}
                  className="flex size-8 items-center justify-center rounded-full text-label-2 transition-colors hover:bg-fill-3 hover:text-label"
                >
                  <ChevronLeft className="size-4" />
                </button>
                <p className="text-subhead text-label-3">{t('picker.selectModel')}</p>
              </div>
              <div className="max-h-[min(52vh,360px)] overflow-y-auto overscroll-contain">
                {models.length === 0 ? <p className="px-3 py-2 text-footnote text-label-3">{t('picker.noModels')}</p> : null}
                {models.map((item) => (
                  <button
                    key={item.id}
                    type="button"
                    onClick={() => {
                      onModelChange(item.id)
                      setView('settings')
                    }}
                    className={cn(
                      'flex min-h-9 w-full items-center gap-2 rounded-[12px] px-2.5 text-left text-callout transition-colors hover:bg-fill-3',
                      item.id === model ? 'text-label' : 'text-label-2 hover:text-label',
                    )}
                  >
                    <PlaygroundModelIcon modelId={item.id} displayName={item.displayName} ownedBy={item.ownedBy} />
                    <span className="min-w-0 flex-1 truncate">{item.displayName || item.id}</span>
                    {item.id === model ? <Check className="size-4 shrink-0 text-label-2" /> : null}
                  </button>
                ))}
              </div>
            </div>
          )}
        </PopoverPrimitive.Content>
      </PopoverPrimitive.Portal>
    </PopoverPrimitive.Root>
  )
}
