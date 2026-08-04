package site

import (
	"testing"

	"xlyra/server/internal/adapter"
)

func TestGatewayConfigFromSiteMetaReturnsNilForUnusableMeta(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		raw  []byte
	}{
		{name: "empty", raw: nil},
		{name: "invalid json", raw: []byte(`not-json`)},
		{name: "missing gateway", raw: []byte(`{"notes":"keep"}`)},
		{name: "null gateway", raw: []byte(`{"gateway":null}`)},
		{name: "invalid gateway value", raw: []byte(`{"gateway":{"request_timeout_ms":0}}`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := GatewayConfigFromSiteMeta(tc.raw); got != nil {
				t.Fatalf("GatewayConfigFromSiteMeta(%s) = %#v, want nil", string(tc.raw), got)
			}
		})
	}
}

func TestNormalizeGatewayConfigKeepsExplicitEmptyDisabledTools(t *testing.T) {
	t.Parallel()

	cfg, err := NormalizeGatewayConfig(&GatewayConfig{
		ResponsesToolPolicy:    " compatibility ",
		DisabledResponsesTools: []string{"unknown-tool", " "},
	})
	if err != nil {
		t.Fatalf("NormalizeGatewayConfig() error = %v", err)
	}
	if cfg.ResponsesToolPolicy != ResponsesToolPolicyCompatibility {
		t.Fatalf("responses tool policy = %q, want %q", cfg.ResponsesToolPolicy, ResponsesToolPolicyCompatibility)
	}
	if cfg.DisabledResponsesTools == nil {
		t.Fatal("explicit disabled_responses_tools input should normalize to an empty slice, not nil")
	}
	if len(cfg.DisabledResponsesTools) != 0 {
		t.Fatalf("disabled responses tools = %#v, want empty", cfg.DisabledResponsesTools)
	}
}

func TestMergeSiteGatewayConfigHandlesNilPatchWithoutParsingStore(t *testing.T) {
	t.Parallel()

	raw, err := MergeSiteGatewayConfig(nil, nil)
	if err != nil {
		t.Fatalf("MergeSiteGatewayConfig(nil, nil) error = %v", err)
	}
	if string(raw) != `{}` {
		t.Fatalf("empty merge = %s, want {}", string(raw))
	}

	existing := []byte(`{"notes":"keep"}`)
	raw, err = MergeSiteGatewayConfig(existing, nil)
	if err != nil {
		t.Fatalf("MergeSiteGatewayConfig(existing, nil) error = %v", err)
	}
	if string(raw) != string(existing) {
		t.Fatalf("nil patch should preserve existing meta bytes, got %s", string(raw))
	}
}

func TestAPIKeyCredentialMetaCapturesUpstreamFields(t *testing.T) {
	t.Parallel()

	meta := apiKeyCredentialMeta(adapter.APIKey{
		ID:         42,
		ExternalID: " upstream-key ",
		Name:       "Imported Key",
		Status:     "active",
		Key:        " ",
		MaskedKey:  " sk-...123 ",
		Raw: map[string]any{
			"remain_quota":         float64(12),
			"used_quota":           float64(3),
			"unlimited_quota":      false,
			"model_limits_enabled": true,
			"model_limits":         []any{"gpt-5"},
			"expired_time":         "2026-07-01",
			"group":                "default",
			"ignored":              "not copied",
		},
	})

	if meta["name"] != "Imported Key" || meta["upstream_id"] != 42 || meta["status"] != "active" {
		t.Fatalf("unexpected identity meta: %#v", meta)
	}
	if meta["upstream_key_id"] != "upstream-key" || meta["upstream_masked_key"] != "sk-...123" {
		t.Fatalf("unexpected upstream key meta: %#v", meta)
	}
	if meta["raw_key_missing"] != true {
		t.Fatalf("raw_key_missing = %#v, want true", meta["raw_key_missing"])
	}
	if meta["remain_quota"] != float64(12) || meta["used_quota"] != float64(3) || meta["group"] != "default" {
		t.Fatalf("quota fields were not copied from raw payload: %#v", meta)
	}
	if _, ok := meta["ignored"]; ok {
		t.Fatalf("unexpected raw field copied: %#v", meta)
	}

	withRawKey := apiKeyCredentialMeta(adapter.APIKey{Key: "sk-raw"})
	if withRawKey["raw_key_missing"] != false {
		t.Fatalf("raw_key_missing with raw key = %#v, want false", withRawKey["raw_key_missing"])
	}
	if _, ok := withRawKey["upstream_key_id"]; ok {
		t.Fatalf("blank external id should be omitted: %#v", withRawKey)
	}
	if _, ok := withRawKey["upstream_masked_key"]; ok {
		t.Fatalf("blank masked key should be omitted: %#v", withRawKey)
	}
}

func TestAPIKeyCredentialTypeForRefreshUsesStableFallbacks(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		key  adapter.APIKey
		want string
	}{
		{name: "external id", key: adapter.APIKey{ExternalID: " upstream-id "}, want: "api_key:upstream-id"},
		{name: "numeric id", key: adapter.APIKey{ID: 17}, want: "api_key:17"},
		{name: "default", key: adapter.APIKey{}, want: "api_key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := apiKeyCredentialTypeForRefresh(tc.key); got != tc.want {
				t.Fatalf("apiKeyCredentialTypeForRefresh() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestOAuthConnectionQuotaAuthMessageFiltersQuotaStates(t *testing.T) {
	t.Parallel()

	if got := oauthConnectionQuotaAuthMessage("codex", map[string]any{
		"quota": map[string]any{"available": true, "error": "invalid_grant"},
	}); got != "" {
		t.Fatalf("available quota message = %q, want empty", got)
	}

	if got := oauthConnectionQuotaAuthMessage("openai", map[string]any{
		"quota": map[string]any{"available": false, "message": "invalid_grant"},
	}); got != "" {
		t.Fatalf("non-oauth provider quota message = %q, want empty", got)
	}

	if got := oauthConnectionQuotaAuthMessage("codex", map[string]any{
		"quota": map[string]any{"available": false, "detail": "refresh_token_reused"},
	}); got != "refresh_token_reused" {
		t.Fatalf("codex quota auth message = %q, want refresh_token_reused", got)
	}

	if got := oauthConnectionQuotaAuthMessage("codex", map[string]any{
		"quota": map[string]any{
			"available": false,
			"message":   `codex upstream returned 403: <html><head><meta http-equiv="refresh" content="360"></head></html>`,
		},
	}); got != "" {
		t.Fatalf("transient html refresh message = %q, want empty", got)
	}
}

func TestPricingKeyUsesUnitSeparator(t *testing.T) {
	t.Parallel()

	want := "gpt-test" + string(rune(31)) + "default"
	if got := pricingKey("gpt-test", "default"); got != want {
		t.Fatalf("pricingKey() = %q, want %q", got, want)
	}
}
