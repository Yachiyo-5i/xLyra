import { BrandMark } from '@/components/common/brand-mark'
import { modelNameIconInfo } from '@/features/sites/lib/model-icon'

type PlaygroundModelIconProps = {
  modelId: string
  displayName?: string
  ownedBy?: string
  size?: 'xs' | 'sm'
}

export function PlaygroundModelIcon({
  modelId,
  displayName,
  ownedBy,
  size = 'xs',
}: PlaygroundModelIconProps) {
  const icon = modelNameIconInfo([modelId, displayName, ownedBy], modelId)

  return (
    <BrandMark
      iconPath={icon.iconPath}
      label={icon.label}
      fallback={icon.fallback}
      fallbackText={icon.fallbackText}
      size={size}
    />
  )
}
