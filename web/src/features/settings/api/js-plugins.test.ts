import { describe, expect, it } from 'vitest'
import enSettings from '@/locales/en/settings.json'
import jpSettings from '@/locales/jp/settings.json'
import zhSettings from '@/locales/zh/settings.json'
import {
  automationManifest,
  enableJSPluginRequestBody,
  grantedHostPermissions,
  siteProviderOf,
} from '@/features/settings/api/js-plugins'
import { readJSPluginErrorFields } from '@/features/settings/components/js-plugins/js-plugin-errors'
import { permissionLabelKey } from '@/features/settings/components/js-plugins/permissions'

function leafPaths(value: unknown, prefix = ''): string[] {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) return prefix ? [prefix] : []
  return Object.keys(value).sort().flatMap((key) => leafPaths((value as Record<string, unknown>)[key], prefix ? `${prefix}.${key}` : key))
}

describe('automationManifest', () => {
  it('reads the permissions an automation version declares', () => {
    const manifest = automationManifest({
      kind: 'automation',
      automation: {
        subscribes: ['oauth.quota_synced'],
        permissions: ['apikey.reset_usage'],
        inputs: [
          { name: 'account', type: 'oauth_connection', providers: ['codex'], eventSubject: true },
          { name: 'keys', type: 'api_key', multiple: true },
        ],
      },
    })
    expect(manifest?.permissions).toEqual(['apikey.reset_usage'])
    expect(manifest?.inputs.map((input) => input.name)).toEqual(['account', 'keys'])
  })

  it('is null for other kinds, so enabling them asks for nothing extra', () => {
    expect(automationManifest({ kind: 'model_list' })).toBeNull()
    expect(automationManifest(undefined)).toBeNull()
  })

  it('tolerates a section without permissions', () => {
    expect(automationManifest({ automation: { inputs: [] } })?.permissions).toEqual([])
  })
})

describe('enableJSPluginRequestBody', () => {
  it('includes confirm_hosts and the other enable flags', () => {
    expect(enableJSPluginRequestBody({ confirm_hosts: ['api.example.com', 'files.example.com'] })).toEqual({
      confirm_hosts: ['api.example.com', 'files.example.com'],
    })
    expect(enableJSPluginRequestBody({
      confirm_untrusted: true,
      grant_permissions: ['notify'],
      confirm_hosts: ['api.example.com'],
    })).toEqual({
      confirm_untrusted: true,
      grant_permissions: ['notify'],
      confirm_hosts: ['api.example.com'],
    })
    expect(enableJSPluginRequestBody()).toEqual({})
  })
})

describe('readJSPluginErrorFields', () => {
  it('reads hosts and the other plugin error fields', () => {
    expect(readJSPluginErrorFields({
      code: 'js_plugin_hosts_required',
      message: 'confirm hosts',
      hosts: ['api.example.com'],
      from: 'acme.example/chat',
      to: 'acme.example/other',
      count: 2,
      site_type: 'acme.example/chat',
    })).toEqual({
      hosts: ['api.example.com'],
      from: 'acme.example/chat',
      to: 'acme.example/other',
      count: 2,
      site_type: 'acme.example/chat',
    })
  })

  it('omits hosts that are not a list of hostnames', () => {
    expect(readJSPluginErrorFields({ hosts: 'api.example.com' })).toEqual({})
    expect(readJSPluginErrorFields(undefined)).toEqual({})
  })
})

describe('siteProviderOf', () => {
  it('reads the site type, display name and hosts', () => {
    expect(siteProviderOf({
      kind: 'site_provider',
      siteProvider: {
        siteType: 'acme.example/chat',
        display: { name: { zh: '示例', en: 'Example' } },
        hosts: ['api.example.com'],
      },
    })).toEqual({
      siteType: 'acme.example/chat',
      displayName: { zh: '示例', en: 'Example' },
      hosts: ['api.example.com'],
    })
    expect(siteProviderOf({ kind: 'automation' })).toBeNull()
    expect(grantedHostPermissions(['host:api.example.com', 'notify', 'host:'])).toEqual(['host:api.example.com'])
  })
})

describe('jsPlugins locales', () => {
  it('keeps zh, en and jp keys aligned', () => {
    const zh = leafPaths(zhSettings.jsPlugins)
    expect(leafPaths(enSettings.jsPlugins)).toEqual(zh)
    expect(leafPaths(jpSettings.jsPlugins)).toEqual(zh)
  })
})

describe('permissionLabelKey', () => {
  it('maps known permissions to a locale key and leaves unknown ones without one', () => {
    expect(permissionLabelKey('apikey.reset_usage')).toBe('settings:jsPlugins.permission.apikeyResetUsage')
    expect(permissionLabelKey('db.drop')).toBeNull()
  })
})
