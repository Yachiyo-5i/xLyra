import { describe, expect, it } from 'vitest'
import { canonicalModelIconInfo, modelNameIconInfo } from './model-icon'

describe('model icon resolution', () => {
  it('uses the provider icon when a canonical model has no icon URL', () => {
    const icon = canonicalModelIconInfo({
      id: 'canonical-jev-latest',
      model_key: 'jev-latest',
      display_name: 'Jev Latest',
      provider: ' TYPESAFE ',
      category: 'chat',
      capabilities: {},
      status: 'active',
      created_at: '',
      updated_at: '',
      site_model_count: 1,
      site_count: 1,
      icon_url: '',
      aliases: [],
    })

    expect(icon).toMatchObject({
      iconPath: '/brand-icons/typesafe-dark.png',
      label: 'TypeSafe',
    })
  })

  it('recognizes Jev models without canonical metadata', () => {
    expect(modelNameIconInfo('jev-latest')).toMatchObject({
      iconPath: '/brand-icons/typesafe-dark.png',
      label: 'TypeSafe',
    })
  })
})
