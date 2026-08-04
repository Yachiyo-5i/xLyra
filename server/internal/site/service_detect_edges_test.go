package site

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"xlyra/server/internal/store"
)

func TestDetectSiteTypeRequiresBaseURLBeforeAdapterDetection(t *testing.T) {
	t.Parallel()

	service := siteServiceWithoutStore()
	_, err := service.DetectSiteType(context.Background(), " \t\n ")
	if err == nil || !strings.Contains(err.Error(), "base_url is required") {
		t.Fatalf("DetectSiteType blank baseURL error = %v, want base_url is required", err)
	}
}

func TestDetectSiteTypeMatchesNewAPIStatusEndpoint(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/status" {
			t.Fatalf("path = %q, want /api/status", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"success": true,
			"data": {
				"version": "v0.8.0",
				"quota_per_unit": 500000
			}
		}`))
	}))
	t.Cleanup(server.Close)

	result, err := siteServiceWithoutStore().DetectSiteType(context.Background(), server.URL+"/")
	if err != nil {
		t.Fatalf("DetectSiteType returned error: %v", err)
	}
	if !result.Matched || result.SiteType != "newapi" {
		t.Fatalf("DetectSiteType result = %#v, want matched newapi", result)
	}
	if result.Features["version"] != "v0.8.0" {
		t.Fatalf("features = %#v, want version marker", result.Features)
	}
}

func TestDetectSiteTypeReturnsUnknownWhenDetectorsDoNotMatch(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":false,"data":{"name":"plain service"}}`))
	}))
	t.Cleanup(server.Close)

	result, err := siteServiceWithoutStore().DetectSiteType(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("DetectSiteType returned error: %v", err)
	}
	if result.Matched || result.SiteType != "unknown" || result.Confidence != 0 {
		t.Fatalf("DetectSiteType result = %#v, want unknown non-match", result)
	}
	if len(result.Features) != 0 {
		t.Fatalf("unknown features = %#v, want empty", result.Features)
	}
}

func TestRunSiteHealthCheckUnsupportedSiteTypeDoesNotNeedStore(t *testing.T) {
	t.Parallel()

	service := siteServiceWithoutStore()
	check := service.runSiteHealthCheck(context.Background(), store.Site{
		ID:       uuid.New(),
		SiteType: "unknown-provider",
		BaseURL:  "https://example.invalid",
	})

	if check.success {
		t.Fatal("unsupported site type health check should not succeed")
	}
	if check.endpoint != "detect" || check.method != http.MethodGet {
		t.Fatalf("health check endpoint/method = %q/%q, want detect/GET", check.endpoint, check.method)
	}
	if check.errorType != "unsupported_site_type" {
		t.Fatalf("errorType = %q, want unsupported_site_type", check.errorType)
	}
	if !strings.Contains(check.message, `unsupported site_type "unknown-provider"`) {
		t.Fatalf("message = %q, want unsupported site_type", check.message)
	}
	if check.metadata["site_type"] != "unknown-provider" {
		t.Fatalf("metadata = %#v, want site_type unknown-provider", check.metadata)
	}
}

func TestRunSiteHealthCheckNewAPIDetectorBranches(t *testing.T) {
	t.Parallel()

	service := siteServiceWithoutStore()
	cases := []struct {
		name      string
		status    int
		body      string
		wantOK    bool
		wantError string
	}{
		{
			name:   "matched",
			status: http.StatusOK,
			body: `{
				"success": true,
				"data": {"version":"v0.8.0","quota_display_type":"currency"}
			}`,
			wantOK: true,
		},
		{
			name:      "mismatch",
			status:    http.StatusOK,
			body:      `{"success":false,"data":{"version":"v0.8.0"}}`,
			wantError: "site_type_mismatch",
		},
		{
			name:      "upstream error",
			status:    http.StatusInternalServerError,
			body:      `{"error":"down"}`,
			wantError: "upstream_unreachable",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(server.Close)

			check := service.runSiteHealthCheck(context.Background(), store.Site{
				ID:       uuid.New(),
				SiteType: "newapi",
				BaseURL:  server.URL,
			})

			if check.success != tc.wantOK {
				t.Fatalf("success = %v, want %v; check=%#v", check.success, tc.wantOK, check)
			}
			if check.endpoint != "GET /api/status" {
				t.Fatalf("endpoint = %q, want GET /api/status", check.endpoint)
			}
			if tc.wantError != "" && check.errorType != tc.wantError {
				t.Fatalf("errorType = %q, want %q; message=%q", check.errorType, tc.wantError, check.message)
			}
			if tc.wantOK && (check.message != "ok" || check.errorType != "") {
				t.Fatalf("successful check message/error = %q/%q", check.message, check.errorType)
			}
			if !tc.wantOK && strings.TrimSpace(check.message) == "" {
				t.Fatalf("failed check should include message: %#v", check)
			}
		})
	}
}
