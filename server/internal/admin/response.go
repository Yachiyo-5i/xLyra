package admin

import (
	"net/http"

	"github.com/go-chi/chi/v5/middleware"

	"xlyra/server/internal/httpx"
)

func (h Handler) writeError(w http.ResponseWriter, r *http.Request, status int, code string, message string) {
	httpx.Error(w, r, status, code, message)
}

func (h Handler) writeErrorFields(w http.ResponseWriter, r *http.Request, status int, code string, message string, fields map[string]any) {
	body := map[string]any{"code": code, "message": message}
	if requestID := middleware.GetReqID(r.Context()); requestID != "" {
		body["request_id"] = requestID
	}
	for key, value := range fields {
		body[key] = value
	}
	httpx.JSON(w, status, map[string]any{"error": body})
}

func (h Handler) decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := httpx.DecodeJSONBody(r, dst); err != nil {
		h.writeError(w, r, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
		return false
	}
	return true
}

func (h Handler) writeItems(w http.ResponseWriter, status int, items any, meta map[string]any) {
	if meta == nil {
		meta = map[string]any{}
	}
	httpx.JSON(w, status, map[string]any{
		"items": items,
		"meta":  meta,
	})
}

func (h Handler) writeResource(w http.ResponseWriter, status int, key string, value any) {
	httpx.JSON(w, status, map[string]any{key: value})
}

func (h Handler) writePayload(w http.ResponseWriter, status int, payload any) {
	httpx.JSON(w, status, payload)
}
