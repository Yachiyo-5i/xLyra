import { focusManager, QueryClient, QueryObserver } from '@tanstack/react-query'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { listPlaygroundModels } from '@/features/playground/api/playground'
import {
  invalidatePlaygroundModels,
  playgroundModelQueryKey,
  playgroundModelQueryOptions,
} from '@/features/playground/api/model-queries'

vi.mock('@/features/playground/api/playground', () => ({ listPlaygroundModels: vi.fn() }))

describe('playground model cache', () => {
  let client: QueryClient
  let unsubscribe: () => void
  const fetchModels = vi.mocked(listPlaygroundModels)
  const oldModels = [{ id: 'old-model', displayName: 'Old model', category: 'chat', endpointTypes: ['openai'] }]
  const newModels = [{ id: 'new-model', displayName: 'New model', category: 'chat', endpointTypes: ['openai'] }]

  beforeEach(() => {
    client = new QueryClient()
    client.mount()
    unsubscribe = () => {}
    focusManager.setFocused(true)
    fetchModels.mockReset().mockResolvedValue(newModels)
  })

  afterEach(() => {
    unsubscribe()
    client.unmount()
    client.clear()
    focusManager.setFocused(undefined)
  })

  it('invalidates only the edited key and fetches its new models on entry', async () => {
    client.setQueryData(playgroundModelQueryKey('edited'), oldModels)
    client.setQueryData(playgroundModelQueryKey('other'), oldModels)

    await invalidatePlaygroundModels(client, 'edited')

    expect(client.getQueryState(playgroundModelQueryKey('edited'))?.isInvalidated).toBe(true)
    expect(client.getQueryState(playgroundModelQueryKey('other'))?.isInvalidated).toBe(false)
    expect(fetchModels).not.toHaveBeenCalled()
    unsubscribe = new QueryObserver(client, playgroundModelQueryOptions('edited')).subscribe(() => {})
    await vi.waitFor(() => expect(client.getQueryData(playgroundModelQueryKey('edited'))).toEqual(newModels))
    expect(client.getQueryData(playgroundModelQueryKey('other'))).toEqual(oldModels)
  })

  it('cancels an old request and prevents its late response from restoring removed models', async () => {
    let finishOld!: (models: typeof oldModels) => void
    fetchModels.mockImplementationOnce(() => new Promise((resolve) => { finishOld = resolve }))
    unsubscribe = new QueryObserver(client, playgroundModelQueryOptions('edited')).subscribe(() => {})
    const oldSignal = fetchModels.mock.calls[0][1]

    await invalidatePlaygroundModels(client, 'edited')

    expect(oldSignal?.aborted).toBe(true)
    expect(fetchModels).toHaveBeenCalledTimes(2)
    expect(client.getQueryData(playgroundModelQueryKey('edited'))).toEqual(newModels)
    finishOld(oldModels)
    await Promise.resolve()
    await Promise.resolve()
    expect(client.getQueryData(playgroundModelQueryKey('edited'))).toEqual(newModels)
  })

  it('revalidates fresh models on entry and on focus after changes from another device', async () => {
    client.setQueryData(playgroundModelQueryKey('edited'), oldModels)
    unsubscribe = new QueryObserver(client, playgroundModelQueryOptions('edited')).subscribe(() => {})
    await vi.waitFor(() => expect(client.getQueryData(playgroundModelQueryKey('edited'))).toEqual(newModels))

    fetchModels.mockResolvedValue([])
    focusManager.setFocused(false)
    focusManager.setFocused(true)

    await vi.waitFor(() => expect(client.getQueryData(playgroundModelQueryKey('edited'))).toEqual([]))
    expect(fetchModels).toHaveBeenCalledTimes(2)
  })

  it('does not fetch without a selected key', async () => {
    unsubscribe = new QueryObserver(client, playgroundModelQueryOptions(null)).subscribe(() => {})
    focusManager.setFocused(false)
    focusManager.setFocused(true)
    await Promise.resolve()
    expect(fetchModels).not.toHaveBeenCalled()
  })
})
