import { newId } from '@/features/playground/lib/storage'
import type { ImageConversation, ImageHistoryEntry } from '@/features/playground/lib/types'

const DB_NAME = 'xlyra-playground'
const STORE_NAME = 'kv'
const RECORD_KEY = 'image-conversations'
const LEGACY_CONVERSATIONS_KEY = 'xlyra-playground-image-conversations'
const LEGACY_FLAT_KEY = 'xlyra-playground-images'
const MAX_CONVERSATIONS = 50

function openDatabase(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open(DB_NAME, 1)
    request.onupgradeneeded = () => {
      if (!request.result.objectStoreNames.contains(STORE_NAME)) {
        request.result.createObjectStore(STORE_NAME)
      }
    }
    request.onsuccess = () => resolve(request.result)
    request.onerror = () => reject(request.error)
  })
}

async function idbGet<T>(key: string): Promise<T | null> {
  const db = await openDatabase()
  try {
    return await new Promise<T | null>((resolve, reject) => {
      const request = db.transaction(STORE_NAME, 'readonly').objectStore(STORE_NAME).get(key)
      request.onsuccess = () => resolve((request.result ?? null) as T | null)
      request.onerror = () => reject(request.error)
    })
  } finally {
    db.close()
  }
}

async function idbSet(key: string, value: unknown): Promise<void> {
  const db = await openDatabase()
  try {
    await new Promise<void>((resolve, reject) => {
      const tx = db.transaction(STORE_NAME, 'readwrite')
      tx.objectStore(STORE_NAME).put(value, key)
      tx.oncomplete = () => resolve()
      tx.onerror = () => reject(tx.error)
    })
  } finally {
    db.close()
  }
}

function readLocalStorage<T>(key: string): T | null {
  try {
    const raw = localStorage.getItem(key)
    if (!raw) return null
    return JSON.parse(raw) as T
  } catch {
    return null
  }
}

function removeLocalStorageItem(key: string) {
  try {
    localStorage.removeItem(key)
  } catch {
    return
  }
}

function migrateFromLocalStorage(): ImageConversation[] {
  const stored = readLocalStorage<ImageConversation[]>(LEGACY_CONVERSATIONS_KEY)
  if (Array.isArray(stored) && stored.length > 0) {
    removeLocalStorageItem(LEGACY_CONVERSATIONS_KEY)
    return stored
  }
  const flat = readLocalStorage<ImageHistoryEntry[]>(LEGACY_FLAT_KEY)
  if (Array.isArray(flat) && flat.length > 0) {
    const ordered = [...flat].reverse()
    const now = Date.now()
    removeLocalStorageItem(LEGACY_FLAT_KEY)
    return [
      {
        id: newId(),
        title: ordered[0]?.prompt?.slice(0, 40) ?? '',
        entries: ordered,
        createdAt: now,
        updatedAt: now,
      },
    ]
  }
  return []
}

export async function loadImageConversationsAsync(): Promise<ImageConversation[]> {
  const stored = await idbGet<ImageConversation[]>(RECORD_KEY).catch(() => null)
  if (Array.isArray(stored) && stored.length > 0) {
    return stored
  }
  const migrated = migrateFromLocalStorage()
  if (migrated.length > 0) {
    await idbSet(RECORD_KEY, migrated).catch(() => undefined)
  }
  return migrated
}

export async function saveImageConversationsAsync(items: ImageConversation[]): Promise<void> {
  await idbSet(RECORD_KEY, items.slice(0, MAX_CONVERSATIONS)).catch(() => undefined)
}
