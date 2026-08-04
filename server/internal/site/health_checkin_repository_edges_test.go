package site

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/adapter"
	"xlyra/server/internal/store"
	"xlyra/server/internal/upstream"
)

type siteHealthCredentialValidator struct {
	calls *[]string
}

type siteHealthLimitedValidator struct {
	err error
}

func (siteHealthLimitedValidator) SiteTypes() []string {
	return []string{"health-api-key-limited"}
}

func (v siteHealthLimitedValidator) ValidateCredentials(context.Context, adapter.SiteConfig, string) error {
	return v.err
}

func (siteHealthCredentialValidator) SiteTypes() []string {
	return []string{"health-api-key"}
}

func (v siteHealthCredentialValidator) ValidateCredentials(_ context.Context, _ adapter.SiteConfig, apiKey string) error {
	*v.calls = append(*v.calls, apiKey)
	if apiKey == "high-priority" {
		return errors.New("high-priority rejected")
	}
	return nil
}

func TestSiteHealthMethodsReturnSiteLookupErrors(t *testing.T) {
	t.Parallel()

	queryErr := errors.New("site lookup unavailable")
	service := siteServiceWithQueryError(t, queryErr)
	siteID := uuid.New()

	_, err := service.CheckSiteHealth(t.Context(), siteID, " scheduler ")
	assertSiteQueryError(t, "CheckSiteHealth", err, queryErr)
	_, err = service.SiteHealth(t.Context(), siteID)
	assertSiteQueryError(t, "SiteHealth", err, queryErr)
}

func TestRunNewAPICheckinsReturnsListError(t *testing.T) {
	t.Parallel()

	queryErr := errors.New("list sites unavailable")
	service := siteServiceWithQueryError(t, queryErr)

	items, err := service.RunNewAPICheckins(t.Context())
	if items != nil {
		t.Fatalf("RunNewAPICheckins items = %#v, want nil", items)
	}
	assertSiteQueryError(t, "RunNewAPICheckins", err, queryErr)
}

func TestValidateAndSyncModelsRejectUnsupportedSiteType(t *testing.T) {
	t.Parallel()

	service := siteServiceReturningSite(t, store.Site{
		ID:       uuid.New(),
		Name:     "Unsupported",
		Slug:     "unsupported",
		SiteType: "unsupported_site_type",
		BaseURL:  "https://example.invalid",
		Status:   "active",
		Enabled:  true,
	})

	if err := service.Validate(t.Context(), uuid.New()); err == nil || !strings.Contains(err.Error(), `unsupported site_type "unsupported_site_type"`) {
		t.Fatalf("Validate error = %v, want unsupported site_type", err)
	}
	if _, err := service.SyncModels(t.Context(), uuid.New()); err == nil || !strings.Contains(err.Error(), `unsupported site_type "unsupported_site_type"`) {
		t.Fatalf("SyncModels error = %v, want unsupported site_type", err)
	}
}

func TestRunHealthCheckReportsMissingAPIKeyCredential(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	credentialErr := errors.New("credential lookup unavailable")
	service := siteServiceReturningSiteWithCredentialError(t, store.Site{
		ID:       siteID,
		Name:     "OpenAI",
		Slug:     "openai",
		SiteType: "openai",
		BaseURL:  "https://api.example.invalid",
		Status:   "active",
		Enabled:  true,
	}, credentialErr)

	check := service.runSiteHealthCheck(t.Context(), store.Site{
		ID:       siteID,
		SiteType: "openai",
		BaseURL:  "https://api.example.invalid",
	})

	if check.success {
		t.Fatal("health check should fail when api key credential lookup fails")
	}
	if check.endpoint != "GET /v1/models" || check.errorType != "missing_credential" {
		t.Fatalf("health check endpoint/error = %q/%q, want api key missing credential", check.endpoint, check.errorType)
	}
	if !strings.Contains(check.message, credentialErr.Error()) {
		t.Fatalf("health check message = %q, want credential lookup error", check.message)
	}
}

func TestRunHealthCheckFallsBackAcrossAPIKeysByRoutingPriority(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	highID := uuid.New()
	lowID := uuid.New()
	high := siteEncryptedCredential(t, highID, siteID, defaultCredentialType+":high", "high-priority", nil)
	high.DisplayName = "Primary"
	high.RoutingPriority = 5
	low := siteEncryptedCredential(t, lowID, siteID, defaultCredentialType+":low", "low-priority", nil)
	low.DisplayName = "Backup"
	low.RoutingPriority = 2
	service := siteServiceWithCallbacks(t, siteGormCallbacks{
		query: func(tx *gorm.DB) {
			switch dest := tx.Statement.Dest.(type) {
			case *[]store.SiteCredential:
				*dest = []store.SiteCredential{low, high}
				tx.RowsAffected = 2
			case *[]store.SiteAPIKeyState:
				*dest = []store.SiteAPIKeyState{
					{SiteID: siteID, SiteCredentialID: highID, Enabled: true},
					{SiteID: siteID, SiteCredentialID: lowID, Enabled: true},
				}
				tx.RowsAffected = 2
			default:
				tx.AddError(gorm.ErrInvalidData)
			}
		},
	})
	calls := []string{}
	service.adapters.Register(siteHealthCredentialValidator{calls: &calls})

	check := service.runSiteHealthCheck(t.Context(), store.Site{ID: siteID, SiteType: "health-api-key", BaseURL: "https://example.invalid"})
	if !check.success || len(calls) != 2 || calls[0] != "high-priority" || calls[1] != "low-priority" {
		t.Fatalf("health check = %#v calls = %#v", check, calls)
	}
	credential, ok := check.metadata["credential"].(map[string]any)
	if !ok || credential["id"] != lowID.String() || credential["name"] != "Backup" {
		t.Fatalf("selected credential metadata = %#v", check.metadata["credential"])
	}
	attempts, ok := check.metadata["credential_attempts"].([]map[string]any)
	if !ok || len(attempts) != 2 || attempts[0]["status"] != "failed" || attempts[1]["status"] != "ok" {
		t.Fatalf("credential attempts metadata = %#v", check.metadata["credential_attempts"])
	}
}

func TestRunHealthCheckTreatsCredentialQuotaAsReachable(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	credentialID := uuid.New()
	credential := siteEncryptedCredential(t, credentialID, siteID, defaultCredentialType+":limited", "limited-key", nil)
	service := siteServiceWithCallbacks(t, siteGormCallbacks{
		query: func(tx *gorm.DB) {
			switch dest := tx.Statement.Dest.(type) {
			case *[]store.SiteCredential:
				*dest = []store.SiteCredential{credential}
				tx.RowsAffected = 1
			case *[]store.SiteAPIKeyState:
				*dest = []store.SiteAPIKeyState{{SiteID: siteID, SiteCredentialID: credentialID, Enabled: true}}
				tx.RowsAffected = 1
			default:
				tx.AddError(gorm.ErrInvalidData)
			}
		},
	})
	service.adapters.Register(siteHealthLimitedValidator{err: upstream.NewHTTPError(
		"upstream returned",
		http.StatusUnauthorized,
		nil,
		[]byte(`{"error":{"code":"api_key_weekly_quota_exhausted","scope":"weekly"}}`),
	)})

	check := service.runSiteHealthCheck(t.Context(), store.Site{ID: siteID, SiteType: "health-api-key-limited", BaseURL: "https://example.invalid"})
	if !check.success || check.errorType != "" || check.message != "ok" {
		t.Fatalf("limited health check = %#v, want reachable site", check)
	}
	attempts, ok := check.metadata["credential_attempts"].([]map[string]any)
	if !ok || len(attempts) != 1 || attempts[0]["status"] != "limited" {
		t.Fatalf("limited health attempts = %#v", check.metadata["credential_attempts"])
	}
}

func TestSetCredentialRejectsBlankSecretBeforeRepositoryAccess(t *testing.T) {
	t.Parallel()

	service := siteServiceWithUninitializedStore()

	credential, err := service.SetCredential(t.Context(), uuid.New(), CredentialInput{
		Type:   defaultCredentialType,
		Secret: " \t\n ",
	})
	if credential.ID != uuid.Nil || err == nil || !strings.Contains(err.Error(), "secret must not be empty") {
		t.Fatalf("SetCredential = %#v, %v, want blank secret validation", credential, err)
	}
}

func TestCleanupExpiredHealthDataRequiresInitializedStore(t *testing.T) {
	t.Parallel()

	service := siteServiceWithUninitializedStore()

	err := service.CleanupExpiredHealthData(t.Context(), time.Time{})
	if err == nil || !strings.Contains(err.Error(), "store is not initialized") {
		t.Fatalf("CleanupExpiredHealthData error = %v, want initialized store guard", err)
	}
}
