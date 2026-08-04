import { useTranslation } from 'react-i18next'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
} from '@/components/ui/select'
import type { ChatProtocol } from '@/features/playground/lib/types'

type ProtocolPickerProps = {
  protocols: ChatProtocol[]
  value: ChatProtocol
  onChange: (protocol: ChatProtocol) => void
  disabled?: boolean
}

export function ProtocolPicker({ protocols, value, onChange, disabled }: ProtocolPickerProps) {
  const { t } = useTranslation('playground')
  const label = (protocol: ChatProtocol) =>
    protocol === 'responses' ? t('protocol.responses') : t('protocol.chat')

  return (
    <Select
      value={value}
      onValueChange={(next) => onChange(next as ChatProtocol)}
      disabled={disabled}
    >
      <SelectTrigger className="inline-flex h-8 w-auto gap-1 rounded-full border-none bg-transparent px-2.5 text-xs font-medium text-muted-soft outline-none transition-colors hover:bg-[hsl(var(--surface-subtle))] hover:text-foreground focus-visible:ring-2 focus-visible:ring-[hsl(var(--ring-soft))] disabled:opacity-50">
        {label(value)}
      </SelectTrigger>
      <SelectContent searchable={false} widthMode="content">
        {protocols.map((protocol) => (
          <SelectItem key={protocol} value={protocol}>
            {label(protocol)}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}
