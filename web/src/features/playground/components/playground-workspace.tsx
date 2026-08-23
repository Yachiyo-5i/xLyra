import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { SquarePen } from 'lucide-react'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
} from '@/components/ui/select'
import { EmptyState } from '@/components/common/empty-state'
import { APIError } from '@/lib/http'
import {
  downstreamAPIKeyQueryKeys,
  deleteServerConversation,
  listDownstreamAPIKeys,
  listPlaygroundModels,
  listServerConversations,
} from '@/features/playground/api/playground'
import { sortAPIKeysForDisplay } from '@/features/api-keys/lib/api-key-utils'
import { PlaygroundRail } from '@/features/playground/components/playground-rail'
import { PlaygroundModeSwitcher } from '@/features/playground/components/playground-mode-switcher'
import { MobileConversationPicker } from '@/features/playground/components/mobile-conversation-picker'
import { ChatView } from '@/features/playground/components/chat-view'
import { ImageView } from '@/features/playground/components/image-view'
import {
  loadConversations,
  loadSettings,
  newId,
  saveConversations,
  saveSettings,
} from '@/features/playground/lib/storage'
import {
  loadImageConversationsAsync,
  saveImageConversationsAsync,
} from '@/features/playground/lib/image-store'
import {
  hydrateChatAttachmentsAsync,
  pruneChatAttachmentDataAsync,
} from '@/features/playground/lib/attachment-store'
import { normalizeReasoningEffort } from '@/features/playground/lib/reasoning'
import type {
  Conversation,
  ImageConversation,
  PlaygroundMode,
  PlaygroundSettings,
  ReasoningEffort,
} from '@/features/playground/lib/types'

function createConversation(): Conversation {
  const now = Date.now()
  return { id: newId(), title: '', model: '', systemPrompt: '', messages: [], createdAt: now, updatedAt: now }
}

function createImageConversation(): ImageConversation {
  const now = Date.now()
  return { id: newId(), title: '', entries: [], createdAt: now, updatedAt: now }
}

function isChatModel(category: string): boolean {
  return !['image', 'audio', 'embedding'].includes(category)
}

export function PlaygroundWorkspace() {
  const { t } = useTranslation('playground')

  const [settings, setSettings] = useState<PlaygroundSettings>(() => loadSettings())
  const [conversations, setConversations] = useState<Conversation[]>(() => {
    const stored = loadConversations()
    return stored.length > 0 ? stored : [createConversation()]
  })
  const [activeId, setActiveId] = useState<string>(() => conversations[0]?.id ?? '')
  const [imageConversations, setImageConversations] = useState<ImageConversation[]>(() => [
    createImageConversation(),
  ])
  const [activeImageId, setActiveImageId] = useState<string>(() => imageConversations[0]?.id ?? '')
  const imageLoadedRef = useRef(false)
  const serverMergedRef = useRef(false)
  const conversationsRef = useRef(conversations)

  useEffect(() => saveSettings(settings), [settings])
  useEffect(() => {
    let cancelled = false
    void hydrateChatAttachmentsAsync(conversationsRef.current).then((hydrated) => {
      if (!cancelled) setConversations(hydrated)
    })
    return () => {
      cancelled = true
    }
  }, [])
  useEffect(() => {
    conversationsRef.current = conversations
    const timer = window.setTimeout(() => {
      saveConversations(conversations)
      void pruneChatAttachmentDataAsync(conversations)
    }, 300)
    return () => window.clearTimeout(timer)
  }, [conversations])
  useEffect(() => () => saveConversations(conversationsRef.current), [])

  useEffect(() => {
    let cancelled = false
    void loadImageConversationsAsync().then((stored) => {
      if (cancelled) return
      imageLoadedRef.current = true
      if (stored.length > 0) {
        setImageConversations((current) => {
          const storedIds = new Set(stored.map((item) => item.id))
          return [...current.filter((item) => item.serverPersisted && !storedIds.has(item.id)), ...stored]
        })
      }
    })
    return () => {
      cancelled = true
    }
  }, [])

  useEffect(() => {
    if (!imageLoadedRef.current) return
    void saveImageConversationsAsync(imageConversations)
  }, [imageConversations])

  const apiKeysQuery = useQuery({
    queryKey: downstreamAPIKeyQueryKeys.list(),
    queryFn: listDownstreamAPIKeys,
  })
  const apiKeys = useMemo(
    () => sortAPIKeysForDisplay((apiKeysQuery.data?.items ?? []).filter((key) => key.status !== 'disabled')),
    [apiKeysQuery.data],
  )

  const effectiveApiKeyId = useMemo(() => {
    if (settings.apiKeyId && apiKeys.some((key) => key.id === settings.apiKeyId)) {
      return settings.apiKeyId
    }
    return apiKeys[0]?.id ?? null
  }, [apiKeys, settings.apiKeyId])

  const modelsQuery = useQuery({
    queryKey: ['playground', 'models', effectiveApiKeyId],
    queryFn: () => listPlaygroundModels(effectiveApiKeyId as string),
    enabled: Boolean(effectiveApiKeyId),
    staleTime: 5 * 60 * 1000,
    retry: false,
  })
  const serverConversationsQuery = useQuery({
    queryKey: ['playground', 'conversations'],
    queryFn: () => listServerConversations(),
    staleTime: 30 * 1000,
    retry: false,
  })

  useEffect(() => {
    if (!serverConversationsQuery.data) return
    let cancelled = false
    const items = serverConversationsQuery.data
    const exhaustive = items.length < 50
    queueMicrotask(() => {
      if (cancelled) return
      const chatItems = items.flatMap((item) => item.chat ? [{
        ...item.chat,
        serverPersisted: true,
        lastOrdinal: item.last_ordinal,
        activeRun: item.run,
      }] : [])
      const imageItems = items.flatMap((item) => item.image ? [{
        ...item.image,
        serverPersisted: true,
        lastOrdinal: item.last_ordinal,
        activeRun: item.run,
      }] : [])
      setConversations((current) => {
        const serverIds = new Set(chatItems.map((item) => item.id))
        return [...chatItems, ...current.filter((item) =>
          !serverIds.has(item.id) && !(exhaustive && item.serverPersisted),
        )]
      })
      setImageConversations((current) => {
        const serverIds = new Set(imageItems.map((item) => item.id))
        return [...imageItems, ...current.filter((item) =>
          !serverIds.has(item.id) && !(exhaustive && item.serverPersisted),
        )]
      })
      if (!serverMergedRef.current) {
        if (chatItems.length > 0) setActiveId(chatItems[0].id)
        if (imageItems.length > 0) setActiveImageId(imageItems[0].id)
        serverMergedRef.current = true
      }
    })
    return () => {
      cancelled = true
    }
  }, [serverConversationsQuery.data])
  const models = useMemo(() => modelsQuery.data ?? [], [modelsQuery.data])
  const chatModels = useMemo(() => models.filter((model) => isChatModel(model.category)), [models])
  const imageModels = useMemo(() => models.filter((model) => model.category === 'image'), [models])

  const effectiveChatModel = useMemo(() => {
    if (settings.chatModel && chatModels.some((model) => model.id === settings.chatModel)) {
      return settings.chatModel
    }
    return chatModels[0]?.id ?? null
  }, [chatModels, settings.chatModel])

  const effectiveImageModel = useMemo(() => {
    if (settings.imageModel && imageModels.some((model) => model.id === settings.imageModel)) {
      return settings.imageModel
    }
    return imageModels[0]?.id ?? null
  }, [imageModels, settings.imageModel])

  const activeConversation = useMemo(
    () => conversations.find((conversation) => conversation.id === activeId) ?? conversations[0],
    [conversations, activeId],
  )
  const activeImageConversation = useMemo(
    () => imageConversations.find((conversation) => conversation.id === activeImageId) ?? imageConversations[0],
    [imageConversations, activeImageId],
  )

  const modelsError = useMemo(() => {
    const error = modelsQuery.error ?? serverConversationsQuery.error
    if (!error) return null
    return error instanceof APIError || error instanceof Error ? error.message : String(error)
  }, [modelsQuery.error, serverConversationsQuery.error])

  const setApiKeyId = (id: string) => {
    setSettings((current) => ({
      ...current,
      apiKeyId: id,
      chatModel: null,
      imageModel: null,
    }))
  }

  const updateActiveConversation = useCallback((updater: (conversation: Conversation) => Conversation) => {
    setConversations((current) =>
      current.map((conversation) =>
        conversation.id === activeConversation?.id ? updater(conversation) : conversation,
      ),
    )
  }, [activeConversation?.id])

  const updateActiveImageConversation = useCallback((updater: (conversation: ImageConversation) => ImageConversation) => {
    setImageConversations((current) =>
      current.map((conversation) =>
        conversation.id === activeImageConversation?.id ? updater(conversation) : conversation,
      ),
    )
  }, [activeImageConversation?.id])

  const handleNewConversation = () => {
    if (activeConversation && activeConversation.messages.length === 0) {
      setActiveId(activeConversation.id)
      return
    }
    const conversation = createConversation()
    setConversations((current) => [conversation, ...current])
    setActiveId(conversation.id)
  }

  const handleDeleteConversation = (id: string) => {
    const target = conversations.find((conversation) => conversation.id === id)
    if (target?.serverPersisted) void deleteServerConversation(id).catch(() => {})
    setConversations((current) => {
      const next = current.filter((conversation) => conversation.id !== id)
      if (next.length === 0) {
        const fresh = createConversation()
        setActiveId(fresh.id)
        return [fresh]
      }
      if (id === activeId) setActiveId(next[0].id)
      return next
    })
  }

  const handleNewImageConversation = () => {
    if (activeImageConversation && activeImageConversation.entries.length === 0) {
      setActiveImageId(activeImageConversation.id)
      return
    }
    const conversation = createImageConversation()
    setImageConversations((current) => [conversation, ...current])
    setActiveImageId(conversation.id)
  }

  const handleDeleteImageConversation = (id: string) => {
    const target = imageConversations.find((conversation) => conversation.id === id)
    if (target?.serverPersisted) void deleteServerConversation(id).catch(() => {})
    setImageConversations((current) => {
      const next = current.filter((conversation) => conversation.id !== id)
      if (next.length === 0) {
        const fresh = createImageConversation()
        setActiveImageId(fresh.id)
        return [fresh]
      }
      if (id === activeImageId) setActiveImageId(next[0].id)
      return next
    })
  }

  const setMode = (mode: PlaygroundMode) => setSettings((current) => ({ ...current, mode }))

  const setChatModel = (id: string) => {
    const selectedModel = chatModels.find((model) => model.id === id)
    setSettings((current) => ({
      ...current,
      chatModel: id,
      reasoningEffort: normalizeReasoningEffort(selectedModel, current.reasoningEffort),
    }))
  }

  const hasKeys = apiKeys.length > 0
  const mode = settings.mode
  const isChat = mode === 'chat'
  const activeKey = apiKeys.find((key) => key.id === effectiveApiKeyId)

  const sessions = useMemo(() => {
    const source = isChat ? conversations : imageConversations
    return [...source].sort((a, b) => b.updatedAt - a.updatedAt)
  }, [isChat, conversations, imageConversations])
  const activeSessionId = (isChat ? activeConversation?.id : activeImageConversation?.id) ?? null
  const onSelectSession = isChat ? setActiveId : setActiveImageId
  const onNewSession = isChat ? handleNewConversation : handleNewImageConversation
  const onDeleteSession = isChat ? handleDeleteConversation : handleDeleteImageConversation
  return (
    <div className="flex h-full min-h-0 flex-col">
      {!hasKeys && !apiKeysQuery.isLoading ? (
        <EmptyState title={t('noKeys.title')} description={t('noKeys.description')} />
      ) : (
        <div className="flex min-h-0 flex-1 overflow-hidden">
          <aside className="hidden w-[264px] shrink-0 border-r border-[hsl(var(--glass-border))] md:block">
            <PlaygroundRail
              sessions={sessions}
              activeId={activeSessionId}
              mode={mode}
              onModeChange={setMode}
              onSelect={onSelectSession}
              onNew={onNewSession}
              onDelete={onDeleteSession}
            />
          </aside>

          <div className="flex min-w-0 flex-1 flex-col">
            <div className="shrink-0 border-b border-[hsl(var(--glass-border))]">
              <div className="hidden items-center px-2 py-2 md:flex">
                <Select value={effectiveApiKeyId ?? undefined} onValueChange={setApiKeyId} disabled={!hasKeys}>
                  <SelectTrigger
                    variant="filter"
                    filterLabel={t('credential.keyLabel')}
                    active={Boolean(activeKey)}
                    className="h-9 max-w-[15rem] px-3"
                  >
                    {activeKey?.name ?? t('credential.keyPlaceholder')}
                  </SelectTrigger>
                  <SelectContent searchable={false} widthMode="content">
                    {apiKeys.map((key) => (
                      <SelectItem key={key.id} value={key.id} textValue={`${key.name} ${key.key_prefix}`}>
                        <span className="truncate">{key.name}</span>
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>

              <div className="flex items-center gap-2 px-2 py-2 md:hidden">
                <PlaygroundModeSwitcher value={mode} onChange={setMode} compact />
                <div className="flex min-w-0 flex-1 items-center gap-1">
                  <MobileConversationPicker
                    conversations={sessions}
                    value={activeSessionId}
                    onChange={onSelectSession}
                    onDelete={onDeleteSession}
                  />
                  <button
                    type="button"
                    onClick={onNewSession}
                    className="flex h-11 w-11 shrink-0 items-center justify-center rounded-lg text-muted-soft transition-colors active:bg-[hsl(var(--surface-subtle))] active:text-foreground"
                    aria-label={t('conversations.new')}
                    title={t('conversations.new')}
                  >
                    <SquarePen className="h-4 w-4" />
                  </button>
                </div>
              </div>
            </div>

            {isChat ? (
              activeConversation ? (
                <ChatView
                  apiKeys={apiKeys}
                  apiKeyId={effectiveApiKeyId}
                  onAPIKeyChange={setApiKeyId}
                  model={effectiveChatModel}
                  models={chatModels}
                  onModelChange={setChatModel}
                  effort={settings.reasoningEffort}
                  onEffortChange={(next: ReasoningEffort) => setSettings((current) => ({ ...current, reasoningEffort: next }))}
                  conversation={activeConversation}
                  onChange={updateActiveConversation}
                  onImageMode={() => setMode('image')}
                />
              ) : null
            ) : activeImageConversation ? (
              <ImageView
                key={activeImageConversation.id}
                apiKeys={apiKeys}
                apiKeyId={effectiveApiKeyId}
                onAPIKeyChange={setApiKeyId}
                model={effectiveImageModel}
                models={imageModels}
                onModelChange={(id) => setSettings((current) => ({ ...current, imageModel: id }))}
                conversation={activeImageConversation}
                onChange={updateActiveImageConversation}
              />
            ) : null}

            {modelsError ? (
              <p className="px-4 pb-2 text-center text-xs text-red-500">{modelsError}</p>
            ) : null}
          </div>
        </div>
      )}
    </div>
  )
}
