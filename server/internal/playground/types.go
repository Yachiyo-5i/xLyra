package playground

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/store"
)

const (
	ModeChat  = "chat"
	ModeImage = "image"
)

type Attachment struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	MIMEType string `json:"mimeType"`
	Size     int64  `json:"size"`
	DataURL  string `json:"dataURL,omitempty"`
	AssetID  string `json:"assetId,omitempty"`
	Src      string `json:"src,omitempty"`
}

type Usage struct {
	PromptTokens     int64 `json:"prompt_tokens,omitempty"`
	CompletionTokens int64 `json:"completion_tokens,omitempty"`
	TotalTokens      int64 `json:"total_tokens,omitempty"`
}

type ChatMessage struct {
	ID                 string       `json:"id"`
	Role               string       `json:"role"`
	Content            string       `json:"content"`
	Reasoning          string       `json:"reasoning,omitempty"`
	Error              string       `json:"error,omitempty"`
	Usage              *Usage       `json:"usage,omitempty"`
	Model              string       `json:"model,omitempty"`
	SiteName           string       `json:"siteName,omitempty"`
	ResponseDurationMS *int64       `json:"responseDurationMs,omitempty"`
	Attachments        []Attachment `json:"attachments,omitempty"`
	CreatedAt          int64        `json:"createdAt"`
}

type ChatConversation struct {
	ID              string        `json:"id"`
	Title           string        `json:"title"`
	Model           string        `json:"model"`
	SystemPrompt    string        `json:"systemPrompt"`
	Messages        []ChatMessage `json:"messages"`
	CreatedAt       int64         `json:"createdAt"`
	UpdatedAt       int64         `json:"updatedAt"`
	ServerPersisted bool          `json:"serverPersisted"`
}

type ImageResult struct {
	ID      string `json:"id"`
	Src     string `json:"src"`
	AssetID string `json:"assetId,omitempty"`
}

type ImageEntry struct {
	ID                 string        `json:"id"`
	Mode               string        `json:"mode"`
	Model              string        `json:"model"`
	Prompt             string        `json:"prompt"`
	Size               string        `json:"size,omitempty"`
	SourceImages       []string      `json:"sourceImages,omitempty"`
	SourceAssetIDs     []string      `json:"sourceAssetIds,omitempty"`
	Images             []ImageResult `json:"images"`
	SiteName           string        `json:"siteName,omitempty"`
	ResponseDurationMS *int64        `json:"responseDurationMs,omitempty"`
	Pending            bool          `json:"pending,omitempty"`
	Error              string        `json:"error,omitempty"`
	CreatedAt          int64         `json:"createdAt"`
}

type ImageConversation struct {
	ID              string       `json:"id"`
	Title           string       `json:"title"`
	Entries         []ImageEntry `json:"entries"`
	CreatedAt       int64        `json:"createdAt"`
	UpdatedAt       int64        `json:"updatedAt"`
	ServerPersisted bool         `json:"serverPersisted"`
}

type TurnRequest struct {
	Mode            string             `json:"mode"`
	APIKeyID        string             `json:"api_key_id"`
	Model           string             `json:"model"`
	Protocol        string             `json:"protocol,omitempty"`
	ReasoningEffort string             `json:"reasoning_effort,omitempty"`
	IdempotencyKey  string             `json:"idempotency_key"`
	LegacyImport    bool               `json:"legacy_import,omitempty"`
	Chat            *ChatConversation  `json:"chat,omitempty"`
	Image           *ImageConversation `json:"image,omitempty"`
}

type RunPayload struct {
	Mode            string             `json:"mode"`
	Model           string             `json:"model"`
	Protocol        string             `json:"protocol,omitempty"`
	ReasoningEffort string             `json:"reasoning_effort,omitempty"`
	Chat            *ChatConversation  `json:"chat,omitempty"`
	Image           *ImageConversation `json:"image,omitempty"`
	MessageID       string             `json:"message_id,omitempty"`
	EntryID         string             `json:"entry_id,omitempty"`
}

type runMetadata struct {
	MessageID       string `json:"message_id,omitempty"`
	EntryID         string `json:"entry_id,omitempty"`
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
}

type RunView struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	Error       string `json:"error,omitempty"`
	CreatedAt   int64  `json:"created_at"`
	CompletedAt *int64 `json:"completed_at,omitempty"`
}

func runView(item store.PlaygroundRun) RunView {
	view := RunView{ID: item.ID.String(), Status: item.Status, Error: item.Error, CreatedAt: item.CreatedAt.UnixMilli()}
	if item.CompletedAt != nil {
		value := item.CompletedAt.UnixMilli()
		view.CompletedAt = &value
	}
	return view
}

type ConversationView struct {
	ID          string             `json:"id"`
	Mode        string             `json:"mode"`
	Title       string             `json:"title"`
	Chat        *ChatConversation  `json:"chat,omitempty"`
	Image       *ImageConversation `json:"image,omitempty"`
	Run         *RunView           `json:"run,omitempty"`
	LastOrdinal int64              `json:"last_ordinal"`
	UpdatedAt   int64              `json:"updated_at"`
}

type Event struct {
	Timestamp time.Time       `json:"timestamp"`
	Ordinal   int64           `json:"ordinal"`
	Type      string          `json:"type"`
	RunID     string          `json:"run_id,omitempty"`
	Payload   json.RawMessage `json:"payload"`
}

type appendResult struct {
	Ordinal int64
	Offset  int64
}

type snapshotPayload struct {
	Mode  string             `json:"mode"`
	Chat  *ChatConversation  `json:"chat,omitempty"`
	Image *ImageConversation `json:"image,omitempty"`
}

type chatAppendPayload struct {
	Title        string        `json:"title"`
	Model        string        `json:"model"`
	SystemPrompt string        `json:"system_prompt"`
	UpdatedAt    int64         `json:"updated_at"`
	Messages     []ChatMessage `json:"messages"`
}

type imageAppendPayload struct {
	Title     string     `json:"title"`
	UpdatedAt int64      `json:"updated_at"`
	Entry     ImageEntry `json:"entry"`
}

type deltaPayload struct {
	MessageID string `json:"message_id"`
	Content   string `json:"content,omitempty"`
	Reasoning string `json:"reasoning,omitempty"`
}

type failurePayload struct {
	MessageID          string `json:"message_id,omitempty"`
	EntryID            string `json:"entry_id,omitempty"`
	Error              string `json:"error"`
	ResponseDurationMS int64  `json:"response_duration_ms"`
}

func parseUUID(value string) (uuid.UUID, error) {
	return uuid.Parse(value)
}
