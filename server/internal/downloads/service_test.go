package downloads

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/auth"
)

func TestRegisterAndConsumeDownloadOnce(t *testing.T) {
	svc := NewService(time.Minute)
	adminID := uuid.New()
	ctx := auth.WithAdminActor(context.Background(), auth.AdminActor{Type: "session", AdminID: adminID, SessionID: uuid.New()})

	ticket, err := svc.Register(ctx, RegisterInput{
		Filename:    "../codex.json",
		ContentType: "application/json",
		Scope:       "oauth.export",
		Data:        []byte(`{"ok":true}`),
	})
	if err != nil {
		t.Fatalf("register download: %v", err)
	}
	if ticket.Filename != "codex.json" {
		t.Fatalf("filename = %q, want codex.json", ticket.Filename)
	}

	consumed, data, err := svc.Consume(ctx, ticket.ID)
	if err != nil {
		t.Fatalf("consume download: %v", err)
	}
	if consumed.ID != ticket.ID || string(data) != `{"ok":true}` {
		t.Fatalf("unexpected consumed download: %#v data=%s", consumed, data)
	}

	_, _, err = svc.Consume(ctx, ticket.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("second consume error = %v, want ErrNotFound", err)
	}
}

func TestConsumeRejectsDifferentAdmin(t *testing.T) {
	svc := NewService(time.Minute)
	ownerCtx := auth.WithAdminActor(context.Background(), auth.AdminActor{Type: "session", AdminID: uuid.New(), SessionID: uuid.New()})
	otherCtx := auth.WithAdminActor(context.Background(), auth.AdminActor{Type: "session", AdminID: uuid.New(), SessionID: uuid.New()})

	ticket, err := svc.Register(ownerCtx, RegisterInput{Filename: "backup.xlyra", Data: []byte("backup")})
	if err != nil {
		t.Fatalf("register download: %v", err)
	}
	_, _, err = svc.Consume(otherCtx, ticket.ID)
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("consume error = %v, want ErrForbidden", err)
	}

	_, data, err := svc.Consume(ownerCtx, ticket.ID)
	if err != nil {
		t.Fatalf("owner should still consume after forbidden attempt: %v", err)
	}
	if string(data) != "backup" {
		t.Fatalf("data = %q, want backup", data)
	}
}

func TestConsumeExpiresDownload(t *testing.T) {
	svc := NewService(time.Minute)
	now := time.Date(2026, 5, 23, 0, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	ctx := auth.WithAdminActor(context.Background(), auth.AdminActor{Type: "session", AdminID: uuid.New(), SessionID: uuid.New()})

	ticket, err := svc.Register(ctx, RegisterInput{Filename: "backup.xlyra", TTL: time.Second, Data: []byte("backup")})
	if err != nil {
		t.Fatalf("register download: %v", err)
	}
	svc.now = func() time.Time { return now.Add(2 * time.Second) }

	_, _, err = svc.Consume(ctx, ticket.ID)
	if !errors.Is(err, ErrExpired) {
		t.Fatalf("consume error = %v, want ErrExpired", err)
	}
}

func TestRegisterRejectsUnavailableMissingContentAndAdminActor(t *testing.T) {
	t.Parallel()

	ctx := auth.WithAdminActor(context.Background(), auth.AdminActor{Type: "session", AdminID: uuid.New(), SessionID: uuid.New()})
	var nilService *Service
	if _, err := nilService.Register(ctx, RegisterInput{Data: []byte("backup")}); err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("nil service Register error = %v, want not available", err)
	}

	svc := NewService(time.Minute)
	if _, err := svc.Register(ctx, RegisterInput{Filename: "empty.xlyra"}); err == nil || !strings.Contains(err.Error(), "content is required") {
		t.Fatalf("missing content Register error = %v, want content is required", err)
	}
	if _, err := svc.Register(context.Background(), RegisterInput{Data: []byte("backup")}); err == nil || !strings.Contains(err.Error(), "admin actor is required") {
		t.Fatalf("missing actor Register error = %v, want admin actor is required", err)
	}
}

func TestRegisterDefaultsAndSanitizesFilename(t *testing.T) {
	t.Parallel()

	svc := NewService(time.Minute)
	ctx := auth.WithAdminActor(context.Background(), auth.AdminActor{Type: "session", AdminID: uuid.New(), SessionID: uuid.New()})

	ticket, err := svc.Register(ctx, RegisterInput{
		Filename: " ../exports/backup\r\n\x00.json ",
		Data:     []byte("backup"),
	})
	if err != nil {
		t.Fatalf("register download: %v", err)
	}
	if ticket.Filename != "backup.json" {
		t.Fatalf("filename = %q, want backup.json", ticket.Filename)
	}
	if ticket.ContentType != "application/octet-stream" {
		t.Fatalf("content type = %q, want application/octet-stream", ticket.ContentType)
	}
}

func TestSanitizeFilenameFallsBackForUnsafeNames(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"", ".", "..", "/"} {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := sanitizeFilename(name); got != "download.bin" {
				t.Fatalf("sanitizeFilename(%q) = %q, want download.bin", name, got)
			}
		})
	}
}
