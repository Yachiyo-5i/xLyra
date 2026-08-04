package site

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/adapter"
	"xlyra/server/internal/store"
)

type credentialHealthProbeModule struct {
	calls *[]string
}

func (credentialHealthProbeModule) SiteTypes() []string {
	return []string{"health-credential-probe"}
}

func (credentialHealthProbeModule) Scope() adapter.HealthProbeScope {
	return adapter.HealthProbeCredentialScope
}

func (m credentialHealthProbeModule) ProbeHealth(_ context.Context, _ adapter.SiteConfig, credential string) ([]adapter.Model, error) {
	*m.calls = append(*m.calls, credential)
	return []adapter.Model{{UpstreamName: "probe-model"}}, nil
}

func TestRunSiteHealthCheckUsesOpenCodeHealthProbe(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("health probe path = %q, want /v1/models", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"gpt-5.6-luna"}]}`))
	}))
	t.Cleanup(server.Close)

	check := siteServiceWithoutStore().runSiteHealthCheck(t.Context(), store.Site{
		ID:       uuid.New(),
		SiteType: "opencode_go",
		BaseURL:  server.URL,
	})
	if !check.success || check.errorType != "" {
		t.Fatalf("health check = %#v, want successful probe", check)
	}
	if check.endpoint != "GET /v1/models" || check.metadata["model_count"] != 1 {
		t.Fatalf("health check endpoint/metadata = %q/%#v", check.endpoint, check.metadata)
	}
}

func TestRunSiteHealthCheckRejectsEmptyHealthProbeModelList(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[]}`))
	}))
	t.Cleanup(server.Close)

	check := siteServiceWithoutStore().runSiteHealthCheck(t.Context(), store.Site{
		ID:       uuid.New(),
		SiteType: "opencode_go",
		BaseURL:  server.URL,
	})
	if check.success || check.errorType != "health_probe_empty_models" {
		t.Fatalf("health check = %#v, want empty-model probe failure", check)
	}
}

func TestRunSiteHealthCheckPassesCredentialToCredentialHealthProbe(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	credential := siteEncryptedCredential(t, uuid.New(), siteID, defaultCredentialType, "probe-secret", nil)
	calls := []string{}
	service := siteServiceWithQueryCallback(t, func(tx *gorm.DB) {
		switch destination := tx.Statement.Dest.(type) {
		case *[]store.SiteCredential:
			*destination = []store.SiteCredential{credential}
			tx.RowsAffected = 1
		default:
			tx.AddError(gorm.ErrInvalidData)
		}
	})
	service.adapters.Register(credentialHealthProbeModule{calls: &calls})

	check := service.runSiteHealthCheck(t.Context(), store.Site{
		ID:       siteID,
		SiteType: "health-credential-probe",
		BaseURL:  "https://example.invalid",
	})
	if !check.success || check.errorType != "" {
		t.Fatalf("health check = %#v, want successful credential probe", check)
	}
	if len(calls) != 1 || calls[0] != "probe-secret" {
		t.Fatalf("credential probe calls = %#v, want decrypted credential", calls)
	}
}
