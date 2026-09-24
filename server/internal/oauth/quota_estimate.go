package oauth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/store"
)

const (
	quotaResetRemainingThreshold = 90.0
	quotaResetJumpThreshold      = 10.0
	quotaMinPercentDelta         = 1.0
	quotaExternalCostEpsilon     = 0.01
)

type quotaWindowObservation struct {
	UsedPercent      *float64
	RemainingPercent *float64
	ResetAt          *time.Time
	Present          bool
}

type QuotaEstimateWindowView struct {
	Status              string     `json:"status"`
	RemainingPercent    *float64   `json:"remaining_percent,omitempty"`
	EstimatedTotal      *float64   `json:"estimated_total,omitempty"`
	EstimatedRemaining  *float64   `json:"estimated_remaining,omitempty"`
	SpendRatePerHour    *float64   `json:"spend_rate_per_hour,omitempty"`
	ExhaustInSeconds    *int64     `json:"exhaust_in_seconds,omitempty"`
	ResetAt             *time.Time `json:"reset_at,omitempty"`
	ObservedFrom        *time.Time `json:"observed_from,omitempty"`
	ObservedTo          *time.Time `json:"observed_to,omitempty"`
	RequestCount        int64      `json:"request_count"`
	SystemCost          float64    `json:"system_cost"`
	SampleCount         int        `json:"sample_count"`
	CumulativeUsedPct   float64    `json:"cumulative_used_percent"`
	Confidence          string     `json:"confidence"`
	ExternalUsageHint   bool       `json:"external_usage_hint,omitempty"`
	PreviousTotal       *float64   `json:"previous_estimated_total,omitempty"`
	Currency            string     `json:"currency"`
}

type QuotaEstimateView struct {
	FiveHour *QuotaEstimateWindowView `json:"five_hour,omitempty"`
	Weekly   *QuotaEstimateWindowView `json:"weekly,omitempty"`
}

// RecordCodexQuotaSnapshot persists a usage snapshot and updates five_hour /
// weekly estimate rounds for the connection. Safe to call after every successful
// Codex quota sync; failures are returned so callers can log without failing refresh.
func (s *Service) RecordCodexQuotaSnapshot(ctx context.Context, connectionID uuid.UUID, siteID uuid.UUID, quota map[string]any, observedAt time.Time) (QuotaEstimateView, error) {
	if s.db == nil || connectionID == uuid.Nil || siteID == uuid.Nil || len(quota) == 0 {
		return QuotaEstimateView{}, nil
	}
	if observedAt.IsZero() {
		observedAt = time.Now()
	}
	repo := store.NewOAuthQuotaEstimateRepository(s.db.DB())

	fiveHour := parseQuotaWindowObservation(quota["five_hour"])
	weekly := parseQuotaWindowObservation(quota["weekly"])
	if !fiveHour.Present && !weekly.Present {
		return QuotaEstimateView{}, nil
	}

	available, _ := quota["available"].(bool)
	raw, _ := json.Marshal(quota)
	snapshot, err := repo.CreateSnapshot(ctx, store.CreateOAuthQuotaSnapshotParams{
		OAuthConnectionID:        connectionID,
		SiteID:                   siteID,
		ObservedAt:               observedAt,
		FiveHourUsedPercent:      floatPtrAny(fiveHour.UsedPercent),
		FiveHourRemainingPercent: floatPtrAny(fiveHour.RemainingPercent),
		FiveHourResetAt:          timePtrAny(fiveHour.ResetAt),
		WeeklyUsedPercent:        floatPtrAny(weekly.UsedPercent),
		WeeklyRemainingPercent:   floatPtrAny(weekly.RemainingPercent),
		WeeklyResetAt:            timePtrAny(weekly.ResetAt),
		Available:                available,
		RawQuota:                 store.JSON(raw),
	})
	if err != nil {
		return QuotaEstimateView{}, err
	}

	if fiveHour.Present {
		if err := s.updateQuotaWindowRound(ctx, repo, connectionID, siteID, store.OAuthQuotaWindowFiveHour, fiveHour, snapshot); err != nil {
			return QuotaEstimateView{}, err
		}
	}
	if weekly.Present {
		if err := s.updateQuotaWindowRound(ctx, repo, connectionID, siteID, store.OAuthQuotaWindowWeekly, weekly, snapshot); err != nil {
			return QuotaEstimateView{}, err
		}
	}
	return s.QuotaEstimateView(ctx, connectionID)
}

func (s *Service) QuotaEstimateView(ctx context.Context, connectionID uuid.UUID) (QuotaEstimateView, error) {
	if s.db == nil || connectionID == uuid.Nil {
		return QuotaEstimateView{}, nil
	}
	repo := store.NewOAuthQuotaEstimateRepository(s.db.DB())
	rounds, err := repo.ListRoundsByConnection(ctx, connectionID)
	if err != nil {
		return QuotaEstimateView{}, err
	}
	view := QuotaEstimateView{}
	byKey := map[string]store.OAuthQuotaWindowRound{}
	for _, round := range rounds {
		byKey[round.WindowType+"|"+round.Status] = round
	}
	if current, ok := byKey[store.OAuthQuotaWindowFiveHour+"|"+store.OAuthQuotaRoundCurrent]; ok {
		previous, _ := byKey[store.OAuthQuotaWindowFiveHour+"|"+store.OAuthQuotaRoundPrevious]
		view.FiveHour = quotaWindowView(current, previous)
		attachObservedTo(ctx, s.db.DB(), view.FiveHour, current)
	}
	if current, ok := byKey[store.OAuthQuotaWindowWeekly+"|"+store.OAuthQuotaRoundCurrent]; ok {
		previous, _ := byKey[store.OAuthQuotaWindowWeekly+"|"+store.OAuthQuotaRoundPrevious]
		view.Weekly = quotaWindowView(current, previous)
		attachObservedTo(ctx, s.db.DB(), view.Weekly, current)
	}
	return view, nil
}

func attachObservedTo(ctx context.Context, db *gorm.DB, view *QuotaEstimateWindowView, current store.OAuthQuotaWindowRound) {
	if view == nil || !current.LatestSnapshotID.Valid {
		return
	}
	snapshot, err := loadQuotaSnapshot(ctx, db, current.LatestSnapshotID.UUID)
	if err != nil {
		return
	}
	observedTo := snapshot.ObservedAt
	view.ObservedTo = &observedTo
}

func (s *Service) updateQuotaWindowRound(
	ctx context.Context,
	repo store.OAuthQuotaEstimateRepository,
	connectionID uuid.UUID,
	siteID uuid.UUID,
	windowType string,
	observation quotaWindowObservation,
	snapshot store.OAuthQuotaSnapshot,
) error {
	current, err := repo.GetRound(ctx, connectionID, windowType, store.OAuthQuotaRoundCurrent)
	if err != nil {
		if isNotFound(err) {
			_, createErr := repo.UpsertRound(ctx, newBaselineRound(connectionID, siteID, windowType, observation, snapshot))
			return createErr
		}
		return err
	}

	if quotaWindowResetDetected(current, observation, snapshot.ObservedAt) {
		return repo.RotateRoundOnReset(
			ctx,
			current,
			snapshot.ObservedAt,
			newBaselineRound(connectionID, siteID, windowType, observation, snapshot),
		)
	}

	from := current.StartedAt
	if current.LatestSnapshotID.Valid {
		prevSnapshot, snapErr := loadQuotaSnapshot(ctx, s.db.DB(), current.LatestSnapshotID.UUID)
		if snapErr == nil {
			from = prevSnapshot.ObservedAt
		} else if !current.UpdatedAt.IsZero() {
			from = current.UpdatedAt
		}
	}
	to := snapshot.ObservedAt
	costSummary, err := repo.SumSiteQuotaCost(ctx, siteID, from, to)
	if err != nil {
		return err
	}

	usedDelta := quotaUsedPercentDelta(current, observation)
	current.CumulativeSystemCost += costSummary.Cost
	current.RequestCount += costSummary.RequestCount
	if usedDelta > 0 {
		current.CumulativeUsedPercent += usedDelta
	}
	current.SampleCount++
	current.LatestUsedPercent = nullFloatFromPtr(observation.UsedPercent)
	current.LatestRemainingPercent = nullFloatFromPtr(observation.RemainingPercent)
	current.LatestResetAt = nullTimeFromPtr(observation.ResetAt)
	current.LatestSnapshotID = uuid.NullUUID{UUID: snapshot.ID, Valid: true}
	current.SiteID = siteID

	if current.CumulativeUsedPercent >= quotaMinPercentDelta && current.CumulativeSystemCost > 0 {
		total := current.CumulativeSystemCost / (current.CumulativeUsedPercent / 100.0)
		current.EstimatedTotal = sql.NullFloat64{Float64: total, Valid: true}
		if current.CumulativeUsedPercent >= 5 && current.RequestCount >= 3 {
			current.Confidence = store.OAuthQuotaConfidenceReady
		} else {
			current.Confidence = store.OAuthQuotaConfidenceLowSample
		}
	} else if current.SampleCount > 0 {
		current.Confidence = store.OAuthQuotaConfidenceSampling
	}

	// Hint only: percent dropped with essentially no system spend.
	if usedDelta >= quotaMinPercentDelta && costSummary.Cost < quotaExternalCostEpsilon {
		current.ExternalUsageHint = true
	}

	_, err = repo.SaveRound(ctx, current)
	return err
}

func loadQuotaSnapshot(ctx context.Context, db *gorm.DB, id uuid.UUID) (store.OAuthQuotaSnapshot, error) {
	var item store.OAuthQuotaSnapshot
	if err := db.WithContext(ctx).Where(&store.OAuthQuotaSnapshot{ID: id}).First(&item).Error; err != nil {
		return store.OAuthQuotaSnapshot{}, err
	}
	return item, nil
}

func isNotFound(err error) bool {
	for err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return true
		}
		err = errors.Unwrap(err)
	}
	return false
}

func newBaselineRound(
	connectionID uuid.UUID,
	siteID uuid.UUID,
	windowType string,
	observation quotaWindowObservation,
	snapshot store.OAuthQuotaSnapshot,
) store.OAuthQuotaWindowRound {
	return store.OAuthQuotaWindowRound{
		OAuthConnectionID:      connectionID,
		SiteID:                 siteID,
		WindowType:             windowType,
		Status:                 store.OAuthQuotaRoundCurrent,
		StartedAt:              snapshot.ObservedAt,
		StartUsedPercent:       nullFloatFromPtr(observation.UsedPercent),
		LatestUsedPercent:      nullFloatFromPtr(observation.UsedPercent),
		StartRemainingPercent:  nullFloatFromPtr(observation.RemainingPercent),
		LatestRemainingPercent: nullFloatFromPtr(observation.RemainingPercent),
		StartResetAt:           nullTimeFromPtr(observation.ResetAt),
		LatestResetAt:          nullTimeFromPtr(observation.ResetAt),
		StartSnapshotID:        uuid.NullUUID{UUID: snapshot.ID, Valid: true},
		LatestSnapshotID:       uuid.NullUUID{UUID: snapshot.ID, Valid: true},
		Confidence:             store.OAuthQuotaConfidenceBaseline,
		Currency:               "USD",
	}
}

func quotaWindowResetDetected(current store.OAuthQuotaWindowRound, observation quotaWindowObservation, observedAt time.Time) bool {
	if observation.RemainingPercent == nil {
		return false
	}
	remaining := *observation.RemainingPercent
	previousRemaining := 0.0
	hasPrevious := false
	if current.LatestRemainingPercent.Valid {
		previousRemaining = current.LatestRemainingPercent.Float64
		hasPrevious = true
	}
	if !hasPrevious {
		return false
	}
	jumpedUp := remaining > previousRemaining+quotaResetJumpThreshold
	nearFull := remaining >= quotaResetRemainingThreshold
	if !(jumpedUp && nearFull) {
		return false
	}
	if current.LatestResetAt.Valid {
		if observation.ResetAt != nil && observation.ResetAt.After(current.LatestResetAt.Time) {
			return true
		}
		if !observedAt.Before(current.LatestResetAt.Time) {
			return true
		}
	}
	// No prior reset_at: treat a clear refill as a new window.
	return jumpedUp && nearFull
}

func quotaUsedPercentDelta(current store.OAuthQuotaWindowRound, observation quotaWindowObservation) float64 {
	if observation.UsedPercent != nil && current.LatestUsedPercent.Valid {
		delta := *observation.UsedPercent - current.LatestUsedPercent.Float64
		if delta > 0 {
			return delta
		}
	}
	if observation.RemainingPercent != nil && current.LatestRemainingPercent.Valid {
		delta := current.LatestRemainingPercent.Float64 - *observation.RemainingPercent
		if delta > 0 {
			return delta
		}
	}
	return 0
}

func quotaWindowView(current store.OAuthQuotaWindowRound, previous store.OAuthQuotaWindowRound) *QuotaEstimateWindowView {
	view := &QuotaEstimateWindowView{
		Status:            current.Confidence,
		RequestCount:      current.RequestCount,
		SystemCost:        current.CumulativeSystemCost,
		SampleCount:       current.SampleCount,
		CumulativeUsedPct: current.CumulativeUsedPercent,
		Confidence:        current.Confidence,
		ExternalUsageHint: current.ExternalUsageHint,
		Currency:          defaultCurrency(current.Currency),
		ObservedFrom:      timePtr(current.StartedAt),
	}
	if current.LatestRemainingPercent.Valid {
		value := current.LatestRemainingPercent.Float64
		view.RemainingPercent = &value
	}
	if current.LatestResetAt.Valid {
		value := current.LatestResetAt.Time
		view.ResetAt = &value
	}
	if current.LatestSnapshotID.Valid {
		// ObservedTo approximated by UpdatedAt of the round.
		value := current.UpdatedAt
		if !value.IsZero() {
			view.ObservedTo = &value
		}
	}
	if current.EstimatedTotal.Valid && current.EstimatedTotal.Float64 > 0 {
		total := current.EstimatedTotal.Float64
		view.EstimatedTotal = &total
		if view.RemainingPercent != nil {
			remaining := total * (*view.RemainingPercent / 100.0)
			view.EstimatedRemaining = &remaining
		}
	}
	if previous.ID != uuid.Nil && previous.EstimatedTotal.Valid && previous.EstimatedTotal.Float64 > 0 {
		value := previous.EstimatedTotal.Float64
		view.PreviousTotal = &value
	}

	elapsed := current.UpdatedAt.Sub(current.StartedAt)
	if elapsed <= 0 && view.ObservedTo != nil {
		elapsed = view.ObservedTo.Sub(current.StartedAt)
	}
	if elapsed > time.Minute && current.CumulativeSystemCost > 0 {
		rate := current.CumulativeSystemCost / elapsed.Hours()
		view.SpendRatePerHour = &rate
		if view.EstimatedRemaining != nil && rate > 0 {
			hours := *view.EstimatedRemaining / rate
			seconds := int64(math.Round(hours * 3600))
			if seconds > 0 {
				view.ExhaustInSeconds = &seconds
			}
		}
	}
	return view
}

func parseQuotaWindowObservation(raw any) quotaWindowObservation {
	window, ok := raw.(map[string]any)
	if !ok || len(window) == 0 {
		return quotaWindowObservation{}
	}
	obs := quotaWindowObservation{Present: true}
	if used, ok := floatFromAny(window["used_percent"]); ok {
		obs.UsedPercent = &used
	}
	if remaining, ok := floatFromAny(window["remaining_percent"]); ok {
		obs.RemainingPercent = &remaining
	} else if obs.UsedPercent != nil {
		remaining := 100 - *obs.UsedPercent
		obs.RemainingPercent = &remaining
	}
	if resetAt := parseQuotaResetTime(window["reset_at"]); resetAt != nil {
		obs.ResetAt = resetAt
	}
	return obs
}

func parseQuotaResetTime(value any) *time.Time {
	switch v := value.(type) {
	case nil:
		return nil
	case time.Time:
		if v.IsZero() {
			return nil
		}
		return &v
	case int64:
		if v <= 0 {
			return nil
		}
		t := time.Unix(v, 0).UTC()
		return &t
	case int:
		if v <= 0 {
			return nil
		}
		t := time.Unix(int64(v), 0).UTC()
		return &t
	case float64:
		if v <= 0 {
			return nil
		}
		t := time.Unix(int64(v), 0).UTC()
		return &t
	case string:
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			if parsed, err := time.Parse(time.RFC3339, trimmed); err == nil {
				return &parsed
			}
		}
		return nil
	default:
		return nil
	}
}

func floatFromAny(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

func floatPtrAny(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

func timePtrAny(value *time.Time) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullFloatFromPtr(value *float64) sql.NullFloat64 {
	if value == nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: *value, Valid: true}
}

func nullTimeFromPtr(value *time.Time) sql.NullTime {
	if value == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *value, Valid: true}
}

func timePtr(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}

func defaultCurrency(value string) string {
	if value == "" {
		return "USD"
	}
	return value
}

