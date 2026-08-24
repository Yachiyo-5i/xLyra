package playground

import (
	"bytes"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/store"
)

func testEvent(t *testing.T, ordinal int64, eventType string, payload any) Event {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return Event{Timestamp: time.Unix(ordinal, 0), Ordinal: ordinal, Type: eventType, Payload: encoded}
}

func TestBuildViewReplaysChatDeltasAndFinal(t *testing.T) {
	id := uuid.New()
	conversation := store.PlaygroundConversation{ID: id, Mode: ModeChat, Title: "hello", UpdatedAt: time.Unix(1, 0)}
	started := ChatConversation{
		ID: id.String(), Title: "hello", Messages: []ChatMessage{
			{ID: "user", Role: "user", Content: "hello"},
			{ID: "assistant", Role: "assistant"},
		},
	}
	final := ChatMessage{ID: "assistant", Role: "assistant", Content: "hello world", Reasoning: "done", Model: "model"}
	events := []Event{
		testEvent(t, 1, "conversation_snapshot", snapshotPayload{Mode: ModeChat, Chat: &started}),
		testEvent(t, 2, "assistant_delta", deltaPayload{MessageID: "assistant", Content: "hello ", Reasoning: "do"}),
		testEvent(t, 3, "assistant_delta", deltaPayload{MessageID: "assistant", Content: "world", Reasoning: "ne"}),
		testEvent(t, 4, "assistant_final", final),
	}
	view, err := BuildView(conversation, events)
	if err != nil {
		t.Fatal(err)
	}
	if view.Chat == nil || len(view.Chat.Messages) != 2 {
		t.Fatalf("unexpected chat view: %#v", view.Chat)
	}
	if got := view.Chat.Messages[1]; got.Content != "hello world" || got.Reasoning != "done" || got.Model != "model" {
		t.Fatalf("unexpected assistant message: %#v", got)
	}
	if !view.Chat.ServerPersisted || view.LastOrdinal != 4 || view.UpdatedAt != time.Unix(4, 0).UnixMilli() {
		t.Fatalf("unexpected view metadata: %#v", view)
	}
}

func TestBuildViewAppendsNewChatTurnWithoutRepeatingHistory(t *testing.T) {
	id := uuid.New()
	conversation := store.PlaygroundConversation{ID: id, Mode: ModeChat, UpdatedAt: time.Unix(1, 0)}
	chat := ChatConversation{ID: id.String(), Messages: []ChatMessage{{ID: "old", Role: "assistant", Content: "old"}}}
	added := chatAppendPayload{
		Title: "next", Model: "model", UpdatedAt: 5,
		Messages: []ChatMessage{{ID: "user", Role: "user", Content: "next"}, {ID: "assistant", Role: "assistant"}},
	}
	events := []Event{
		testEvent(t, 1, "conversation_snapshot", snapshotPayload{Mode: ModeChat, Chat: &chat}),
		testEvent(t, 2, "message_added", added),
		testEvent(t, 3, "assistant_delta", deltaPayload{MessageID: "assistant", Content: "answer"}),
	}
	view, err := BuildView(conversation, events)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Chat.Messages) != 3 || view.Chat.Messages[2].Content != "answer" || view.Chat.Title != "next" {
		t.Fatalf("unexpected appended chat: %#v", view.Chat)
	}
}

func TestBuildViewAppliesInterruptedImageRun(t *testing.T) {
	id := uuid.New()
	conversation := store.PlaygroundConversation{ID: id, Mode: ModeImage, UpdatedAt: time.Unix(1, 0)}
	image := ImageConversation{ID: id.String(), Entries: []ImageEntry{{ID: "entry", Prompt: "draw", Pending: true}}}
	events := []Event{
		testEvent(t, 1, "conversation_snapshot", snapshotPayload{Mode: ModeImage, Image: &image}),
		testEvent(t, 2, "turn_interrupted", failurePayload{EntryID: "entry", Error: "server restarted", ResponseDurationMS: 25}),
	}
	view, err := BuildView(conversation, events)
	if err != nil {
		t.Fatal(err)
	}
	entry := view.Image.Entries[0]
	if entry.Pending || entry.Error != "server restarted" || entry.ResponseDurationMS == nil || *entry.ResponseDurationMS != 25 {
		t.Fatalf("unexpected image entry: %#v", entry)
	}
}

func TestSSEDecoderHandlesSplitEvents(t *testing.T) {
	var values []string
	decoder := sseDecoder{onData: func(value string) error {
		values = append(values, value)
		return nil
	}}
	for _, chunk := range []string{"event: message\nda", "ta: {\"a\":1}\n\n", "data: [DONE]\n\n", "data: second\r\n\r\n"} {
		if err := decoder.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	if len(values) != 2 || values[0] != `{"a":1}` || values[1] != "second" {
		t.Fatalf("unexpected SSE values: %#v", values)
	}
}

func TestValidateRemoteURLRejectsPrivateAddresses(t *testing.T) {
	parsed, err := url.Parse("http://127.0.0.1/image.png")
	if err != nil {
		t.Fatal(err)
	}
	if err := validateRemoteURL(t.Context(), parsed); err == nil {
		t.Fatal("expected private address rejection")
	}
}

func TestLastRolloutOrdinalReadsLargeFinalLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	events := []Event{
		testEvent(t, 7, "assistant_delta", deltaPayload{MessageID: "assistant", Content: "first"}),
		testEvent(t, 8, "assistant_final", ChatMessage{ID: "assistant", Content: string(bytes.Repeat([]byte("x"), 130*1024))}),
	}
	var data bytes.Buffer
	for _, event := range events {
		encoded, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		data.Write(encoded)
		data.WriteByte('\n')
	}
	if err := os.WriteFile(path, data.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	ordinal, err := lastRolloutOrdinal(path)
	if err != nil {
		t.Fatal(err)
	}
	if ordinal != 8 {
		t.Fatalf("ordinal = %d, want 8", ordinal)
	}
}

func TestRepairRolloutTailDropsIncompleteEvent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	complete, err := json.Marshal(testEvent(t, 3, "turn_started", map[string]any{"model": "model"}))
	if err != nil {
		t.Fatal(err)
	}
	data := append(append(complete, '\n'), []byte(`{"ordinal":4,"type":"assistant_delta"`)...)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := repairRolloutTail(path); err != nil {
		t.Fatal(err)
	}
	ordinal, err := lastRolloutOrdinal(path)
	if err != nil {
		t.Fatal(err)
	}
	if ordinal != 3 {
		t.Fatalf("ordinal = %d, want 3", ordinal)
	}
}

func TestReadFromContinuesAtLastCompleteOffset(t *testing.T) {
	root := t.TempDir()
	relative := filepath.Join("sessions", "rollout.jsonl")
	path := filepath.Join(root, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	var data bytes.Buffer
	for ordinal := int64(1); ordinal <= 2; ordinal++ {
		encoded, err := json.Marshal(testEvent(t, ordinal, "assistant_delta", deltaPayload{MessageID: "assistant", Content: "x"}))
		if err != nil {
			t.Fatal(err)
		}
		data.Write(encoded)
		data.WriteByte('\n')
	}
	if err := os.WriteFile(path, data.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	rollout := NewRolloutStore(root, store.PlaygroundRepository{})
	conversation := store.PlaygroundConversation{RolloutPath: relative}
	events, offset, err := rollout.ReadFrom(conversation, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Ordinal != 2 || offset != int64(data.Len()) {
		t.Fatalf("unexpected first read: events=%#v offset=%d", events, offset)
	}
	events, nextOffset, err := rollout.ReadFrom(conversation, offset, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 || nextOffset != offset {
		t.Fatalf("unexpected continuation: events=%#v offset=%d", events, nextOffset)
	}
}
