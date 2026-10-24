import { createInstance } from 'i18next'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, type ReactNode } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { I18nextProvider } from 'react-i18next'
import { beforeAll, describe, expect, it, vi } from 'vitest'
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

type MiniListener = (event: MiniEvent) => void

const miniListeners = new WeakMap<object, Map<string, Set<MiniListener>>>()

function trackListener(target: object, type: string, listener: MiniListener) {
  const byType = miniListeners.get(target) ?? new Map<string, Set<MiniListener>>()
  const set = byType.get(type) ?? new Set<MiniListener>()
  set.add(listener)
  byType.set(type, set)
  miniListeners.set(target, byType)
}

function untrackListener(target: object, type: string, listener: MiniListener) {
  miniListeners.get(target)?.get(type)?.delete(listener)
}

function emitListeners(target: object, event: MiniEvent) {
  for (const listener of miniListeners.get(target)?.get(event.type) ?? []) listener(event)
}

interface MiniEvent {
  type: string
  target: MiniNode | null
  currentTarget: MiniNode | null
  bubbles: boolean
  cancelable: boolean
  defaultPrevented: boolean
  eventPhase: number
  timeStamp: number
  preventDefault: () => void
  stopPropagation: () => void
}

// Vitest here is node, with no jsdom or testing-library. This is enough of a
// DOM for react-dom/client to mount SiteTypeIcon and deliver <img onError>.
class MiniNode {
  nodeType = 1
  nodeName: string
  tagName: string
  namespaceURI: string | null = 'http://www.w3.org/1999/xhtml'
  ownerDocument: MiniDocument
  parentNode: MiniNode | null = null
  childNodes: MiniNode[] = []
  nodeValue: string | null = null
  attributes: Record<string, string> = {}
  style: { setProperty: (name: string, value: string) => void } = { setProperty: () => {} }

  constructor(tag: string, ownerDocument: MiniDocument) {
    this.tagName = tag.toUpperCase()
    this.nodeName = this.tagName
    this.ownerDocument = ownerDocument
  }

  get firstChild(): MiniNode | null {
    return this.childNodes[0] ?? null
  }

  get lastChild(): MiniNode | null {
    return this.childNodes[this.childNodes.length - 1] ?? null
  }

  get nextSibling(): MiniNode | null {
    if (!this.parentNode) return null
    const index = this.parentNode.childNodes.indexOf(this)
    return this.parentNode.childNodes[index + 1] ?? null
  }

  get textContent(): string {
    if (this.nodeType === 3) return this.nodeValue ?? ''
    return this.childNodes.map((child) => child.textContent).join('')
  }

  set textContent(value: string) {
    this.childNodes = []
    if (value) this.appendChild(this.ownerDocument.createTextNode(value))
  }

  get className() {
    return this.attributes.class ?? ''
  }

  set className(value: string) {
    this.attributes.class = String(value)
  }

  setAttribute(name: string, value: string) {
    this.attributes[name] = String(value)
  }

  getAttribute(name: string) {
    return this.attributes[name] ?? null
  }

  removeAttribute(name: string) {
    delete this.attributes[name]
  }

  hasAttribute(name: string) {
    return Object.prototype.hasOwnProperty.call(this.attributes, name)
  }

  setAttributeNS(_namespace: string, name: string, value: string) {
    this.setAttribute(name, value)
  }

  appendChild<T extends MiniNode>(child: T): T {
    this.insertBefore(child, null)
    return child
  }

  insertBefore<T extends MiniNode>(child: T, before: MiniNode | null): T {
    if (child.parentNode) child.parentNode.removeChild(child)
    child.parentNode = this
    if (!before) {
      this.childNodes.push(child)
      return child
    }
    const index = this.childNodes.indexOf(before)
    if (index < 0) throw new Error('insertBefore target is not a child')
    this.childNodes.splice(index, 0, child)
    return child
  }

  removeChild<T extends MiniNode>(child: T): T {
    const index = this.childNodes.indexOf(child)
    if (index < 0) throw new Error('removeChild target is not a child')
    this.childNodes.splice(index, 1)
    child.parentNode = null
    return child
  }

  addEventListener(type: string, listener: MiniListener) {
    trackListener(this, type, listener)
  }

  removeEventListener(type: string, listener: MiniListener) {
    untrackListener(this, type, listener)
  }

  dispatchEvent(event: MiniEvent) {
    event.target = this
    event.currentTarget = this
    emitListeners(this, event)
    return true
  }
}

class MiniDocument {
  nodeType = 9
  nodeName = '#document'
  documentElement: MiniNode
  body: MiniNode
  defaultView = globalThis

  constructor() {
    this.documentElement = new MiniNode('html', this)
    this.body = new MiniNode('body', this)
    this.documentElement.appendChild(this.body)
  }

  createElement(tag: string) {
    const node = new MiniNode(tag, this)
    if (tag === '#text') {
      node.nodeType = 3
      node.nodeName = '#text'
      node.tagName = '#text'
    }
    return node
  }

  createElementNS(_namespace: string, tag: string) {
    return this.createElement(tag)
  }

  createTextNode(text: string) {
    const node = this.createElement('#text')
    node.nodeValue = text
    return node
  }

  addEventListener(type: string, listener: MiniListener) {
    trackListener(this, type, listener)
  }

  removeEventListener(type: string, listener: MiniListener) {
    untrackListener(this, type, listener)
  }
}

function findTag(node: MiniNode, tag: string): MiniNode | null {
  if (node.tagName === tag) return node
  for (const child of node.childNodes) {
    const found = findTag(child, tag)
    if (found) return found
  }
  return null
}

function errorEvent(target: MiniNode): MiniEvent {
  return {
    type: 'error',
    target,
    currentTarget: target,
    bubbles: false,
    cancelable: false,
    defaultPrevented: false,
    eventPhase: 2,
    timeStamp: 0,
    preventDefault: () => {},
    stopPropagation: () => {},
  }
}

async function mountIcon(node: ReactNode) {
  const doc = new MiniDocument()
  const container = doc.createElement('div')
  const globals = globalThis as unknown as {
    window: unknown
    document: unknown
    HTMLIFrameElement?: unknown
    IS_REACT_ACT_ENVIRONMENT?: boolean
  }
  const previous = {
    window: globals.window,
    document: globals.document,
    HTMLIFrameElement: globals.HTMLIFrameElement,
  }
  class FrameElement {}
  globals.window = globalThis
  globals.document = doc
  globals.HTMLIFrameElement = FrameElement
  globals.IS_REACT_ACT_ENVIRONMENT = true
  const restore = () => {
    globals.window = previous.window
    globals.document = previous.document
    globals.HTMLIFrameElement = previous.HTMLIFrameElement
  }
  try {
    const { createRoot } = await import('react-dom/client')
    const root = createRoot(container as unknown as Element)
    await act(async () => {
      root.render(node)
    })
    return {
      container,
      async unmount() {
        await act(async () => {
          root.unmount()
        })
        restore()
      },
    }
  } catch (error) {
    restore()
    throw error
  }
}

describe('SiteTypeIcon', () => {
  it('uses the first character when the image is missing or the load failed', async () => {
    const missing = render(<SiteTypeIcon src="" name=" 示例 " className="h-8 w-8" />)
    expect(missing).not.toContain('<img')
    expect(missing).toContain('示')

    const mounted = await mountIcon(<SiteTypeIcon src="https://cdn.example/icon.webp" name="Acme" className="h-8 w-8" />)
    try {
      const img = findTag(mounted.container, 'IMG')
      expect(img).not.toBeNull()
      expect(img?.getAttribute('src')).toBe('https://cdn.example/icon.webp')
      expect(mounted.container.textContent).not.toContain('A')
      if (!img) return
      await act(async () => {
        img.dispatchEvent(errorEvent(img))
      })
      expect(findTag(mounted.container, 'IMG')?.tagName ?? null).toBeNull()
      expect(mounted.container.textContent).toBe('A')
    } finally {
      await mounted.unmount()
    }

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
