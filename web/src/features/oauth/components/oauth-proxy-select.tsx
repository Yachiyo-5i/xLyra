import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { fetchSystemProxyConfig } from '@/features/settings/api/settings'

export function OAuthProxySelect({
  value,
  disabled,
  onChange,
}: {
  value: string
  disabled?: boolean
  onChange: (value: string) => void
}) {
  const { t } = useTranslation('oauth')
  const systemProxyQuery = useQuery({ queryKey: ['settings', 'system-proxy'], queryFn: fetchSystemProxyConfig })
  const proxies = systemProxyQuery.data?.proxies ?? []

  return (
    <div className="space-y-2">
      <div className="text-sm font-medium text-foreground">{t('edit.fields.proxy')}</div>
      <Select value={value || 'direct'} onValueChange={onChange} disabled={disabled || systemProxyQuery.isLoading}>
        <SelectTrigger>
          <SelectValue />
        </SelectTrigger>
        <SelectContent searchable={false}>
          <SelectItem value="direct">{t('edit.fields.proxyDirect')}</SelectItem>
          {proxies.map((proxy) => (
            <SelectItem key={proxy.id} value={proxy.id}>
              {proxy.name}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <p className="text-xs leading-5 text-muted-soft">{t('edit.fields.proxyDesc')}</p>
    </div>
  )
}
