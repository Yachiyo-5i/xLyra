package downloads

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"xlyra/server/internal/auth"
	"xlyra/server/internal/httpx"
)

const defaultTTL = 5 * time.Minute

var (
	ErrNotFound  = errors.New("download was not found")
	ErrExpired   = errors.New("download has expired")
	ErrForbidden = errors.New("download does not belong to the current admin")
)

type Generator func(ctx context.Context) ([]byte, error)

type RegisterInput struct {
	Filename    string
	ContentType string
	Scope       string
	TTL         time.Duration
	Data        []byte
	Generate    Generator
	FilePath    string
	DeleteFile  bool
}

type Ticket struct {
	ID          uuid.UUID `json:"id"`
	URL         string    `json:"url"`
	Filename    string    `json:"filename"`
	ContentType string    `json:"content_type"`
	ExpiresAt   time.Time `json:"expires_at"`
}

type item struct {
	id          uuid.UUID
	filename    string
	contentType string
	scope       string
	ownerAdmin  uuid.UUID
	expiresAt   time.Time
	generate    Generator
	filePath    string
	deleteFile  bool
}

type Service struct {
	mu    sync.Mutex
	items map[uuid.UUID]item
	now   func() time.Time
	ttl   time.Duration
}

func NewService(ttls ...time.Duration) *Service {
	ttl := defaultTTL
	if len(ttls) > 0 && ttls[0] > 0 {
		ttl = ttls[0]
	}
	return &Service{
		items: map[uuid.UUID]item{},
		now:   time.Now,
		ttl:   ttl,
	}
}

func (s *Service) Register(ctx context.Context, input RegisterInput) (Ticket, error) {
	if s == nil {
		return Ticket{}, fmt.Errorf("download service is not available")
	}
	filePath := strings.TrimSpace(input.FilePath)
	if input.Generate == nil && input.Data == nil && filePath == "" {
		return Ticket{}, fmt.Errorf("download content is required")
	}
	actor, ok := auth.AdminActorFromContext(ctx)
	if !ok || actor.AdminID == uuid.Nil {
		return Ticket{}, fmt.Errorf("admin actor is required")
	}
	filename := sanitizeFilename(input.Filename)
	contentType := strings.TrimSpace(input.ContentType)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	generate := input.Generate
	if generate == nil {
		if filePath == "" {
			data := append([]byte(nil), input.Data...)
			generate = func(context.Context) ([]byte, error) {
				return append([]byte(nil), data...), nil
			}
		}
	}
	ttl := input.TTL
	if ttl <= 0 {
		ttl = s.ttl
	}
	id := uuid.New()
	expiresAt := s.now().Add(ttl)
	registered := item{
		id:          id,
		filename:    filename,
		contentType: contentType,
		scope:       strings.TrimSpace(input.Scope),
		ownerAdmin:  actor.AdminID,
		expiresAt:   expiresAt,
		generate:    generate,
		filePath:    filePath,
		deleteFile:  input.DeleteFile,
	}
	s.mu.Lock()
	s.purgeExpiredLocked()
	s.items[id] = registered
	s.mu.Unlock()
	return Ticket{
		ID:          id,
		URL:         "/api/v1/downloads/" + id.String(),
		Filename:    filename,
		ContentType: contentType,
		ExpiresAt:   expiresAt,
	}, nil
}

func (s *Service) Consume(ctx context.Context, id uuid.UUID) (Ticket, []byte, error) {
	registered, err := s.consumeItem(ctx, id)
	if err != nil {
		return Ticket{}, nil, err
	}
	data, err := registered.content(ctx)
	if registered.filePath != "" && registered.deleteFile {
		_ = os.Remove(registered.filePath)
	}
	if err != nil {
		return Ticket{}, nil, err
	}
	return registered.ticket(), data, nil
}

func (s *Service) consumeItem(ctx context.Context, id uuid.UUID) (item, error) {
	if s == nil {
		return item{}, ErrNotFound
	}
	actor, ok := auth.AdminActorFromContext(ctx)
	if !ok || actor.AdminID == uuid.Nil {
		return item{}, ErrForbidden
	}
	s.mu.Lock()
	registered, ok := s.items[id]
	if !ok {
		s.mu.Unlock()
		return item{}, ErrNotFound
	}
	if s.now().After(registered.expiresAt) {
		delete(s.items, id)
		s.mu.Unlock()
		registered.deleteRegisteredFile()
		return item{}, ErrExpired
	}
	if registered.ownerAdmin != actor.AdminID {
		s.mu.Unlock()
		return item{}, ErrForbidden
	}
	delete(s.items, id)
	s.mu.Unlock()
	return registered, nil
}

func (s *Service) Download(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "downloadID"))
	if err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_download_id", "download id must be a valid UUID")
		return
	}
	registered, err := s.consumeItem(r.Context(), id)
	if err != nil {
		status := http.StatusInternalServerError
		code := "download_failed"
		switch {
		case errors.Is(err, ErrNotFound):
			status = http.StatusNotFound
			code = "download_not_found"
		case errors.Is(err, ErrForbidden):
			status = http.StatusForbidden
			code = "download_forbidden"
		case errors.Is(err, ErrExpired):
			status = http.StatusGone
			code = "download_expired"
		}
		httpx.Error(w, r, status, code, err.Error())
		return
	}
	ticket := registered.ticket()
	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": ticket.Filename})
	w.Header().Set("Content-Type", ticket.ContentType)
	w.Header().Set("Content-Disposition", disposition)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Download-Filename", ticket.Filename)
	w.Header().Set("X-Backup-Filename", ticket.Filename)

	if registered.filePath != "" {
		file, err := os.Open(registered.filePath)
		if registered.deleteFile {
			defer os.Remove(registered.filePath)
		}
		if err != nil {
			httpx.Error(w, r, http.StatusInternalServerError, "download_failed", err.Error())
			return
		}
		defer file.Close()
		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, file)
		return
	}

	data, err := registered.content(r.Context())
	if err != nil {
		httpx.Error(w, r, http.StatusInternalServerError, "download_failed", err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (i item) ticket() Ticket {
	return Ticket{
		ID:          i.id,
		URL:         "/api/v1/downloads/" + i.id.String(),
		Filename:    i.filename,
		ContentType: i.contentType,
		ExpiresAt:   i.expiresAt,
	}
}

func (i item) content(ctx context.Context) ([]byte, error) {
	if i.generate != nil {
		return i.generate(ctx)
	}
	if i.filePath == "" {
		return nil, fmt.Errorf("download content is required")
	}
	return os.ReadFile(i.filePath)
}

func (i item) deleteRegisteredFile() {
	if i.filePath != "" && i.deleteFile {
		_ = os.Remove(i.filePath)
	}
}

func sanitizeFilename(filename string) string {
	filename = strings.TrimSpace(filename)
	filename = strings.ReplaceAll(filename, "\\", "/")
	filename = filepath.Base(filename)
	filename = strings.Map(func(r rune) rune {
		switch r {
		case 0, '\r', '\n':
			return -1
		default:
			return r
		}
	}, filename)
	if filename == "" || filename == "." || filename == ".." || filename == "/" {
		return "download.bin"
	}
	return filename
}

func (s *Service) purgeExpiredLocked() {
	now := s.now()
	for id, registered := range s.items {
		if now.After(registered.expiresAt) {
			delete(s.items, id)
			registered.deleteRegisteredFile()
		}
	}
}
