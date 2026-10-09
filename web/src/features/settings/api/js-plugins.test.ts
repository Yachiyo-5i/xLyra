import { describe, expect, it } from 'vitest'
import { automationManifest } from '@/features/settings/api/js-plugins'
import { permissionLabelKey } from '@/features/settings/components/js-plugins/permissions'

describe('automationManifest', () => {
  it('reads the permissions an automation version declares', () => {
    const manifest = automationManifest({
      kind: 'automation',
      automation: {
        subscribes: ['oauth.quota_synced'],
        permissions: ['apikey.reset_usage'],
        binding: { subject: { type: 'oauth_connection', providers: ['codex'] }, target: { type: 'api_key' } },
      },
    })
    expect(manifest?.permissions).toEqual(['apikey.reset_usage'])
    expect(manifest?.binding.subject.providers).toEqual(['codex'])
  })

  it('is null for other kinds, so enabling them asks for nothing extra', () => {
    expect(automationManifest({ kind: 'model_list' })).toBeNull()
    expect(automationManifest(undefined)).toBeNull()
  })

  it('tolerates a section without permissions', () => {
    expect(automationManifest({ automation: { binding: { subject: { type: 'oauth_connection' } } } })?.permissions).toEqual([])
  })
})

describe('permissionLabelKey', () => {
  it('maps known permissions to a locale key and leaves unknown ones without one', () => {
    expect(permissionLabelKey('apikey.reset_usage')).toBe('settings:jsPlugins.permission.apikeyResetUsage')
    expect(permissionLabelKey('db.drop')).toBeNull()
  })
})
