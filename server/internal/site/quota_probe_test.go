package site

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

	if got := defaultQuotaProbeTypeForSite(store.Site{SiteType: "kimi_code"}); got != QuotaProbeTypeKimi {
		t.Fatalf("kimi_code default probe = %q, want %q", got, QuotaProbeTypeKimi)
	}
	for _, siteType := range []string{"moonshot", "glm_code", "newapi", "openai"} {
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
