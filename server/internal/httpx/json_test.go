package httpx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeJSONBodyRejectsTrailingJSONValues(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"ok":true}{"extra":true}`))
	var payload map[string]any
	if err := DecodeJSONBody(req, &payload); err == nil {
		t.Fatal("expected trailing json to be rejected")
	}
}

func TestDecodeJSONBodyAcceptsSingleValue(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"ok":true}`))
	var payload map[string]any
	if err := DecodeJSONBody(req, &payload); err != nil {
		t.Fatalf("expected single json value to decode, got %v", err)
	}
}

func TestDecodeJSONBodyRejectsTrailingMalformedJSON(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"ok":true} {`))
	var payload map[string]any
	err := DecodeJSONBody(req, &payload)

	if err == nil {
		t.Fatal("expected trailing malformed json to be rejected")
	}
	if !strings.Contains(err.Error(), "decode json body:") {
		t.Fatalf("error = %q, want decode json body prefix", err.Error())
	}
}
