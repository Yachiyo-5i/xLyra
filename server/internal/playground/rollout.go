package playground

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/store"
)

type RolloutStore struct {
	root  string
	repo  store.PlaygroundRepository
	mu    sync.Mutex
	locks map[uuid.UUID]*sync.Mutex
}

func NewRolloutStore(root string, repo store.PlaygroundRepository) *RolloutStore {
	return &RolloutStore{root: root, repo: repo, locks: map[uuid.UUID]*sync.Mutex{}}
}

func (s *RolloutStore) Root() string {
	return s.root
}

func (s *RolloutStore) NewPath(id uuid.UUID, now time.Time) string {
	name := fmt.Sprintf("rollout-%s-%s.jsonl", now.UTC().Format("2006-01-02T15-04-05"), id.String())
	return filepath.ToSlash(filepath.Join("sessions", now.UTC().Format("2006"), now.UTC().Format("01"), now.UTC().Format("02"), name))
}

func (s *RolloutStore) mutex(id uuid.UUID) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	if value := s.locks[id]; value != nil {
		return value
	}
	value := &sync.Mutex{}
	s.locks[id] = value
	return value
}

func (s *RolloutStore) Append(ctx context.Context, conversation store.PlaygroundConversation, eventType string, runID uuid.UUID, payload any, title string) (appendResult, error) {
	lock := s.mutex(conversation.ID)
	lock.Lock()
	defer lock.Unlock()

	absPath := filepath.Join(s.root, filepath.FromSlash(conversation.RolloutPath))
	if err := os.MkdirAll(filepath.Dir(absPath), 0o750); err != nil {
		return appendResult{}, fmt.Errorf("create rollout directory: %w", err)
	}
	fileLock, err := s.acquireFileLock(ctx, conversation.ID)
	if err != nil {
		return appendResult{}, err
	}
	defer s.releaseFileLock(fileLock)
	if err := repairRolloutTail(absPath); err != nil {
		return appendResult{}, err
	}

	encodedPayload, err := json.Marshal(payload)
	if err != nil {
		return appendResult{}, fmt.Errorf("encode rollout payload: %w", err)
	}
	lastOrdinal, err := lastRolloutOrdinal(absPath)
	if err != nil {
		return appendResult{}, err
	}
	if conversation.LastOrdinal > lastOrdinal {
		lastOrdinal = conversation.LastOrdinal
	}
	ordinal := lastOrdinal + 1
	event := Event{Timestamp: time.Now().UTC(), Ordinal: ordinal, Type: eventType, Payload: encodedPayload}
	if runID != uuid.Nil {
		event.RunID = runID.String()
	}
	line, err := json.Marshal(event)
	if err != nil {
		return appendResult{}, fmt.Errorf("encode rollout event: %w", err)
	}
	line = append(line, '\n')

	file, err := os.OpenFile(absPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
	if err != nil {
		return appendResult{}, fmt.Errorf("open rollout: %w", err)
	}
	stat, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return appendResult{}, fmt.Errorf("inspect rollout: %w", err)
	}
	if _, err := file.Write(line); err != nil {
		_ = file.Close()
		return appendResult{}, fmt.Errorf("append rollout: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return appendResult{}, fmt.Errorf("sync rollout: %w", err)
	}
	if err := file.Close(); err != nil {
		return appendResult{}, fmt.Errorf("close rollout: %w", err)
	}
	offset := stat.Size() + int64(len(line))
	if err := s.repo.UpdateConversationCursor(ctx, conversation.ID, title, ordinal, offset, time.Now()); err != nil {
		return appendResult{}, err
	}
	return appendResult{Ordinal: ordinal, Offset: offset}, nil
}

func repairRolloutTail(path string) error {
	file, err := os.OpenFile(path, os.O_RDWR, 0o640)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil || stat.Size() == 0 {
		return err
	}
	last := make([]byte, 1)
	if _, err := file.ReadAt(last, stat.Size()-1); err != nil {
		return err
	}
	if last[0] == '\n' {
		return nil
	}
	end := stat.Size()
	for end > 0 {
		start := end - 64*1024
		if start < 0 {
			start = 0
		}
		chunk := make([]byte, end-start)
		if _, err := file.ReadAt(chunk, start); err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		if index := bytes.LastIndexByte(chunk, '\n'); index >= 0 {
			return file.Truncate(start + int64(index) + 1)
		}
		end = start
	}
	return file.Truncate(0)
}

func lastRolloutOrdinal(path string) (int64, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return 0, err
	}
	if stat.Size() == 0 {
		return 0, nil
	}
	end := stat.Size()
	var collected []byte
	for end > 0 {
		start := end - 64*1024
		if start < 0 {
			start = 0
		}
		chunk := make([]byte, end-start)
		if _, err := file.ReadAt(chunk, start); err != nil && !errors.Is(err, io.EOF) {
			return 0, err
		}
		collected = append(chunk, collected...)
		trimmed := bytes.TrimRight(collected, "\r\n")
		if index := bytes.LastIndexByte(trimmed, '\n'); index >= 0 || start == 0 {
			line := trimmed
			if index >= 0 {
				line = trimmed[index+1:]
			}
			var event Event
			if err := json.Unmarshal(line, &event); err != nil {
				return 0, fmt.Errorf("decode last rollout event: %w", err)
			}
			return event.Ordinal, nil
		}
		end = start
	}
	return 0, nil
}

func (s *RolloutStore) acquireFileLock(ctx context.Context, id uuid.UUID) (*os.File, error) {
	dir := filepath.Join(s.root, "thread-writer-locks")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("create writer lock directory: %w", err)
	}
	path := filepath.Join(dir, id.String()+".lock")
	for {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			return file, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("acquire rollout writer lock: %w", err)
		}
		if stat, statErr := os.Stat(path); statErr == nil && time.Since(stat.ModTime()) > 2*time.Minute {
			_ = os.Remove(path)
			continue
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
}

func (s *RolloutStore) releaseFileLock(file *os.File) {
	if file == nil {
		return
	}
	path := file.Name()
	_ = file.Close()
	_ = os.Remove(path)
}

func (s *RolloutStore) Read(conversation store.PlaygroundConversation, after int64) ([]Event, error) {
	events, _, err := s.ReadFrom(conversation, 0, after)
	return events, err
}

func (s *RolloutStore) ReadFrom(conversation store.PlaygroundConversation, offset int64, after int64) ([]Event, int64, error) {
	absPath := filepath.Join(s.root, filepath.FromSlash(conversation.RolloutPath))
	file, err := os.Open(absPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, offset, nil
	}
	if err != nil {
		return nil, offset, fmt.Errorf("open rollout: %w", err)
	}
	defer file.Close()
	if offset > 0 {
		if _, err := file.Seek(offset, io.SeekStart); err != nil {
			return nil, offset, fmt.Errorf("seek rollout: %w", err)
		}
	}

	reader := bufio.NewReaderSize(file, 64*1024)
	events := make([]Event, 0)
	nextOffset := offset
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 && line[len(line)-1] == '\n' {
			var event Event
			if err := json.Unmarshal(line, &event); err != nil {
				return nil, nextOffset, fmt.Errorf("decode rollout event: %w", err)
			}
			nextOffset += int64(len(line))
			if event.Ordinal > after {
				events = append(events, event)
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, nextOffset, fmt.Errorf("read rollout: %w", readErr)
		}
	}
	return events, nextOffset, nil
}

func BuildView(conversation store.PlaygroundConversation, events []Event) (ConversationView, error) {
	view := ConversationView{ID: conversation.ID.String(), Mode: conversation.Mode, Title: conversation.Title, LastOrdinal: conversation.LastOrdinal, UpdatedAt: conversation.UpdatedAt.UnixMilli()}
	for _, event := range events {
		if event.Ordinal > view.LastOrdinal {
			view.LastOrdinal = event.Ordinal
			view.UpdatedAt = event.Timestamp.UnixMilli()
		}
		switch event.Type {
		case "conversation_snapshot", "branch_reset":
			var payload snapshotPayload
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				return ConversationView{}, err
			}
			view.Chat = payload.Chat
			view.Image = payload.Image
		case "message_added":
			if view.Chat == nil {
				continue
			}
			var payload chatAppendPayload
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				return ConversationView{}, err
			}
			view.Chat.Title = payload.Title
			view.Chat.Model = payload.Model
			view.Chat.SystemPrompt = payload.SystemPrompt
			view.Chat.UpdatedAt = payload.UpdatedAt
			view.Chat.Messages = append(view.Chat.Messages, payload.Messages...)
		case "image_entry_added":
			if view.Image == nil {
				continue
			}
			var payload imageAppendPayload
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				return ConversationView{}, err
			}
			view.Image.Title = payload.Title
			view.Image.UpdatedAt = payload.UpdatedAt
			view.Image.Entries = append(view.Image.Entries, payload.Entry)
		case "assistant_delta":
			if view.Chat == nil {
				continue
			}
			var payload deltaPayload
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				return ConversationView{}, err
			}
			for index := range view.Chat.Messages {
				if view.Chat.Messages[index].ID == payload.MessageID {
					view.Chat.Messages[index].Content += payload.Content
					view.Chat.Messages[index].Reasoning += payload.Reasoning
				}
			}
		case "assistant_final":
			if view.Chat == nil {
				continue
			}
			var message ChatMessage
			if err := json.Unmarshal(event.Payload, &message); err != nil {
				return ConversationView{}, err
			}
			for index := range view.Chat.Messages {
				if view.Chat.Messages[index].ID == message.ID {
					view.Chat.Messages[index] = message
				}
			}
		case "image_final":
			if view.Image == nil {
				continue
			}
			var entry ImageEntry
			if err := json.Unmarshal(event.Payload, &entry); err != nil {
				return ConversationView{}, err
			}
			for index := range view.Image.Entries {
				if view.Image.Entries[index].ID == entry.ID {
					view.Image.Entries[index] = entry
				}
			}
		case "turn_failed", "turn_cancelled", "turn_interrupted":
			var payload failurePayload
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				return ConversationView{}, err
			}
			if view.Chat != nil {
				for index := range view.Chat.Messages {
					if view.Chat.Messages[index].ID == payload.MessageID {
						view.Chat.Messages[index].Error = payload.Error
						value := payload.ResponseDurationMS
						view.Chat.Messages[index].ResponseDurationMS = &value
					}
				}
			}
			if view.Image != nil {
				for index := range view.Image.Entries {
					if view.Image.Entries[index].ID == payload.EntryID {
						view.Image.Entries[index].Pending = false
						view.Image.Entries[index].Error = payload.Error
						value := payload.ResponseDurationMS
						view.Image.Entries[index].ResponseDurationMS = &value
					}
				}
			}
		}
	}
	if view.Chat != nil {
		view.Chat.ServerPersisted = true
	}
	if view.Image != nil {
		view.Image.ServerPersisted = true
	}
	return view, nil
}

func (s *RolloutStore) Remove(conversation store.PlaygroundConversation) error {
	absPath := filepath.Join(s.root, filepath.FromSlash(conversation.RolloutPath))
	if err := os.Remove(absPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
