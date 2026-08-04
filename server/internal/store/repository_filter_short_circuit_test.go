package store

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestAdminAuditLogDirectFiltersMatchEveryDimensionAndInclusiveBounds(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, 6, 22, 10, 30, 0, 0, time.UTC)
	success := true
	item := AdminAuditLog{
		Action:    "site.update",
		ActorType: "admin",
		Success:   true,
		CreatedAt: createdAt,
	}

	if !adminAuditLogMatches(item, AdminAuditLogFilters{
		Action:    "site.update",
		ActorType: "admin",
		Success:   &success,
		DateFrom:  &createdAt,
		DateTo:    &createdAt,
	}) {
		t.Fatal("exact matching filters and equal date bounds should keep the audit log")
	}

	mismatchCases := []struct {
		name    string
		filters AdminAuditLogFilters
	}{
		{name: "action differs", filters: AdminAuditLogFilters{Action: "site.delete"}},
		{name: "actor type differs", filters: AdminAuditLogFilters{ActorType: "system"}},
		{name: "success flag differs", filters: AdminAuditLogFilters{Success: filterBool(false)}},
		{name: "created after lower bound", filters: AdminAuditLogFilters{DateFrom: filterTime(createdAt.Add(time.Nanosecond))}},
		{name: "created before upper bound", filters: AdminAuditLogFilters{DateTo: filterTime(createdAt.Add(-time.Nanosecond))}},
	}
	assertAdminAuditLogRejectedByFilters(t, item, mismatchCases)
}

func TestOAuthConnectionDefaultStringOnlyReplacesEmptyValue(t *testing.T) {
	t.Parallel()

	if got := defaultOAuthConnectionString("", "connected"); got != "connected" {
		t.Fatalf("empty value should use fallback, got %q", got)
	}
	if got := defaultOAuthConnectionString("revoked", "connected"); got != "revoked" {
		t.Fatalf("non-empty value should be kept, got %q", got)
	}
	if got := defaultOAuthConnectionString("  ", "Bearer"); got != "  " {
		t.Fatalf("whitespace-only value is still non-empty and should be kept, got %q", got)
	}
}

func TestAPIKeyAccessSiteModelDetailMapsEmptyInputSkipsDB(t *testing.T) {
	t.Parallel()

	for _, ids := range [][]uuid.UUID{nil, {}} {
		siteModels, sites, canonicalModels, err := NewAPIKeyAccessRepository(nil).siteModelDetailMaps(t.Context(), ids)
		if err != nil {
			t.Fatalf("empty site model details should not fail: %v", err)
		}
		if siteModels == nil || sites == nil || canonicalModels == nil {
			t.Fatalf("expected initialized maps, got siteModels=%#v sites=%#v canonicalModels=%#v", siteModels, sites, canonicalModels)
		}
		if len(siteModels) != 0 || len(sites) != 0 || len(canonicalModels) != 0 {
			t.Fatalf("expected empty maps, got siteModels=%#v sites=%#v canonicalModels=%#v", siteModels, sites, canonicalModels)
		}
	}
}

func TestAPIKeyAccessSiteModelDetailMapsDeduplicatesAndBatchesIDs(t *testing.T) {
	t.Parallel()

	db := storeRepositoryOfflineGorm(t)
	queryCount := 0
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		items, ok := tx.Statement.Dest.(*[]SiteModel)
		if !ok {
			t.Fatalf("unexpected site model detail query destination %T", tx.Statement.Dest)
		}
		queryCount++
		*items = nil
		tx.RowsAffected = 0
		tx.Statement.RowsAffected = 0
	})

	uniqueIDs := make([]uuid.UUID, 0, apiKeyAccessDetailBatchSize+1)
	for range apiKeyAccessDetailBatchSize + 1 {
		uniqueIDs = append(uniqueIDs, uuid.New())
	}
	ids := make([]uuid.UUID, 0, len(uniqueIDs)*2)
	for _, id := range uniqueIDs {
		ids = append(ids, id, id)
	}

	_, _, _, err := NewAPIKeyAccessRepository(db).siteModelDetailMaps(t.Context(), ids)
	if err != nil {
		t.Fatalf("site model detail batching failed: %v", err)
	}
	if queryCount != 2 {
		t.Fatalf("query count = %d, want 2 deduplicated batches", queryCount)
	}
}

func TestRequestLogSearchAndModelFiltersBlankInputSkipDB(t *testing.T) {
	t.Parallel()

	repo := NewRequestLogRepository(nil)
	for _, value := range []string{"", " \t\n "} {
		ids, err := repo.requestLogSearchIDs(t.Context(), value)
		if err != nil {
			t.Fatalf("blank requestLogSearchIDs should not access repository: %v", err)
		}
		if len(ids.SiteIDs) != 0 || len(ids.CanonicalModelIDs) != 0 || len(ids.SiteModelIDs) != 0 {
			t.Fatalf("blank search ids = %#v, want empty", ids)
		}

		expr, err := repo.requestLogModelFilterExpression(t.Context(), value)
		if err != nil {
			t.Fatalf("blank model filter expression: %v", err)
		}
		assertEqClause(t, expr, "id", uuid.Nil)
	}
}

func assertAdminAuditLogRejectedByFilters(t *testing.T, item AdminAuditLog, cases []struct {
	name    string
	filters AdminAuditLogFilters
}) {
	t.Helper()

	for _, tc := range cases {
		if adminAuditLogMatches(item, tc.filters) {
			t.Fatalf("%s should reject the audit log", tc.name)
		}
	}
}

func filterBool(value bool) *bool {
	return &value
}

func filterTime(value time.Time) *time.Time {
	return &value
}
