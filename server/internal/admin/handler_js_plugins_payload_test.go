package admin

import (
	"encoding/json"
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
