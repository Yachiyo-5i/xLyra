import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { isUUID } from '@/features/playground/lib/storage'
import { loadImageConversationsAsync } from '@/features/playground/lib/image-store'

const values = new Map<string, string>()

function installIndexedDB(initial: Record<string, unknown>) {
  const records = new Map(Object.entries(initial))
  const database = {
    objectStoreNames: { contains: () => true },
    transaction: (_storeName: string, mode: IDBTransactionMode) => {
      const transaction = {
        objectStore: () => ({
          get: (key: string) => {
            const request = { result: records.get(key) } as IDBRequest<unknown>
            queueMicrotask(() => request.onsuccess?.(new Event('success')))
            return request
          },
          put: (value: unknown, key: string) => {
            records.set(key, value)
          },
        }),
      } as unknown as IDBTransaction
      if (mode === 'readwrite') queueMicrotask(() => transaction.oncomplete?.(new Event('complete')))
      return transaction
    },
    close: () => undefined,
  } as unknown as IDBDatabase
  vi.stubGlobal('indexedDB', {
    open: () => {
      const request = { result: database } as IDBOpenDBRequest
      queueMicrotask(() => request.onsuccess?.(new Event('success')))
      return request
    },
  })
  return records
}

beforeEach(() => {
  values.clear()
  vi.stubGlobal('localStorage', {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => values.set(key, value),
    removeItem: (key: string) => values.delete(key),
  })
  vi.stubGlobal('indexedDB', undefined)
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('image conversation storage', () => {
  it('migrates and persists legacy ids already stored in IndexedDB', async () => {
    const records = installIndexedDB({
      'image-conversations': [{
        id: 'id-indexeddb-image',
        title: 'Stored image',
        entries: [],
        createdAt: 1,
        updatedAt: 1,
        serverPersisted: true,
      }],
    })

    const [conversation] = await loadImageConversationsAsync()
    const [persisted] = records.get('image-conversations') as Array<{ id: string; serverPersisted?: boolean }>

    expect(isUUID(conversation.id)).toBe(true)
    expect(conversation.serverPersisted).toBe(false)
    expect(persisted).toMatchObject({ id: conversation.id, serverPersisted: false })
  })

  it('migrates legacy image conversation ids while keeping entries', async () => {
    values.set('xlyra-playground-image-conversations', JSON.stringify([{
      id: 'id-legacy-image',
      title: 'Legacy image',
      entries: [{
        id: 'entry-1',
        mode: 'generation',
        model: 'gpt-image',
        prompt: 'a lighthouse',
        images: [],
        createdAt: 1,
      }],
      createdAt: 1,
      updatedAt: 1,
      serverPersisted: true,
      lastOrdinal: 7,
      activeRun: { id: 'run-1', status: 'running' },
    }]))

    const [conversation] = await loadImageConversationsAsync()

    expect(isUUID(conversation.id)).toBe(true)
    expect(conversation.serverPersisted).toBe(false)
    expect(conversation.lastOrdinal).toBeUndefined()
    expect(conversation.activeRun).toBeUndefined()
    expect(conversation.entries[0].id).toBe('entry-1')
    expect(values.has('xlyra-playground-image-conversations')).toBe(false)
  })

  it('keeps valid UUID image conversations unchanged', async () => {
    const id = '123e4567-e89b-42d3-a456-426614174000'
    const records = installIndexedDB({
      'image-conversations': [{
        id,
        title: 'Stored image',
        entries: [],
        createdAt: 1,
        updatedAt: 1,
        serverPersisted: true,
        lastOrdinal: 7,
      }],
    })

    const [conversation] = await loadImageConversationsAsync()

    expect(conversation.id).toBe(id)
    expect(conversation.serverPersisted).toBe(true)
    expect(conversation.lastOrdinal).toBe(7)
    expect(records.get('image-conversations')).toMatchObject([{ id, serverPersisted: true }])
  })
})
