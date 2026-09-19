package scheduler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"xlyra/server/internal/claudeversion"
)

func TestClaudeVersionRefreshUpdatesAndPreservesLastGoodVersion(t *testing.T) {
	var fetchErr error
	calls := 0
	t.Cleanup(claudeversion.WithFetcher(func(ctx context.Context) (string, error) {
		calls++
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("refresh context has no deadline")
		}
		return "2.999.0", fetchErr
	}))
	s := New(slog.New(slog.NewTextHandler(io.Discard, nil)), Options{}, nil, nil, nil)
	s.claudeVersioning.Store(true)
	s.runClaudeVersionRefresh()
	if calls != 0 {
		t.Fatal("refresh did not skip an active run")
	}
	s.claudeVersioning.Store(false)
	s.runClaudeVersionRefresh()
	if calls != 1 || claudeversion.Version() != "2.999.0" || s.claudeVersioning.Load() {
		t.Fatalf("successful refresh: calls=%d version=%s active=%t", calls, claudeversion.Version(), s.claudeVersioning.Load())
	}
	fetchErr = errors.New("registry unavailable")
	s.runClaudeVersionRefresh()
	if calls != 2 || claudeversion.Version() != "2.999.0" || s.claudeVersioning.Load() {
		t.Fatalf("failed refresh: calls=%d version=%s active=%t", calls, claudeversion.Version(), s.claudeVersioning.Load())
	}
}
