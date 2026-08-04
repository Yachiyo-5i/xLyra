package site

import (
	"context"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/adapter"
	"xlyra/server/internal/store"
)

// recoverCodexQuotaCooldown lifts a credential's Codex quota cooldown as soon as a
// refresh observes usable quota again — whether OpenAI auto-reset the windows or the
// user redeemed a reset credit. Only quota-reason cooldowns are cleared; auth,
// capacity, and transport cooldowns are left untouched. Best-effort.
func (s *Service) recoverCodexQuotaCooldown(ctx context.Context, siteID uuid.UUID, credentialID uuid.UUID, usage any) {
	if credentialID == uuid.Nil {
		return
	}
	if !adapter.CodexUsageAvailable(usage) {
		return
	}
	_, _ = store.NewRouteCooldownRepository(s.db.DB()).ClearActiveMatching(ctx, store.ClearActiveCooldownFilter{
		SiteID:           siteID,
		SiteCredentialID: uuid.NullUUID{UUID: credentialID, Valid: true},
		Reasons:          store.CodexQuotaCooldownReasons(),
	})
}

// primeCodexWindow sends one minimal request when the account's 5-hour window is idle
// (reset but not yet counting), so the 5-hour countdown starts immediately instead of
// on the user's next real request. Best-effort and gated by config; the natural
// idempotency is that once primed the window reports a future reset_at and subsequent
// refreshes no longer match.
func (s *Service) primeCodexWindow(ctx context.Context, adapterSite adapter.SiteConfig, auth adapter.SystemAuth, usage any, models []refreshKeyModel) {
	if !adapter.CodexShouldPrimeFiveHour(usage, time.Now()) {
		return
	}
	model := firstCodexPrimeModel(models)
	if model == "" {
		return
	}
	primeCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	_ = adapter.NewCodex().PrimeUsageWindow(primeCtx, adapterSite, auth, model)
}

func firstCodexPrimeModel(models []refreshKeyModel) string {
	for _, item := range models {
		if name := item.model.UpstreamName; name != "" {
			return name
		}
	}
	return ""
}
