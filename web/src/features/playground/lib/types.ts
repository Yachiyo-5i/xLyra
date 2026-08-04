export type ChatRole = 'system' | 'user' | 'assistant'

export type ChatAttachment = {
  id: string
  name: string
  mimeType: string
  size: number
  dataURL?: string
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
}

export type GatewayModel = {
  id: string
  displayName: string
  category: string
  ownedBy?: string
  endpointTypes: string[]
}

export type ChatProtocol = 'chat' | 'responses' | 'messages'

export type ReasoningEffort = 'low' | 'medium' | 'high' | 'xhigh' | 'max'

export type ImageResultItem = {
  id: string
  src: string
}

export type ImageHistoryEntry = {
  id: string
  mode: 'generation' | 'edit'
  model: string
  prompt: string
  size?: string
  sourceImages?: string[]
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
}

export type PlaygroundMode = 'chat' | 'image'

export type PlaygroundSettings = {
  apiKeyId: string | null
  mode: PlaygroundMode
  chatModel: string | null
  reasoningEffort: ReasoningEffort
  imageModel: string | null
}
