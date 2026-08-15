package settings

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"xlyra/server/internal/backup"
)

func TestStreamBackupRestoreWritesProgressAndTerminalSummary(t *testing.T) {
	req := httptest.NewRequest("POST", "/settings/backup/automatic/files/restore", nil)
	rec := httptest.NewRecorder()
	summary := backup.ImportSummary{Tables: 22, Rows: 800000, ConfigKeys: 3, FormatVersion: 2}

	streamBackupRestore(rec, req, "download", func(_ context.Context, progress backup.ProgressFunc) (backup.ImportSummary, error) {
		progress(backup.ProgressEvent{Step: "download", Status: "complete"})
		progress(backup.ProgressEvent{Step: "complete", Status: "complete", Rows: summary.Rows, TotalRows: summary.Rows, Summary: &summary})
		return summary, nil
	})

	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "text/event-stream") {
		t.Fatalf("content type = %q", got)
	}
	events := backupProgressEvents(t, rec.Body.String())
	if len(events) != 2 || events[1].Summary == nil || events[1].Summary.Rows != summary.Rows || events[1].TotalRows != summary.Rows {
		t.Fatalf("events = %#v", events)
	}
}

func TestStreamBackupRestoreReportsTheFailingStep(t *testing.T) {
	req := httptest.NewRequest("POST", "/settings/backup/automatic/files/restore", nil)
	rec := httptest.NewRecorder()

	streamBackupRestore(rec, req, "download", func(_ context.Context, progress backup.ProgressFunc) (backup.ImportSummary, error) {
		progress(backup.ProgressEvent{Step: "import", Status: "in_progress", Rows: 2000, Table: "request_logs"})
		return backup.ImportSummary{}, errors.New("insert request_logs failed")
	})

	events := backupProgressEvents(t, rec.Body.String())
	if len(events) != 2 || events[1].Step != "import" || events[1].Status != "error" {
		t.Fatalf("events = %#v", events)
	}
}

func backupProgressEvents(t *testing.T, body string) []backup.ProgressEvent {
	t.Helper()
	frames := strings.Split(strings.TrimSpace(body), "\n\n")
	events := make([]backup.ProgressEvent, 0, len(frames))
	for _, frame := range frames {
		data := strings.TrimPrefix(frame, "data: ")
		var event backup.ProgressEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			t.Fatalf("decode %q: %v", frame, err)
		}
		events = append(events, event)
	}
	return events
}
