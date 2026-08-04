import type { ChatAttachment, Conversation } from '@/features/playground/lib/types'

const DB_NAME = 'xlyra-playground'
const STORE_NAME = 'kv'
const RECORD_KEY = 'chat-attachments'

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

async function readAttachmentData(): Promise<Record<string, string>> {
  const db = await openDatabase()
  try {
    return await new Promise<Record<string, string>>((resolve, reject) => {
      const request = db.transaction(STORE_NAME, 'readonly').objectStore(STORE_NAME).get(RECORD_KEY)
      request.onsuccess = () => resolve((request.result ?? {}) as Record<string, string>)
      request.onerror = () => reject(request.error)
    })
  } finally {
    db.close()
  }
}

async function updateAttachmentData(
  update: (value: Record<string, string>) => Record<string, string>,
): Promise<void> {
  const db = await openDatabase()
  try {
    await new Promise<void>((resolve, reject) => {
      const tx = db.transaction(STORE_NAME, 'readwrite')
      const store = tx.objectStore(STORE_NAME)
      const request = store.get(RECORD_KEY)
      request.onsuccess = () => {
        store.put(update((request.result ?? {}) as Record<string, string>), RECORD_KEY)
      }
      request.onerror = () => reject(request.error)
      tx.oncomplete = () => resolve()
      tx.onerror = () => reject(tx.error)
    })
  } finally {
    db.close()
  }
}

export async function saveChatAttachmentDataAsync(attachments: ChatAttachment[]): Promise<void> {
  await updateAttachmentData((data) => {
    for (const attachment of attachments) {
      if (attachment.dataURL) data[attachment.id] = attachment.dataURL
    }
    return data
  }).catch(() => undefined)
}

export async function hydrateChatAttachmentsAsync(items: Conversation[]): Promise<Conversation[]> {
  const data = await readAttachmentData().catch((): Record<string, string> => ({}))
  return items.map((conversation) => ({
    ...conversation,
    messages: conversation.messages.map((message) => ({
      ...message,
      attachments: message.attachments?.map((attachment) => ({
        ...attachment,
        dataURL: attachment.dataURL ?? data[attachment.id],
      })),
    })),
  }))
}

export async function pruneChatAttachmentDataAsync(items: Conversation[]): Promise<void> {
  const activeIDs = new Set(
    items.flatMap((conversation) =>
      conversation.messages.flatMap((message) => message.attachments?.map((attachment) => attachment.id) ?? []),
    ),
  )
  await updateAttachmentData((data) => (
    Object.fromEntries(Object.entries(data).filter(([id]) => activeIDs.has(id)))
  )).catch(() => undefined)
}
