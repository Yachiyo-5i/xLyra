import type { JSPluginErrorFields } from '@/features/settings/components/js-plugins/js-plugin-errors'

export type EnableTrust = 'trusted' | 'untrusted_signer' | 'unsigned'

export type EnableFlowState = {
  pluginId: string
  version: string
  trust: EnableTrust
  signer?: string
  permissions: string[]
  /** site_provider never offers the untrusted checkbox. confirm_untrusted cannot bypass it. */
  siteProvider: boolean
  trustAccepted: boolean
  hosts: string[]
  hostsRequired: boolean
  hostsAccepted: boolean
  dialog: 'trust' | 'hosts' | null
}

export type EnableRequest = {
  pluginId: string
  version: string
  confirmUntrusted?: boolean
  grantPermissions?: string[]
  confirmHosts?: string[]
}

export type EnableFlowStep =
  | { type: 'dialog'; state: EnableFlowState }
  | { type: 'request'; state: EnableFlowState; request: EnableRequest }
  | { type: 'toast'; state: EnableFlowState; code: string; fields: JSPluginErrorFields }
  | { type: 'cancel' }
  | { type: 'unhandled'; state: EnableFlowState }

export type BeginEnableInput = {
  pluginId: string
  version: string
  trust?: EnableTrust
  signer?: string
  permissions?: string[]
  siteProvider?: boolean
}

export type EnableFailure = {
  code?: string
  fields?: JSPluginErrorFields
  trust?: EnableTrust
  signer?: string
  permissions?: string[]
  siteProvider?: boolean
}

export function enableFlowOffersUntrusted(state: EnableFlowState) {
  return !state.siteProvider && state.trust !== 'trusted'
}

function needsTrust(state: EnableFlowState) {
  if (state.trustAccepted) return false
  if (state.permissions.length > 0) return true
  if (state.siteProvider) return false
  return state.trust !== 'trusted'
}

function requestOf(state: EnableFlowState): EnableRequest {
  const request: EnableRequest = { pluginId: state.pluginId, version: state.version }
  if (state.trustAccepted && enableFlowOffersUntrusted(state)) request.confirmUntrusted = true
  if (state.trustAccepted && state.permissions.length > 0) request.grantPermissions = [...state.permissions]
  if (state.hostsAccepted) request.confirmHosts = [...state.hosts]
  return request
}

/** Trust and automation permissions come first. A pending host list is next, and is not dropped. */
function present(state: EnableFlowState): EnableFlowStep {
  if (needsTrust(state)) return { type: 'dialog', state: { ...state, dialog: 'trust' } }
  if (state.hostsRequired && !state.hostsAccepted) return { type: 'dialog', state: { ...state, dialog: 'hosts' } }
  return { type: 'request', state: { ...state, dialog: null }, request: requestOf(state) }
}

export function beginEnable(input: BeginEnableInput): EnableFlowStep {
  return present({
    pluginId: input.pluginId,
    version: input.version,
    trust: input.trust ?? 'trusted',
    signer: input.signer,
    permissions: input.permissions ?? [],
    siteProvider: input.siteProvider ?? false,
    trustAccepted: false,
    hosts: [],
    hostsRequired: false,
    hostsAccepted: false,
    dialog: null,
  })
}

export function resumeEnableFlow(input: BeginEnableInput & {
  confirmUntrusted?: boolean
  grantPermissions?: string[]
  confirmHosts?: string[]
}): EnableFlowState {
  const granted = input.grantPermissions ?? []
  const hosts = input.confirmHosts
  const trust = input.confirmUntrusted && (input.trust ?? 'trusted') === 'trusted'
    ? 'unsigned'
    : (input.trust ?? 'trusted')
  return {
    pluginId: input.pluginId,
    version: input.version,
    trust,
    signer: input.signer,
    permissions: (input.permissions ?? []).length > 0 ? (input.permissions ?? []) : granted,
    siteProvider: input.siteProvider ?? false,
    trustAccepted: Boolean(input.confirmUntrusted) || granted.length > 0,
    hosts: hosts ?? [],
    hostsRequired: Array.isArray(hosts),
    hostsAccepted: Array.isArray(hosts),
    dialog: null,
  }
}

export function acceptEnableDialog(state: EnableFlowState): EnableFlowStep {
  if (state.dialog === 'trust') return present({ ...state, trustAccepted: true })
  if (state.dialog === 'hosts') return present({ ...state, hostsAccepted: true })
  return present(state)
}

export function cancelEnable(): EnableFlowStep {
  return { type: 'cancel' }
}

export function failEnable(state: EnableFlowState, failure: EnableFailure): EnableFlowStep {
  const next: EnableFlowState = {
    ...state,
    dialog: null,
    trust: failure.trust ?? state.trust,
    signer: failure.signer ?? state.signer,
    permissions: failure.permissions ?? state.permissions,
    siteProvider: failure.siteProvider ?? state.siteProvider,
  }
  const fields = failure.fields ?? {}
  switch (failure.code) {
    case 'js_plugin_untrusted_not_allowed':
      return { type: 'toast', state: next, code: failure.code, fields }
    case 'js_plugin_confirm_required':
      if (next.siteProvider) {
        return { type: 'toast', state: next, code: 'js_plugin_untrusted_not_allowed', fields }
      }
      return present({ ...next, trustAccepted: false })
    case 'js_plugin_permissions_required':
      return present({ ...next, trustAccepted: false })
    case 'js_plugin_hosts_required': {
      if (!fields.hosts || fields.hosts.length === 0) {
        return { type: 'toast', state: next, code: failure.code, fields }
      }
      return present({ ...next, hosts: [...fields.hosts], hostsRequired: true, hostsAccepted: false })
    }
    case 'js_plugin_site_type_locked':
    case 'js_plugin_in_use':
      return { type: 'toast', state: next, code: failure.code, fields }
    default:
      return { type: 'unhandled', state: next }
  }
}
