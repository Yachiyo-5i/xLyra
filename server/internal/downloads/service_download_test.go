package downloads

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"xlyra/server/internal/auth"
)

func TestDownloadServesRegisteredDataWithAttachmentHeaders(t *testing.T) {
	t.Parallel()

	svc := NewService(time.Minute)
	ctx := downloadTestAdminContext()
	ticket, err := svc.Register(ctx, RegisterInput{
		Filename:    "report.json",
		ContentType: "application/json",
		Data:        []byte(`{"ok":true}`),
	})
	if err != nil {
		t.Fatalf("register download: %v", err)
	}
	req := downloadTestRequest(ctx, ticket.ID.String())
	rec := httptest.NewRecorder()

	svc.Download(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != `{"ok":true}` {
		t.Fatalf("body = %q", got)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q", got)
	}
	if got := rec.Header().Get("X-Download-Filename"); got != "report.json" {
		t.Fatalf("X-Download-Filename = %q", got)
	}
	if got := rec.Header().Get("Content-Disposition"); !strings.Contains(got, `filename=report.json`) {
		t.Fatalf("Content-Disposition = %q", got)
	}
}

func TestDownloadMapsConsumeErrorsToHTTPStatus(t *testing.T) {
	t.Parallel()

	svc := NewService(time.Minute)
	ownerCtx := downloadTestAdminContext()
	otherCtx := auth.WithAdminActor(context.Background(), auth.AdminActor{Type: "session", AdminID: uuid.New(), SessionID: uuid.New()})
	ticket, err := svc.Register(ownerCtx, RegisterInput{Filename: "secret.txt", Data: []byte("secret")})
	if err != nil {
		t.Fatalf("register download: %v", err)
	}

	tests := []struct {
		name       string
		service    *Service
		ctx        context.Context
		id         string
		wantStatus int
		wantCode   string
	}{
		{
			name:       "invalid id",
			service:    svc,
			ctx:        ownerCtx,
			id:         "not-a-uuid",
			wantStatus: http.StatusBadRequest,
			wantCode:   "invalid_download_id",
		},
		{
			name:       "not found",
			service:    svc,
			ctx:        ownerCtx,
			id:         uuid.NewString(),
			wantStatus: http.StatusNotFound,
			wantCode:   "download_not_found",
		},
		{
			name:       "forbidden",
			service:    svc,
			ctx:        otherCtx,
			id:         ticket.ID.String(),
			wantStatus: http.StatusForbidden,
			wantCode:   "download_forbidden",
		},
		{
			name:       "nil service",
			service:    nil,
			ctx:        ownerCtx,
			id:         uuid.NewString(),
			wantStatus: http.StatusNotFound,
			wantCode:   "download_not_found",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			tt.service.Download(rec, downloadTestRequest(tt.ctx, tt.id))

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d, body=%s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tt.wantCode) {
				t.Fatalf("response body %q does not contain %q", rec.Body.String(), tt.wantCode)
			}
		})
	}
}

func TestDownloadReportsGeneratorErrorAfterTicketIsConsumed(t *testing.T) {
	t.Parallel()

	svc := NewService(time.Minute)
	ctx := downloadTestAdminContext()
	ticket, err := svc.Register(ctx, RegisterInput{
		Filename: "broken.txt",
		Generate: func(context.Context) ([]byte, error) {
			return nil, errors.New("generation failed")
		},
	})
	if err != nil {
		t.Fatalf("register download: %v", err)
	}

	rec := httptest.NewRecorder()
	svc.Download(rec, downloadTestRequest(ctx, ticket.ID.String()))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "download_failed") {
		t.Fatalf("expected download_failed response, got %s", rec.Body.String())
	}

	_, _, err = svc.Consume(ctx, ticket.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("ticket should be consumed after generator error, got %v", err)
	}
}

func TestDownloadServesRegisteredFileAndDeletesIt(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "backup.xlyra")
	if err := os.WriteFile(path, []byte("file-backup"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	svc := NewService(time.Minute)
	ctx := downloadTestAdminContext()
	ticket, err := svc.Register(ctx, RegisterInput{
		Filename:    "backup.xlyra",
		ContentType: "application/octet-stream",
		FilePath:    path,
		DeleteFile:  true,
	})
	if err != nil {
		t.Fatalf("register file download: %v", err)
	}

	rec := httptest.NewRecorder()
	svc.Download(rec, downloadTestRequest(ctx, ticket.ID.String()))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != "file-backup" {
		t.Fatalf("body = %q, want file-backup", got)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temp file stat error = %v, want not exist", err)
	}
}

func TestDownloadMissingRegisteredFileConsumesTicket(t *testing.T) {
	t.Parallel()

	svc := NewService(time.Minute)
	ctx := downloadTestAdminContext()
	missingPath := filepath.Join(t.TempDir(), "missing.xlyra")
	ticket, err := svc.Register(ctx, RegisterInput{
		Filename: "missing.xlyra",
		FilePath: missingPath,
	})
	if err != nil {
		t.Fatalf("register file download: %v", err)
	}

	rec := httptest.NewRecorder()
	svc.Download(rec, downloadTestRequest(ctx, ticket.ID.String()))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "download_failed") {
		t.Fatalf("expected download_failed response, got %s", rec.Body.String())
	}
	_, _, err = svc.Consume(ctx, ticket.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("ticket should be consumed after file open error, got %v", err)
	}
}

func TestConsumeReadsRegisteredFileAndDeletesIt(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "consume.xlyra")
	if err := os.WriteFile(path, []byte("consume-file"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	svc := NewService(time.Minute)
	ctx := downloadTestAdminContext()
	ticket, err := svc.Register(ctx, RegisterInput{
		Filename:   "consume.xlyra",
		FilePath:   path,
		DeleteFile: true,
	})
	if err != nil {
		t.Fatalf("register file download: %v", err)
	}

	consumed, data, err := svc.Consume(ctx, ticket.ID)
	if err != nil {
		t.Fatalf("consume file download: %v", err)
	}
	if consumed.ID != ticket.ID {
		t.Fatalf("consumed id = %s, want %s", consumed.ID, ticket.ID)
	}
	if got := string(data); got != "consume-file" {
		t.Fatalf("data = %q, want consume-file", got)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temp file stat error = %v, want not exist", err)
	}
}

func TestItemContentReportsMissingInlineContent(t *testing.T) {
	t.Parallel()

	data, err := (item{}).content(context.Background())
	if err == nil || !strings.Contains(err.Error(), "content is required") {
		t.Fatalf("content error = %v, want content is required", err)
	}
	if data != nil {
		t.Fatalf("content data = %q, want nil", data)
	}
}

func TestPurgeExpiredLockedRemovesExpiredFileItemsOnly(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	expiredPath := filepath.Join(dir, "expired.xlyra")
	activePath := filepath.Join(dir, "active.xlyra")
	if err := os.WriteFile(expiredPath, []byte("expired"), 0o600); err != nil {
		t.Fatalf("write expired file: %v", err)
	}
	if err := os.WriteFile(activePath, []byte("active"), 0o600); err != nil {
		t.Fatalf("write active file: %v", err)
	}

	now := time.Date(2026, 6, 23, 10, 0, 0, 0, time.UTC)
	svc := NewService(time.Minute)
	svc.now = func() time.Time { return now }
	expiredID := uuid.New()
	activeID := uuid.New()
	svc.items[expiredID] = item{id: expiredID, filePath: expiredPath, deleteFile: true, expiresAt: now.Add(-time.Second)}
	svc.items[activeID] = item{id: activeID, filePath: activePath, deleteFile: true, expiresAt: now.Add(time.Second)}

	svc.mu.Lock()
	svc.purgeExpiredLocked()
	svc.mu.Unlock()

	if _, ok := svc.items[expiredID]; ok {
		t.Fatalf("expired item remained in service")
	}
	if _, ok := svc.items[activeID]; !ok {
		t.Fatalf("active item was purged")
	}
	if _, err := os.Stat(expiredPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expired file stat error = %v, want not exist", err)
	}
	if _, err := os.Stat(activePath); err != nil {
		t.Fatalf("active file stat error = %v", err)
	}
}

func downloadTestAdminContext() context.Context {
	return auth.WithAdminActor(context.Background(), auth.AdminActor{Type: "session", AdminID: uuid.New(), SessionID: uuid.New()})
}

func downloadTestRequest(ctx context.Context, downloadID string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/downloads/"+downloadID, nil).WithContext(ctx)
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("downloadID", downloadID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
}
