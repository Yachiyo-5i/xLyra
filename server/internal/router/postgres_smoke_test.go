package router

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/config"
	"xlyra/server/internal/store"
)

func TestDevPostgresRouterReadOnlySmoke(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg, err := devPostgresRouterSmokeConfig()
	if err != nil {
		t.Skipf("dev PostgreSQL smoke disabled: %v", err)
	}

	db, err := store.Open(ctx, cfg)
	if err != nil {
		t.Skipf("dev PostgreSQL unavailable: %s", redactRouterDatabaseOpenError(err, cfg))
	}
	defer db.Close()

	service := NewService(db)
	missingKey := "missing-router-smoke-" + uuid.NewString()
	if _, err := service.Candidates(ctx, CandidateQuery{ModelKey: missingKey}); err == nil {
		t.Fatal("expected missing model candidates lookup to fail")
	}
	if _, err := service.Select(ctx, CandidateQuery{ModelKey: missingKey}); err == nil {
		t.Fatal("expected missing model select lookup to fail")
	}
	if _, err := service.Plan(ctx, CandidateQuery{ModelKey: missingKey}); err == nil {
		t.Fatal("expected missing model plan lookup to fail")
	}

	modelKey, candidates := routerSmokeCandidates(t, ctx, db, service)
	assertRouterSmokeCandidateListStable(t, candidates)

	selection, err := service.Select(ctx, CandidateQuery{ModelKey: modelKey, EndpointType: "openai"})
	if err != nil {
		t.Fatalf("select route candidate: %v", err)
	}
	assertRouterSmokeSelectionStable(t, selection)
	if selection.CanonicalModel.ID != candidates.CanonicalModel.ID {
		t.Fatalf("selection canonical id = %s, want %s", selection.CanonicalModel.ID, candidates.CanonicalModel.ID)
	}
	if selection.Candidate.Model.SiteModelID != candidates.Items[0].Model.SiteModelID {
		t.Fatalf("selected model id = %s, want top candidate %s", selection.Candidate.Model.SiteModelID, candidates.Items[0].Model.SiteModelID)
	}

	plan, err := service.Plan(ctx, CandidateQuery{ModelKey: modelKey, EndpointType: "openai", FailoverLimit: 2})
	if err != nil {
		t.Fatalf("plan route candidates: %v", err)
	}
	assertRouterSmokePlanStable(t, plan)
	if plan.Selected.Model.SiteModelID != selection.Candidate.Model.SiteModelID {
		t.Fatalf("plan selected model id = %s, want selected %s", plan.Selected.Model.SiteModelID, selection.Candidate.Model.SiteModelID)
	}
	if len(plan.Failover) > 2 {
		t.Fatalf("failover length = %d, want at most 2", len(plan.Failover))
	}

	excluded := make([]uuid.UUID, 0, len(candidates.Items))
	for _, item := range candidates.Items {
		excluded = append(excluded, item.Model.SiteModelID)
	}
	if _, err := service.Select(ctx, CandidateQuery{ModelKey: modelKey, ExcludeSiteModelIDs: excluded}); !errors.Is(err, ErrNoRouteCandidates) {
		t.Fatalf("select with all candidates excluded error = %v, want ErrNoRouteCandidates", err)
	}
	if _, err := service.Plan(ctx, CandidateQuery{ModelKey: modelKey, ExcludeSiteModelIDs: excluded}); !errors.Is(err, ErrNoRouteCandidates) {
		t.Fatalf("plan with all candidates excluded error = %v, want ErrNoRouteCandidates", err)
	}
}

func routerSmokeCandidates(t *testing.T, ctx context.Context, db *store.Store, service *Service) (string, CandidateList) {
	t.Helper()

	models, err := store.NewCanonicalModelRepository(db.DB()).List(ctx)
	if err != nil {
		t.Fatalf("list canonical models for router smoke: %v", err)
	}
	for _, model := range models {
		if model.ID == uuid.Nil || strings.TrimSpace(model.ModelKey) == "" {
			continue
		}
		candidates, err := service.Candidates(ctx, CandidateQuery{
			ModelKey:     model.ModelKey,
			EndpointType: "openai",
			Debug:        true,
		})
		if err != nil {
			continue
		}
		if len(candidates.Items) > 0 {
			return model.ModelKey, candidates
		}
	}
	t.Skip("dev PostgreSQL has no available route candidates")
	return "", CandidateList{}
}

func assertRouterSmokeCandidateListStable(t *testing.T, candidates CandidateList) {
	t.Helper()

	if candidates.CanonicalModel.ID == uuid.Nil || strings.TrimSpace(candidates.CanonicalModel.ModelKey) == "" {
		t.Fatalf("canonical model is not populated: %#v", candidates.CanonicalModel)
	}
	if len(candidates.Items) == 0 {
		t.Fatal("candidate list is empty")
	}
	previousPriority := candidates.Items[0].Site.RoutingPriority
	previousScore := candidates.Items[0].Score
	for index, item := range candidates.Items {
		if item.Rank != index+1 {
			t.Fatalf("candidate rank at index %d = %d, want %d", index, item.Rank, index+1)
		}
		if index > 0 && item.Site.RoutingPriority > previousPriority {
			t.Fatalf("candidate priorities are not descending at index %d: %v > %v", index, item.Site.RoutingPriority, previousPriority)
		}
		if index > 0 && item.Site.RoutingPriority == previousPriority && item.Score > previousScore {
			t.Fatalf("candidate scores are not descending within priority at index %d: %v > %v", index, item.Score, previousScore)
		}
		previousPriority = item.Site.RoutingPriority
		previousScore = item.Score
		assertRouterSmokeCandidateStable(t, item)
		if len(item.ScoreBreakdown) == 0 {
			t.Fatalf("debug candidate is missing score breakdown: %#v", item)
		}
	}
}

func assertRouterSmokeSelectionStable(t *testing.T, selection Selection) {
	t.Helper()

	if selection.CanonicalModel.ID == uuid.Nil {
		t.Fatalf("selection canonical id is nil: %#v", selection)
	}
	assertRouterSmokeCandidateStable(t, selection.Candidate)
}

func assertRouterSmokePlanStable(t *testing.T, plan SelectionPlan) {
	t.Helper()

	if plan.CanonicalModel.ID == uuid.Nil {
		t.Fatalf("plan canonical id is nil: %#v", plan)
	}
	assertRouterSmokeCandidateStable(t, plan.Selected)
	for _, item := range plan.Failover {
		assertRouterSmokeCandidateStable(t, item)
	}
}

func assertRouterSmokeCandidateStable(t *testing.T, item Candidate) {
	t.Helper()

	if item.Site.ID == uuid.Nil || item.Model.SiteModelID == uuid.Nil {
		t.Fatalf("candidate has nil ids: %#v", item)
	}
	if strings.TrimSpace(item.Site.Name) == "" || strings.TrimSpace(item.Model.UpstreamName) == "" {
		t.Fatalf("candidate has blank route names: %#v", item)
	}
	if item.Availability.AvailableAPIKeys <= 0 {
		t.Fatalf("candidate has no available api keys: %#v", item.Availability)
	}
	if item.Availability.TotalAPIKeys < item.Availability.AvailableAPIKeys {
		t.Fatalf("candidate total api keys is less than available: %#v", item.Availability)
	}
}

func devPostgresRouterSmokeConfig() (config.Config, error) {
	cfg := config.Config{
		AppEnv:           "test",
		DBConnectTimeout: 2 * time.Second,
		DBMinConns:       0,
		DBMaxConns:       2,
		PostgresDSN:      strings.TrimSpace(os.Getenv("POSTGRES_DSN")),
		DBHost:           routerGetenvDefault("DB_HOST", "127.0.0.1"),
		DBPort:           5432,
		DBName:           routerGetenvDefault("DB_NAME", "xlyra"),
		DBUser:           routerGetenvDefault("DB_USER", "postgres"),
		DBPassword:       routerGetenvDefault("DB_PASSWORD", "postgres"),
		DBSSLMode:        routerGetenvDefault("DB_SSLMODE", "disable"),
	}
	if value := strings.TrimSpace(os.Getenv("DB_PORT")); value != "" {
		port, err := strconv.Atoi(value)
		if err != nil {
			return config.Config{}, fmt.Errorf("invalid DB_PORT %q", value)
		}
		cfg.DBPort = port
	}
	return cfg, nil
}

func routerGetenvDefault(key string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func redactRouterDatabaseOpenError(err error, cfg config.Config) string {
	msg := err.Error()
	secrets := []string{cfg.DBPassword}
	if parsed, parseErr := url.Parse(strings.TrimSpace(cfg.PostgresDSN)); parseErr == nil && parsed.User != nil {
		if password, ok := parsed.User.Password(); ok {
			secrets = append(secrets, password)
		}
	}
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		msg = strings.ReplaceAll(msg, secret, "[redacted]")
		msg = strings.ReplaceAll(msg, url.QueryEscape(secret), "[redacted]")
		msg = strings.ReplaceAll(msg, url.PathEscape(secret), "[redacted]")
	}
	return msg
}
