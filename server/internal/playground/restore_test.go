package playground

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/store"
)

func TestQuiesceForRestoreCancelsRunsAndClearsState(t *testing.T) {
	t.Parallel()

	runID := uuid.New()
	conversationID := uuid.New()
	cancelled := false
	finished := make(chan struct{})
	service := &Service{
		cancels: map[uuid.UUID]context.CancelFunc{
			runID: func() { cancelled = true },
		},
		finishes: map[uuid.UUID]chan struct{}{runID: finished},
		turns:    map[uuid.UUID]*sync.Mutex{conversationID: {}},
		runs:     map[uuid.UUID]store.PlaygroundRun{conversationID: {ID: runID}},
	}

	go func() {
		time.Sleep(10 * time.Millisecond)
		close(finished)
	}()

	if err := service.QuiesceForRestore(context.Background()); err != nil {
		t.Fatalf("quiesce for restore: %v", err)
	}
	if !cancelled {
		t.Fatal("expected active run to be cancelled")
	}
	if len(service.runs) != 0 || len(service.turns) != 0 {
		t.Fatalf("expected in-memory state cleared, got runs=%d turns=%d", len(service.runs), len(service.turns))
	}
	if !service.restoring {
		t.Fatal("expected restoring gate to be set after quiesce")
	}
}

func TestQuiesceForRestoreWithoutActiveRuns(t *testing.T) {
	t.Parallel()

	service := &Service{
		cancels:  map[uuid.UUID]context.CancelFunc{},
		finishes: map[uuid.UUID]chan struct{}{},
		turns:    map[uuid.UUID]*sync.Mutex{},
		runs:     map[uuid.UUID]store.PlaygroundRun{},
	}
	if err := service.QuiesceForRestore(context.Background()); err != nil {
		t.Fatalf("quiesce without runs: %v", err)
	}
}

func TestStartTurnRejectedWhileRestoring(t *testing.T) {
	t.Parallel()

	service := &Service{restoring: true}
	if _, err := service.StartTurn(context.Background(), uuid.New(), uuid.New(), TurnRequest{Mode: ModeChat}); !errors.Is(err, ErrPlaygroundRestoring) {
		t.Fatalf("expected restoring error, got %v", err)
	}
}

func TestQuiesceForRestoreRejectsConcurrentRestore(t *testing.T) {
	t.Parallel()

	service := &Service{
		restoring: true,
		cancels:   map[uuid.UUID]context.CancelFunc{},
		finishes:  map[uuid.UUID]chan struct{}{},
		turns:     map[uuid.UUID]*sync.Mutex{},
		runs:      map[uuid.UUID]store.PlaygroundRun{},
	}
	if err := service.QuiesceForRestore(context.Background()); !errors.Is(err, ErrPlaygroundRestoring) {
		t.Fatalf("expected restoring error, got %v", err)
	}
}
