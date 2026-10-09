/** Locale key (under settings:jsPlugins.permission) of each automation permission. */
const PERMISSION_LABEL_KEYS: Record<string, string> = {
  'apikey.reset_usage': 'apikeyResetUsage',
  notify: 'notify',
}

export function permissionLabelKey(permission: string) {
  const key = PERMISSION_LABEL_KEYS[permission]
  return key ? `settings:jsPlugins.permission.${key}` : null
}
