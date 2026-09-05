package playground

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/auth"
	"xlyra/server/internal/httpx"
	"xlyra/server/internal/store"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) Handler {
	return Handler{service: service}
}

func (h Handler) List(w http.ResponseWriter, r *http.Request) {
	adminID, ok := adminID(w, r)
	if !ok {
		return
	}
	items, err := h.service.List(r.Context(), adminID, r.URL.Query().Get("mode"))
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h Handler) Get(w http.ResponseWriter, r *http.Request) {
	adminID, id, ok := h.conversationIDs(w, r)
	if !ok {
		return
	}
	item, err := h.service.Get(r.Context(), adminID, id)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, item)
}

func (h Handler) StartTurn(w http.ResponseWriter, r *http.Request) {
	adminID, id, ok := h.conversationIDs(w, r)
	if !ok {
		return
	}
	var input TurnRequest
	if err := httpx.DecodeJSONBody(r, &input); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
		return
	}
	item, err := h.service.StartTurn(r.Context(), adminID, id, input)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, item)
}

func (h Handler) Delete(w http.ResponseWriter, r *http.Request) {
	adminID, id, ok := h.conversationIDs(w, r)
	if !ok {
		return
	}
	if err := h.service.Delete(r.Context(), adminID, id); err != nil {
		h.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h Handler) Cancel(w http.ResponseWriter, r *http.Request) {
	adminID, ok := adminID(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "runID"))
	if err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_run_id", "run id must be a UUID")
		return
	}
	if err := h.service.Cancel(r.Context(), adminID, id); err != nil {
		h.writeError(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusAccepted, map[string]any{"status": "cancelling"})
}

func (h Handler) Events(w http.ResponseWriter, r *http.Request) {
	adminID, id, ok := h.conversationIDs(w, r)
	if !ok {
		return
	}
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	flusher, ok := w.(http.Flusher)
	if !ok {
		httpx.Error(w, r, http.StatusInternalServerError, "streaming_unsupported", "streaming is not supported")
		return
	}
	eventsChannel, unsubscribe := h.service.subscribe(id)
	defer unsubscribe()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	lastActivity := time.Now()
	var offset int64
	writeEvent := func(event Event) error {
		if event.Ordinal <= after {
			return nil
		}
		encoded, err := json.Marshal(event)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "id: %d\nevent: rollout\ndata: %s\n\n", event.Ordinal, encoded); err != nil {
			return err
		}
		after = event.Ordinal
		lastActivity = time.Now()
		return nil
	}
	for {
		events, run, nextOffset, err := h.service.Events(r.Context(), adminID, id, offset, after)
		if err != nil {
			return
		}
		offset = nextOffset
		for _, event := range events {
			if err := writeEvent(event); err != nil {
				return
			}
		}
		if len(events) > 0 {
			flusher.Flush()
		}
		active := run.ID != uuid.Nil && (run.Status == "queued" || run.Status == "running")
		if !active {
			encoded, _ := json.Marshal(runView(run))
			_, _ = fmt.Fprintf(w, "event: run_status\ndata: %s\n\n", encoded)
			flusher.Flush()
			return
		}
		if time.Since(lastActivity) >= 15*time.Second {
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
			lastActivity = time.Now()
		}
		select {
		case <-r.Context().Done():
			return
		case event := <-eventsChannel:
			if err := writeEvent(event); err != nil {
				return
			}
			flusher.Flush()
		case <-ticker.C:
		}
	}
}

func (h Handler) Asset(w http.ResponseWriter, r *http.Request) {
	adminID, ok := adminID(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "assetID"))
	if err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_asset_id", "asset id must be a UUID")
		return
	}
	asset, data, err := h.service.assets.Read(r.Context(), id)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	if _, err := h.service.repo.GetConversation(r.Context(), adminID, asset.ConversationID); err != nil {
		h.writeError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", asset.MIMEType)
	w.Header().Set("Content-Length", strconv.FormatInt(int64(len(data)), 10))
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	if len(asset.MIMEType) < 6 || asset.MIMEType[:6] != "image/" {
		w.Header().Set("Content-Disposition", `attachment; filename="download"`)
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (h Handler) Models(w http.ResponseWriter, r *http.Request) {
	if _, ok := adminID(w, r); !ok {
		return
	}
	apiKeyID, err := uuid.Parse(r.URL.Query().Get("api_key_id"))
	if err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_api_key_id", "api key id must be a UUID")
		return
	}
	apiKey, err := store.NewAPIKeyRepository(h.service.db.DB()).GetByID(r.Context(), apiKeyID)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	var body bytes.Buffer
	capture := newCaptureWriter(func(data []byte) error {
		_, writeErr := body.Write(data)
		return writeErr
	})
	request, err := http.NewRequestWithContext(auth.WithAPIKey(r.Context(), apiKey), http.MethodGet, "/models", nil)
	if err != nil {
		h.writeError(w, r, err)
		return
	}
	h.service.gateway.Models(capture, request)
	for key, values := range capture.Header() {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(capture.status)
	_, _ = w.Write(body.Bytes())
}

func adminID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, ok := auth.AdminIDFromContext(r.Context())
	if !ok {
		httpx.Error(w, r, http.StatusUnauthorized, "unauthorized", "admin session is required")
		return uuid.Nil, false
	}
	return id, true
}

func (h Handler) conversationIDs(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	adminID, ok := adminID(w, r)
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	id, err := uuid.Parse(chi.URLParam(r, "conversationID"))
	if err != nil {
		httpx.Error(w, r, http.StatusBadRequest, "invalid_conversation_id", "conversation id must be a UUID")
		return uuid.Nil, uuid.Nil, false
	}
	return adminID, id, true
}

func (h Handler) writeError(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusBadRequest
	code := "playground_error"
	if errors.Is(err, gorm.ErrRecordNotFound) {
		status = http.StatusNotFound
		code = "not_found"
	}
	if errors.Is(err, context.Canceled) {
		status = http.StatusRequestTimeout
		code = "request_cancelled"
	}
	if errors.Is(err, ErrPlaygroundRestoring) {
		status = http.StatusConflict
		code = "playground_restoring"
	}
	httpx.Error(w, r, status, code, err.Error())
}
