import { describe, expect, it } from 'vitest'
import {
  acceptEnableDialog,
  beginEnable,
  cancelEnable,
  enableFlowOffersUntrusted,
  failEnable,
  resumeEnableFlow,
  type EnableFlowState,
  type EnableFlowStep,
} from '@/features/settings/components/js-plugins/enable-flow'
import { jsPluginErrorNotice } from '@/features/settings/components/js-plugins/js-plugin-errors'

function stateOf(step: EnableFlowStep): EnableFlowState {
  if (step.type === 'cancel') throw new Error('missing state')
  return step.state
}

describe('jsPluginErrorNotice', () => {
  it.each([
    ['js_plugin_untrusted_not_allowed', {}, { key: 'settings:jsPlugins.errors.untrustedNotAllowed' }],
    ['js_plugin_site_type_locked', { from: 'acme.example/chat', to: 'acme.example/other', count: 2 }, {
      key: 'settings:jsPlugins.errors.siteTypeLocked',
      params: { from: 'acme.example/chat', to: 'acme.example/other', count: 2 },
    }],
    ['js_plugin_in_use', { site_type: 'acme.example/chat', count: 3 }, {
      key: 'settings:jsPlugins.errors.inUse',
      params: { count: 3, site_type: 'acme.example/chat' },
    }],
    ['js_plugin_hosts_required', {}, { key: 'settings:jsPlugins.errors.hostsRequired' }],
    ['js_plugin_unknown', {}, null],
    [undefined, {}, null],
  ] as const)('maps %s', (code, fields, notice) => {
    expect(jsPluginErrorNotice(code, fields)).toEqual(notice)
  })
})

describe('enable flow', () => {
  it('opens the host list after hosts_required and retries with confirm_hosts', () => {
    const started = beginEnable({ pluginId: 'acme.example', version: '1.1.0', trust: 'trusted', siteProvider: true })
    expect(started).toMatchObject({
      type: 'request',
      request: { pluginId: 'acme.example', version: '1.1.0' },
    })
    if (started.type !== 'request') return
    expect(started.request.confirmHosts).toBeUndefined()
    expect(started.request.confirmUntrusted).toBeUndefined()

    const asked = failEnable(started.state, {
      code: 'js_plugin_hosts_required',
      fields: { hosts: ['api.example.com', 'files.example.com'] },
    })
    expect(asked.type).toBe('dialog')
    expect(stateOf(asked).dialog).toBe('hosts')
    expect(stateOf(asked).hosts).toEqual(['api.example.com', 'files.example.com'])

    const accepted = acceptEnableDialog(stateOf(asked))
    expect(accepted).toMatchObject({
      type: 'request',
      request: {
        pluginId: 'acme.example',
        version: '1.1.0',
        confirmHosts: ['api.example.com', 'files.example.com'],
      },
    })
    if (accepted.type === 'request') expect(accepted.request.confirmUntrusted).toBeUndefined()
  })

  it('does not enable when the host confirmation is cancelled', () => {
    const started = beginEnable({ pluginId: 'acme.example', version: '1.0.0', siteProvider: true })
    if (started.type !== 'request') throw new Error('expected request')
    const asked = failEnable(started.state, { code: 'js_plugin_hosts_required', fields: { hosts: ['api.example.com'] } })
    expect(cancelEnable()).toEqual({ type: 'cancel' })
    expect(asked.type).not.toBe('request')
  })

  it('asks for untrusted confirmation and permissions before hosts, and keeps both', () => {
    const started = beginEnable({
      pluginId: 'auto.example',
      version: '1.0.0',
      trust: 'untrusted_signer',
      signer: 'dev',
      permissions: ['notify'],
    })
    expect(started.type).toBe('dialog')
    expect(stateOf(started).dialog).toBe('trust')
    expect(enableFlowOffersUntrusted(stateOf(started))).toBe(true)

    const acceptedTrust = acceptEnableDialog(stateOf(started))
    expect(acceptedTrust).toMatchObject({
      type: 'request',
      request: {
        confirmUntrusted: true,
        grantPermissions: ['notify'],
      },
    })
    if (acceptedTrust.type !== 'request') return
    expect(acceptedTrust.request.confirmHosts).toBeUndefined()

    const asked = failEnable(acceptedTrust.state, {
      code: 'js_plugin_hosts_required',
      fields: { hosts: ['api.example.com'] },
    })
    expect(stateOf(asked).dialog).toBe('hosts')
    expect(stateOf(asked).trustAccepted).toBe(true)

    const acceptedHosts = acceptEnableDialog(stateOf(asked))
    expect(acceptedHosts).toMatchObject({
      type: 'request',
      request: {
        pluginId: 'auto.example',
        version: '1.0.0',
        confirmUntrusted: true,
        grantPermissions: ['notify'],
        confirmHosts: ['api.example.com'],
      },
    })
  })

  it('shows trust before hosts when both are still pending, without dropping the host list', () => {
    const started = beginEnable({
      pluginId: 'auto.example',
      version: '1.0.0',
      trust: 'unsigned',
      permissions: ['notify'],
    })
    const both = failEnable(stateOf(started), {
      code: 'js_plugin_hosts_required',
      fields: { hosts: ['api.example.com'] },
    })
    expect(stateOf(both).dialog).toBe('trust')
    expect(stateOf(both).hosts).toEqual(['api.example.com'])

    const afterTrust = acceptEnableDialog(stateOf(both))
    expect(stateOf(afterTrust).dialog).toBe('hosts')
    expect(afterTrust.type).toBe('dialog')

    const afterHosts = acceptEnableDialog(stateOf(afterTrust))
    expect(afterHosts).toMatchObject({
      type: 'request',
      request: {
        confirmUntrusted: true,
        grantPermissions: ['notify'],
        confirmHosts: ['api.example.com'],
      },
    })
  })

  it('does not open the untrusted dialog for an untrusted site_provider', () => {
    const started = beginEnable({
      pluginId: 'acme.example',
      version: '1.0.0',
      trust: 'unsigned',
      siteProvider: true,
    })
    expect(started.type).toBe('request')
    expect(enableFlowOffersUntrusted(stateOf(started))).toBe(false)
    if (started.type !== 'request') return
    expect(started.request.confirmUntrusted).toBeUndefined()

    const denied = failEnable(started.state, { code: 'js_plugin_untrusted_not_allowed' })
    expect(denied).toMatchObject({ type: 'toast', code: 'js_plugin_untrusted_not_allowed' })
    expect(stateOf(denied).dialog).toBeNull()

    const confirm = failEnable(started.state, { code: 'js_plugin_confirm_required' })
    expect(confirm).toMatchObject({ type: 'toast', code: 'js_plugin_untrusted_not_allowed' })
    expect(stateOf(confirm).dialog).toBeNull()
  })

  it('keeps an accepted host list when permissions are required again', () => {
    const started = beginEnable({
      pluginId: 'auto.example',
      version: '2.0.0',
      trust: 'trusted',
      permissions: ['notify'],
    })
    const granted = acceptEnableDialog(stateOf(started))
    if (granted.type !== 'request') throw new Error('expected request')
    const hosts = failEnable(granted.state, { code: 'js_plugin_hosts_required', fields: { hosts: ['api.example.com'] } })
    const withHosts = acceptEnableDialog(stateOf(hosts))
    if (withHosts.type !== 'request') throw new Error('expected request')

    const again = failEnable(withHosts.state, {
      code: 'js_plugin_permissions_required',
      permissions: ['notify', 'http.request'],
    })
    expect(stateOf(again).dialog).toBe('trust')
    expect(stateOf(again).hostsAccepted).toBe(true)
    expect(enableFlowOffersUntrusted(stateOf(again))).toBe(false)

    const retried = acceptEnableDialog(stateOf(again))
    expect(retried).toMatchObject({
      type: 'request',
      request: {
        grantPermissions: ['notify', 'http.request'],
        confirmHosts: ['api.example.com'],
      },
    })
    if (retried.type === 'request') expect(retried.request.confirmUntrusted).toBeUndefined()
  })

  it('toasts when hosts_required has no host list', () => {
    const started = beginEnable({ pluginId: 'acme.example', version: '1.0.0', siteProvider: true })
    if (started.type !== 'request') throw new Error('expected request')
    const failed = failEnable(started.state, { code: 'js_plugin_hosts_required', fields: {} })
    expect(failed).toMatchObject({ type: 'toast', code: 'js_plugin_hosts_required' })
    expect(stateOf(failed).dialog).toBeNull()
  })

  it('resumes confirm_untrusted from the request that was already sent', () => {
    const resumed = resumeEnableFlow({
      pluginId: 'auto.example',
      version: '1.0.0',
      trust: 'trusted',
      permissions: ['notify'],
      confirmUntrusted: true,
      grantPermissions: ['notify'],
    })
    const asked = failEnable(resumed, { code: 'js_plugin_hosts_required', fields: { hosts: ['api.example.com'] } })
    expect(stateOf(asked).dialog).toBe('hosts')
    const accepted = acceptEnableDialog(stateOf(asked))
    expect(accepted).toMatchObject({
      type: 'request',
      request: {
        confirmUntrusted: true,
        grantPermissions: ['notify'],
        confirmHosts: ['api.example.com'],
      },
    })
  })
})
