package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"xlyra/server/internal/site"
)

func TestResolveSiteCredentialsGrokAddsNoInlineAccounts(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sites", nil)
	credentials, ok := (Handler{}).resolveSiteCredentials(rec, req, siteUpsertRequest{SiteType: "grok"}, true)
	if !ok {
		t.Fatal("resolveSiteCredentials() ok = false, want true (grok accounts are added via the OAuth device flow)")
	}
	if len(credentials) != 0 {
		t.Fatalf("credentials len = %d, want 0", len(credentials))
	}
}

func TestListSiteTypesIncludesGrokDefault(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	(Handler{}).ListSiteTypes(rec, httptest.NewRequest(http.MethodGet, "/api/v1/site-types", nil))
	adminAssertStatus(t, rec, http.StatusOK)
	body := adminDecodeJSON[struct {
		Items []siteTypeInfo `json:"items"`
	}](t, rec)
	for _, item := range body.Items {
		if item.SiteType == "grok" {
			if item.CredentialType != "grok_oauth" || item.OfficialBaseURL != "https://cli-chat-proxy.grok.com" || !item.ShowInCreateDialog {
				t.Fatalf("grok site type = %#v", item)
			}
			return
		}
	}
	t.Fatal("grok site type was not listed")
}

func TestListSiteTypesOrdersOpenCodeGoAfterGrok(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	(Handler{}).ListSiteTypes(rec, httptest.NewRequest(http.MethodGet, "/api/v1/site-types", nil))
	adminAssertStatus(t, rec, http.StatusOK)
	body := adminDecodeJSON[struct {
		Items []siteTypeInfo `json:"items"`
	}](t, rec)
	positions := map[string]int{}
	for index, item := range body.Items {
		positions[item.SiteType] = index
	}
	if positions["google_gemini"]+1 != positions["grok"] || positions["grok"]+1 != positions["opencode_go"] || positions["opencode_go"]+1 != positions["deepseek"] {
		t.Fatalf("site type positions = %#v, want google_gemini, grok, opencode_go, deepseek adjacency", positions)
	}
}

func TestGrokAccountResponseContainsMaskWithoutToken(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(site.GrokAccount{MaskedToken: "abcd...wxyz"})
	if err != nil {
		t.Fatalf("marshal account: %v", err)
	}
	if !strings.Contains(string(encoded), "masked_token") || strings.Contains(string(encoded), `"token"`) {
		t.Fatalf("account response = %s", encoded)
	}
}
