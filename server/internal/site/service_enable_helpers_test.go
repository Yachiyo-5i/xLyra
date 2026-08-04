package site

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/google/uuid"

	"xlyra/server/internal/httpclient"
	"xlyra/server/internal/store"
)

func TestEnableBlockedReasonHelpersCoverPureBranches(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		status string
		want   string
	}{
		{name: "failed status trims reason", status: " Failed ", want: "site status is Failed"},
		{name: "error status blocks", status: "error", want: "site status is error"},
		{name: "unhealthy status blocks", status: "unhealthy", want: "site status is unhealthy"},
		{name: "invalid status blocks", status: "invalid", want: "site status is invalid"},
		{name: "unavailable status blocks", status: "unavailable", want: "site status is unavailable"},
		{name: "blank status allows", status: " ", want: ""},
		{name: "active status allows", status: "active", want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := siteStatusEnableBlockedReason(tc.status); got != tc.want {
				t.Fatalf("siteStatusEnableBlockedReason(%q) = %q, want %q", tc.status, got, tc.want)
			}
		})
	}

	for _, tc := range []struct {
		name  string
		state store.SiteState
		want  string
	}{
		{
			name: "failed validation blocks before sync status",
			state: store.SiteState{
				ValidationOK: sql.NullBool{Bool: false, Valid: true},
				SyncStatus:   "failed",
			},
			want: "site validation failed",
		},
		{
			name: "failed sync blocks",
			state: store.SiteState{
				ValidationOK: sql.NullBool{Bool: true, Valid: true},
				SyncStatus:   " Failed ",
			},
			want: "site sync failed",
		},
		{
			name: "unknown validation with blank sync allows",
			state: store.SiteState{
				ValidationOK: sql.NullBool{},
				SyncStatus:   " ",
			},
			want: "",
		},
		{
			name: "valid partial sync allows",
			state: store.SiteState{
				ValidationOK: sql.NullBool{Bool: true, Valid: true},
				SyncStatus:   "partial",
			},
			want: "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := siteStateEnableBlockedReason(tc.state); got != tc.want {
				t.Fatalf("siteStateEnableBlockedReason() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestProxyIDFromMetaCoversEmptyInvalidAndTrimmedBranches(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		raw  store.JSON
		want string
	}{
		{name: "nil meta", raw: nil, want: ""},
		{name: "empty meta", raw: store.JSON{}, want: ""},
		{name: "invalid json", raw: store.JSON(`not-json`), want: ""},
		{name: "missing proxy id", raw: store.JSON(`{"gateway":{}}`), want: ""},
		{name: "null proxy id", raw: store.JSON(`{"proxy_id":null}`), want: ""},
		{name: "blank proxy id", raw: store.JSON(`{"proxy_id":" \t "}`), want: ""},
		{name: "non string proxy id", raw: store.JSON(`{"proxy_id":123}`), want: ""},
		{name: "trimmed proxy id", raw: store.JSON(`{"proxy_id":" proxy-a "}`), want: "proxy-a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := proxyIDFromMeta(tc.raw); got != tc.want {
				t.Fatalf("proxyIDFromMeta(%s) = %q, want %q", string(tc.raw), got, tc.want)
			}
		})
	}
}

func TestHTTPClientProfileFromGatewayConfigDefaultsAndEmptyConfig(t *testing.T) {
	t.Parallel()

	want := httpclient.DefaultProfile()
	want.ProxyID = "proxy-main"
	if got := httpclientProfileFromGatewayConfig(nil, "proxy-main"); got != want {
		t.Fatalf("nil gateway config profile = %#v, want %#v", got, want)
	}

	if got, want := httpclientProfileFromGatewayConfig(&GatewayConfig{}, ""), httpclient.DefaultProfile(); got != want {
		t.Fatalf("empty gateway config profile = %#v, want %#v", got, want)
	}
}

func TestValidateProxyIDCoversNilBlankAndMissingConfigBranches(t *testing.T) {
	t.Parallel()

	service := siteServiceWithoutStore()
	if err := service.validateProxyID(nil); err != nil {
		t.Fatalf("nil proxy id error = %v, want nil", err)
	}

	blank := " \t\n "
	if err := service.validateProxyID(&blank); err != nil {
		t.Fatalf("blank proxy id error = %v, want nil", err)
	}

	missing := " missing-proxy "
	err := service.validateProxyID(&missing)
	if err == nil {
		t.Fatal("missing proxy id with nil config should return an error")
	}
	if !strings.Contains(err.Error(), `proxy_id "missing-proxy" was not found`) {
		t.Fatalf("missing proxy id error = %q, want trimmed not found error", err.Error())
	}
}

func TestAPIKeyCredentialFromStoreAppliesCredentialMetaBranches(t *testing.T) {
	t.Parallel()

	service := siteServiceWithoutStore()
	encryptedSecret, maskedSecret, err := service.credentials.Encrypt(" sk-live-secret ")
	if err != nil {
		t.Fatalf("encrypt fixture secret: %v", err)
	}

	for _, tc := range []struct {
		name              string
		meta              store.JSON
		wantSecret        string
		wantName          string
		wantUpstreamID    int
		wantEnabled       bool
		wantMaskedSecret  string
		wantSecretMissing bool
	}{
		{
			name: "raw key missing uses upstream masked key and hides decrypted secret",
			meta: store.JSON(`{
				"name":" Imported Key ",
				"upstream_id":7,
				"enabled":false,
				"raw_key_missing":true,
				"upstream_masked_key":" upstream-mask "
			}`),
			wantSecret:        "",
			wantName:          "Imported Key",
			wantUpstreamID:    7,
			wantEnabled:       false,
			wantMaskedSecret:  "upstream-mask",
			wantSecretMissing: true,
		},
		{
			name:              "invalid meta falls back to decrypted secret defaults",
			meta:              store.JSON(`not-json`),
			wantSecret:        "sk-live-secret",
			wantName:          "",
			wantUpstreamID:    0,
			wantEnabled:       true,
			wantMaskedSecret:  maskedSecret,
			wantSecretMissing: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			credential := store.SiteCredential{
				ID:              uuid.New(),
				SiteID:          uuid.New(),
				CredentialType:  defaultCredentialType + ":imported",
				EncryptedSecret: encryptedSecret,
				MaskedSecret:    maskedSecret,
				Meta:            tc.meta,
			}

			got, err := service.apiKeyCredentialFromStore(credential)
			if err != nil {
				t.Fatalf("apiKeyCredentialFromStore() error = %v", err)
			}
			if got.Credential.ID != credential.ID {
				t.Fatalf("credential id = %s, want %s", got.Credential.ID, credential.ID)
			}
			if got.Secret != tc.wantSecret || got.Name != tc.wantName || got.UpstreamID != tc.wantUpstreamID {
				t.Fatalf("credential identity fields = secret %q name %q upstream %d, want %q %q %d", got.Secret, got.Name, got.UpstreamID, tc.wantSecret, tc.wantName, tc.wantUpstreamID)
			}
			if got.Enabled != tc.wantEnabled || got.SecretMissing != tc.wantSecretMissing {
				t.Fatalf("credential flags = enabled %v missing %v, want %v %v", got.Enabled, got.SecretMissing, tc.wantEnabled, tc.wantSecretMissing)
			}
			if got.MaskedSecret != tc.wantMaskedSecret {
				t.Fatalf("masked secret = %q, want %q", got.MaskedSecret, tc.wantMaskedSecret)
			}
		})
	}
}
