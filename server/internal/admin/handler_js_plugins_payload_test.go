package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"xlyra/server/internal/store"
)

// The version payload carries the manifest and self-test as objects. store.JSON
// is a byte slice, so sent as is it would reach the page as a base64 string.
func TestJSPluginVersionPayloadSendsStoredJSONAsObjects(t *testing.T) {
	payload := jsPluginVersionPayload(store.JSPluginVersion{
		PluginID: "acme", Version: "1.0.0",
		Manifest:           store.JSON(`{"name":"Acme","kind":"automation"}`),
		SelfTest:           store.JSON(`{"ok":true}`),
		GrantedPermissions: store.JSON(`["notify"]`),
	})
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Manifest           map[string]any `json:"manifest"`
		Selftest           map[string]any `json:"selftest"`
		GrantedPermissions []string       `json:"granted_permissions"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("payload is not shaped as objects: %v (%s)", err, raw)
	}
	if decoded.Manifest["name"] != "Acme" || decoded.Selftest["ok"] != true || len(decoded.GrantedPermissions) != 1 {
		t.Fatalf("decoded = %+v", decoded)
	}
}

// A request the page abandoned is not logged as a server error.
func TestAbandonIfClientGoneAnswersACancelledRequestBelow500(t *testing.T) {
	live := httptest.NewRecorder()
	if abandonIfClientGone(live, httptest.NewRequest(http.MethodGet, "/", nil)) {
		t.Fatal("a live request was abandoned")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	gone := httptest.NewRecorder()
	if !abandonIfClientGone(gone, httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)) {
		t.Fatal("a cancelled request was not abandoned")
	}
	if gone.Code != statusClientClosedRequest || gone.Code >= http.StatusInternalServerError {
		t.Fatalf("status = %d, want a 4xx so it is not logged as a server error", gone.Code)
	}
}
