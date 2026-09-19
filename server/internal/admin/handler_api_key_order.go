package admin

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"xlyra/server/internal/store"
)

func (h Handler) ReorderAPIKeys(w http.ResponseWriter, r *http.Request) {
	if h.auth == nil {
		h.writeError(w, r, http.StatusServiceUnavailable, "auth_unavailable", "auth service is not available")
		return
	}
	var payload struct {
		IDs []uuid.UUID `json:"ids"`
	}
	if !h.decodeJSON(w, r, &payload) {
		return
	}
	if len(payload.IDs) == 0 {
		h.writeError(w, r, http.StatusBadRequest, "invalid_api_key_order", "ids are required")
		return
	}
	if err := h.auth.ReorderAPIKeys(r.Context(), payload.IDs); err != nil {
		switch {
		case errors.Is(err, store.ErrInvalidAPIKeyOrder):
			h.writeError(w, r, http.StatusBadRequest, "invalid_api_key_order", err.Error())
		default:
			h.writeError(w, r, http.StatusInternalServerError, "api_key_order_failed", "failed to save api key order")
		}
		return
	}
	h.writePayload(w, http.StatusOK, map[string]any{"success": true})
}
