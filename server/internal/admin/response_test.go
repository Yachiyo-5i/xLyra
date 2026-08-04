package admin

import (
	"net/http"
	"testing"
)

func TestDecodeJSONRejectsTrailingJSONValues(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	req := adminTestRequest(http.MethodPost, "/", `{"ok":true}{"extra":true}`)
	rec := adminRecorder()

	var payload map[string]any
	if handler.decodeJSON(rec, req, &payload) {
		t.Fatal("expected trailing json values to be rejected")
	}
	adminAssertStatus(t, rec, http.StatusBadRequest)
}

func TestWriteItemsKeepsItemsAndMetaShape(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	for _, tc := range []struct {
		name       string
		status     int
		items      []string
		meta       map[string]any
		wantMeta   map[string]float64
		wantItem   string
		wantStatus int
	}{
		{
			name:       "provided meta",
			status:     http.StatusOK,
			items:      []string{"site-1"},
			meta:       map[string]any{"count": 1},
			wantMeta:   map[string]float64{"count": 1},
			wantItem:   "site-1",
			wantStatus: http.StatusOK,
		},
		{
			name:       "nil meta writes empty object",
			status:     http.StatusAccepted,
			items:      []string{"site-2"},
			wantMeta:   map[string]float64{},
			wantItem:   "site-2",
			wantStatus: http.StatusAccepted,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := adminRecorder()

			handler.writeItems(rec, tc.status, tc.items, tc.meta)

			adminAssertStatus(t, rec, tc.wantStatus)
			body := adminDecodeJSON[struct {
				Items []string           `json:"items"`
				Meta  map[string]float64 `json:"meta"`
			}](t, rec)
			if len(body.Items) != 1 || body.Items[0] != tc.wantItem {
				t.Fatalf("items = %#v, want [%s]", body.Items, tc.wantItem)
			}
			if len(body.Meta) != len(tc.wantMeta) {
				t.Fatalf("meta = %#v, want %#v", body.Meta, tc.wantMeta)
			}
			for key, want := range tc.wantMeta {
				if body.Meta[key] != want {
					t.Fatalf("meta[%s] = %#v, want %#v", key, body.Meta[key], want)
				}
			}
		})
	}
}

func TestWritePayloadAndResourceShape(t *testing.T) {
	t.Parallel()

	handler := Handler{}
	rec := adminRecorder()
	handler.writePayload(rec, http.StatusCreated, map[string]any{"ok": true})
	adminAssertStatus(t, rec, http.StatusCreated)
	payload := adminDecodeJSON[map[string]any](t, rec)
	if payload["ok"] != true {
		t.Fatalf("payload body = %#v", payload)
	}

	rec = adminRecorder()
	handler.writeResource(rec, http.StatusAccepted, "site", map[string]any{"id": "site-1"})
	adminAssertStatus(t, rec, http.StatusAccepted)
	resource := adminDecodeJSON[map[string]map[string]any](t, rec)
	if resource["site"]["id"] != "site-1" {
		t.Fatalf("resource body = %#v", resource)
	}
}
