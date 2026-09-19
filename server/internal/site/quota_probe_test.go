package site

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/store"
)

func TestNormalizeQuotaProbeType(t *testing.T) {
	for input, expected := range map[string]string{
		"":        "",
		"none":    "",
		"sub2api": QuotaProbeTypeSub2API,
		"newapi":  QuotaProbeTypeNewAPI,
		"xlyra":   QuotaProbeTypeXLyra,
		"kimi":    QuotaProbeTypeKimi,
		"glm":     QuotaProbeTypeGLM,
	} {
		value, err := NormalizeQuotaProbeType(input)
		if err != nil || value != expected {
			t.Fatalf("NormalizeQuotaProbeType(%q) = %q, %v; expected %q", input, value, err, expected)
		}
	}
	if _, err := NormalizeQuotaProbeType("oneapi"); err == nil {
		t.Fatal("expected error for unsupported probe type")
	}
}

func TestProbeSub2APIQuotaLimited(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/usage" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer sk-test" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"mode": "quota_limited",
			"quota": {"limit": 20, "used": 7.5, "remaining": 12.5, "unit": "USD"},
			"remaining": 12.5,
			"rate_limits": [{"window": "5h", "limit": 100, "used": 3, "remaining": 97}]
		}`))
	}))
	defer server.Close()

	result := probeQuota(context.Background(), server.Client(), QuotaProbeTypeSub2API, server.URL, "sk-test")
	if result.Status != "ok" {
		t.Fatalf("expected ok, got %+v", result)
	}
	if result.Kind != "mixed" || len(result.Entries) != 2 {
		t.Fatalf("expected mixed kind with 2 entries, got %+v", result)
	}
	balance := result.Entries[0]
	if balance.Label != "balance" || balance.Unit != "usd" || balance.Remaining == nil || *balance.Remaining != 12.5 {
		t.Fatalf("unexpected balance entry %+v", balance)
	}
	window := result.Entries[1]
	if window.Label != "5h" || window.Unit != "requests" || *window.Remaining != 97 {
		t.Fatalf("unexpected window entry %+v", window)
	}
}

func TestProbeSub2APIUnrestrictedSubscription(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"mode": "unrestricted",
			"remaining": 4.2,
			"subscription": {
				"daily_usage_usd": 0.8, "daily_limit_usd": 5,
				"weekly_usage_usd": 3.1, "weekly_limit_usd": 20,
				"monthly_usage_usd": 3.1, "monthly_limit_usd": 0
			}
		}`))
	}))
	defer server.Close()

	result := probeQuota(context.Background(), server.Client(), QuotaProbeTypeSub2API, server.URL, "sk-test")
	if result.Status != "ok" || len(result.Entries) != 3 {
		t.Fatalf("expected balance + daily + weekly entries, got %+v", result)
	}
	daily := result.Entries[1]
	if daily.Label != "daily" || *daily.Remaining != 4.2 {
		t.Fatalf("unexpected daily entry %+v", daily)
	}
}

func TestProbeSub2APIPlanSubscription(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/usage" || r.Header.Get("Authorization") != "Bearer plan-key" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"isValid": true,
			"mode": "unrestricted",
			"planName": "待宵计划",
			"subscription": {
				"daily": {"percentage": 0, "resets_at": "2026-08-19T00:00:00+08:00"},
				"weekly": {"percentage": 25.5, "resets_at": "2026-08-25T17:12:24.203076+08:00"},
				"monthly": {"percentage": 100, "resets_at": "2026-09-18T17:12:24.203076+08:00"},
				"expires_at": "2026-09-17T17:12:24.203076+08:00"
			},
			"unit": "%"
		}`))
	}))
	defer server.Close()

	result := probeQuota(context.Background(), server.Client(), QuotaProbeTypeSub2API, server.URL, "plan-key")
	if result.Status != "ok" || result.Kind != "subscription_plan" || result.Plan != "待宵计划" {
		t.Fatalf("unexpected plan result %+v", result)
	}
	if result.ExpiresAt == nil || *result.ExpiresAt != "2026-09-17T17:12:24.203076+08:00" {
		t.Fatalf("unexpected expiry %+v", result.ExpiresAt)
	}
	if len(result.Entries) != 3 {
		t.Fatalf("entries = %+v, want daily, weekly, monthly", result.Entries)
	}
	daily, weekly, monthly := result.Entries[0], result.Entries[1], result.Entries[2]
	if daily.Label != "daily" || daily.Unit != "percent" || daily.Used == nil || *daily.Used != 0 || daily.Remaining == nil || *daily.Remaining != 100 || daily.ResetAt == nil {
		t.Fatalf("unexpected daily entry %+v", daily)
	}
	if weekly.Remaining == nil || *weekly.Remaining != 74.5 {
		t.Fatalf("unexpected weekly entry %+v", weekly)
	}
	if monthly.Remaining == nil || *monthly.Remaining != 0 {
		t.Fatalf("unexpected monthly entry %+v", monthly)
	}
}

func TestProbeSub2APIRejectsInvalidKeyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"isValid": false,
			"mode": "quota_limited",
			"quota": {"limit": 20, "used": 5, "remaining": 15, "unit": "USD"}
		}`))
	}))
	defer server.Close()

	result := probeQuota(context.Background(), server.Client(), QuotaProbeTypeSub2API, server.URL, "disabled-key")
	if result.Status != "error" || result.Error != "usage endpoint reported invalid API key" || len(result.Entries) != 0 {
		t.Fatalf("invalid key result = %+v, want probe error", result)
	}
}

func TestProbeNewAPITokenUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/usage/token" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code": true, "data": {"object": "token_usage", "total_granted": 25000000, "total_used": 5000000, "total_available": 20000000, "unlimited_quota": false}}`))
	}))
	defer server.Close()

	result := probeQuota(context.Background(), server.Client(), QuotaProbeTypeNewAPI, server.URL, "sk-test")
	if result.Status != "ok" || len(result.Entries) != 1 {
		t.Fatalf("expected token usage entry, got %+v", result)
	}
	entry := result.Entries[0]
	if *entry.Remaining != 40 || *entry.Limit != 50 || *entry.Used != 10 {
		t.Fatalf("expected quota converted by 500000, got %+v", entry)
	}
}

func TestProbeNewAPITokenUsageUnlimited(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/usage/token" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code": true, "data": {"total_used": 1000000, "unlimited_quota": true}}`))
	}))
	defer server.Close()

	result := probeQuota(context.Background(), server.Client(), QuotaProbeTypeNewAPI, server.URL, "sk-test")
	if result.Status != "ok" || result.Kind != "unlimited" || !result.Entries[0].Unlimited {
		t.Fatalf("expected unlimited result, got %+v", result)
	}
}

func TestProbeNewAPIQuota(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/dashboard/billing/subscription":
			_, _ = w.Write([]byte(`{"hard_limit_usd": 50}`))
		case "/v1/dashboard/billing/usage":
			if r.URL.Query().Get("start_date") == "" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{"total_usage": 1234}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	result := probeQuota(context.Background(), server.Client(), QuotaProbeTypeNewAPI, server.URL, "sk-test")
	if result.Status != "ok" || len(result.Entries) != 1 {
		t.Fatalf("expected single balance entry, got %+v", result)
	}
	entry := result.Entries[0]
	if *entry.Limit != 50 || *entry.Used != 12.34 || *entry.Remaining != 37.66 {
		t.Fatalf("unexpected entry %+v", entry)
	}
}

func TestProbeNewAPIQuotaFallbackPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/dashboard/billing/subscription":
			_, _ = w.Write([]byte(`{"hard_limit_usd": 10}`))
		case "/dashboard/billing/usage":
			_, _ = w.Write([]byte(`{"total_usage": 100}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	result := probeQuota(context.Background(), server.Client(), QuotaProbeTypeNewAPI, server.URL, "sk-test")
	if result.Status != "ok" {
		t.Fatalf("expected fallback path to succeed, got %+v", result)
	}
	if *result.Entries[0].Remaining != 9 {
		t.Fatalf("unexpected remaining %+v", result.Entries[0])
	}
}

func TestProbeXLyraQuota(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/user/balance" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"is_active": true, "balance": 8.5, "unit": "USD", "quota_limit": 10, "quota_used": 21.5, "quota_total_used": 1.5, "quota_unlimited": false}`))
	}))
	defer server.Close()

	result := probeQuota(context.Background(), server.Client(), QuotaProbeTypeXLyra, server.URL, "xlyra-test")
	if result.Status != "ok" || result.Kind != "balance" {
		t.Fatalf("expected balance result, got %+v", result)
	}
	entry := result.Entries[0]
	if *entry.Remaining != 8.5 || *entry.Limit != 10 || *entry.Used != 1.5 {
		t.Fatalf("unexpected entry %+v", entry)
	}
}

func TestProbeXLyraQuotaUnlimited(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"is_active": true, "balance": null, "unit": "USD", "quota_limit": null, "quota_used": 3.2, "quota_total_used": 0, "quota_unlimited": true}`))
	}))
	defer server.Close()

	result := probeQuota(context.Background(), server.Client(), QuotaProbeTypeXLyra, server.URL, "xlyra-test")
	if result.Status != "ok" || result.Kind != "unlimited" {
		t.Fatalf("expected unlimited result, got %+v", result)
	}
	if !result.Entries[0].Unlimited {
		t.Fatalf("expected unlimited entry, got %+v", result.Entries[0])
	}
	if result.Entries[0].Used == nil || *result.Entries[0].Used != 3.2 {
		t.Fatalf("expected cumulative usage for unlimited entry, got %+v", result.Entries[0])
	}
}

func TestProbeQuotaUpstreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid key"}`))
	}))
	defer server.Close()

	result := probeQuota(context.Background(), server.Client(), QuotaProbeTypeSub2API, server.URL, "bad")
	if result.Status != "error" || result.Error == "" {
		t.Fatalf("expected error result, got %+v", result)
	}
}

func TestQuotaProbePrimaryEntry(t *testing.T) {
	remaining := 3.5
	limit := 10.0
	used := 6.5
	windowRemaining := 42.0
	result := QuotaProbeResult{Entries: []QuotaProbeEntry{
		{Label: "5h", Unit: "requests", Remaining: &windowRemaining},
		{Label: "balance", Unit: "usd", Remaining: &remaining, Limit: &limit, Used: &used},
	}}
	entry, ok := quotaProbePrimaryEntry(result)
	if !ok || entry.Label != "balance" || *entry.Remaining != 3.5 || entry.Unit != "usd" {
		t.Fatalf("expected balance entry preferred, got %+v %v", entry, ok)
	}
	if entry.Limit == nil || *entry.Limit != 10 || entry.Used == nil || *entry.Used != 6.5 {
		t.Fatalf("expected limit and used carried through, got %+v", entry)
	}

	// 无 balance 时选剩余最小（最紧张）的窗口
	looser := 42.0
	tighter := 10.0
	windows := QuotaProbeResult{Entries: []QuotaProbeEntry{
		{Label: "five_hour", Unit: "percent", Remaining: &looser},
		{Label: "weekly", Unit: "percent", Remaining: &tighter},
	}}
	primary, ok := quotaProbePrimaryEntry(windows)
	if !ok || primary.Label != "weekly" || *primary.Remaining != 10.0 {
		t.Fatalf("expected tightest window selected, got %+v %v", primary, ok)
	}

	unlimited := QuotaProbeResult{Entries: []QuotaProbeEntry{{Label: "balance", Unlimited: true}}}
	if _, ok := quotaProbePrimaryEntry(unlimited); ok {
		t.Fatal("unlimited entries must not produce a primary entry")
	}
}

func TestQuotaProbeSummaryEntryKeepsSub2APIPlanOutOfAccountBalance(t *testing.T) {
	accountRemaining := 42.0
	planRemaining := 75.0
	plan := QuotaProbeResult{Entries: []QuotaProbeEntry{{Label: "daily", Unit: "percent", Remaining: &planRemaining}}}
	if _, ok := quotaProbeSummaryEntry(QuotaProbeTypeSub2API, plan); ok {
		t.Fatal("sub2api plan window should not become the account balance")
	}

	withBalance := QuotaProbeResult{Entries: []QuotaProbeEntry{
		{Label: "daily", Unit: "percent", Remaining: &planRemaining},
		{Label: "balance", Unit: "usd", Remaining: &accountRemaining},
	}}
	entry, ok := quotaProbeSummaryEntry(QuotaProbeTypeSub2API, withBalance)
	if !ok || entry.Label != "balance" || entry.Remaining == nil || *entry.Remaining != accountRemaining {
		t.Fatalf("summary entry = %+v, %v, want account balance", entry, ok)
	}

	entry, ok = quotaProbeSummaryEntry(QuotaProbeTypeKimi, plan)
	if !ok || entry.Label != "daily" {
		t.Fatalf("non-sub2api summary entry = %+v, %v, want existing window behavior", entry, ok)
	}
}

func TestPreserveQuotaProbeResultKeepsLastKnownEntries(t *testing.T) {
	t.Parallel()

	previous := store.JSON(`{"quota_probe":{"status":"ok","kind":"balance","entries":[{"label":"balance","unit":"usd","remaining":42.5,"limit":100}],"fetched_at":"2026-07-17T10:00:00Z"}}`)
	failed := QuotaProbeResult{Status: "error", Error: "HTTP 503: upstream down", FetchedAt: time.Now().UTC()}

	preserved := preserveQuotaProbeResult(previous, failed)
	if preserved.Status != "error" || preserved.Error != "HTTP 503: upstream down" {
		t.Fatalf("preserved status = %s/%s, want error kept", preserved.Status, preserved.Error)
	}
	if len(preserved.Entries) != 1 || preserved.Entries[0].Remaining == nil || *preserved.Entries[0].Remaining != 42.5 {
		t.Fatalf("preserved entries = %#v, want last known balance kept", preserved.Entries)
	}
	if preserved.Kind != "balance" {
		t.Fatalf("preserved kind = %q, want balance", preserved.Kind)
	}
	if got := preserved.FetchedAt.Format(time.RFC3339); got != "2026-07-17T10:00:00Z" {
		t.Fatalf("preserved fetched_at = %s, want previous timestamp", got)
	}
}

func TestPreserveQuotaProbeResultSurvivesConsecutiveFailures(t *testing.T) {
	t.Parallel()

	previous := store.JSON(`{"quota_probe":{"status":"error","error":"HTTP 500","kind":"balance","entries":[{"label":"balance","unit":"usd","remaining":13}],"fetched_at":"2026-07-17T10:00:00Z"}}`)
	failed := QuotaProbeResult{Status: "error", Error: "timeout", FetchedAt: time.Now().UTC()}

	preserved := preserveQuotaProbeResult(previous, failed)
	if len(preserved.Entries) != 1 || preserved.Entries[0].Remaining == nil || *preserved.Entries[0].Remaining != 13 {
		t.Fatalf("preserved entries = %#v, want values kept across consecutive failures", preserved.Entries)
	}
}

func TestPreserveQuotaProbeResultWithoutHistoryReturnsFailure(t *testing.T) {
	t.Parallel()

	failed := QuotaProbeResult{Status: "error", Error: "timeout"}
	for _, meta := range []store.JSON{nil, store.JSON(`{}`), store.JSON(`{bad`), store.JSON(`{"quota_probe":{"status":"error","error":"old"}}`)} {
		preserved := preserveQuotaProbeResult(meta, failed)
		if len(preserved.Entries) != 0 || preserved.Status != "error" {
			t.Fatalf("preserved for %s = %#v, want plain failure", meta, preserved)
		}
	}
}

func TestCredentialQuotaProbeResetAt(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 20, 4, 3, 37, 0, time.UTC)
	want := time.Date(2026, 8, 21, 14, 29, 8, 997742000, time.UTC)
	valid := store.JSON(`{"quota_probe":{"status":"ok","entries":[{"label":"daily","remaining":100,"reset_at":"2026-08-21T00:00:00+08:00"},{"label":"weekly","remaining":0,"reset_at":"2026-08-21T22:29:08.997742+08:00"}],"fetched_at":"2026-08-20T00:00:14Z"}}`)
	if got, ok := CredentialQuotaProbeResetAt(valid, "weekly", now); !ok || !got.Equal(want) {
		t.Fatalf("CredentialQuotaProbeResetAt() = %s, %v, want %s, true", got, ok, want)
	}

	for _, test := range []struct {
		name string
		meta store.JSON
	}{
		{name: "failed probe", meta: store.JSON(`{"quota_probe":{"status":"error","entries":[{"label":"weekly","remaining":0,"reset_at":"2026-08-21T22:29:08.997742+08:00"}],"fetched_at":"2026-08-20T00:00:14Z"}}`)},
		{name: "missing observation time", meta: store.JSON(`{"quota_probe":{"status":"ok","entries":[{"label":"weekly","remaining":0,"reset_at":"2026-08-21T22:29:08.997742+08:00"}]}}`)},
		{name: "future observation", meta: store.JSON(`{"quota_probe":{"status":"ok","entries":[{"label":"weekly","remaining":0,"reset_at":"2026-08-21T22:29:08.997742+08:00"}],"fetched_at":"2026-08-20T05:00:00Z"}}`)},
		{name: "past reset", meta: store.JSON(`{"quota_probe":{"status":"ok","entries":[{"label":"weekly","remaining":0,"reset_at":"2026-08-19T22:29:08.997742+08:00"}],"fetched_at":"2026-08-20T00:00:14Z"}}`)},
		{name: "wrong window", meta: valid},
		{name: "invalid metadata", meta: store.JSON(`{invalid`)},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			window := "weekly"
			if test.name == "wrong window" {
				window = "usage"
			}
			if got, ok := CredentialQuotaProbeResetAt(test.meta, window, now); ok || !got.IsZero() {
				t.Fatalf("CredentialQuotaProbeResetAt() = %s, %v, want zero, false", got, ok)
			}
		})
	}
}

func TestSub2APISubscriptionQuotaRecovered(t *testing.T) {
	t.Parallel()

	zero := 0.0
	positive := 2.5
	for _, test := range []struct {
		name   string
		result QuotaProbeResult
		window string
		want   bool
	}{
		{name: "daily zero", result: QuotaProbeResult{Status: "ok", Entries: []QuotaProbeEntry{{Label: "daily", Remaining: &zero}}}, window: "daily"},
		{name: "daily positive", result: QuotaProbeResult{Status: "ok", Entries: []QuotaProbeEntry{{Label: "daily", Remaining: &positive}}}, window: "daily", want: true},
		{name: "missing corresponding window", result: QuotaProbeResult{Status: "ok", Entries: []QuotaProbeEntry{{Label: "weekly", Remaining: &positive}}}, window: "daily"},
		{name: "failed preserved positive", result: QuotaProbeResult{Status: "error", Entries: []QuotaProbeEntry{{Label: "daily", Remaining: &positive}}}, window: "daily"},
		{name: "usage all positive", result: QuotaProbeResult{Status: "ok", Entries: []QuotaProbeEntry{{Label: "daily", Remaining: &positive}, {Label: "weekly", Remaining: &positive}}}, window: "usage", want: true},
		{name: "usage balance and window positive", result: QuotaProbeResult{Status: "ok", Entries: []QuotaProbeEntry{{Label: "balance", Remaining: &positive}, {Label: "daily", Remaining: &positive}}}, window: "usage", want: true},
		{name: "usage balance exhausted", result: QuotaProbeResult{Status: "ok", Entries: []QuotaProbeEntry{{Label: "balance", Remaining: &zero}, {Label: "daily", Remaining: &positive}}}, window: "usage"},
		{name: "usage one exhausted", result: QuotaProbeResult{Status: "ok", Entries: []QuotaProbeEntry{{Label: "daily", Remaining: &positive}, {Label: "weekly", Remaining: &zero}}}, window: "usage"},
		{name: "usage balance alone is ambiguous", result: QuotaProbeResult{Status: "ok", Entries: []QuotaProbeEntry{{Label: "balance", Remaining: &positive}}}, window: "usage"},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := sub2APISubscriptionQuotaRecovered(test.result, test.window); got != test.want {
				t.Fatalf("sub2APISubscriptionQuotaRecovered() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestSub2APISubscriptionQuotaEntry(t *testing.T) {
	t.Parallel()

	remaining := 0.0
	result := QuotaProbeResult{Entries: []QuotaProbeEntry{{Label: "weekly", Remaining: &remaining}}}
	entry, ok := sub2APISubscriptionQuotaEntry(result, "WEEKLY")
	if !ok || entry.Label != "weekly" || entry.Remaining == nil || *entry.Remaining != 0 {
		t.Fatalf("entry = %+v, %v, want weekly exhausted entry", entry, ok)
	}
	if _, ok := sub2APISubscriptionQuotaEntry(result, "usage"); ok {
		t.Fatal("usage should not select a specific subscription window")
	}
}

func TestRecoverSub2APISubscriptionCooldownUpdatesExhaustedReset(t *testing.T) {
	t.Parallel()

	observedAt := time.Now().UTC()
	credentialID := uuid.New()
	cooldownID := uuid.New()
	resetAt := observedAt.Add(2 * time.Hour)
	updates := 0
	service := siteServiceWithCallbacks(t, siteGormCallbacks{
		query: func(tx *gorm.DB) {
			items, ok := tx.Statement.Dest.(*[]store.RouteCooldown)
			if !ok {
				tx.AddError(gorm.ErrInvalidData)
				return
			}
			*items = []store.RouteCooldown{{
				ID:               cooldownID,
				SiteID:           uuid.New(),
				SiteCredentialID: uuid.NullUUID{UUID: credentialID, Valid: true},
				Reason:           store.CooldownReasonUpstreamSubscriptionLimitExceeded,
				ActiveUntil:      observedAt.Add(7 * 24 * time.Hour),
				CreatedAt:        observedAt.Add(-time.Minute),
				Metadata:         store.JSON(`{"limit_window":"weekly","reset_at":"2099-01-05T00:00:00Z"}`),
			}}
			tx.RowsAffected = 1
			tx.Statement.RowsAffected = 1
		},
		update: func(tx *gorm.DB) {
			updates++
			tx.RowsAffected = 1
			tx.Statement.RowsAffected = 1
		},
	})
	remaining := 0.0
	reset := resetAt.Format(time.RFC3339Nano)
	service.recoverSub2APISubscriptionCooldown(context.Background(), uuid.New(), credentialID, QuotaProbeResult{
		Status:    "ok",
		FetchedAt: observedAt,
		Entries:   []QuotaProbeEntry{{Label: "weekly", Remaining: &remaining, ResetAt: &reset}},
	})
	if updates != 1 {
		t.Fatalf("cooldown updates = %d, want 1", updates)
	}
}

func TestRecoverSub2APISubscriptionCooldownKeepsGenericUsageRecovery(t *testing.T) {
	t.Parallel()

	observedAt := time.Now().UTC()
	credentialID := uuid.New()
	updates := 0
	service := siteServiceWithCallbacks(t, siteGormCallbacks{
		query: func(tx *gorm.DB) {
			items, ok := tx.Statement.Dest.(*[]store.RouteCooldown)
			if !ok {
				tx.AddError(gorm.ErrInvalidData)
				return
			}
			*items = []store.RouteCooldown{{
				ID:               uuid.New(),
				SiteID:           uuid.New(),
				SiteCredentialID: uuid.NullUUID{UUID: credentialID, Valid: true},
				Reason:           store.CooldownReasonUpstreamSubscriptionLimitExceeded,
				ActiveUntil:      observedAt.Add(24 * time.Hour),
				CreatedAt:        observedAt.Add(-time.Minute),
				Metadata:         store.JSON(`{"limit_window":"usage"}`),
			}}
			tx.RowsAffected = 1
			tx.Statement.RowsAffected = 1
		},
		update: func(tx *gorm.DB) {
			updates++
			tx.RowsAffected = 1
			tx.Statement.RowsAffected = 1
		},
	})
	positive := 1.0
	service.recoverSub2APISubscriptionCooldown(context.Background(), uuid.New(), credentialID, QuotaProbeResult{
		Status:    "ok",
		FetchedAt: observedAt,
		Entries: []QuotaProbeEntry{
			{Label: "balance", Remaining: &positive},
			{Label: "daily", Remaining: &positive},
			{Label: "weekly", Remaining: &positive},
		},
	})
	if updates != 1 {
		t.Fatalf("generic usage cooldown updates = %d, want 1", updates)
	}
}

func TestRecoverSub2APISubscriptionCooldownClearsOnlyPositiveCurrentProbe(t *testing.T) {
	t.Parallel()

	observedAt := time.Now().UTC()
	positive := 1.0
	for _, test := range []struct {
		name        string
		fetchedAt   time.Time
		createdAt   []time.Time
		wantUpdates int
	}{
		{name: "current positive probe", fetchedAt: observedAt, createdAt: []time.Time{observedAt.Add(-time.Minute)}, wantUpdates: 1},
		{name: "stale positive probe", fetchedAt: observedAt, createdAt: []time.Time{observedAt.Add(time.Minute)}},
		{name: "newer cooldown protects all rows", fetchedAt: observedAt, createdAt: []time.Time{observedAt.Add(-time.Minute), observedAt.Add(time.Minute)}},
		{name: "missing observation time", createdAt: []time.Time{observedAt.Add(-time.Minute)}},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			siteID := uuid.New()
			credentialID := uuid.New()
			updates := 0
			service := siteServiceWithCallbacks(t, siteGormCallbacks{
				query: func(tx *gorm.DB) {
					items, ok := tx.Statement.Dest.(*[]store.RouteCooldown)
					if !ok {
						tx.AddError(gorm.ErrInvalidData)
						return
					}
					for _, createdAt := range test.createdAt {
						*items = append(*items, store.RouteCooldown{
							SiteID:           siteID,
							SiteCredentialID: uuid.NullUUID{UUID: credentialID, Valid: true},
							Reason:           store.CooldownReasonUpstreamSubscriptionLimitExceeded,
							ActiveUntil:      observedAt.Add(time.Hour),
							CreatedAt:        createdAt,
							Metadata:         store.JSON(`{"limit_window":"daily"}`),
						})
					}
					tx.RowsAffected = 1
					tx.Statement.RowsAffected = 1
				},
				update: func(tx *gorm.DB) {
					updates++
					tx.RowsAffected = 1
					tx.Statement.RowsAffected = 1
				},
			})

			service.recoverSub2APISubscriptionCooldown(t.Context(), siteID, credentialID, QuotaProbeResult{
				Status:    "ok",
				FetchedAt: test.fetchedAt,
				Entries:   []QuotaProbeEntry{{Label: "daily", Remaining: &positive}},
			})
			if updates != test.wantUpdates {
				t.Fatalf("cooldown clear updates = %d, want %d", updates, test.wantUpdates)
			}
		})
	}
}

func TestPreserveQuotaSummaryValuesKeepsNumbersOnTotalFailure(t *testing.T) {
	t.Parallel()

	siteMeta := map[string]any{
		"quota_probe_summary": map[string]any{
			"status":        "ok",
			"remaining_min": 42.5,
			"unit":          "usd",
			"limit":         100.0,
			"used":          57.5,
		},
	}
	summary := map[string]any{"status": "error", "ok_count": 0}
	preserveQuotaSummaryValues(siteMeta, summary)
	if summary["remaining_min"] != 42.5 || summary["limit"] != 100.0 || summary["used"] != 57.5 || summary["unit"] != "usd" {
		t.Fatalf("summary = %#v, want previous numeric fields preserved", summary)
	}
	if summary["status"] != "error" {
		t.Fatalf("summary status = %v, want error kept", summary["status"])
	}
}

func TestPreserveQuotaSummaryValuesWithoutHistoryLeavesSummary(t *testing.T) {
	t.Parallel()

	summary := map[string]any{"status": "error"}
	preserveQuotaSummaryValues(map[string]any{}, summary)
	if len(summary) != 1 {
		t.Fatalf("summary = %#v, want untouched", summary)
	}
}

const kimiUsagesFixture = `{
	"user": {
		"userId": "cnh27phkqq4gq1algqc0",
		"region": "REGION_CN",
		"membership": {"level": "LEVEL_INTERMEDIATE"},
		"businessId": ""
	},
	"usage": {
		"limit": "100", "used": "90", "remaining": "10",
		"resetTime": "2026-07-27T02:19:14.762634Z"
	},
	"limits": [
		{
			"window": {"duration": 300, "timeUnit": "TIME_UNIT_MINUTE"},
			"detail": {
				"limit": "100", "used": "44",
				"resetTime": "2026-07-26T23:19:14.762634Z"
			}
		}
	],
	"parallel": {"limit": "20", "details": ["348b44c7-8e24-420e-804d-c8730878916b"]},
	"totalQuota": {},
	"authentication": {"method": "METHOD_API_KEY", "scope": "FEATURE_CODING"},
	"subType": "TYPE_PURCHASE",
	"domain": "DOMAIN_NEXUS"
}`

func TestProbeKimiQuota(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/coding/v1/usages" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer sk-kimi" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(kimiUsagesFixture))
	}))
	defer server.Close()

	result := probeQuota(context.Background(), server.Client(), QuotaProbeTypeKimi, server.URL+"/coding", "sk-kimi")
	if result.Status != "ok" || result.Kind != "token_plan" {
		t.Fatalf("expected ok token_plan result, got %+v", result)
	}
	if result.Plan != "Allegretto" {
		t.Fatalf("plan = %q, want Allegretto (REGION_CN + LEVEL_INTERMEDIATE)", result.Plan)
	}
	if len(result.Entries) != 2 {
		t.Fatalf("expected five_hour + weekly entries, got %+v", result.Entries)
	}

	fiveHour := result.Entries[0]
	if fiveHour.Label != "five_hour" || fiveHour.Unit != "percent" {
		t.Fatalf("unexpected five_hour entry %+v", fiveHour)
	}
	if fiveHour.Remaining == nil || *fiveHour.Remaining != 56 || fiveHour.Used == nil || *fiveHour.Used != 44 {
		t.Fatalf("five_hour numbers = %+v, want remaining 56 used 44", fiveHour)
	}
	if fiveHour.ResetAt == nil || *fiveHour.ResetAt != "2026-07-26T23:19:14.762634Z" {
		t.Fatalf("five_hour reset_at = %v, want fixture resetTime", fiveHour.ResetAt)
	}

	weekly := result.Entries[1]
	if weekly.Label != "weekly" || weekly.Remaining == nil || *weekly.Remaining != 10 {
		t.Fatalf("unexpected weekly entry %+v", weekly)
	}
	if weekly.ResetAt == nil || *weekly.ResetAt != "2026-07-27T02:19:14.762634Z" {
		t.Fatalf("weekly reset_at = %v, want fixture resetTime", weekly.ResetAt)
	}

	// 周额度剩余 10% 更紧张，应成为主 entry 供 summary 展示
	primary, ok := quotaProbePrimaryEntry(result)
	if !ok || primary.Label != "weekly" || *primary.Remaining != 10 {
		t.Fatalf("primary entry = %+v %v, want tightest window (weekly 10%%)", primary, ok)
	}
}

func TestProbeKimiQuotaToleratesTrailingV1BaseURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/coding/v1/usages" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(kimiUsagesFixture))
	}))
	defer server.Close()

	result := probeQuota(context.Background(), server.Client(), QuotaProbeTypeKimi, server.URL+"/coding/v1", "sk-kimi")
	if result.Status != "ok" {
		t.Fatalf("expected ok with /v1-suffixed base URL, got %+v", result)
	}
}

func TestProbeKimiQuotaPartialEntries(t *testing.T) {
	// 只有 usage 块、limits 为空：weekly 单独成 entry，不报错；membership 缺失时 plan 为空
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"usage": {"limit": "100", "used": "10", "remaining": "90", "resetTime": "2026-07-27T02:19:14Z"},
			"limits": []
		}`))
	}))
	defer server.Close()

	result := probeQuota(context.Background(), server.Client(), QuotaProbeTypeKimi, server.URL, "sk-kimi")
	if result.Status != "ok" || len(result.Entries) != 1 {
		t.Fatalf("expected ok with single weekly entry, got %+v", result)
	}
	if result.Entries[0].Label != "weekly" || result.Plan != "" {
		t.Fatalf("expected weekly-only entries without plan, got %+v", result)
	}
}

func TestKimiUsageDetailEntry(t *testing.T) {
	t.Parallel()

	if _, ok := kimiUsageDetailEntry("weekly", "not-a-map"); ok {
		t.Fatal("non-map detail must not produce an entry")
	}
	if _, ok := kimiUsageDetailEntry("weekly", map[string]any{"used": "3"}); ok {
		t.Fatal("detail without limit/remaining must not produce an entry")
	}
	entry, ok := kimiUsageDetailEntry("five_hour", map[string]any{"limit": "100", "remaining": "56"})
	if !ok || entry.Remaining == nil || *entry.Remaining != 56 || entry.ResetAt != nil {
		t.Fatalf("entry = %+v %v, want remaining 56 without reset_at", entry, ok)
	}
	exhausted, ok := kimiUsageDetailEntry("five_hour", map[string]any{"limit": "100", "used": "100"})
	if !ok || exhausted.Remaining == nil || *exhausted.Remaining != 0 {
		t.Fatalf("entry = %+v %v, want derived remaining 0", exhausted, ok)
	}
}

func TestQuotaProbeKimiUsagesURL(t *testing.T) {
	t.Parallel()

	for input, want := range map[string]string{
		"https://api.kimi.com/coding":     "https://api.kimi.com/coding/v1/usages",
		"https://api.kimi.com/coding/":    "https://api.kimi.com/coding/v1/usages",
		"https://api.kimi.com/coding/v1":  "https://api.kimi.com/coding/v1/usages",
		"https://api.kimi.com/coding/v1/": "https://api.kimi.com/coding/v1/usages",
		"":                                "https://api.kimi.com/coding/v1/usages",
	} {
		if got := quotaProbeKimiUsagesURL(input); got != want {
			t.Fatalf("quotaProbeKimiUsagesURL(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestKimiMembershipPlanName(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		level  string
		region string
		want   string
	}{
		"cn basic is andante":        {"LEVEL_BASIC", "REGION_CN", "Andante"},
		"global basic is moderato":   {"LEVEL_BASIC", "REGION_GLOBAL", "Moderato"},
		"standard is moderato":       {"LEVEL_STANDARD", "REGION_CN", "Moderato"},
		"intermediate is allegretto": {"LEVEL_INTERMEDIATE", "REGION_CN", "Allegretto"},
		"advanced is allegro":        {"LEVEL_ADVANCED", "REGION_CN", "Allegro"},
		"premium is vivace":          {"LEVEL_PREMIUM", "REGION_GLOBAL", "Vivace"},
		"empty level":                {"", "REGION_CN", ""},
		"unknown falls back":         {"LEVEL_FUTURE_TIER", "REGION_CN", "Future tier"},
		"lowercase tolerated":        {"level_advanced", "REGION_CN", "Allegro"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := kimiMembershipPlanName(tc.level, tc.region); got != tc.want {
				t.Fatalf("kimiMembershipPlanName(%q, %q) = %q, want %q", tc.level, tc.region, got, tc.want)
			}
		})
	}
}

func TestKimiWindowIsFiveHour(t *testing.T) {
	t.Parallel()

	fiveHour := map[string]any{"duration": float64(300), "timeUnit": "TIME_UNIT_MINUTE"}
	if !kimiWindowIsFiveHour(fiveHour) {
		t.Fatal("300 minutes window should be recognized as five-hour")
	}
	for _, window := range []map[string]any{
		{"duration": float64(10080), "timeUnit": "TIME_UNIT_MINUTE"},
		{"duration": float64(7), "timeUnit": "TIME_UNIT_DAY"},
		{"duration": float64(300)},
		nil,
	} {
		if kimiWindowIsFiveHour(window) {
			t.Fatalf("window %+v must not be recognized as five-hour", window)
		}
	}
}

func TestDefaultQuotaProbeTypeForSite(t *testing.T) {
	t.Parallel()

	// 空 base_url 视为官方默认地址，默认开启探测。
	if got := defaultQuotaProbeTypeForSite(store.Site{SiteType: "kimi_code"}); got != QuotaProbeTypeKimi {
		t.Fatalf("kimi_code default probe = %q, want %q", got, QuotaProbeTypeKimi)
	}
	if got := defaultQuotaProbeTypeForSite(store.Site{SiteType: "glm_code"}); got != QuotaProbeTypeGLM {
		t.Fatalf("glm_code default probe = %q, want %q", got, QuotaProbeTypeGLM)
	}

	official := []struct {
		siteType string
		baseURL  string
		want     string
	}{
		{"kimi_code", "https://api.kimi.com/coding", QuotaProbeTypeKimi},
		{"kimi_code", "https://api.kimi.com/coding/v1", QuotaProbeTypeKimi},
		{"glm_code", "https://open.bigmodel.cn/api/coding/paas/v4", QuotaProbeTypeGLM},
		{"glm_code", "https://api.z.ai/api/coding/paas/v4", QuotaProbeTypeGLM},
	}
	for _, tc := range official {
		if got := defaultQuotaProbeTypeForSite(store.Site{SiteType: tc.siteType, BaseURL: tc.baseURL}); got != tc.want {
			t.Fatalf("site_type %q base_url %q default probe = %q, want %q", tc.siteType, tc.baseURL, got, tc.want)
		}
	}

	// 指向中转/镜像的站点不默认探测，避免对未实现额度接口的端点持续报错。
	thirdParty := []struct {
		siteType string
		baseURL  string
	}{
		{"kimi_code", "https://relay.example.com/coding"},
		{"glm_code", "https://relay.example.com/api/coding/paas/v4"},
		{"glm_code", "https://bigmodel.cn.evil.example.com/api/coding/paas/v4"},
		{"glm_code", "not a url"},
	}
	for _, tc := range thirdParty {
		if got := defaultQuotaProbeTypeForSite(store.Site{SiteType: tc.siteType, BaseURL: tc.baseURL}); got != "" {
			t.Fatalf("site_type %q base_url %q default probe = %q, want empty", tc.siteType, tc.baseURL, got)
		}
	}

	for _, siteType := range []string{"moonshot", "zhipu", "newapi", "openai"} {
		if got := defaultQuotaProbeTypeForSite(store.Site{SiteType: siteType}); got != "" {
			t.Fatalf("site_type %q default probe = %q, want empty", siteType, got)
		}
	}
}

func TestPreserveQuotaSummaryValuesKeepsEntriesAndPlan(t *testing.T) {
	t.Parallel()

	siteMeta := map[string]any{
		"quota_probe_summary": map[string]any{
			"status":        "ok",
			"remaining_min": 10.0,
			"unit":          "percent",
			"plan":          "Allegretto",
			"entries": []any{
				map[string]any{"label": "five_hour", "remaining": 56.0},
			},
		},
	}
	summary := map[string]any{"status": "error", "ok_count": 0}
	preserveQuotaSummaryValues(siteMeta, summary)
	if summary["plan"] != "Allegretto" || summary["remaining_min"] != 10.0 || summary["unit"] != "percent" {
		t.Fatalf("summary = %#v, want plan and percent fields preserved", summary)
	}
	if _, ok := summary["entries"].([]any); !ok {
		t.Fatalf("summary entries = %#v, want preserved", summary["entries"])
	}
}

// 与 2026-09 实测 open.bigmodel.cn 响应一致，另混入 TIME_LIMIT（MCP 月度）与
// 未知日窗口（unit=1×1），断言三者按 label 折算/忽略后不破坏窗口条目。
const glmQuotaLimitFixture = `{
	"code": 200,
	"msg": "操作成功",
	"data": {
		"level": "pro",
		"limits": [
			{"type": "TOKENS_LIMIT", "unit": 3, "number": 5, "percentage": 0},
			{"type": "TOKENS_LIMIT", "unit": 6, "number": 1, "percentage": 100, "nextResetTime": 1789005599998},
			{"type": "TIME_LIMIT", "unit": 5, "number": 1, "usage": 1000, "currentValue": 7, "remaining": 993, "percentage": 1, "nextResetTime": 1790128799997},
			{"type": "TOKENS_LIMIT", "unit": 1, "number": 1, "percentage": 42}
		]
	},
	"success": true
}`

// glmQuotaFixtureFloatNear 比较浮点百分比（绝对值折算路径经 currentValue/usage
// 除法产生，不能用 != 精确比较）。
func glmQuotaFixtureFloatNear(got float64, want float64) bool {
	diff := got - want
	return diff < 1e-9 && diff > -1e-9
}

// 2026-09 积分制套餐实测响应（CREDIT_LIMIT）：usage 为积分总额度，
// currentValue 为已用积分，percentage 为服务端取整。
const glmQuotaCreditFixture = `{
	"code": 200,
	"msg": "操作成功",
	"data": {
		"level": "pro",
		"limits": [
			{"type": "CREDIT_LIMIT", "unit": 3, "number": 5, "usage": 12000, "currentValue": 1711, "remaining": 10288, "percentage": 14, "nextResetTime": 1789717705754},
			{"type": "CREDIT_LIMIT", "unit": 6, "number": 1, "usage": 60000, "currentValue": 15892, "remaining": 44107, "percentage": 26, "nextResetTime": 1790232772960}
		]
	},
	"success": true
}`

func TestProbeGLMQuota(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/monitor/usage/quota/limit" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer sk-glm" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(glmQuotaLimitFixture))
	}))
	defer server.Close()

	// base_url 带 /api/coding/paas/v4 时仍应打到 origin 根路径
	result := probeQuota(context.Background(), server.Client(), QuotaProbeTypeGLM, server.URL+"/api/coding/paas/v4", "sk-glm")
	if result.Status != "ok" || result.Kind != "token_plan" {
		t.Fatalf("expected ok token_plan result, got %+v", result)
	}
	if result.Plan != "Pro" {
		t.Fatalf("plan = %q, want Pro (level pro)", result.Plan)
	}
	if len(result.Entries) != 3 {
		t.Fatalf("expected five_hour + weekly + monthly entries, got %+v", result.Entries)
	}

	fiveHour := result.Entries[0]
	if fiveHour.Label != "five_hour" || fiveHour.Unit != "percent" {
		t.Fatalf("unexpected five_hour entry %+v", fiveHour)
	}
	if fiveHour.Remaining == nil || *fiveHour.Remaining != 100 || fiveHour.Used == nil || *fiveHour.Used != 0 {
		t.Fatalf("five_hour numbers = %+v, want remaining 100 used 0", fiveHour)
	}

	weekly := result.Entries[1]
	if weekly.Label != "weekly" || weekly.Remaining == nil || *weekly.Remaining != 0 {
		t.Fatalf("unexpected weekly entry %+v", weekly)
	}
	if weekly.ResetAt == nil || *weekly.ResetAt != "2026-09-10T01:59:59Z" {
		t.Fatalf("weekly reset_at = %v, want 2026-09-10T01:59:59Z (fixture nextResetTime ms)", weekly.ResetAt)
	}

	monthly := result.Entries[2]
	if monthly.Label != "monthly" || monthly.Unit != "percent" {
		t.Fatalf("unexpected monthly (MCP TIME_LIMIT) entry %+v", monthly)
	}
	if monthly.Used == nil || !glmQuotaFixtureFloatNear(*monthly.Used, 0.7) || monthly.Remaining == nil || !glmQuotaFixtureFloatNear(*monthly.Remaining, 99.3) {
		t.Fatalf("monthly numbers = %+v, want used 0.7 remaining 99.3 (7/1000 calls)", monthly)
	}

	// 周额度剩余 0% 最紧张，应成为主 entry 供 summary 展示
	primary, ok := quotaProbePrimaryEntry(result)
	if !ok || primary.Label != "weekly" || *primary.Remaining != 0 {
		t.Fatalf("primary entry = %+v %v, want tightest window (weekly 0%%)", primary, ok)
	}
}

func TestProbeGLMQuotaRejectsErrorCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code": 401, "msg": "未授权", "success": false}`))
	}))
	defer server.Close()

	result := probeQuota(context.Background(), server.Client(), QuotaProbeTypeGLM, server.URL, "sk-glm")
	if result.Status != "error" || result.Error == "" {
		t.Fatalf("expected error result for non-200 code, got %+v", result)
	}
}

// 积分制套餐（2026-07-30 起）：type 变为 CREDIT_LIMIT，按 usage/currentValue
// 绝对值折算精确百分比，忽略上游取整的 percentage。
func TestProbeGLMQuotaCreditLimitPlan(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(glmQuotaCreditFixture))
	}))
	defer server.Close()

	result := probeQuota(context.Background(), server.Client(), QuotaProbeTypeGLM, server.URL, "sk-glm")
	if result.Status != "ok" || result.Kind != "token_plan" {
		t.Fatalf("expected ok token_plan result for credit plan, got %+v", result)
	}
	if result.Plan != "Pro" {
		t.Fatalf("plan = %q, want Pro", result.Plan)
	}
	if len(result.Entries) != 2 {
		t.Fatalf("expected five_hour + weekly entries, got %+v", result.Entries)
	}

	fiveHour := result.Entries[0]
	if fiveHour.Label != "five_hour" || fiveHour.Unit != "percent" {
		t.Fatalf("unexpected five_hour entry %+v", fiveHour)
	}
	wantUsed := 1711.0 / 12000 * 100
	if fiveHour.Used == nil || !glmQuotaFixtureFloatNear(*fiveHour.Used, wantUsed) {
		t.Fatalf("five_hour used = %+v, want %v (1711/12000 exact, not upstream-rounded 14)", fiveHour.Used, wantUsed)
	}
	if fiveHour.Remaining == nil || !glmQuotaFixtureFloatNear(*fiveHour.Remaining, 100-wantUsed) {
		t.Fatalf("five_hour remaining = %+v, want %v", fiveHour.Remaining, 100-wantUsed)
	}

	weekly := result.Entries[1]
	if weekly.Label != "weekly" || weekly.Used == nil || !glmQuotaFixtureFloatNear(*weekly.Used, 15892.0/60000*100) {
		t.Fatalf("unexpected weekly entry %+v", weekly)
	}
	if weekly.ResetAt == nil || *weekly.ResetAt != "2026-09-24T06:52:52Z" {
		t.Fatalf("weekly reset_at = %v, want 2026-09-24T06:52:52Z", weekly.ResetAt)
	}
}

// 积分制但缺少绝对值时（理论上不应出现），仍用 percentage 兜底，不能解析失败。
func TestProbeGLMQuotaCreditLimitPercentageFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": 200,
			"data": {"level": "pro", "limits": [
				{"type": "CREDIT_LIMIT", "unit": 3, "number": 5, "percentage": 14}
			]},
			"success": true
		}`))
	}))
	defer server.Close()

	result := probeQuota(context.Background(), server.Client(), QuotaProbeTypeGLM, server.URL, "sk-glm")
	if result.Status != "ok" || len(result.Entries) != 1 {
		t.Fatalf("expected ok with one entry, got %+v", result)
	}
	entry := result.Entries[0]
	if entry.Used == nil || *entry.Used != 14 || entry.Remaining == nil || *entry.Remaining != 86 {
		t.Fatalf("percentage fallback numbers = %+v, want used 14 remaining 86", entry)
	}
}

// 同一窗口以不同单位编码返回两次时（unit=3/number=5 与 unit=5/number=300 都是
// 300 分钟），按 label 去重并保留剩余最少的一条，避免前端 find 与 summary 的
// min 选择口径不一致。
func TestProbeGLMQuotaDeduplicatesWindowEncodings(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": 200,
			"data": {
				"level": "pro",
				"limits": [
					{"type": "TOKENS_LIMIT", "unit": 3, "number": 5, "percentage": 10},
					{"type": "TOKENS_LIMIT", "unit": 5, "number": 300, "percentage": 40}
				]
			},
			"success": true
		}`))
	}))
	defer server.Close()

	result := probeQuota(context.Background(), server.Client(), QuotaProbeTypeGLM, server.URL, "sk-glm")
	if result.Status != "ok" {
		t.Fatalf("expected ok result, got %+v", result)
	}
	if len(result.Entries) != 1 {
		t.Fatalf("expected 1 deduplicated five_hour entry, got %+v", result.Entries)
	}
	entry := result.Entries[0]
	if entry.Label != "five_hour" || entry.Remaining == nil || *entry.Remaining != 60 {
		t.Fatalf("entry = %+v, want five_hour with tightest remaining 60%%", entry)
	}
}

func TestProbeGLMQuotaMCPDoesNotControlChatQuota(t *testing.T) {
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name          string
		fiveHourUsed  int
		weeklyUsed    int
		wantWindow    string
		wantDeadline  time.Time
		wantRemaining float64
	}{
		{name: "chat available", fiveHourUsed: 1200, weeklyUsed: 6000, wantRemaining: 90},
		{name: "five hour exhausted", fiveHourUsed: 12000, weeklyUsed: 6000, wantWindow: "five_hour", wantDeadline: now.Add(time.Hour)},
		{name: "weekly exhausted", fiveHourUsed: 1200, weeklyUsed: 60000, wantWindow: "weekly", wantDeadline: now.Add(3 * 24 * time.Hour)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"code":200,"data":{"limits":[
                    {"type":"CREDIT_LIMIT","unit":3,"number":5,"usage":12000,"currentValue":%d,"nextResetTime":%d},
                    {"type":"CREDIT_LIMIT","unit":6,"number":1,"usage":60000,"currentValue":%d,"nextResetTime":%d},
                    {"type":"TIME_LIMIT","usage":1000,"currentValue":1000,"nextResetTime":%d}
                ]}}`, tc.fiveHourUsed, now.Add(time.Hour).UnixMilli(), tc.weeklyUsed, now.Add(3*24*time.Hour).UnixMilli(), now.Add(30*24*time.Hour).UnixMilli())
			}))
			defer server.Close()
			result := probeQuota(context.Background(), server.Client(), QuotaProbeTypeGLM, server.URL, "sk-glm")
			if result.Status != "ok" || len(result.Entries) != 3 {
				t.Fatalf("probe result = %+v, want all three quota rows", result)
			}
			monthly := result.Entries[2]
			if monthly.Label != "monthly" || monthly.Remaining == nil || *monthly.Remaining != 0 {
				t.Fatalf("MCP entry = %+v, want exhausted monthly quota retained for display", monthly)
			}
			deadline, windows := codingPlanQuotaCooldownDeadline(result, now)
			if !deadline.Equal(tc.wantDeadline) {
				t.Fatalf("deadline = %v, want %v; windows=%v", deadline, tc.wantDeadline, windows)
			}
			if tc.wantWindow == "" {
				if len(windows) != 0 {
					t.Fatalf("windows = %v, want none", windows)
				}
			} else if len(windows) != 1 || windows[0] != tc.wantWindow {
				t.Fatalf("windows = %v, want [%s]", windows, tc.wantWindow)
			}
			summary, ok := quotaProbeSummaryEntry(QuotaProbeTypeGLM, result)
			if !ok || summary.Label == "monthly" || summary.Remaining == nil || *summary.Remaining != tc.wantRemaining {
				t.Fatalf("summary = %+v, want chat remaining %v", summary, tc.wantRemaining)
			}
		})
	}
}

func TestProbeGLMQuotaRejectsMCPOnlyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"data":{"limits":[{"type":"TIME_LIMIT","usage":1000,"currentValue":0}]}}`))
	}))
	defer server.Close()
	result := probeQuota(context.Background(), server.Client(), QuotaProbeTypeGLM, server.URL, "sk-glm")
	if result.Status != "error" {
		t.Fatalf("result = %+v, missing chat quota must not be treated as quota recovery", result)
	}
}

func TestGLMPlanName(t *testing.T) {
	t.Parallel()

	if got := glmPlanName("pro"); got != "Pro" {
		t.Fatalf("glmPlanName(pro) = %q, want Pro", got)
	}
	if got := glmPlanName("  MAX "); got != "Max" {
		t.Fatalf("glmPlanName(\"  MAX \") = %q, want Max", got)
	}
	if got := glmPlanName(""); got != "" {
		t.Fatalf("glmPlanName(\"\") = %q, want empty", got)
	}
	// 非 ASCII level 不应切出半个多字节字符（无效 UTF-8）
	if got := glmPlanName("旗舰版"); !utf8.ValidString(got) || got == "" {
		t.Fatalf("glmPlanName(旗舰版) = %q, want valid non-empty UTF-8", got)
	}
}

func TestCodingPlanQuotaCooldownDeadline(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	zero := 0.0
	positive := 42.0
	future1h := now.Add(time.Hour).Format(time.RFC3339)
	future3d := now.Add(3 * 24 * time.Hour).Format(time.RFC3339)
	past := now.Add(-time.Hour).Format(time.RFC3339)

	// 两个窗口都耗尽时，冷却截止取最晚的重置时间（所有窗口都有额度才可用）
	deadline, windows := codingPlanQuotaCooldownDeadline(QuotaProbeResult{Entries: []QuotaProbeEntry{
		{Label: "five_hour", Remaining: &zero, ResetAt: &future1h},
		{Label: "weekly", Remaining: &zero, ResetAt: &future3d},
	}}, now)
	if !deadline.Equal(now.Add(3*24*time.Hour)) || len(windows) != 2 {
		t.Fatalf("deadline = %v windows = %v, want 3d deadline with both windows", deadline, windows)
	}

	// 只有 5 小时窗耗尽时按 5 小时窗重置时间冷却
	deadline, windows = codingPlanQuotaCooldownDeadline(QuotaProbeResult{Entries: []QuotaProbeEntry{
		{Label: "five_hour", Remaining: &zero, ResetAt: &future1h},
		{Label: "weekly", Remaining: &positive, ResetAt: &future3d},
	}}, now)
	if !deadline.Equal(now.Add(time.Hour)) || len(windows) != 1 || windows[0] != "five_hour" {
		t.Fatalf("deadline = %v windows = %v, want 1h deadline with five_hour only", deadline, windows)
	}

	// 耗尽但没有重置时间、重置时间已过、重置时间无法解析：都不冷却
	for name, entries := range map[string][]QuotaProbeEntry{
		"no reset":      {{Label: "five_hour", Remaining: &zero}},
		"past reset":    {{Label: "five_hour", Remaining: &zero, ResetAt: &past}},
		"invalid reset": {{Label: "five_hour", Remaining: &zero, ResetAt: new("soon")}},
		"quota remains": {{Label: "five_hour", Remaining: &positive, ResetAt: &future1h}},
		"nil remaining": {{Label: "five_hour", ResetAt: &future1h}},
	} {
		if deadline, windows := codingPlanQuotaCooldownDeadline(QuotaProbeResult{Entries: entries}, now); !deadline.IsZero() || len(windows) != 0 {
			t.Fatalf("%s: deadline = %v windows = %v, want zero", name, deadline, windows)
		}
	}
}

func TestSyncCodingPlanQuotaCooldownActivatesOnExhaustedWindow(t *testing.T) {
	t.Parallel()

	credentialID := uuid.New()
	reset := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	resetStr := reset.Format(time.RFC3339)
	zero := 0.0

	var created *store.RouteCooldown
	db := siteTransactionPostgresGorm(t)
	siteReplaceUpdateCallback(t, db, func(tx *gorm.DB) {
		tx.RowsAffected = 1
		tx.Statement.RowsAffected = 1
	})
	siteReplaceCreateCallback(t, db, func(tx *gorm.DB) {
		item, ok := tx.Statement.Dest.(*store.RouteCooldown)
		if !ok {
			tx.AddError(gorm.ErrInvalidData)
			return
		}
		copied := *item
		created = &copied
		tx.RowsAffected = 1
		tx.Statement.RowsAffected = 1
	})
	service := NewService(siteStoreWithGorm(t, db), siteTestMasterKey)

	service.syncCodingPlanQuotaCooldown(context.Background(), uuid.New(), credentialID, QuotaProbeResult{
		Status:  "ok",
		Entries: []QuotaProbeEntry{{Label: "five_hour", Remaining: &zero, ResetAt: &resetStr}},
	})

	if created == nil {
		t.Fatal("expected a credential cooldown to be activated")
	}
	if !created.SiteCredentialID.Valid || created.SiteCredentialID.UUID != credentialID {
		t.Fatalf("cooldown credential = %+v, want %v", created.SiteCredentialID, credentialID)
	}
	if created.Scope != "credential" || created.Source != "quota_probe" || created.Reason != store.CooldownReasonCodingPlanQuotaExhausted {
		t.Fatalf("cooldown scope/source/reason = %q/%q/%q", created.Scope, created.Source, created.Reason)
	}
	if !created.ActiveUntil.Equal(reset) {
		t.Fatalf("cooldown active_until = %v, want %v", created.ActiveUntil, reset)
	}
	meta := siteMustJSONMap(t, created.Metadata)
	if meta["reset_at"] != resetStr {
		t.Fatalf("cooldown metadata reset_at = %v, want %v", meta["reset_at"], resetStr)
	}
	windows, ok := meta["exhausted_windows"].([]any)
	if !ok || len(windows) != 1 || windows[0] != "five_hour" {
		t.Fatalf("cooldown metadata exhausted_windows = %v, want [five_hour]", meta["exhausted_windows"])
	}
}

func TestSyncCodingPlanQuotaCooldownClearsWhenQuotaRecovers(t *testing.T) {
	t.Parallel()

	updates := 0
	service := siteServiceWithCallbacks(t, siteGormCallbacks{
		update: func(tx *gorm.DB) {
			updates++
			tx.RowsAffected = 1
			tx.Statement.RowsAffected = 1
		},
	})
	positive := 80.0
	service.syncCodingPlanQuotaCooldown(context.Background(), uuid.New(), uuid.New(), QuotaProbeResult{
		Status:  "ok",
		Entries: []QuotaProbeEntry{{Label: "five_hour", Remaining: &positive}},
	})
	if updates != 1 {
		t.Fatalf("cooldown clears = %d, want 1 (clear probe-set cooldown on recovery)", updates)
	}
}

func TestSyncCodingPlanQuotaCooldownSkipsFailedProbe(t *testing.T) {
	t.Parallel()

	updates := 0
	service := siteServiceWithCallbacks(t, siteGormCallbacks{
		update: func(tx *gorm.DB) {
			updates++
			tx.RowsAffected = 1
			tx.Statement.RowsAffected = 1
		},
	})
	service.syncCodingPlanQuotaCooldown(context.Background(), uuid.New(), uuid.New(), QuotaProbeResult{Status: "error"})
	if updates != 0 {
		t.Fatalf("cooldown writes = %d, want 0 for failed probe", updates)
	}
}
