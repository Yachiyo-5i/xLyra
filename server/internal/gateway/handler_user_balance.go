package gateway

import (
	"database/sql"
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/auth"
	"xlyra/server/internal/config"
	"xlyra/server/internal/httpx"
	"xlyra/server/internal/store"
)

const (
	userBalanceMinInterval        = 10 * time.Second
	userBalanceThrottleMaxEntries = 20000
)

type userBalanceThrottle struct {
	mu   sync.Mutex
	seen map[uuid.UUID]time.Time
}

func newUserBalanceThrottle() *userBalanceThrottle {
	return &userBalanceThrottle{seen: make(map[uuid.UUID]time.Time)}
}

func (t *userBalanceThrottle) reserve(id uuid.UUID, now time.Time) (allowed bool, retryAfter time.Duration) {
	if t == nil {
		return true, 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if last, ok := t.seen[id]; ok {
		if wait := userBalanceMinInterval - now.Sub(last); wait > 0 {
			return false, wait
		}
	}
	// Bound the map: once it grows large, drop entries older than the throttle
	// interval (a stale entry would be allowed on its next reserve anyway).
	if len(t.seen) >= userBalanceThrottleMaxEntries {
		for key, last := range t.seen {
			if now.Sub(last) >= userBalanceMinInterval {
				delete(t.seen, key)
			}
		}
	}
	t.seen[id] = now
	return true, 0
}

func (h Handler) UserBalance(w http.ResponseWriter, r *http.Request) {
	apiKey, ok := auth.APIKeyFromContext(r.Context())
	if !ok {
		h.writeGatewayError(w, r, http.StatusUnauthorized, "unauthorized", "valid api key is required")
		return
	}

	if allowed, wait := h.userBalanceThrottle.reserve(apiKey.ID, time.Now()); !allowed {
		seconds := int64(math.Ceil(wait.Seconds()))
		seconds = max(seconds, 1)
		w.Header().Set("Retry-After", strconv.FormatInt(seconds, 10))
		w.Header().Set("Cache-Control", "no-store")
		httpx.JSON(w, http.StatusTooManyRequests, map[string]any{
			"error": map[string]any{
				"code":                "rate_limited",
				"message":             "user balance can be queried at most once every 10 seconds",
				"retry_after_seconds": seconds,
			},
		})
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	httpx.JSON(w, http.StatusOK, userBalancePayloadAt(apiKey, time.Now(), h.recorder.timeZone))
}

func userBalancePayload(apiKey store.APIKey) map[string]any {
	return userBalancePayloadAt(apiKey, time.Now(), config.ResolveTimeZone())
}

func userBalancePayloadAt(apiKey store.APIKey, now time.Time, timeZone config.TimeZone) map[string]any {
	dailyUsed := apiKey.EffectiveDailyQuotaUsed(now, timeZone)
	weeklyUsed := apiKey.EffectiveWeeklyQuotaUsed(now, timeZone)
	return map[string]any{
		"is_active":              apiKey.Status == "active",
		"balance":                userBalanceAvailable(apiKey),
		"unit":                   "USD",
		"quota_limit":            userBalanceNullFloat64(apiKey.QuotaLimit),
		"quota_used":             apiKey.QuotaUsed,
		"quota_unlimited":        apiKey.QuotaUnlimited,
		"quota_daily_limit":      userBalanceNullFloat64(apiKey.QuotaDailyLimit),
		"quota_daily_used":       dailyUsed,
		"quota_daily_available":  userBalancePeriodicAvailable(apiKey.QuotaDailyLimit, dailyUsed, apiKey.QuotaDailyUnlimited),
		"quota_daily_unlimited":  apiKey.QuotaDailyUnlimited,
		"quota_daily_reset_at":   userBalanceResetAt(timeZone.StartOfDay(now).AddDate(0, 0, 1), apiKey.QuotaDailyUnlimited),
		"quota_weekly_limit":     userBalanceNullFloat64(apiKey.QuotaWeeklyLimit),
		"quota_weekly_used":      weeklyUsed,
		"quota_weekly_available": userBalancePeriodicAvailable(apiKey.QuotaWeeklyLimit, weeklyUsed, apiKey.QuotaWeeklyUnlimited),
		"quota_weekly_unlimited": apiKey.QuotaWeeklyUnlimited,
		"quota_weekly_reset_at":  userBalanceResetAt(timeZone.StartOfWeek(now).AddDate(0, 0, 7), apiKey.QuotaWeeklyUnlimited),
	}
}

func userBalancePeriodicAvailable(limit sql.NullFloat64, used float64, unlimited bool) any {
	if unlimited || !limit.Valid {
		return nil
	}
	remaining := limit.Float64 - used
	if remaining < 0 {
		return float64(0)
	}
	return remaining
}

func userBalanceResetAt(resetAt time.Time, unlimited bool) any {
	if unlimited {
		return nil
	}
	return resetAt.Format(time.RFC3339)
}

func userBalanceAvailable(apiKey store.APIKey) any {
	if apiKey.QuotaUnlimited || !apiKey.QuotaLimit.Valid {
		return nil
	}
	remaining := apiKey.QuotaLimit.Float64 - apiKey.QuotaUsed
	if remaining < 0 {
		return float64(0)
	}
	return remaining
}

func userBalanceNullFloat64(value sql.NullFloat64) any {
	if !value.Valid {
		return nil
	}
	return value.Float64
}
