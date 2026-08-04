package admin

import (
	"net/http"

	"xlyra/server/internal/httpx"
)

func (h Handler) writeError(w http.ResponseWriter, r *http.Request, status int, code string, message string) {
	httpx.Error(w, r, status, code, message)
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
