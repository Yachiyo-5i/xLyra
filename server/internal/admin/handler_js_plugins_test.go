package admin

import (
	"net/http"
	"testing"

	"xlyra/server/internal/jsplugin"
)

func TestListBuiltinJSPlugins(t *testing.T) {
	rec := adminPerform(Handler{}.ListBuiltinJSPlugins, adminTestRequest(http.MethodGet, "/api/v1/js-plugins/builtins", ""))
	adminAssertStatus(t, rec, http.StatusOK)
	body := adminDecodeJSON[map[string]any](t, rec)
	items, _ := body["items"].([]any)
	if len(items) < 8 {
		t.Fatalf("items = %d, want at least 8 builtins (7 probes + systemone)", len(items))
	}
	registry, err := jsplugin.LoadBuiltins()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != len(registry.Plugins()) {
		t.Fatalf("items = %d, registry = %d", len(items), len(registry.Plugins()))
	}
}
