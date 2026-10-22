import { createInstance } from 'i18next'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderToStaticMarkup } from 'react-dom/server'
import { I18nextProvider } from 'react-i18next'
import { beforeAll, describe, expect, it, vi } from 'vitest'
import type { ReactNode } from 'react'
import type { SiteAccount } from '@/features/sites/api/sites'
import { AccountImportDialog } from '@/features/sites/components/account-import-dialog'
import { pluginRowView } from '@/features/sites/components/account-provider-rows'
import { SiteTypeIcon } from '@/features/sites/components/site-type-icon'
import { resolveSiteTypeIcon } from '@/features/sites/lib/site-utils'
import translations from '@/locales/zh/sites.json'

// DialogContent portals into document.body. These tests render with
// renderToStaticMarkup, which has no document, so keep the dialog inline.
vi.mock('@/components/ui/dialog', () => ({
  Dialog: ({ children }: { children?: ReactNode }) => <div>{children}</div>,
  DialogContent: ({ children }: { children?: ReactNode }) => <div>{children}</div>,
  DialogHeader: ({ children }: { children?: ReactNode }) => <div>{children}</div>,
  DialogBody: ({ children }: { children?: ReactNode }) => <div>{children}</div>,
  DialogFooter: ({ cancel, confirm }: { cancel?: ReactNode; confirm?: ReactNode }) => <div>{cancel}{confirm}</div>,
  DialogTitle: ({ children }: { children?: ReactNode }) => <h2>{children}</h2>,
}))

const i18n = createInstance()

beforeAll(async () => {
  await i18n.init({ lng: 'zh', resources: { zh: { sites: translations } }, interpolation: { escapeValue: false } })
})

function render(node: ReactNode) {
  const client = new QueryClient()
  return renderToStaticMarkup(
    <QueryClientProvider client={client}>
      <I18nextProvider i18n={i18n}>{node}</I18nextProvider>
    </QueryClientProvider>,
  )
}

function account(extra: Record<string, unknown> = {}): SiteAccount {
  return { credential_id: 'cred-1', masked_token: 'sk-…', enabled: true, ...extra } as SiteAccount
}

describe('SiteTypeIcon', () => {
  it('uses the first character when the image is missing or the load failed', () => {
    const missing = render(<SiteTypeIcon src="" name=" 示例 " className="h-8 w-8" />)
    expect(missing).not.toContain('<img')
    expect(missing).toContain('示')

    const image = render(<SiteTypeIcon src="https://cdn.example/icon.webp" name="Acme" className="h-8 w-8" />)
    expect(image).toContain('<img')
    expect(image).toContain('src="https://cdn.example/icon.webp"')
    expect(image).not.toContain('>A<')

    expect(resolveSiteTypeIcon('https://cdn.example/icon.webp', true, 'hidden', 'Acme')).toEqual({ kind: 'letter', text: 'A' })
    expect(resolveSiteTypeIcon('https://cdn.example/icon.webp', true, 'letter', '  ')).toEqual({ kind: 'letter', text: '?' })
    expect(resolveSiteTypeIcon('  https://cdn.example/icon.webp  ', true, 'hidden', '示例')).toEqual({ kind: 'letter', text: '示' })
  })
})

describe('plugin row pills', () => {
  it('falls back to the raw value when a label is missing for that value or language', () => {
    const labeled = render(pluginRowView(account({ fields: { realm: 'intl', seats: 3, active: true } }), [
      { field: 'realm', labels: { cn: { zh: '国内' } } },
      { field: 'seats' },
      { field: 'active' },
      { field: '  ' },
      { field: 'missing' },
    ], 'zh').pills)
    expect(labeled).toContain('intl')
    expect(labeled).not.toContain('国内')
    expect(labeled).toContain('3')
    expect(labeled).toContain('true')

    const english = render(pluginRowView(account({ fields: { realm: 'cn' } }), [
      { field: 'realm', labels: { cn: { en: 'China', zh: '' } } },
    ], 'zh').pills)
    expect(english).toContain('China')

    const raw = render(pluginRowView(account({ fields: { realm: 'cn' } }), [
      { field: 'realm', labels: { cn: { fr: '' } } },
    ], 'zh').pills)
    expect(raw).toContain('cn')
    expect(pluginRowView(account({ fields: { realm: '   ' } }), [{ field: 'realm' }], 'zh').pills).toBeUndefined()
  })
})

describe('account import copy', () => {
  it('uses i18n when a text key is missing and the plugin string when it is set', () => {
    const markup = render(
      <AccountImportDialog
        open
        siteId="site-1"
        regionLabel={(id) => id}
        onOpenChange={() => {}}
        onImported={() => {}}
        language="zh"
        texts={{ import_dropzone: { zh: '放到这里' } }}
      />,
    )
    expect(markup).toContain('导入账号')
    expect(markup).toContain('放到这里')
    expect(markup).toContain('取消')
    expect(markup).not.toContain('拖拽 JSON 文件到此处')
  })
})
