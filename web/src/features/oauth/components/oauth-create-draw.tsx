import type { ReactNode } from 'react'
import { LoaderCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Draw, DrawBody, DrawContent, DrawFooter, DrawHeader, DrawTitle } from '@/components/ui/draw'
import { Input } from '@/components/ui/input'
import { cn } from '@/lib/utils'
import type { OAuthProvider } from '@/features/oauth/api/oauth'
import { OAuthProxySelect } from '@/features/oauth/components/oauth-proxy-select'
import { copyText, DEFAULT_CREATE_STATE, getOAuthSources } from '@/features/oauth/lib/oauth-utils'
import type { OAuthCreateState } from '@/features/oauth/lib/types'

export function OAuthCreateDraw({
  open,
  state,
  startPending,
  startSource,
  completePending,
  onOpenChange,
  onStateChange,
  onGenerate,
  onSubmitCallback,
  title,
  proxyId,
  onProxyIdChange,
}: {
  open: boolean
  state: OAuthCreateState
  startPending: boolean
  startSource: OAuthProvider | null
  completePending: boolean
  onOpenChange: (open: boolean) => void
  onStateChange: (state: OAuthCreateState) => void
  onGenerate: (source: OAuthProvider) => void
  onSubmitCallback: () => void
  title?: string
  proxyId: string
  onProxyIdChange: (value: string) => void
}) {
  const { t } = useTranslation('oauth')
  const oauthSources = getOAuthSources(t)
  const activeSource = oauthSources.find((item) => item.value === state.source) ?? oauthSources[0]

  return (
    <Draw open={open} onOpenChange={onOpenChange}>
      <DrawContent side="right" size="wide" onOpenAutoFocus={(event) => event.preventDefault()}>
        <DrawHeader>
          <DrawTitle>{title ?? t('create.title')}</DrawTitle>
        </DrawHeader>
        <DrawBody className="min-w-0 max-w-full space-y-5">
          {!state.authorize ? (
            <div className="space-y-3">
              <div className="space-y-3">
                {oauthSources.map((item) => {
                  const generating = startPending && startSource === item.value
                  return (
                    <button
                      key={item.value}
                      type="button"
                      disabled={!item.enabled || generating}
                      onClick={() => onGenerate(item.value)}
                      className={cn(
                        'flex w-full items-center gap-3 rounded-lg border px-4 py-3 text-left transition-colors',
                        'border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-panel))] hover:bg-[hsl(var(--surface-subtle))]',
                        (!item.enabled || generating) && 'cursor-not-allowed opacity-55',
                      )}
                    >
                      <img src={item.iconPath} alt="" className="h-8 w-8 shrink-0 rounded-md object-contain" />
                      <div className="min-w-0 flex-1">
                        <div className="text-sm font-semibold text-foreground">{item.label}</div>
                        <div className="truncate text-xs text-muted-soft">{item.subtitle}</div>
                      </div>
                      {!item.enabled ? (
                        <Badge variant="neutral">{t('sources.comingSoon')}</Badge>
                      ) : generating ? (
                        <Badge variant="accent">
                          <LoaderCircle className="h-3.5 w-3.5 animate-spin" />
                          {t('sources.generating')}
                        </Badge>
                      ) : null}
                    </button>
                  )
                })}
              </div>
            </div>
          ) : (
            <div className="min-w-0 max-w-full space-y-4 overflow-hidden">
              <div className="flex max-w-full items-center gap-3 rounded-lg border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-subtle))] px-4 py-3">
                <img src={activeSource.iconPath} alt="" className="h-9 w-9 shrink-0 rounded-md object-contain" />
                <div className="min-w-0">
                  <div className="text-sm font-semibold text-foreground">{activeSource.label}</div>
                  <div className="truncate text-xs text-muted-soft">{activeSource.subtitle}</div>
                </div>
              </div>

              <ReadOnlyField
                label={t('create.authorizeUrl')}
                value={state.authorize.authorize_url}
                actions={(
                  <>
                    <Button variant="secondary" size="sm" onClick={() => copyText(state.authorize?.authorize_url ?? '', t('workspace.toast.linkCopied'))}>
                      {t('create.copyLink')}
                    </Button>
                    <Button variant="secondary" size="sm" onClick={() => window.open(state.authorize?.authorize_url, '_blank', 'noopener,noreferrer')}>
                      {t('create.openPage')}
                    </Button>
                  </>
                )}
              />

              <div className="max-w-full space-y-2 rounded-lg border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-subtle))] px-4 py-3 text-sm text-muted-soft">
                <div>{t('create.instructions')}</div>
              </div>

              <OAuthProxySelect
                value={proxyId}
                onChange={onProxyIdChange}
              />

              <div className="max-w-full space-y-2">
                <div className="text-sm font-medium text-foreground">{t('create.resultUrl')}</div>
                <Input
                  value={state.callbackInput}
                  onChange={(event) => onStateChange({ ...state, callbackInput: event.target.value })}
                  placeholder={t('create.resultPlaceholder')}
                />
              </div>
            </div>
          )}
        </DrawBody>
        <DrawFooter>
          {state.authorize ? (
            <>
              <Button
                onClick={onSubmitCallback}
                disabled={!state.callbackInput.trim() || completePending}
              >
                {completePending ? <LoaderCircle className="h-4 w-4 animate-spin" /> : null}
                {t('create.submit')}
              </Button>
              <Button
                variant="ghost"
                onClick={() => onStateChange({ ...DEFAULT_CREATE_STATE })}
                disabled={completePending}
              >
                {t('create.back')}
              </Button>
            </>
          ) : null}
        </DrawFooter>
      </DrawContent>
    </Draw>
  )
}

function ReadOnlyField({
  label,
  value,
  actions,
}: {
  label: string
  value: string
  actions?: ReactNode
}) {
  return (
    <div className="min-w-0 max-w-full space-y-2 overflow-hidden">
      <div className="text-sm font-medium text-foreground">{label}</div>
      <code className="block w-full min-w-0 max-w-full overflow-x-auto rounded-md border border-[hsl(var(--glass-border))] bg-[hsl(var(--surface-panel))] px-3 py-2 whitespace-nowrap text-xs text-foreground">
        {value}
      </code>
      {actions ? (
        <div className="flex flex-wrap gap-2">
          {actions}
        </div>
      ) : null}
    </div>
  )
}
