package auth

import (
	"net/http/httptest"
	"testing"
)

func TestGeminiDownstreamAPIKeyAliases(t *testing.T) {
	list := httptest.NewRequest("GET", "/v1beta/models?key=list-key", nil)
	if got := apiKeyFromRequest(list); got != "list-key" {
		t.Fatalf("Gemini models query key = %q, want list-key", got)
	}

	list.Header.Set("x-goog-api-key", " list-header-key ")
	if got := apiKeyFromRequest(list); got != "list-header-key" {
		t.Fatalf("Gemini models x-goog-api-key = %q, want list-header-key", got)
	}

	req := httptest.NewRequest("POST", "/v1beta/models/gemini-2.5-pro:generateContent", nil)
	req.Header.Set("x-goog-api-key", " goog-key ")
	if got := apiKeyFromRequest(req); got != "goog-key" {
		t.Fatalf("x-goog-api-key = %q, want goog-key", got)
	}

	query := httptest.NewRequest("POST", "/v1beta/models/gemini-2.5-pro:streamGenerateContent?key=query-key", nil)
	if got := apiKeyFromRequest(query); got != "query-key" {
		t.Fatalf("Gemini key query = %q, want query-key", got)
	}

	other := httptest.NewRequest("POST", "/v1/chat/completions?key=query-key", nil)
	other.Header.Set("x-goog-api-key", "goog-key")
	if got := apiKeyFromRequest(other); got != "" {
		t.Fatalf("non-Gemini aliases must be ignored, got %q", got)
	}
}
