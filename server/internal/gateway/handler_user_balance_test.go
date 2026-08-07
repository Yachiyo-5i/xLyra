package gateway

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"xlyra/server/internal/auth"
	"xlyra/server/internal/store"
)

func TestUserBalanceReportsRemainingQuotaForCurrentAPIKey(t *testing.T) {
	t.Parallel()

	apiKey := store.APIKey{
		ID:     uuid.New(),
		Name:   "client key",
		Status: "active",
		QuotaLimit: sql.NullFloat64{
			Float64: 10,
			Valid:   true,
		},
		QuotaUsed:            103.5,
		QuotaTotalUsed:       3.5,
		QuotaUnlimited:       false,
		QuotaDailyUnlimited:  true,
		QuotaWeeklyUnlimited: true,
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/user/balance", nil)
	req = req.WithContext(auth.WithAPIKey(req.Context(), apiKey))
	rec := httptest.NewRecorder()

	Handler{userBalanceThrottle: newUserBalanceThrottle()}.UserBalance(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	wantKeys := []string{
		"is_active", "balance", "unit", "quota_limit", "quota_used", "quota_total_used", "quota_total_available", "quota_total_reset_at", "quota_unlimited",
		"quota_daily_limit", "quota_daily_used", "quota_daily_available", "quota_daily_unlimited", "quota_daily_reset_at",
		"quota_weekly_limit", "quota_weekly_used", "quota_weekly_available", "quota_weekly_unlimited", "quota_weekly_reset_at",
	}
	if len(body) != len(wantKeys) {
		t.Fatalf("response keys = %v, want exactly %v", keysOf(body), wantKeys)
	}
	for _, k := range wantKeys {
		if _, ok := body[k]; !ok {
			t.Fatalf("response missing key %q; got %v", k, keysOf(body))
		}
	}
	for _, forbidden := range []string{"api_key_id", "api_key_name", "key_prefix", "masked_key", "last_used_at", "expires_at"} {
		if _, ok := body[forbidden]; ok {
			t.Fatalf("response must not include %q; got %v", forbidden, keysOf(body))
		}
	}
	if body["is_active"] != true {
		t.Fatal("expected active key to report is_active=true")
	}
	if balance, ok := body["balance"].(float64); !ok || balance != 6.5 {
		t.Fatalf("balance = %#v, want 6.5", body["balance"])
	}
	if body["unit"] != "USD" {
		t.Fatalf("unit = %q, want USD", body["unit"])
	}
	if limit, ok := body["quota_limit"].(float64); !ok || limit != 10 {
		t.Fatalf("quota_limit = %#v, want 10", body["quota_limit"])
	}
	if used, ok := body["quota_used"].(float64); !ok || used != 103.5 {
		t.Fatalf("quota_used = %#v, want 103.5", body["quota_used"])
	}
	if used, ok := body["quota_total_used"].(float64); !ok || used != 3.5 {
		t.Fatalf("quota_total_used = %#v, want 3.5", body["quota_total_used"])
	}
	if body["quota_unlimited"] != false {
		t.Fatal("quota_unlimited = true, want false")
	}
}

func TestUserBalanceReportsUnlimitedQuotaWithNullBalance(t *testing.T) {
	t.Parallel()

	apiKey := store.APIKey{
		ID:                   uuid.New(),
		Status:               "active",
		QuotaUnlimited:       true,
		QuotaDailyUnlimited:  true,
		QuotaWeeklyUnlimited: true,
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/user/balance", nil)
	req = req.WithContext(auth.WithAPIKey(req.Context(), apiKey))
	rec := httptest.NewRecorder()

	Handler{userBalanceThrottle: newUserBalanceThrottle()}.UserBalance(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["is_active"] != true {
		t.Fatal("expected active key to report is_active=true")
	}
	if body["balance"] != nil {
		t.Fatalf("balance = %#v, want nil for unlimited quota", body["balance"])
	}
	if body["quota_limit"] != nil {
		t.Fatalf("quota_limit = %#v, want nil", body["quota_limit"])
	}
	if body["quota_unlimited"] != true {
		t.Fatal("quota_unlimited = false, want true")
	}
}

func TestUserBalanceRateLimitsRepeatedQueriesWithinTenSeconds(t *testing.T) {
	t.Parallel()

	apiKey := store.APIKey{ID: uuid.New(), Status: "active", QuotaUnlimited: true}
	handler := Handler{userBalanceThrottle: newUserBalanceThrottle()}

	first := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodGet, "/v1/user/balance", nil)
	req1 = req1.WithContext(auth.WithAPIKey(req1.Context(), apiKey))
	handler.UserBalance(first, req1)
	if first.Code != http.StatusOK {
		t.Fatalf("first call status = %d, want %d", first.Code, http.StatusOK)
	}

	second := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/v1/user/balance", nil)
	req2 = req2.WithContext(auth.WithAPIKey(req2.Context(), apiKey))
	handler.UserBalance(second, req2)
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second call status = %d, want %d", second.Code, http.StatusTooManyRequests)
	}
	if got := second.Header().Get("Retry-After"); got == "" {
		t.Fatal("Retry-After header must be set on 429")
	}
	if got := second.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	var body struct {
		Error struct {
			Code              string `json:"code"`
			RetryAfterSeconds int    `json:"retry_after_seconds"`
		} `json:"error"`
	}
	if err := json.NewDecoder(second.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Error.Code != "rate_limited" {
		t.Fatalf("error code = %q, want rate_limited", body.Error.Code)
	}
	if body.Error.RetryAfterSeconds < 1 || body.Error.RetryAfterSeconds > 10 {
		t.Fatalf("retry_after_seconds = %d, want in [1,10]", body.Error.RetryAfterSeconds)
	}
}

func TestUserBalanceRateLimitIsolatedPerAPIKey(t *testing.T) {
	t.Parallel()

	handler := Handler{userBalanceThrottle: newUserBalanceThrottle()}
	keyA := store.APIKey{ID: uuid.New(), Status: "active", QuotaUnlimited: true}
	keyB := store.APIKey{ID: uuid.New(), Status: "active", QuotaUnlimited: true}

	for _, k := range []store.APIKey{keyA, keyB} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/v1/user/balance", nil)
		req = req.WithContext(auth.WithAPIKey(req.Context(), k))
		handler.UserBalance(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("first call for key %s status = %d, want %d", k.ID, rec.Code, http.StatusOK)
		}
	}
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
