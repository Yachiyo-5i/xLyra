import { describe, expect, it } from 'vitest'
import { moveAPIKey, sortAPIKeysForDisplay } from './api-key-order'

describe('sortAPIKeysForDisplay', () => {
  const keys = [
    { id: 'new', status: 'active', created_at: '2026-03-01T00:00:00Z' },
    { id: 'middle', status: 'active', created_at: '2026-02-01T00:00:00Z' },
    { id: 'old', status: 'disabled', created_at: '2026-01-01T00:00:00Z' },
  ]

  it('normalizes a legacy newest-first response to oldest first regardless of status', () => {
    expect(sortAPIKeysForDisplay(keys).map((key) => key.id)).toEqual(['old', 'middle', 'new'])
    expect(keys.map((key) => key.id)).toEqual(['new', 'middle', 'old'])
  })

  it('uses oldest first before ranks are initialized', () => {
    expect(sortAPIKeysForDisplay(keys.map((key) => ({ ...key, sort_order: 0 }))).map((key) => key.id)).toEqual(['old', 'middle', 'new'])
  })

  it('preserves saved ranks ahead of creation time and appends unranked keys', () => {
    const ranked = [{ ...keys[0], sort_order: 2 }, { ...keys[1], sort_order: 1 }, keys[2]]
    expect(sortAPIKeysForDisplay(ranked).map((key) => key.id)).toEqual(['middle', 'new', 'old'])
  })
})

describe('moveAPIKey', () => {
  const items = [{ id: 'a' }, { id: 'b' }, { id: 'c' }, { id: 'd' }]

  it('moves in either direction while preserving every other key and the source list', () => {
    expect(moveAPIKey(items, 'a', 'c').map((item) => item.id)).toEqual(['b', 'c', 'a', 'd'])
    expect(moveAPIKey(items, 'd', 'b').map((item) => item.id)).toEqual(['a', 'd', 'b', 'c'])
    expect(items.map((item) => item.id)).toEqual(['a', 'b', 'c', 'd'])
  })

  it('ignores missing targets and dropping a key onto itself', () => {
    expect(moveAPIKey(items, 'a', 'missing')).toBe(items)
    expect(moveAPIKey(items, 'missing', 'a')).toBe(items)
    expect(moveAPIKey(items, 'a', 'a')).toBe(items)
  })
})
