export type ChatRole = 'system' | 'user' | 'assistant'

export type ChatAttachment = {
  id: string
  name: string
  mimeType: string
  size: number
  dataURL?: string
  assetId?: string
  src?: string
}

export type ChatMessage = {
  id: string
  role: ChatRole
  content: string
  reasoning?: string
  error?: string
  usage?: ChatUsage
  model?: string
  siteName?: string
  responseDurationMs?: number
  attachments?: ChatAttachment[]
  createdAt: number
}

export type ChatUsage = {
  prompt_tokens?: number
  completion_tokens?: number
  total_tokens?: number
}

export type Conversation = {
  id: string
  title: string
  model: string
  systemPrompt: string
  messages: ChatMessage[]
  createdAt: number
  updatedAt: number
  serverPersisted?: boolean
  lastOrdinal?: number
  activeRun?: PlaygroundRun
}

export type GatewayModel = {
  id: string
  mappedModel?: string
  displayName: string
  category: string
  ownedBy?: string
  endpointTypes: string[]
  siteTypes?: string[]
  reasoning?: GatewayModelReasoning
}

export type ChatProtocol = 'chat' | 'responses' | 'messages' | 'gemini'

export type ReasoningEffort = 'none' | 'minimal' | 'low' | 'medium' | 'high' | 'xhigh' | 'max' | 'ultra'

/** Server-declared reasoning effort spec for a model (from protocol_specs.json via the gateway). */
export type GatewayModelReasoning = {
  levels: ReasoningEffort[]
  thinkingMandatory?: boolean
}

export type ImageResultItem = {
  id: string
  src: string
  assetId?: string
}

export type ImageHistoryEntry = {
  id: string
  mode: 'generation' | 'edit'
  model: string
  prompt: string
  size?: string
  sourceImages?: string[]
  sourceAssetIds?: string[]
  images: ImageResultItem[]
  siteName?: string
  responseDurationMs?: number
  pending?: boolean
  error?: string
  createdAt: number
}

export type ImageConversation = {
  id: string
  title: string
  entries: ImageHistoryEntry[]
  createdAt: number
  updatedAt: number
  serverPersisted?: boolean
  lastOrdinal?: number
  activeRun?: PlaygroundRun
}

export type PlaygroundRun = {
  id: string
  status: 'queued' | 'running' | 'completed' | 'partial' | 'failed' | 'cancelled' | 'interrupted' | string
  error?: string
  created_at: number
  completed_at?: number
}

export type PlaygroundMode = 'chat' | 'image'

export type PlaygroundSettings = {
  apiKeyId: string | null
  mode: PlaygroundMode
  chatModel: string | null
  reasoningEffort: ReasoningEffort
  imageModel: string | null
}
