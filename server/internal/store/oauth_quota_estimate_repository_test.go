package store

import (
	"context"
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestOAuthQuotaEstimateSumSiteQuotaCostAndRotate(t *testing.T) {
	db, cfg, cleanup := openTemporaryMigrationStore(t)
	defer cleanup()
	ctx := context.Background()
	if err := ensureDatabaseInitializedOnce(ctx, cfg); err != nil {
		t.Fatalf("initialize schema: %v", err)
	}

	repo := NewOAuthQuotaEstimateRepository(db)
	site := createQuotaEstimateTestSite(t, ctx, db)
	connection := createQuotaEstimateTestConnection(t, ctx, db, site.ID)

	from := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	mid := from.Add(30 * time.Minute)
	to := from.Add(time.Hour)

	// Token pricing: prefer base_estimated_cost over usage_records.estimated_cost.
	createQuotaEstimatePricedRequest(t, ctx, db, site.ID, mid.Add(-time.Minute), map[string]any{
		"cost_calculation": map[string]any{
			"formula":             "input_value",
			"base_estimated_cost": 1.5,
			"group_ratio":         2.0,
		},
	}, 9.9)

	// per_request with null per_request_value: strip baked-in group_ratio.
	createQuotaEstimatePricedRequest(t, ctx, db, site.ID, mid, map[string]any{
		"cost_calculation": map[string]any{
			"formula":             "per_request_value",
			"base_estimated_cost": 4.0,
			"group_ratio":         2.0,
			"per_request_value":   nil,
		},
	}, 4.0)

	// Outside window: ignored.
	createQuotaEstimatePricedRequest(t, ctx, db, site.ID, to.Add(time.Minute), map[string]any{
		"cost_calculation": map[string]any{
			"base_estimated_cost": 100.0,
		},
	}, 100.0)

	summary, err := repo.SumSiteQuotaCost(ctx, site.ID, from, to)
	if err != nil {
		t.Fatalf("SumSiteQuotaCost: %v", err)
	}
	// 1.5 (token base) + 4.0/2.0 (per_request stripped) = 3.5
	if math.Abs(summary.Cost-3.5) > 1e-9 {
		t.Fatalf("cost = %v, want 3.5", summary.Cost)
	}
	if summary.RequestCount != 2 {
		t.Fatalf("request_count = %d, want 2", summary.RequestCount)
	}

	empty, err := repo.SumSiteQuotaCost(ctx, site.ID, to, to)
	if err != nil {
		t.Fatalf("empty window SumSiteQuotaCost: %v", err)
	}
	if empty.Cost != 0 || empty.RequestCount != 0 {
		t.Fatalf("empty window summary = %#v", empty)
	}

	started := from
	snapshot, err := repo.CreateSnapshot(ctx, CreateOAuthQuotaSnapshotParams{
		OAuthConnectionID:        connection.ID,
		SiteID:                   site.ID,
		ObservedAt:               started,
		FiveHourUsedPercent:      10.0,
		FiveHourRemainingPercent: 90.0,
		Available:                true,
		RawQuota:                 JSON(`{"five_hour":{"used_percent":10,"remaining_percent":90}}`),
	})
	if err != nil {
		t.Fatalf("CreateSnapshot baseline: %v", err)
	}

	current, err := repo.UpsertRound(ctx, OAuthQuotaWindowRound{
		OAuthConnectionID:      connection.ID,
		SiteID:                 site.ID,
		WindowType:             OAuthQuotaWindowFiveHour,
		Status:                 OAuthQuotaRoundCurrent,
		StartedAt:              started,
		StartUsedPercent:       nullFloatFromAny(10.0),
		LatestUsedPercent:      nullFloatFromAny(10.0),
		StartRemainingPercent:  nullFloatFromAny(90.0),
		LatestRemainingPercent: nullFloatFromAny(90.0),
		StartSnapshotID:        uuid.NullUUID{UUID: snapshot.ID, Valid: true},
		LatestSnapshotID:       uuid.NullUUID{UUID: snapshot.ID, Valid: true},
		CumulativeSystemCost:   3.5,
		CumulativeUsedPercent:  5,
		RequestCount:           2,
		SampleCount:            1,
		EstimatedTotal:         nullFloatFromAny(70.0),
		Confidence:             OAuthQuotaConfidenceReady,
		Currency:               "USD",
	})
	if err != nil {
		t.Fatalf("UpsertRound current: %v", err)
	}

	resetAt := started.Add(5 * time.Hour)
	nextSnapshot, err := repo.CreateSnapshot(ctx, CreateOAuthQuotaSnapshotParams{
		OAuthConnectionID:        connection.ID,
		SiteID:                   site.ID,
		ObservedAt:               to,
		FiveHourUsedPercent:      0.0,
		FiveHourRemainingPercent: 100.0,
		FiveHourResetAt:          resetAt,
		Available:                true,
		RawQuota:                 JSON(`{"five_hour":{"used_percent":0,"remaining_percent":100}}`),
	})
	if err != nil {
		t.Fatalf("CreateSnapshot reset: %v", err)
	}

	baseline := OAuthQuotaWindowRound{
		OAuthConnectionID:      connection.ID,
		SiteID:                 site.ID,
		WindowType:             OAuthQuotaWindowFiveHour,
		Status:                 OAuthQuotaRoundCurrent,
		StartedAt:              to,
		StartUsedPercent:       nullFloatFromAny(0.0),
		LatestUsedPercent:      nullFloatFromAny(0.0),
		StartRemainingPercent:  nullFloatFromAny(100.0),
		LatestRemainingPercent: nullFloatFromAny(100.0),
		StartResetAt:           nullTimeFromAny(resetAt),
		LatestResetAt:          nullTimeFromAny(resetAt),
		StartSnapshotID:        uuid.NullUUID{UUID: nextSnapshot.ID, Valid: true},
		LatestSnapshotID:       uuid.NullUUID{UUID: nextSnapshot.ID, Valid: true},
		Confidence:             OAuthQuotaConfidenceBaseline,
		Currency:               "USD",
	}
	if err := repo.RotateRoundOnReset(ctx, current, to, baseline); err != nil {
		t.Fatalf("RotateRoundOnReset: %v", err)
	}

	rounds, err := repo.ListRoundsByConnection(ctx, connection.ID)
	if err != nil {
		t.Fatalf("ListRoundsByConnection: %v", err)
	}
	if len(rounds) != 2 {
		t.Fatalf("rounds = %d, want 2", len(rounds))
	}

	var previousRound, currentRound *OAuthQuotaWindowRound
	for index := range rounds {
		switch rounds[index].Status {
		case OAuthQuotaRoundPrevious:
			previousRound = &rounds[index]
		case OAuthQuotaRoundCurrent:
			currentRound = &rounds[index]
		}
	}
	if previousRound == nil || currentRound == nil {
		t.Fatalf("missing round status after rotate: %#v", rounds)
	}
	if !previousRound.EstimatedTotal.Valid || previousRound.EstimatedTotal.Float64 != 70 {
		t.Fatalf("previous estimated_total = %#v, want 70", previousRound.EstimatedTotal)
	}
	if previousRound.ID != current.ID {
		t.Fatalf("previous id = %s, want promoted %s", previousRound.ID, current.ID)
	}
	if currentRound.Confidence != OAuthQuotaConfidenceBaseline {
		t.Fatalf("current confidence = %q, want baseline", currentRound.Confidence)
	}
	if currentRound.CumulativeSystemCost != 0 || currentRound.CumulativeUsedPercent != 0 {
		t.Fatalf("new baseline should reset cumulatives: %#v", currentRound)
	}
}

func createQuotaEstimateTestSite(t *testing.T, ctx context.Context, db *gorm.DB) Site {
	t.Helper()
	site, err := NewSiteRepository(db).Create(ctx, CreateSiteParams{
		Name:            "Quota Estimate Test",
		Slug:            "quota-estimate-" + uuid.NewString(),
		SiteType:        "codex",
		BaseURL:         "https://example.invalid",
		Status:          "active",
		Enabled:         true,
		RoutingPriority: 1,
	})
	if err != nil {
		t.Fatalf("create site: %v", err)
	}
	return site
}

func createQuotaEstimateTestConnection(t *testing.T, ctx context.Context, db *gorm.DB, siteID uuid.UUID) OAuthConnection {
	t.Helper()
	siteIDCopy := siteID
	item := OAuthConnection{
		Provider:          "codex",
		SiteID:            &siteIDCopy,
		Status:            "active",
		AccountID:         "acct-" + uuid.NewString(),
		Email:             "quota-estimate-" + uuid.NewString() + "@example.com",
		MaskedAccessToken: "sk-...test",
		RawProfile:        JSON(`{}`),
		Metadata:          JSON(`{}`),
	}
	if err := db.WithContext(ctx).Create(&item).Error; err != nil {
		t.Fatalf("create oauth connection: %v", err)
	}
	return item
}

func createQuotaEstimatePricedRequest(
	t *testing.T,
	ctx context.Context,
	db *gorm.DB,
	siteID uuid.UUID,
	createdAt time.Time,
	metadata map[string]any,
	estimatedCost float64,
) {
	t.Helper()
	raw, err := json.Marshal(metadata)
	if err != nil {
		t.Fatalf("marshal request metadata: %v", err)
	}
	requestLogID := uuid.New()
	if err := db.WithContext(ctx).Exec(`
		INSERT INTO request_logs (
			id, request_id, site_id, endpoint, status_code, success, metadata, created_at
		) VALUES (?, ?, ?, ?, 200, TRUE, ?::jsonb, ?)
	`, requestLogID, "quota-estimate-"+uuid.NewString(), siteID, "/v1/responses", string(raw), createdAt).Error; err != nil {
		t.Fatalf("insert request log: %v", err)
	}
	if err := db.WithContext(ctx).Exec(`
		INSERT INTO usage_records (
			id, request_log_id, site_id, prompt_tokens, completion_tokens, total_tokens, estimated_cost, currency, created_at
		) VALUES (?, ?, ?, 10, 0, 10, ?, 'USD', ?)
	`, uuid.New(), requestLogID, siteID, estimatedCost, createdAt).Error; err != nil {
		t.Fatalf("insert usage record: %v", err)
	}
}
