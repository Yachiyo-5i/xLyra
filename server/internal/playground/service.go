package playground

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/gateway"
	"xlyra/server/internal/store"
)

type Service struct {
	logger    *slog.Logger
	db        *store.Store
	repo      store.PlaygroundRepository
	rollout   *RolloutStore
	assets    *AssetStore
	gateway   gateway.Handler
	mu        sync.Mutex
	cancels   map[uuid.UUID]context.CancelFunc
	finishes  map[uuid.UUID]chan struct{}
	turns     map[uuid.UUID]*sync.Mutex
	runs      map[uuid.UUID]store.PlaygroundRun
	restoring bool
}

var ErrPlaygroundRestoring = errors.New("playground is restoring from a backup")

func NewService(logger *slog.Logger, db *store.Store, gatewayHandler gateway.Handler, root string) *Service {
	if db == nil {
		return nil
	}
	repo := store.NewPlaygroundRepository(db.DB())
	service := &Service{
		logger: logger, db: db, repo: repo, gateway: gatewayHandler.WithRouteSiteHeader(),
		rollout: NewRolloutStore(root, repo), assets: NewAssetStore(root, repo),
		cancels:  map[uuid.UUID]context.CancelFunc{},
		finishes: map[uuid.UUID]chan struct{}{},
		turns:    map[uuid.UUID]*sync.Mutex{},
		runs:     map[uuid.UUID]store.PlaygroundRun{},
	}
	service.recoverActiveRuns()
	return service
}

func (s *Service) turnLock(conversationID uuid.UUID) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	if lock, ok := s.turns[conversationID]; ok {
		return lock
	}
	lock := &sync.Mutex{}
	s.turns[conversationID] = lock
	return lock
}

func (s *Service) rememberRun(run store.PlaygroundRun) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, exists := s.runs[run.ConversationID]
	if !exists || current.ID == run.ID || current.CreatedAt.Before(run.CreatedAt) {
		s.runs[run.ConversationID] = run
	}
}

func (s *Service) forgetConversation(conversationID uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.runs, conversationID)
	delete(s.turns, conversationID)
}

func (s *Service) latestRun(ctx context.Context, conversationID uuid.UUID) (store.PlaygroundRun, error) {
	s.mu.Lock()
	cached, ok := s.runs[conversationID]
	s.mu.Unlock()
	if ok {
		return cached, nil
	}
	run, err := s.repo.LatestRun(ctx, conversationID)
	if err != nil {
		return store.PlaygroundRun{}, err
	}
	s.rememberRun(run)
	return run, nil
}

func applyRunUpdates(run *store.PlaygroundRun, updates map[string]any) {
	for key, value := range updates {
		switch key {
		case "status":
			if text, ok := value.(string); ok {
				run.Status = text
			}
		case "error":
			if text, ok := value.(string); ok {
				run.Error = text
			}
		case "cancel_requested":
			if flag, ok := value.(bool); ok {
				run.CancelRequested = flag
			}
		case "started_at":
			if moment, ok := value.(time.Time); ok {
				run.StartedAt = &moment
			}
		case "completed_at":
			if moment, ok := value.(time.Time); ok {
				run.CompletedAt = &moment
			}
		case "updated_at":
			if moment, ok := value.(time.Time); ok {
				run.UpdatedAt = moment
			}
		}
	}
}

func (s *Service) patchRun(ctx context.Context, conversationID uuid.UUID, runID uuid.UUID, updates map[string]any) error {
	if err := s.repo.UpdateRun(ctx, runID, updates); err != nil {
		return err
	}
	s.mu.Lock()
	if cached, ok := s.runs[conversationID]; ok && cached.ID == runID {
		applyRunUpdates(&cached, updates)
		s.runs[conversationID] = cached
	}
	s.mu.Unlock()
	return nil
}

func (s *Service) waitRunFinished(runID uuid.UUID, timeout time.Duration) {
	s.mu.Lock()
	done := s.finishes[runID]
	s.mu.Unlock()
	if done == nil {
		return
	}
	select {
	case <-done:
	case <-time.After(timeout):
	}
}

func (s *Service) recoverActiveRuns() {
	ctx := context.Background()
	runs, err := s.repo.ListActiveRuns(ctx)
	if err != nil {
		if s.logger != nil {
			s.logger.Error("failed to list active playground runs", "error", err)
		}
		return
	}
	for _, run := range runs {
		conversation, getErr := s.repo.GetConversationByID(ctx, run.ConversationID)
		if getErr != nil {
			continue
		}
		var metadata runMetadata
		if json.Unmarshal(run.Params, &metadata) != nil {
			continue
		}
		result, _ := s.append(ctx, &conversation, "turn_interrupted", run.ID, failurePayload{MessageID: metadata.MessageID, EntryID: metadata.EntryID, Error: "server restarted while the run was active"}, conversation.Title)
		now := time.Now()
		_ = s.patchRun(ctx, run.ConversationID, run.ID, map[string]any{"status": "interrupted", "error": "server restarted while the run was active", "completed_at": now, "updated_at": now})
		_ = s.repo.UpdateTurnIndex(ctx, run.ID, "interrupted", result.Ordinal, result.Offset)
	}
}

func (s *Service) QuiesceForRestore(ctx context.Context) error {
	s.mu.Lock()
	if s.restoring {
		s.mu.Unlock()
		return ErrPlaygroundRestoring
	}
	s.restoring = true
	cancels := make([]context.CancelFunc, 0, len(s.cancels))
	for _, cancel := range s.cancels {
		cancels = append(cancels, cancel)
	}
	s.mu.Unlock()

	finished := false
	defer func() {
		if !finished {
			s.mu.Lock()
			s.restoring = false
			s.mu.Unlock()
		}
	}()

	for _, cancel := range cancels {
		cancel()
	}

	s.mu.Lock()
	finishes := make([]chan struct{}, 0, len(s.finishes))
	for _, done := range s.finishes {
		finishes = append(finishes, done)
	}
	s.mu.Unlock()
	deadline := time.After(30 * time.Second)
	for _, done := range finishes {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return fmt.Errorf("timeout waiting for playground runs to stop")
		}
	}

	s.resetRunState()
	finished = true
	return nil
}

func (s *Service) RecoverAfterRestore(_ context.Context) error {
	s.mu.Lock()
	s.restoring = false
	s.mu.Unlock()
	s.resetRunState()
	s.recoverActiveRuns()
	return nil
}

func (s *Service) resetRunState() {
	s.mu.Lock()
	s.runs = map[uuid.UUID]store.PlaygroundRun{}
	s.turns = map[uuid.UUID]*sync.Mutex{}
	s.mu.Unlock()
}

func (s *Service) List(ctx context.Context, adminID uuid.UUID, mode string) ([]ConversationView, error) {
	items, err := s.repo.ListConversations(ctx, adminID, mode, 50)
	if err != nil {
		return nil, err
	}
	views := make([]ConversationView, 0, len(items))
	for _, item := range items {
		view, err := s.view(ctx, item)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

func (s *Service) Get(ctx context.Context, adminID uuid.UUID, id uuid.UUID) (ConversationView, error) {
	item, err := s.repo.GetConversation(ctx, adminID, id)
	if err != nil {
		return ConversationView{}, err
	}
	return s.view(ctx, item)
}

func (s *Service) view(ctx context.Context, item store.PlaygroundConversation) (ConversationView, error) {
	events, err := s.rollout.Read(item, 0)
	if err != nil {
		return ConversationView{}, err
	}
	view, err := BuildView(item, events)
	if err != nil {
		return ConversationView{}, err
	}
	run, err := s.latestRun(ctx, item.ID)
	if err == nil {
		value := runView(run)
		view.Run = &value
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return ConversationView{}, err
	}
	return view, nil
}

func (s *Service) Events(ctx context.Context, adminID uuid.UUID, id uuid.UUID, offset int64, after int64) ([]Event, store.PlaygroundRun, int64, error) {
	item, err := s.repo.GetConversation(ctx, adminID, id)
	if err != nil {
		return nil, store.PlaygroundRun{}, offset, err
	}
	events, nextOffset, err := s.rollout.ReadFrom(item, offset, after)
	if err != nil {
		return nil, store.PlaygroundRun{}, offset, err
	}
	run, runErr := s.latestRun(ctx, id)
	if errors.Is(runErr, gorm.ErrRecordNotFound) {
		return events, store.PlaygroundRun{}, nextOffset, nil
	}
	return events, run, nextOffset, runErr
}

func (s *Service) StartTurn(ctx context.Context, adminID uuid.UUID, conversationID uuid.UUID, input TurnRequest) (ConversationView, error) {
	s.mu.Lock()
	restoring := s.restoring
	s.mu.Unlock()
	if restoring {
		return ConversationView{}, ErrPlaygroundRestoring
	}
	if input.Mode != ModeChat && input.Mode != ModeImage {
		return ConversationView{}, fmt.Errorf("unsupported playground mode")
	}
	apiKeyID, err := uuid.Parse(input.APIKeyID)
	if err != nil {
		return ConversationView{}, fmt.Errorf("invalid api key id")
	}
	apiKey, err := store.NewAPIKeyRepository(s.db.DB()).GetByID(ctx, apiKeyID)
	if err != nil {
		return ConversationView{}, fmt.Errorf("api key not found")
	}
	if apiKey.Status != "active" {
		return ConversationView{}, fmt.Errorf("api key is not active")
	}
	input.Model = strings.TrimSpace(input.Model)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if input.Model == "" || input.IdempotencyKey == "" {
		return ConversationView{}, fmt.Errorf("model and idempotency key are required")
	}

	lock := s.turnLock(conversationID)
	lock.Lock()
	defer lock.Unlock()

	conversation, created, err := s.ensureConversation(ctx, adminID, conversationID, input)
	if err != nil {
		return ConversationView{}, err
	}
	if existing, err := s.repo.GetRunByIdempotency(ctx, conversationID, input.IdempotencyKey); err == nil {
		s.rememberRun(existing)
		view, viewErr := s.view(ctx, conversation)
		if viewErr == nil {
			value := runView(existing)
			view.Run = &value
		}
		return view, viewErr
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return ConversationView{}, err
	}
	if latest, err := s.latestRun(ctx, conversationID); err == nil && (latest.Status == "queued" || latest.Status == "running") {
		return ConversationView{}, fmt.Errorf("conversation already has an active run")
	} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return ConversationView{}, err
	}

	runID := uuid.New()
	payload, title, err := s.normalizeTurn(ctx, conversation, runID, input)
	if err != nil {
		if created {
			_ = s.repo.DeleteConversation(ctx, adminID, conversationID)
			_ = os.RemoveAll(filepath.Join(s.rollout.Root(), "assets", conversationID.String()))
			s.forgetConversation(conversationID)
		}
		return ConversationView{}, err
	}
	historyEventType, historyEventPayload, err := s.turnHistoryEvent(ctx, conversation, created, payload)
	if err != nil {
		return ConversationView{}, err
	}
	metadata := runMetadata{MessageID: payload.MessageID, EntryID: payload.EntryID, ReasoningEffort: payload.ReasoningEffort}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return ConversationView{}, err
	}
	now := time.Now()
	run := store.PlaygroundRun{
		ID: runID, ConversationID: conversationID, APIKeyID: apiKeyID, IdempotencyKey: input.IdempotencyKey,
		Mode: input.Mode, Protocol: input.Protocol, Model: input.Model, Status: "queued", Params: store.JSON(encoded),
		CreatedAt: now, UpdatedAt: now,
	}
	inserted, err := s.repo.CreateRun(ctx, &run)
	if err != nil {
		return ConversationView{}, err
	}
	if !inserted {
		existing, getErr := s.repo.GetRunByIdempotency(ctx, conversationID, input.IdempotencyKey)
		if getErr != nil {
			return ConversationView{}, getErr
		}
		s.rememberRun(existing)
		view, viewErr := s.view(ctx, conversation)
		if viewErr == nil {
			value := runView(existing)
			view.Run = &value
		}
		return view, viewErr
	}
	s.rememberRun(run)

	if created {
		result, appendErr := s.append(ctx, &conversation, "session_meta", uuid.Nil, map[string]any{"mode": input.Mode, "created_at": now.UTC()}, title)
		if appendErr != nil {
			s.finishFailedRun(run, &conversation, payload, nil, now, appendErr, false)
			return ConversationView{}, appendErr
		}
		_ = result
	}
	start, err := s.append(ctx, &conversation, historyEventType, runID, historyEventPayload, title)
	if err != nil {
		s.finishFailedRun(run, &conversation, payload, nil, now, err, false)
		return ConversationView{}, err
	}
	if _, err := s.append(ctx, &conversation, "turn_started", runID, map[string]any{"model": input.Model, "protocol": input.Protocol}, title); err != nil {
		s.finishFailedRun(run, &conversation, payload, nil, now, err, false)
		return ConversationView{}, err
	}
	index := store.PlaygroundTurnIndex{ID: uuid.New(), ConversationID: conversationID, RunID: runID, StartOrdinal: start.Ordinal, StartByte: start.Offset, Status: "running"}
	if err := s.repo.CreateTurnIndex(ctx, &index); err != nil {
		s.finishFailedRun(run, &conversation, payload, nil, now, err, false)
		return ConversationView{}, err
	}

	ready := make(chan struct{})
	go s.execute(run, apiKey, conversation, payload, ready)
	<-ready
	view, err := s.view(ctx, conversation)
	if err != nil {
		return ConversationView{}, err
	}
	value := runView(run)
	view.Run = &value
	return view, nil
}

func (s *Service) turnHistoryEvent(ctx context.Context, conversation store.PlaygroundConversation, created bool, payload RunPayload) (string, any, error) {
	snapshot := snapshotPayload{Mode: payload.Mode, Chat: payload.Chat, Image: payload.Image}
	if created {
		return "conversation_snapshot", snapshot, nil
	}
	previous, err := s.view(ctx, conversation)
	if err != nil {
		return "", nil, err
	}
	if payload.Chat != nil && previous.Chat != nil {
		incoming := payload.Chat.Messages
		existing := previous.Chat.Messages
		if payload.Chat.SystemPrompt == previous.Chat.SystemPrompt && len(incoming) == len(existing)+2 && reflect.DeepEqual(existing, incoming[:len(existing)]) {
			return "message_added", chatAppendPayload{
				Title: payload.Chat.Title, Model: payload.Chat.Model, SystemPrompt: payload.Chat.SystemPrompt,
				UpdatedAt: payload.Chat.UpdatedAt, Messages: incoming[len(existing):],
			}, nil
		}
	}
	if payload.Image != nil && previous.Image != nil {
		incoming := payload.Image.Entries
		existing := previous.Image.Entries
		if len(incoming) == len(existing)+1 && reflect.DeepEqual(existing, incoming[:len(existing)]) {
			return "image_entry_added", imageAppendPayload{Title: payload.Image.Title, UpdatedAt: payload.Image.UpdatedAt, Entry: incoming[len(incoming)-1]}, nil
		}
	}
	return "branch_reset", snapshot, nil
}

func (s *Service) ensureConversation(ctx context.Context, adminID uuid.UUID, id uuid.UUID, input TurnRequest) (store.PlaygroundConversation, bool, error) {
	item, err := s.repo.GetConversation(ctx, adminID, id)
	if err == nil {
		if item.Mode != input.Mode {
			return store.PlaygroundConversation{}, false, fmt.Errorf("conversation mode does not match")
		}
		return item, false, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return store.PlaygroundConversation{}, false, err
	}
	now := time.Now()
	title := ""
	if input.Chat != nil {
		title = input.Chat.Title
	}
	if input.Image != nil {
		title = input.Image.Title
	}
	item = store.PlaygroundConversation{ID: id, AdminID: adminID, Mode: input.Mode, Title: title, RolloutPath: s.rollout.NewPath(id, now), CreatedAt: now, UpdatedAt: now}
	if input.LegacyImport {
		item.LegacyClientID = id.String()
	}
	if err := s.repo.CreateConversation(ctx, &item); err != nil {
		return store.PlaygroundConversation{}, false, err
	}
	return item, true, nil
}

func (s *Service) normalizeTurn(ctx context.Context, conversation store.PlaygroundConversation, runID uuid.UUID, input TurnRequest) (RunPayload, string, error) {
	payload := RunPayload{Mode: input.Mode, Model: input.Model, Protocol: input.Protocol, ReasoningEffort: input.ReasoningEffort}
	if input.Mode == ModeChat {
		if input.Chat == nil || len(input.Chat.Messages) == 0 {
			return RunPayload{}, "", fmt.Errorf("chat conversation is required")
		}
		chat := *input.Chat
		chat.ID = conversation.ID.String()
		chat.ServerPersisted = true
		for messageIndex := range chat.Messages {
			for attachmentIndex := range chat.Messages[messageIndex].Attachments {
				attachment, err := s.normalizeAttachment(ctx, conversation.ID, runID, chat.Messages[messageIndex].Attachments[attachmentIndex])
				if err != nil {
					return RunPayload{}, "", err
				}
				chat.Messages[messageIndex].Attachments[attachmentIndex] = attachment
			}
		}
		messageID := uuid.NewString()
		chat.Messages = append(chat.Messages, ChatMessage{ID: messageID, Role: "assistant", Model: input.Model, CreatedAt: time.Now().UnixMilli()})
		chat.Model = input.Model
		chat.UpdatedAt = time.Now().UnixMilli()
		payload.Chat = &chat
		payload.MessageID = messageID
		return payload, chat.Title, nil
	}
	if input.Image == nil || len(input.Image.Entries) == 0 {
		return RunPayload{}, "", fmt.Errorf("image conversation is required")
	}
	image := *input.Image
	image.ID = conversation.ID.String()
	image.ServerPersisted = true
	for entryIndex := range image.Entries {
		entry := &image.Entries[entryIndex]
		if err := s.normalizeImageEntry(ctx, conversation.ID, runID, entry); err != nil {
			return RunPayload{}, "", err
		}
	}
	entry := &image.Entries[len(image.Entries)-1]
	entry.Pending = true
	entry.Error = ""
	entry.Images = nil
	image.UpdatedAt = time.Now().UnixMilli()
	payload.Image = &image
	payload.EntryID = entry.ID
	return payload, image.Title, nil
}

func (s *Service) normalizeAttachment(ctx context.Context, conversationID uuid.UUID, runID uuid.UUID, attachment Attachment) (Attachment, error) {
	if attachment.AssetID != "" {
		id, err := uuid.Parse(attachment.AssetID)
		if err != nil {
			return Attachment{}, fmt.Errorf("invalid attachment asset id")
		}
		asset, err := s.repo.GetAsset(ctx, id)
		if err != nil || asset.ConversationID != conversationID {
			return Attachment{}, fmt.Errorf("attachment asset not found")
		}
		attachment.DataURL = ""
		attachment.Src = assetURL(id)
		return attachment, nil
	}
	if attachment.DataURL == "" {
		return Attachment{}, fmt.Errorf("attachment data is unavailable")
	}
	asset, err := s.assets.SaveDataURL(ctx, conversationID, runID, "attachments", attachment.Name, attachment.DataURL)
	if err != nil {
		return Attachment{}, err
	}
	attachment.AssetID = asset.ID.String()
	attachment.DataURL = ""
	attachment.Src = assetURL(asset.ID)
	attachment.Size = asset.Size
	attachment.MIMEType = asset.MIMEType
	return attachment, nil
}

func (s *Service) normalizeImageEntry(ctx context.Context, conversationID uuid.UUID, runID uuid.UUID, entry *ImageEntry) error {
	entry.SourceAssetIDs = nil
	for _, source := range entry.SourceImages {
		asset, err := s.normalizeImageSource(ctx, conversationID, runID, "input-images", source)
		if err != nil {
			return err
		}
		entry.SourceAssetIDs = append(entry.SourceAssetIDs, asset.ID.String())
	}
	entry.SourceImages = nil
	if len(entry.SourceAssetIDs) > 0 {
		entry.SourceImages = make([]string, 0, len(entry.SourceAssetIDs))
		for _, value := range entry.SourceAssetIDs {
			id, _ := uuid.Parse(value)
			entry.SourceImages = append(entry.SourceImages, assetURL(id))
		}
	}
	for index := range entry.Images {
		image := &entry.Images[index]
		if image.AssetID != "" {
			id, err := uuid.Parse(image.AssetID)
			if err != nil {
				return fmt.Errorf("invalid image asset id")
			}
			asset, err := s.repo.GetAsset(ctx, id)
			if err != nil || asset.ConversationID != conversationID {
				return fmt.Errorf("image asset not found")
			}
			image.Src = assetURL(id)
			continue
		}
		asset, err := s.normalizeImageSource(ctx, conversationID, runID, "generated-images", image.Src)
		if err != nil {
			return err
		}
		image.AssetID = asset.ID.String()
		image.Src = assetURL(asset.ID)
	}
	return nil
}

func (s *Service) normalizeImageSource(ctx context.Context, conversationID uuid.UUID, runID uuid.UUID, purpose string, source string) (store.PlaygroundAsset, error) {
	if id, ok := assetIDFromURL(source); ok {
		asset, err := s.repo.GetAsset(ctx, id)
		if err != nil || asset.ConversationID != conversationID {
			return store.PlaygroundAsset{}, fmt.Errorf("image asset not found")
		}
		return asset, nil
	}
	if strings.HasPrefix(source, "data:") {
		return s.assets.SaveDataURL(ctx, conversationID, runID, purpose, "image", source)
	}
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		return s.assets.SaveRemote(ctx, conversationID, runID, purpose, source)
	}
	return store.PlaygroundAsset{}, fmt.Errorf("image source is unavailable")
}

func assetURL(id uuid.UUID) string {
	return "/api/v1/playground/assets/" + id.String()
}

func assetIDFromURL(value string) (uuid.UUID, bool) {
	const prefix = "/api/v1/playground/assets/"
	index := strings.Index(value, prefix)
	if index < 0 {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(strings.TrimSpace(value[index+len(prefix):]))
	return id, err == nil
}

func (s *Service) append(ctx context.Context, conversation *store.PlaygroundConversation, eventType string, runID uuid.UUID, payload any, title string) (appendResult, error) {
	result, err := s.rollout.Append(ctx, *conversation, eventType, runID, payload, title)
	if err == nil {
		conversation.LastOrdinal = result.Ordinal
		conversation.LastByteOffset = result.Offset
		conversation.Title = title
		conversation.UpdatedAt = time.Now()
	}
	return result, err
}

func (s *Service) Cancel(ctx context.Context, adminID uuid.UUID, runID uuid.UUID) error {
	run, err := s.repo.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	if _, err := s.repo.GetConversation(ctx, adminID, run.ConversationID); err != nil {
		return err
	}
	if err := s.patchRun(ctx, run.ConversationID, runID, map[string]any{"cancel_requested": true, "updated_at": time.Now()}); err != nil {
		return err
	}
	s.mu.Lock()
	cancel := s.cancels[runID]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

func (s *Service) Delete(ctx context.Context, adminID uuid.UUID, id uuid.UUID) error {
	conversation, err := s.repo.GetConversation(ctx, adminID, id)
	if err != nil {
		return err
	}
	lock := s.turnLock(id)
	lock.Lock()
	defer lock.Unlock()
	if run, err := s.latestRun(ctx, id); err == nil && (run.Status == "queued" || run.Status == "running") {
		_ = s.Cancel(ctx, adminID, run.ID)
		s.waitRunFinished(run.ID, 30*time.Second)
	}
	assetDir := filepath.Join(s.rollout.Root(), "assets", id.String())
	if err := s.repo.DeleteConversation(ctx, adminID, id); err != nil {
		return err
	}
	s.forgetConversation(id)
	_ = s.rollout.Remove(conversation)
	_ = os.RemoveAll(assetDir)
	return nil
}
