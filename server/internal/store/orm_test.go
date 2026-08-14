package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestCredentialUsableReadsJSONMetaDefaults(t *testing.T) {
	if !credentialUsable(SiteCredential{Meta: JSON(`{}`)}) {
		t.Fatal("empty metadata should default to usable")
	}
	if !credentialUsable(SiteCredential{Meta: JSON(`{"enabled":"true","raw_key_missing":"false"}`)}) {
		t.Fatal("string boolean metadata should allow usable credentials")
	}
	if credentialUsable(SiteCredential{Meta: JSON(`{"enabled":false}`)}) {
		t.Fatal("disabled credential should not be usable")
	}
	if !credentialUsable(SiteCredential{Meta: JSON(`{"enabled":1}`)}) {
		t.Fatal("unsupported enabled metadata type should fall back to usable")
	}
	if credentialUsable(SiteCredential{Meta: JSON(`{"raw_key_missing":true}`)}) {
		t.Fatal("credential missing raw key should not be usable")
	}
}

func TestSiteCredentialRoutingConfigDefaultsInvalidValues(t *testing.T) {
	t.Parallel()

	for _, value := range []float64{0, 5.1, math.NaN(), math.Inf(1)} {
		if got := SiteCredentialRoutingPriority(SiteCredential{RoutingPriority: value}); got != 1 {
			t.Fatalf("routing priority %v normalized to %v, want 1", value, got)
		}
	}
	for _, value := range []float64{0, 0.001, 100.1, math.NaN(), math.Inf(-1)} {
		if got := SiteCredentialUpstreamCostMultiplier(SiteCredential{UpstreamCostMultiplier: value}); got != 1 {
			t.Fatalf("upstream cost multiplier %v normalized to %v, want 1", value, got)
		}
	}
}

func TestSortGatewayCredentialsUsesPriorityQuotaAndStableTies(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	modelUpdatedAt := createdAt.Add(time.Hour)
	highPriorityID := uuid.MustParse("00000000-0000-0000-0000-000000000005")
	unlimitedID := uuid.MustParse("00000000-0000-0000-0000-000000000004")
	positiveID := uuid.MustParse("00000000-0000-0000-0000-000000000003")
	unknownID := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	stableID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	items := []GatewayCredential{
		{Credential: SiteCredential{ID: unknownID, RoutingPriority: 3, CreatedAt: createdAt}, State: SiteAPIKeyState{SiteCredentialID: unknownID}, ModelUpdatedAt: modelUpdatedAt},
		{Credential: SiteCredential{ID: positiveID, RoutingPriority: 3, CreatedAt: createdAt}, State: SiteAPIKeyState{SiteCredentialID: positiveID, RemainQuota: sql.NullInt64{Int64: 20, Valid: true}}, ModelUpdatedAt: modelUpdatedAt},
		{Credential: SiteCredential{ID: unlimitedID, RoutingPriority: 3, CreatedAt: createdAt}, State: SiteAPIKeyState{SiteCredentialID: unlimitedID, UnlimitedQuota: true}, ModelUpdatedAt: modelUpdatedAt},
		{Credential: SiteCredential{ID: highPriorityID, RoutingPriority: 5, CreatedAt: createdAt}, State: SiteAPIKeyState{SiteCredentialID: highPriorityID}, ModelUpdatedAt: modelUpdatedAt},
		{Credential: SiteCredential{ID: stableID, RoutingPriority: 3, CreatedAt: createdAt}, State: SiteAPIKeyState{SiteCredentialID: stableID}, ModelUpdatedAt: modelUpdatedAt},
	}

	SortGatewayCredentials(items)
	want := []uuid.UUID{highPriorityID, unlimitedID, positiveID, stableID, unknownID}
	for index, id := range want {
		if items[index].Credential.ID != id {
			t.Fatalf("sorted credential %d = %s, want %s", index, items[index].Credential.ID, id)
		}
	}
}

func TestRequestLogRepositoryKeepsDBDependency(t *testing.T) {
	t.Parallel()

	repo := NewRequestLogRepository(nil)
	if repo.db != nil {
		t.Fatalf("request log repository db = %#v, want nil", repo.db)
	}
}

func TestRequestLogRepositoryListsAttemptsForParentRequest(t *testing.T) {
	t.Parallel()

	firstID := uuid.New()
	secondID := uuid.New()
	db := storeRepositoryOfflineGorm(t)
	storeReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		switch destination := tx.Statement.Dest.(type) {
		case *[]RequestLog:
			where, ok := tx.Statement.Clauses["WHERE"].Expression.(clause.Where)
			if !ok || len(where.Exprs) != 1 {
				tx.AddError(errors.New("expected one parent request filter"))
				return
			}
			expression, ok := where.Exprs[0].(clause.Expr)
			if !ok || expression.SQL != "metadata ->> 'parent_request_id' = ?" || len(expression.Vars) != 1 || expression.Vars[0] != "req-parent" {
				tx.AddError(errors.New("unexpected request attempt parent filter"))
				return
			}
			order, ok := tx.Statement.Clauses["ORDER BY"].Expression.(clause.OrderBy)
			if !ok || len(order.Columns) != 2 || order.Columns[0].Column.Name != "created_at" || order.Columns[0].Desc {
				tx.AddError(errors.New("expected request attempts in execution order"))
				return
			}
			*destination = []RequestLog{
				{ID: firstID, RequestID: "req-parent:1:attempt", Metadata: JSON(`{"parent_request_id":"req-parent","attempt":1}`)},
				{ID: secondID, RequestID: "req-parent:2:attempt", Success: true, Metadata: JSON(`{"parent_request_id":"req-parent","attempt":2}`)},
			}
		case *[]UsageRecord:
			*destination = []UsageRecord{}
		default:
			tx.AddError(errors.New("unexpected request attempt query destination"))
			return
		}
		tx.Statement.RowsAffected = 1
	})

	attempts, err := NewRequestLogRepository(db).ListAttemptsForParentRequest(context.Background(), " req-parent ")
	if err != nil {
		t.Fatalf("ListAttemptsForParentRequest: %v", err)
	}
	if len(attempts) != 2 || attempts[0].ID != firstID || attempts[1].ID != secondID || !attempts[1].Success {
		t.Fatalf("unexpected request attempts: %#v", attempts)
	}

	missing, err := NewRequestLogRepository(nil).ListAttemptsForParentRequest(context.Background(), " \t\n ")
	if err != nil || len(missing) != 0 {
		t.Fatalf("blank parent result = %#v, %v; want empty, nil", missing, err)
	}
}

func TestRequestLogFilterExpressionsBuildTypedClausesWithoutSearchDB(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	apiKeyID := uuid.New()
	success := true
	from := time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC)
	to := time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC)
	repo := NewRequestLogRepository(nil)

	exprs, err := repo.requestLogFilterExpressions(context.Background(), ListRequestLogsParams{
		Success:         &success,
		SiteID:          &siteID,
		APIKeyID:        &apiKeyID,
		ErrorType:       " upstream_failed ",
		Endpoint:        " /v1/responses ",
		HideWithoutSite: true,
		CreatedFrom:     &from,
		CreatedTo:       &to,
		RequestID:       " req-123 ",
	}, true)
	if err != nil {
		t.Fatalf("requestLogFilterExpressions: %v", err)
	}
	if len(exprs) != 9 {
		t.Fatalf("expected 9 expressions, got %#v", exprs)
	}

	assertEqClause(t, exprs[0], "success", true)
	assertEqClause(t, exprs[1], "site_id", siteID)
	assertEqClause(t, exprs[2], "api_key_id", apiKeyID)
	assertEqClause(t, exprs[3], "error_type", "upstream_failed")
	assertEqClause(t, exprs[4], "endpoint", "/v1/responses")
	assertNeqClause(t, exprs[5], "site_id", nil)
	assertGteClause(t, exprs[6], "created_at", from)
	assertLteClause(t, exprs[7], "created_at", to)

	like, ok := exprs[8].(clause.Like)
	if !ok {
		t.Fatalf("expected request_id LIKE expression, got %T", exprs[8])
	}
	if columnName(like.Column) != "request_id" || like.Value != "%req-123%" {
		t.Fatalf("unexpected request_id LIKE expression: %#v", like)
	}
}

func TestRequestLogFilterExpressionsCanSkipSearchAndModelDBWork(t *testing.T) {
	t.Parallel()

	repo := NewRequestLogRepository(nil)
	exprs, err := repo.requestLogFilterExpressions(context.Background(), ListRequestLogsParams{
		RequestID: "req-ignored",
		Search:    "gpt",
	}, false)
	if err != nil {
		t.Fatalf("requestLogFilterExpressions includeSearch=false: %v", err)
	}
	if len(exprs) != 0 {
		t.Fatalf("expected search clauses to be skipped, got %#v", exprs)
	}

	ids, err := repo.requestLogSearchIDs(context.Background(), " \t\n ")
	if err != nil {
		t.Fatalf("blank requestLogSearchIDs should not access repository: %v", err)
	}
	if len(ids.SiteIDs) != 0 || len(ids.CanonicalModelIDs) != 0 || len(ids.SiteModelIDs) != 0 {
		t.Fatalf("blank search ids = %#v, want empty", ids)
	}

	expr, err := repo.requestLogModelFilterExpression(context.Background(), " \t\n ")
	if err != nil {
		t.Fatalf("blank model filter expression: %v", err)
	}
	assertEqClause(t, expr, "id", uuid.Nil)
}

func TestDeletedSiteHelpersPreserveNameAndReleaseSlug(t *testing.T) {
	id := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	slug := deletedSiteSlug("codex-prod", id)
	if slug != "codex-prod-deleted-11111111" {
		t.Fatalf("unexpected deleted slug: %q", slug)
	}

	meta := deletedSiteMeta(JSON(`{"oauth_provider":"codex"}`), "codex-prod")
	var values map[string]any
	if err := json.Unmarshal(meta, &values); err != nil {
		t.Fatalf("deleted metadata should be valid JSON: %v", err)
	}
	if values["deleted_original_slug"] != "codex-prod" {
		t.Fatalf("expected original slug in metadata, got %#v", values["deleted_original_slug"])
	}
	if text, ok := values["deleted_at"].(string); !ok || !strings.Contains(text, "T") {
		t.Fatalf("expected RFC3339 deleted_at, got %#v", values["deleted_at"])
	}
}

func TestFilterActiveSitesExcludesDeleted(t *testing.T) {
	activeID := uuid.New()
	deletedID := uuid.New()
	items := filterActiveSites([]Site{
		{ID: activeID, Status: "active"},
		{ID: deletedID, Status: SiteStatusDeleted},
	})
	if len(items) != 1 || items[0].ID != activeID {
		t.Fatalf("expected only active site, got %#v", items)
	}
}

func TestFindOAuthSiteByIdentityPrefersEmailOverSharedAccountID(t *testing.T) {
	t.Parallel()

	firstID := uuid.New()
	secondID := uuid.New()
	sites := []Site{
		{ID: firstID, Meta: JSON(`{"oauth_account_id":"acct-shared","oauth_email":"first@example.com"}`)},
		{ID: secondID, Meta: JSON(`{"oauth_account_id":"acct-shared","oauth_email":"second@example.com"}`)},
	}

	got, ok := findOAuthSiteByIdentity(sites, "acct-shared", "second@example.com")
	if !ok {
		t.Fatal("expected oauth site to match by email")
	}
	if got.ID != secondID {
		t.Fatalf("expected second site to match by email, got %s", got.ID)
	}
}

func TestFindOAuthSiteByIdentityFallsBackToAccountIDWithoutEmail(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	sites := []Site{
		{ID: siteID, Meta: JSON(`{"oauth_account_id":"acct-legacy"}`)},
	}

	got, ok := findOAuthSiteByIdentity(sites, "acct-legacy", "")
	if !ok {
		t.Fatal("expected legacy oauth site to match by account id when email is empty")
	}
	if got.ID != siteID {
		t.Fatalf("expected legacy site %s, got %s", siteID, got.ID)
	}
}

func TestHasDuplicateOAuthConnectionEmailIdentity(t *testing.T) {
	t.Parallel()

	if hasDuplicateOAuthConnectionEmailIdentity([]OAuthConnection{
		{Provider: "codex", Email: "first@example.com"},
		{Provider: "codex", Email: "second@example.com"},
	}) {
		t.Fatal("expected distinct emails to be unique")
	}
	if hasDuplicateOAuthConnectionEmailIdentity([]OAuthConnection{
		{Provider: "codex", Email: "same@example.com"},
		{Provider: "antigravity", Email: "same@example.com"},
	}) {
		t.Fatal("expected same email across providers to be allowed")
	}
	if !hasDuplicateOAuthConnectionEmailIdentity([]OAuthConnection{
		{Provider: "codex", Email: "same@example.com"},
		{Provider: "codex", Email: "same@example.com"},
	}) {
		t.Fatal("expected duplicate provider and email to be detected")
	}
	if !hasDuplicateOAuthConnectionEmailIdentity([]OAuthConnection{
		{Provider: "codex", Email: ""},
		{Provider: "codex", Email: ""},
	}) {
		t.Fatal("expected duplicate empty emails to be detected")
	}
}

func TestOAuthConnectionEmailIdentityDuplicateIndexError(t *testing.T) {
	t.Parallel()

	err := errors.New("ERROR: could not create unique index \"oauth_connections_provider_email_unique\" (SQLSTATE 23505): duplicated key not allowed")
	if !oauthConnectionEmailIdentityDuplicateIndexError(err) {
		t.Fatal("expected duplicated key index error to be recognized")
	}
	if oauthConnectionEmailIdentityDuplicateIndexError(errors.New("connection refused")) {
		t.Fatal("expected unrelated error to be preserved")
	}
}

func TestSummarizeRequestUsageBySiteRowsAggregatesSummaryRows(t *testing.T) {
	siteID := uuid.New()
	otherSiteID := uuid.New()
	first := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	middle := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	last := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	rows := summarizeRequestUsageBySiteRows([]RequestUsageDailySummary{
		{
			SiteID:           uuid.NullUUID{UUID: siteID, Valid: true},
			SiteName:         "Codex",
			SiteSlug:         "codex",
			SiteType:         "openai",
			RequestCount:     2,
			SuccessCount:     1,
			FailureCount:     1,
			PromptTokens:     10,
			CompletionTokens: 20,
			TotalTokens:      30,
			EstimatedCost:    0.5,
			Currency:         "USD",
			FirstRequestAt:   sql.NullTime{Time: middle, Valid: true},
			LastRequestAt:    sql.NullTime{Time: middle, Valid: true},
		},
		{
			SiteID:           uuid.NullUUID{UUID: siteID, Valid: true},
			RequestCount:     3,
			SuccessCount:     3,
			PromptTokens:     5,
			CompletionTokens: 6,
			TotalTokens:      11,
			EstimatedCost:    1.25,
			FirstRequestAt:   sql.NullTime{Time: first, Valid: true},
			LastRequestAt:    sql.NullTime{Time: last, Valid: true},
		},
		{
			SiteID:        uuid.NullUUID{UUID: otherSiteID, Valid: true},
			RequestCount:  1,
			EstimatedCost: 0.25,
			Currency:      "USD",
		},
		{
			RequestCount:  99,
			EstimatedCost: 99,
		},
	})

	if len(rows) != 2 {
		t.Fatalf("expected two site rows, got %d", len(rows))
	}
	row := rows[0]
	if row.SiteID != siteID {
		t.Fatalf("expected highest cost site first, got %s", row.SiteID)
	}
	if row.RequestCount != 5 || row.SuccessCount != 4 || row.FailedCount != 1 {
		t.Fatalf("unexpected counts: %#v", row)
	}
	if row.PromptTokens != 15 || row.CompletionTokens != 26 || row.TotalTokens != 41 {
		t.Fatalf("unexpected tokens: %#v", row)
	}
	if row.EstimatedCost != 1.75 || row.Currency != "USD" {
		t.Fatalf("unexpected cost: %#v", row)
	}
	if !row.FirstRequestAt.Equal(first) || !row.LastRequestAt.Equal(last) {
		t.Fatalf("unexpected request window: %#v", row)
	}
}

func TestRequestUsageCostSummaryAggregatesTokens(t *testing.T) {
	summary := RequestUsageCostSummary{Currency: requestUsageSummaryDefaultCurrency}

	summary.AddFloat(1.25, "EUR")
	summary.AddUsage(80, 40, 120, 30)
	summary.AddCost(sql.NullFloat64{Float64: 0.75, Valid: true}, "")
	summary.AddUsage(20, 10, 30, 5)

	if summary.TotalTokens != 150 {
		t.Fatalf("expected total tokens 150, got %d", summary.TotalTokens)
	}
	if summary.PromptTokens != 100 || summary.CompletionTokens != 50 || summary.CachedTokens != 35 {
		t.Fatalf("unexpected token breakdown: %#v", summary)
	}
	// F22: TotalCost is the established EUR currency only; the default-currency
	// value is kept separate rather than mixed in.
	if summary.TotalCost != 1.25 {
		t.Fatalf("expected EUR-consistent total cost 1.25, got %f", summary.TotalCost)
	}
	if summary.Currency != "EUR" {
		t.Fatalf("expected currency EUR, got %q", summary.Currency)
	}
}

func TestHealthCheckedAtBeforeClauseUsesUTCCheckedAtCutoff(t *testing.T) {
	t.Parallel()

	location := time.FixedZone("CST", 8*60*60)
	cutoff := time.Date(2026, 5, 30, 12, 0, 0, 0, location)
	where := healthCheckedAtBeforeClause(cutoff)
	if len(where.Exprs) != 1 {
		t.Fatalf("expected one expression, got %#v", where.Exprs)
	}
	lt, ok := where.Exprs[0].(clause.Lt)
	if !ok {
		t.Fatalf("expected less-than clause, got %T", where.Exprs[0])
	}
	column, ok := lt.Column.(clause.Column)
	if !ok || column.Name != "checked_at" {
		t.Fatalf("expected checked_at column, got %#v", lt.Column)
	}
	value, ok := lt.Value.(time.Time)
	if !ok {
		t.Fatalf("expected time cutoff, got %T", lt.Value)
	}
	if !value.Equal(cutoff.UTC()) || value.Location() != time.UTC {
		t.Fatalf("expected UTC cutoff %s, got %s", cutoff.UTC(), value)
	}
}

func TestIPAddressScansPostgresINETStrings(t *testing.T) {
	var ip IPAddress
	if err := ip.Scan("127.0.0.1"); err != nil {
		t.Fatalf("scan ipv4: %v", err)
	}
	if !ip.NetIP().Equal(net.ParseIP("127.0.0.1")) {
		t.Fatalf("expected ipv4 round trip, got %q", ip.NetIP())
	}

	value, err := ip.Value()
	if err != nil {
		t.Fatalf("value: %v", err)
	}
	if value != "127.0.0.1" {
		t.Fatalf("expected driver value 127.0.0.1, got %v", value)
	}

	if err := ip.Scan([]byte("::1")); err != nil {
		t.Fatalf("scan ipv6: %v", err)
	}
	if !ip.NetIP().Equal(net.ParseIP("::1")) {
		t.Fatalf("expected ipv6 round trip, got %q", ip.NetIP())
	}
}

func TestJSONScannerAndDriverValue(t *testing.T) {
	t.Parallel()

	var payload JSON
	value, err := payload.Value()
	if err != nil {
		t.Fatalf("empty JSON value: %v", err)
	}
	if value != nil {
		t.Fatalf("empty JSON value = %#v, want nil", value)
	}

	if err := payload.Scan([]byte(`{"ok":true}`)); err != nil {
		t.Fatalf("scan json bytes: %v", err)
	}
	if string(payload) != `{"ok":true}` {
		t.Fatalf("scanned json = %s", payload)
	}
	value, err = payload.Value()
	if err != nil {
		t.Fatalf("json value: %v", err)
	}
	if value != `{"ok":true}` {
		t.Fatalf("json value = %#v", value)
	}

	if err := payload.Scan(`{"next":1}`); err != nil {
		t.Fatalf("scan json string: %v", err)
	}
	if string(payload) != `{"next":1}` {
		t.Fatalf("scanned string json = %s", payload)
	}
	if err := payload.Scan(123); err == nil {
		t.Fatal("expected unsupported json scan type to fail")
	}
	if err := payload.Scan(nil); err != nil {
		t.Fatalf("scan nil json: %v", err)
	}
	if payload != nil {
		t.Fatalf("nil scan should clear json, got %s", payload)
	}
}

func TestORMNullableHelpers(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	id := uuid.New()

	if got := jsonDefault(nil, "{}"); string(got) != "{}" {
		t.Fatalf("jsonDefault = %s", got)
	}
	if got := jsonFromAny(map[string]any{"ok": true}, "{}"); string(got) != `{"ok":true}` {
		t.Fatalf("jsonFromAny map = %s", got)
	}
	if got := jsonFromAny(make(chan int), "{}"); string(got) != "{}" {
		t.Fatalf("jsonFromAny unmarshalable = %s", got)
	}

	if got := nullStringFromAny("value"); !got.Valid || got.String != "value" {
		t.Fatalf("nullStringFromAny string = %#v", got)
	}
	if got := nullStringFromAny(123); got.Valid {
		t.Fatalf("nullStringFromAny unsupported = %#v", got)
	}
	if got := nullFloatFromAny(int64(7)); !got.Valid || got.Float64 != 7 {
		t.Fatalf("nullFloatFromAny int64 = %#v", got)
	}
	if got := nullFloatFromAny(float32(1.5)); !got.Valid || got.Float64 != float64(float32(1.5)) {
		t.Fatalf("nullFloatFromAny float32 = %#v", got)
	}
	if got := nullInt64FromAny(int32(9)); !got.Valid || got.Int64 != 9 {
		t.Fatalf("nullInt64FromAny int32 = %#v", got)
	}
	if got := nullBoolFromAny(true); !got.Valid || !got.Bool {
		t.Fatalf("nullBoolFromAny bool = %#v", got)
	}
	if got := nullTimeFromAny(now); !got.Valid || !got.Time.Equal(now) {
		t.Fatalf("nullTimeFromAny time = %#v", got)
	}
	if got := nullTimeFromAny(time.Time{}); got.Valid {
		t.Fatalf("zero time should not be valid: %#v", got)
	}
	if got := timePtrFromAny(sql.NullTime{Time: now, Valid: true}); got == nil || !got.Equal(now) {
		t.Fatalf("timePtrFromAny null time = %#v", got)
	}
	if got := timePtrFromAny(sql.NullTime{}); got != nil {
		t.Fatalf("invalid null time pointer = %#v", got)
	}
	if got := uuidPtrFromAny(id); got == nil || *got != id {
		t.Fatalf("uuidPtrFromAny uuid = %#v", got)
	}
	if got := uuidPtrFromAny(uuid.Nil); got != nil {
		t.Fatalf("nil uuid pointer = %#v", got)
	}
	if got := nullUUIDFromAny(&id); !got.Valid || got.UUID != id {
		t.Fatalf("nullUUIDFromAny pointer = %#v", got)
	}
	if got := nullUUIDFromAny((*uuid.UUID)(nil)); got.Valid {
		t.Fatalf("nil uuid pointer should be invalid: %#v", got)
	}
}

func TestModelTableNamesStayStable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "Admin", got: (Admin{}).TableName(), want: "admins"},
		{name: "AdminSession", got: (AdminSession{}).TableName(), want: "admin_sessions"},
		{name: "AdminAccessToken", got: (AdminAccessToken{}).TableName(), want: "admin_access_tokens"},
		{name: "AdminAuditLog", got: (AdminAuditLog{}).TableName(), want: "admin_audit_logs"},
		{name: "APIKey", got: (APIKey{}).TableName(), want: "api_keys"},
		{name: "APIKeySitePermission", got: (APIKeySitePermission{}).TableName(), want: "api_key_site_permissions"},
		{name: "APIKeySiteGroupPermission", got: (APIKeySiteGroupPermission{}).TableName(), want: "api_key_site_group_permissions"},
		{name: "APIKeySiteModelPermission", got: (APIKeySiteModelPermission{}).TableName(), want: "api_key_site_model_permissions"},
		{name: "Site", got: (Site{}).TableName(), want: "sites"},
		{name: "SiteCredential", got: (SiteCredential{}).TableName(), want: "site_credentials"},
		{name: "OAuthConnection", got: (OAuthConnection{}).TableName(), want: "oauth_connections"},
		{name: "SiteModel", got: (SiteModel{}).TableName(), want: "site_models"},
		{name: "SiteModelPricing", got: (SiteModelPricing{}).TableName(), want: "site_model_pricings"},
		{name: "RequestLog", got: (RequestLog{}).TableName(), want: "request_logs"},
		{name: "RequestUsageDailySummary", got: (RequestUsageDailySummary{}).TableName(), want: "request_usage_daily_summaries"},
		{name: "GatewayRateLimit", got: (GatewayRateLimit{}).TableName(), want: "gateway_rate_limits"},
		{name: "GatewayRateLimitWindow", got: (GatewayRateLimitWindow{}).TableName(), want: "gateway_rate_limit_windows"},
	}

	for _, tt := range tests {
		if tt.got != tt.want {
			t.Fatalf("%s table = %q, want %q", tt.name, tt.got, tt.want)
		}
	}
}

func TestFillRouteKeyCountsExcludesDisabledMissingAndCoolingCredentials(t *testing.T) {
	siteID := uuid.New()
	siteModelID := uuid.New()
	okCredentialID := uuid.New()
	disabledCredentialID := uuid.New()
	missingCredentialID := uuid.New()
	coolingCredentialID := uuid.New()
	authFailedCredentialID := uuid.New()

	row := RouteCandidateRow{SiteID: siteID, SiteModelID: siteModelID}
	apiKeyModels := []SiteAPIKeyModel{
		{SiteCredentialID: okCredentialID, SiteModelID: uuid.NullUUID{UUID: siteModelID, Valid: true}, Available: true, Enabled: true},
		{SiteCredentialID: disabledCredentialID, SiteModelID: uuid.NullUUID{UUID: siteModelID, Valid: true}, Available: true, Enabled: true},
		{SiteCredentialID: missingCredentialID, SiteModelID: uuid.NullUUID{UUID: siteModelID, Valid: true}, Available: true, Enabled: true},
		{SiteCredentialID: coolingCredentialID, SiteModelID: uuid.NullUUID{UUID: siteModelID, Valid: true}, Available: true, Enabled: true},
		{SiteCredentialID: authFailedCredentialID, SiteModelID: uuid.NullUUID{UUID: siteModelID, Valid: true}, Available: true, Enabled: true},
	}
	credentials := map[uuid.UUID]SiteCredential{
		okCredentialID:       {ID: okCredentialID, SiteID: siteID, CredentialType: "api_key", Meta: JSON(`{}`)},
		disabledCredentialID: {ID: disabledCredentialID, SiteID: siteID, CredentialType: "api_key", Meta: JSON(`{}`)},
		missingCredentialID:  {ID: missingCredentialID, SiteID: siteID, CredentialType: "api_key", Meta: JSON(`{"raw_key_missing":true}`)},
		coolingCredentialID:  {ID: coolingCredentialID, SiteID: siteID, CredentialType: "api_key", Meta: JSON(`{}`)},
		authFailedCredentialID: {
			ID:             authFailedCredentialID,
			SiteID:         siteID,
			CredentialType: "api_key",
			Meta:           JSON(`{}`),
		},
	}
	states := map[uuid.UUID]SiteAPIKeyState{
		disabledCredentialID: {SiteCredentialID: disabledCredentialID, Enabled: false},
		authFailedCredentialID: {
			SiteCredentialID: authFailedCredentialID,
			Enabled:          true,
			SyncStatus:       "failed",
			SyncMessage:      sql.NullString{String: "codex upstream returned 401: token_invalidated", Valid: true},
		},
	}
	cooldowns := []RouteCooldown{
		{
			SiteID:           siteID,
			SiteModelID:      uuid.NullUUID{UUID: siteModelID, Valid: true},
			SiteCredentialID: uuid.NullUUID{UUID: coolingCredentialID, Valid: true},
			ActiveUntil:      time.Now().Add(time.Hour),
		},
	}

	fillRouteKeyCounts(&row, apiKeyModels, credentials, states, cooldowns)

	if row.ModelAPIKeyCount != 5 {
		t.Fatalf("expected total model key count 5, got %d", row.ModelAPIKeyCount)
	}
	if row.ModelAvailableKeyCount != 1 {
		t.Fatalf("expected only one available key, got %d", row.ModelAvailableKeyCount)
	}
}

func TestFillRouteKeyCountsRejectsMissingCredentialRecord(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	siteModelID := uuid.New()
	credentialID := uuid.New()
	row := RouteCandidateRow{SiteID: siteID, SiteModelID: siteModelID}
	fillRouteKeyCounts(&row, []SiteAPIKeyModel{{
		SiteCredentialID: credentialID,
		SiteModelID:      uuid.NullUUID{UUID: siteModelID, Valid: true},
		Available:        true,
		Enabled:          true,
	}}, map[uuid.UUID]SiteCredential{}, map[uuid.UUID]SiteAPIKeyState{}, nil)

	if row.ModelAPIKeyCount != 1 || row.ModelAvailableKeyCount != 0 || row.PreferredCredentialID.Valid {
		t.Fatalf("route key counts = total %d available %d preferred %#v", row.ModelAPIKeyCount, row.ModelAvailableKeyCount, row.PreferredCredentialID)
	}
}

func TestFillRouteKeyCountsAcceptsGrokModelCredential(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	siteModelID := uuid.New()
	credentialID := uuid.New()
	row := RouteCandidateRow{SiteID: siteID, SiteModelID: siteModelID}
	fillRouteKeyCounts(&row, []SiteAPIKeyModel{{
		SiteCredentialID: credentialID,
		SiteModelID:      uuid.NullUUID{UUID: siteModelID, Valid: true},
		Available:        true,
		Enabled:          true,
	}}, map[uuid.UUID]SiteCredential{
		credentialID: {ID: credentialID, SiteID: siteID, CredentialType: "grok_sso:account", Meta: JSON(`{"enabled":true}`)},
	}, map[uuid.UUID]SiteAPIKeyState{
		credentialID: {SiteCredentialID: credentialID, SiteID: siteID, Enabled: true, SyncStatus: "synced"},
	}, nil)

	if row.ModelAPIKeyCount != 1 || row.ModelAvailableKeyCount != 1 || !row.PreferredCredentialID.Valid || row.PreferredCredentialID.UUID != credentialID {
		t.Fatalf("grok route key counts = total %d available %d preferred %#v", row.ModelAPIKeyCount, row.ModelAvailableKeyCount, row.PreferredCredentialID)
	}
}

func TestNewRouteCandidateRepositoryKeepsDBDependency(t *testing.T) {
	t.Parallel()

	repo := NewRouteCandidateRepository(nil)
	if repo.db != nil {
		t.Fatalf("route candidate repository db = %#v, want nil", repo.db)
	}
}

func TestDefaultHealthStatusFallsBackToUnknown(t *testing.T) {
	t.Parallel()

	if got := defaultHealthStatus(""); got != "unknown" {
		t.Fatalf("empty health status = %q, want unknown", got)
	}
	if got := defaultHealthStatus("healthy"); got != "healthy" {
		t.Fatalf("explicit health status = %q, want healthy", got)
	}
}

func TestFillModelHealthAggregatesRecentGatewayModelSnapshots(t *testing.T) {
	t.Parallel()

	siteModelID := uuid.New()
	otherModelID := uuid.New()
	now := time.Now()
	row := RouteCandidateRow{SiteModelID: siteModelID}

	fillModelHealth(&row, []HealthSnapshot{
		{
			SiteModelID: uuid.NullUUID{UUID: siteModelID, Valid: true},
			Scope:       "model",
			Source:      "gateway",
			Success:     true,
			LatencyMS:   sql.NullInt64{Int64: 100, Valid: true},
			CheckedAt:   now.Add(-time.Hour),
		},
		{
			SiteModelID: uuid.NullUUID{UUID: siteModelID, Valid: true},
			Scope:       "model",
			Source:      "gateway",
			Success:     false,
			LatencyMS:   sql.NullInt64{Int64: 300, Valid: true},
			CheckedAt:   now.Add(-2 * time.Hour),
		},
		{
			SiteModelID: uuid.NullUUID{UUID: siteModelID, Valid: true},
			Scope:       "site",
			Source:      "gateway",
			Success:     true,
			LatencyMS:   sql.NullInt64{Int64: 999, Valid: true},
			CheckedAt:   now.Add(-time.Hour),
		},
		{
			SiteModelID: uuid.NullUUID{UUID: siteModelID, Valid: true},
			Scope:       "model",
			Source:      "diagnostic",
			Success:     true,
			LatencyMS:   sql.NullInt64{Int64: 999, Valid: true},
			CheckedAt:   now.Add(-time.Hour),
		},
		{
			SiteModelID: uuid.NullUUID{UUID: otherModelID, Valid: true},
			Scope:       "model",
			Source:      "gateway",
			Success:     true,
			LatencyMS:   sql.NullInt64{Int64: 999, Valid: true},
			CheckedAt:   now.Add(-time.Hour),
		},
		{
			SiteModelID: uuid.NullUUID{UUID: siteModelID, Valid: true},
			Scope:       "model",
			Source:      "gateway",
			Success:     true,
			LatencyMS:   sql.NullInt64{Int64: 999, Valid: true},
			CheckedAt:   now.Add(-25 * time.Hour),
		},
	})

	if row.ModelRequestCount != 2 {
		t.Fatalf("model request count = %d, want 2", row.ModelRequestCount)
	}
	if !row.ModelSuccessRate.Valid || row.ModelSuccessRate.Float64 != 0.5 {
		t.Fatalf("model success rate = %#v, want 0.5", row.ModelSuccessRate)
	}
	if !row.ModelAvgLatencyMS.Valid || row.ModelAvgLatencyMS.Int64 != 200 {
		t.Fatalf("model avg latency = %#v, want 200", row.ModelAvgLatencyMS)
	}
}

func TestFillRoutePricingChoosesLowestRankedAvailablePricing(t *testing.T) {
	t.Parallel()

	siteModelID := uuid.New()
	otherModelID := uuid.New()
	row := RouteCandidateRow{SiteModelID: siteModelID}
	fillRoutePricing(&row, []SiteModelPricing{
		{
			SiteModelID: uuid.NullUUID{UUID: otherModelID, Valid: true},
			GroupName:   "default",
			InputValue:  sql.NullFloat64{Float64: 0.01, Valid: true},
		},
		{
			SiteModelID:    uuid.NullUUID{UUID: siteModelID, Valid: true},
			GroupName:      "vip",
			Currency:       "USD",
			BillingType:    "tokens",
			InputValue:     sql.NullFloat64{Float64: 1, Valid: true},
			OutputValue:    sql.NullFloat64{Float64: 2, Valid: true},
			ImageRatio:     sql.NullFloat64{Float64: 3, Valid: true},
			QuotaType:      2,
			ModelName:      "expensive",
			ManualNote:     sql.NullString{String: "ignored", Valid: true},
			ManualOverride: true,
		},
		{
			SiteModelID:     uuid.NullUUID{UUID: siteModelID, Valid: true},
			GroupName:       "default",
			Currency:        "EUR",
			BillingType:     "per_request",
			PerRequestValue: sql.NullFloat64{Float64: 0.05, Valid: true},
			QuotaType:       1,
		},
	})

	if row.PricingGroupName.String != "default" || row.PricingCurrency.String != "EUR" || row.PricingBillingType.String != "per_request" {
		t.Fatalf("unexpected selected pricing labels: %#v", row)
	}
	if !row.PricingPerRequestValue.Valid || row.PricingPerRequestValue.Float64 != 0.05 {
		t.Fatalf("unexpected per-request pricing: %#v", row.PricingPerRequestValue)
	}
	if row.PricingQuotaType.Int64 != 1 {
		t.Fatalf("quota type = %#v, want 1", row.PricingQuotaType)
	}
}

func TestCollectSupportedEndpointTypesDeduplicatesAndIgnoresInvalidJSON(t *testing.T) {
	t.Parallel()

	got := collectSupportedEndpointTypes(SiteModel{Capabilities: JSON(`{"supported_endpoint_types":[" openai ","openai","responses",""]}`)})
	if len(got) != 2 || got[0] != "openai" || got[1] != "responses" {
		t.Fatalf("endpoint types = %#v, want [openai responses]", got)
	}
	got = collectSupportedEndpointTypes(SiteModel{Capabilities: JSON(`not json`)})
	if len(got) != 0 {
		t.Fatalf("invalid json endpoint types = %#v, want empty", got)
	}
}

func TestRequestLogFilterHelpersBuildTypedClauses(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	apiKeyID := uuid.New()
	success := true
	start := time.Date(2026, 6, 22, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	exprs := requestLogDirectFilterExpressions(ListRequestLogsParams{
		Success:         &success,
		SiteID:          &siteID,
		APIKeyID:        &apiKeyID,
		ErrorType:       " upstream_failed ",
		Endpoint:        "/v1/responses",
		HideWithoutSite: true,
		CreatedFrom:     &start,
		CreatedTo:       &end,
	})
	if len(exprs) != 8 {
		t.Fatalf("expected 8 filter expressions, got %#v", exprs)
	}
	if eq, ok := exprs[0].(clause.Eq); !ok || eq.Value != true {
		t.Fatalf("expected success eq clause, got %#v", exprs[0])
	}
	if eq, ok := exprs[3].(clause.Eq); !ok || eq.Value != "upstream_failed" {
		t.Fatalf("expected trimmed error type, got %#v", exprs[3])
	}
	if neq, ok := exprs[5].(clause.Neq); !ok || neq.Value != nil {
		t.Fatalf("expected hide-without-site neq nil, got %#v", exprs[5])
	}
	if gte, ok := exprs[6].(clause.Gte); !ok || !gte.Value.(time.Time).Equal(start) {
		t.Fatalf("expected created_from gte, got %#v", exprs[6])
	}
	if lte, ok := exprs[7].(clause.Lte); !ok || !lte.Value.(time.Time).Equal(end) {
		t.Fatalf("expected created_to lte, got %#v", exprs[7])
	}

	if !requestLogTextMatches("codex", "OpenAI", "Codex Gateway") {
		t.Fatal("expected text match to be case-insensitive")
	}
	if requestLogLikePattern(" req-1 ") != "%req-1%" {
		t.Fatalf("unexpected like pattern")
	}
}

func TestRequestLogExpressionHelpers(t *testing.T) {
	t.Parallel()

	first := uuid.New()
	second := uuid.New()
	ids := appendUUIDOnce([]uuid.UUID{first}, first)
	ids = appendUUIDOnce(ids, second)
	if len(ids) != 2 || ids[0] != first || ids[1] != second {
		t.Fatalf("appendUUIDOnce = %#v", ids)
	}

	in, ok := requestLogUUIDInExpression("site_id", []uuid.UUID{first, second}).(clause.IN)
	if !ok {
		t.Fatalf("expected IN expression")
	}
	column, ok := in.Column.(clause.Column)
	if !ok || column.Name != "site_id" || len(in.Values) != 2 {
		t.Fatalf("unexpected IN expression: %#v", in)
	}

	never, ok := requestLogNeverExpression().(clause.Eq)
	if !ok || never.Value != uuid.Nil {
		t.Fatalf("unexpected never expression: %#v", never)
	}

	start := time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC)
	where := requestLogCreatedRangeClause(start, start.Add(time.Hour))
	if len(where.Exprs) != 2 {
		t.Fatalf("range expr count = %d, want 2", len(where.Exprs))
	}
	if _, ok := where.Exprs[0].(clause.Gte); !ok {
		t.Fatalf("expected first range expr to be Gte, got %T", where.Exprs[0])
	}
	if _, ok := where.Exprs[1].(clause.Lt); !ok {
		t.Fatalf("expected second range expr to be Lt, got %T", where.Exprs[1])
	}

	order := requestLogDefaultOrderClause()
	if len(order.Columns) != 2 || order.Columns[0].Column.Name != "created_at" || !order.Columns[0].Desc {
		t.Fatalf("unexpected default order: %#v", order)
	}
}

func TestRateLimitNormalizeHelpers(t *testing.T) {
	t.Parallel()

	if normalizeRateLimitScope(RateLimitScopeGlobal) != RateLimitScopeGlobal {
		t.Fatal("expected global scope to be accepted")
	}
	if normalizeRateLimitScope(RateLimitScopeAPIKey) != RateLimitScopeAPIKey {
		t.Fatal("expected api_key scope to be accepted")
	}
	if normalizeRateLimitScope(" global ") != "" {
		t.Fatal("rate limit scope should not silently trim unknown input")
	}
	if normalizeRateLimitScope("GLOBAL") != "" || normalizeRateLimitScope("api-key") != "" || normalizeRateLimitScope(" api_key") != "" {
		t.Fatal("near-miss rate limit scopes should be rejected")
	}
	if normalizeRateLimitStatus(RateLimitStatusEnabled) != RateLimitStatusEnabled {
		t.Fatal("expected enabled status to be accepted")
	}
	if normalizeRateLimitStatus(RateLimitStatusDisabled) != RateLimitStatusDisabled {
		t.Fatal("expected disabled status to be accepted")
	}
	if normalizeRateLimitStatus("paused") != "" || normalizeRateLimitStatus("ENABLED") != "" || normalizeRateLimitStatus("disabled ") != "" {
		t.Fatal("unexpected or near-miss rate limit statuses should be rejected")
	}
}

func TestFillRouteSiteCredentialCountExcludesPermanentAuthFailures(t *testing.T) {
	siteID := uuid.New()
	okCredentialID := uuid.New()
	authFailedCredentialID := uuid.New()

	row := RouteCandidateRow{SiteID: siteID}
	credentials := []SiteCredential{
		{ID: okCredentialID, SiteID: siteID, CredentialType: "api_key:1", Meta: JSON(`{}`)},
		{ID: authFailedCredentialID, SiteID: siteID, CredentialType: "api_key:2", Meta: JSON(`{}`)},
	}
	states := map[uuid.UUID]SiteAPIKeyState{
		authFailedCredentialID: {
			SiteCredentialID: authFailedCredentialID,
			Enabled:          true,
			SyncStatus:       "failed",
			SyncMessage:      sql.NullString{String: `upstream returned 401: {"error":{"code":"invalid_api_key"}}`, Valid: true},
		},
	}

	fillRouteSiteCredentialCount(&row, credentials, states, nil)

	if row.SiteCredentialCount != 1 {
		t.Fatalf("expected only one fallback credential, got %d", row.SiteCredentialCount)
	}
}

func TestCredentialStateUsableOnlyExcludesPermanentAuthFailures(t *testing.T) {
	credentialID := uuid.New()

	if !credentialStateUsable(SiteAPIKeyState{}) {
		t.Fatal("missing state should default to usable")
	}
	if credentialStateUsable(SiteAPIKeyState{SiteCredentialID: credentialID, Enabled: false}) {
		t.Fatal("disabled state should not be usable")
	}
	if !credentialStateUsable(SiteAPIKeyState{
		SiteCredentialID: credentialID,
		Enabled:          true,
		SyncStatus:       "failed",
		SyncMessage:      sql.NullString{String: "context window is 400000 tokens", Valid: true},
	}) {
		t.Fatal("non-auth failed sync should remain usable")
	}
	if credentialStateUsable(SiteAPIKeyState{
		SiteCredentialID: credentialID,
		Enabled:          true,
		SyncStatus:       "failed",
		SyncMessage:      sql.NullString{String: "google token refresh returned 400: invalid_grant", Valid: true},
	}) {
		t.Fatal("permanent auth failure should not be usable")
	}

	for _, message := range []string{" refresh_token_reused ", "oauth token_invalidated by provider", `HTTP 401: {"error":{"code":"invalid_api_key"}}`, "api key is disabled"} {
		if !credentialStatePermanentAuthFailure(message) {
			t.Fatalf("expected permanent auth failure for %q", message)
		}
	}
	for _, message := range []string{
		"",
		"upstream 429 rate limited",
		"transient 500 from provider",
		"request failed: status=403",
		"HTTP 401 unauthorized",
		`upstream returned 401: {"error":{"code":"api_key_daily_quota_exhausted"}}`,
		`upstream returned 403: {"code":"INSUFFICIENT_BALANCE"}`,
	} {
		if credentialStatePermanentAuthFailure(message) {
			t.Fatalf("did not expect permanent auth failure for %q", message)
		}
	}
}

func TestCredentialStateUsableRequiresStateForUpstreamManagedCredential(t *testing.T) {
	manual := SiteCredential{ID: uuid.New(), CredentialType: "api_key:manual", Meta: JSON(`{"enabled":true}`)}
	managed := SiteCredential{ID: uuid.New(), CredentialType: "api_key:managed", Meta: JSON(`{"upstream_id":42}`)}
	external := SiteCredential{ID: uuid.New(), CredentialType: "api_key:external", Meta: JSON(`{"upstream_key_id":"remote-key"}`)}

	if !CredentialStateUsableForCredential(manual, SiteAPIKeyState{}) {
		t.Fatal("manual credential without state should remain usable")
	}
	if CredentialStateUsableForCredential(managed, SiteAPIKeyState{}) {
		t.Fatal("managed credential without state should not be usable")
	}
	if CredentialStateUsableForCredential(external, SiteAPIKeyState{}) {
		t.Fatal("external credential without state should not be usable")
	}
	if !CredentialStateUsableForCredential(managed, SiteAPIKeyState{SiteCredentialID: managed.ID, Enabled: true, SyncStatus: "synced"}) {
		t.Fatal("managed credential with a synced state should be usable")
	}
}

func TestRouteCoolingSeparatesTransientAndPersistentCooldowns(t *testing.T) {
	siteID := uuid.New()
	siteModelID := uuid.New()
	credentialID := uuid.New()

	if !routeCooling([]RouteCooldown{{
		SiteID:      siteID,
		Source:      "site_health",
		ActiveUntil: time.Now().Add(time.Hour),
	}}, siteID, siteModelID) {
		t.Fatal("site health site cooldown should block routing")
	}

	if routeCooling([]RouteCooldown{{
		SiteID:      siteID,
		SiteModelID: uuid.NullUUID{UUID: siteModelID, Valid: true},
		Source:      "gateway",
		Reason:      "upstream_timeout",
		ActiveUntil: time.Now().Add(time.Hour),
	}}, siteID, siteModelID) {
		t.Fatal("transient gateway model cooldown should stay half-open and not block routing")
	}

	if !routeCooling([]RouteCooldown{{
		SiteID:      siteID,
		SiteModelID: uuid.NullUUID{UUID: siteModelID, Valid: true},
		Source:      "gateway",
		Reason:      CooldownReasonUpstreamModelNotFound,
		ActiveUntil: time.Now().Add(time.Hour),
	}}, siteID, siteModelID) {
		t.Fatal("persistent gateway model cooldown should block routing")
	}

	if !routeCooling([]RouteCooldown{{
		SiteID:      siteID,
		SiteModelID: uuid.NullUUID{UUID: siteModelID, Valid: true},
		Source:      "manual",
		ActiveUntil: time.Now().Add(time.Hour),
	}}, siteID, siteModelID) {
		t.Fatal("manual cooldown should block routing")
	}

	if routeCooling([]RouteCooldown{{
		SiteID:           siteID,
		SiteCredentialID: uuid.NullUUID{UUID: credentialID, Valid: true},
		Source:           "gateway",
		ActiveUntil:      time.Now().Add(time.Hour),
	}}, siteID, siteModelID) {
		t.Fatal("credential cooldown should not be treated as route cooldown")
	}
}

func TestRouteInsightCountsNewAPIChannels(t *testing.T) {
	siteID := uuid.New()
	siteModelID := uuid.New()
	enabledCredentialID := uuid.New()
	disabledCredentialID := uuid.New()
	hiddenCredentialID := uuid.New()

	model := SiteModel{ID: siteModelID, SiteID: siteID, Status: "active"}
	site := Site{ID: siteID, SiteType: "newapi", Status: "active", Enabled: true}
	apiKeyModels := []SiteAPIKeyModel{
		{SiteCredentialID: enabledCredentialID, SiteModelID: uuid.NullUUID{UUID: siteModelID, Valid: true}, Available: true, Enabled: true},
		{SiteCredentialID: disabledCredentialID, SiteModelID: uuid.NullUUID{UUID: siteModelID, Valid: true}, Available: true, Enabled: true},
		{SiteCredentialID: hiddenCredentialID, SiteModelID: uuid.NullUUID{UUID: siteModelID, Valid: true}, Available: false, Enabled: true},
	}
	credentials := map[uuid.UUID]SiteCredential{
		enabledCredentialID:  {ID: enabledCredentialID, SiteID: siteID, CredentialType: "api_key:1", Meta: JSON(`{}`)},
		disabledCredentialID: {ID: disabledCredentialID, SiteID: siteID, CredentialType: "api_key:2", Meta: JSON(`{}`)},
		hiddenCredentialID:   {ID: hiddenCredentialID, SiteID: siteID, CredentialType: "api_key:3", Meta: JSON(`{}`)},
	}
	states := map[uuid.UUID]SiteAPIKeyState{
		disabledCredentialID: {SiteCredentialID: disabledCredentialID, Enabled: false},
	}

	configured := configuredRouteChannelCount(model, site, apiKeyModels, credentials)
	if configured != 2 {
		t.Fatalf("expected configured newapi channels to match visible API key model rows, got %d", configured)
	}

	eligible := eligibleRouteChannelCount(model, site, apiKeyModels, credentials, nil, states, nil)
	if eligible != 1 {
		t.Fatalf("expected only enabled usable API key channel to be eligible, got %d", eligible)
	}
}

func TestRouteInsightCountsNonNewAPIAsSiteModelChannel(t *testing.T) {
	siteID := uuid.New()
	siteModelID := uuid.New()
	credentialID := uuid.New()

	model := SiteModel{ID: siteModelID, SiteID: siteID, Status: "active"}
	site := Site{ID: siteID, SiteType: "codex", Status: "active", Enabled: true}
	credentials := []SiteCredential{
		{ID: credentialID, SiteID: siteID, CredentialType: "api_key:1", Meta: JSON(`{}`)},
	}

	configured := configuredRouteChannelCount(model, site, nil, nil)
	if configured != 1 {
		t.Fatalf("expected non-newapi site model to be one configured channel, got %d", configured)
	}

	eligible := eligibleRouteChannelCount(model, site, nil, nil, credentials, nil, nil)
	if eligible != 1 {
		t.Fatalf("expected usable fallback credential to make non-newapi channel eligible, got %d", eligible)
	}
}

func TestRouteInsightDoesNotFallbackWhenNonNewAPIHasUnusableBindings(t *testing.T) {
	siteID := uuid.New()
	siteModelID := uuid.New()
	credentialID := uuid.New()
	model := SiteModel{ID: siteModelID, SiteID: siteID, Status: "active"}
	site := Site{ID: siteID, SiteType: "openai", Status: "active", Enabled: true}
	credential := SiteCredential{ID: credentialID, SiteID: siteID, CredentialType: "api_key", Meta: JSON(`{}`)}
	bindings := []SiteAPIKeyModel{{
		SiteID: siteID, SiteCredentialID: credentialID, SiteModelID: uuid.NullUUID{UUID: siteModelID, Valid: true}, Available: true, Enabled: false,
	}}

	eligible := eligibleRouteChannelCount(
		model,
		site,
		bindings,
		map[uuid.UUID]SiteCredential{credentialID: credential},
		[]SiteCredential{credential},
		nil,
		nil,
	)
	if eligible != 0 {
		t.Fatalf("eligible channels = %d, want no generic fallback when a binding exists", eligible)
	}
}

func TestRouteInsightLogGroupsUseWindowLogsForStatsAndLastRoute(t *testing.T) {
	modelID := uuid.New()
	firstLogID := uuid.New()
	lastLogID := uuid.New()
	firstCreatedAt := time.Date(2026, 5, 10, 12, 1, 0, 0, time.UTC)
	lastCreatedAt := firstCreatedAt.Add(time.Minute)

	groups := groupRouteInsightLogs([]RequestLog{
		{
			ID:               firstLogID,
			CanonicalModelID: uuid.NullUUID{UUID: modelID, Valid: true},
			CreatedAt:        firstCreatedAt,
			Success:          false,
			StatusCode:       500,
		},
		{
			ID:               lastLogID,
			CanonicalModelID: uuid.NullUUID{UUID: modelID, Valid: true},
			CreatedAt:        lastCreatedAt,
			Success:          true,
			StatusCode:       200,
			LatencyMS:        sql.NullInt64{Int64: 25, Valid: true},
		},
	})

	if len(groups.ByModel[modelID]) != 2 {
		t.Fatalf("expected two window logs, got %d", len(groups.ByModel[modelID]))
	}
	if len(groups.IDs) != 2 || groups.IDs[0] != firstLogID || groups.IDs[1] != lastLogID {
		t.Fatalf("expected window log ids, got %#v", groups.IDs)
	}

	row := RouteOverviewRow{CanonicalModelID: modelID}
	fillRouteOverviewStats(&row, groups.ByModel[modelID], nil, nil)

	if row.RequestCount24h != 2 || row.SuccessCount24h != 1 {
		t.Fatalf("expected window stats, got requests=%d success=%d", row.RequestCount24h, row.SuccessCount24h)
	}
	if !row.LastRoutedAt.Valid || !row.LastRoutedAt.Time.Equal(lastCreatedAt) {
		t.Fatalf("expected last routed to use latest window log, got %#v", row.LastRoutedAt)
	}
}

func TestRouteInsightStatsLeaveLastRouteEmptyWithoutWindowTraffic(t *testing.T) {
	modelID := uuid.New()
	row := RouteOverviewRow{CanonicalModelID: modelID}
	fillRouteOverviewStats(&row, nil, nil, nil)

	if row.RequestCount24h != 0 {
		t.Fatalf("expected no window request count, got %d", row.RequestCount24h)
	}
	if row.LastRoutedAt.Valid {
		t.Fatalf("expected empty last routed without window traffic, got %#v", row.LastRoutedAt)
	}
}

func TestRouteInsightStatsAggregateUsageAndChooseLastRouteByTimeAndID(t *testing.T) {
	modelID := uuid.New()
	siteID := uuid.New()
	lowerSiteID := uuid.New()
	firstLogID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	lowerTieLogID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	higherTieLogID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	firstCreatedAt := time.Date(2026, 5, 10, 12, 1, 0, 0, time.UTC)
	lastCreatedAt := firstCreatedAt.Add(time.Minute)

	row := RouteOverviewRow{CanonicalModelID: modelID}
	fillRouteOverviewStats(&row, []RequestLog{
		{
			ID:               firstLogID,
			CanonicalModelID: uuid.NullUUID{UUID: modelID, Valid: true},
			SiteID:           uuid.NullUUID{UUID: siteID, Valid: true},
			CreatedAt:        firstCreatedAt,
			Success:          true,
			StatusCode:       200,
			LatencyMS:        sql.NullInt64{Int64: 30, Valid: true},
		},
		{
			ID:               lowerTieLogID,
			CanonicalModelID: uuid.NullUUID{UUID: modelID, Valid: true},
			SiteID:           uuid.NullUUID{UUID: lowerSiteID, Valid: true},
			CreatedAt:        lastCreatedAt,
			Success:          false,
			StatusCode:       503,
			LatencyMS:        sql.NullInt64{Int64: 90, Valid: true},
		},
		{
			ID:               higherTieLogID,
			CanonicalModelID: uuid.NullUUID{UUID: modelID, Valid: true},
			SiteID:           uuid.NullUUID{UUID: siteID, Valid: true},
			CreatedAt:        lastCreatedAt,
			Success:          true,
			StatusCode:       201,
		},
	}, map[uuid.UUID]UsageRecord{
		firstLogID: {
			RequestLogID:     firstLogID,
			PromptTokens:     10,
			CompletionTokens: 20,
			EstimatedCost:    sql.NullFloat64{Float64: 0.15, Valid: true},
		},
		lowerTieLogID: {
			RequestLogID:     lowerTieLogID,
			CompletionTokens: 5,
		},
		higherTieLogID: {
			RequestLogID:  higherTieLogID,
			PromptTokens:  7,
			EstimatedCost: sql.NullFloat64{Float64: 0.25, Valid: true},
		},
	}, map[uuid.UUID]Site{
		siteID:      {ID: siteID, Name: "Winning Site"},
		lowerSiteID: {ID: lowerSiteID, Name: "Lower Tie Site"},
	})

	if row.RequestCount24h != 3 || row.SuccessCount24h != 2 {
		t.Fatalf("expected request and success counts, got requests=%d success=%d", row.RequestCount24h, row.SuccessCount24h)
	}
	if !row.SuccessRate24h.Valid || math.Abs(row.SuccessRate24h.Float64-float64(2)/float64(3)) > 0.000001 {
		t.Fatalf("success rate = %#v, want 2/3", row.SuccessRate24h)
	}
	if !row.AvgLatencyMS24h.Valid || row.AvgLatencyMS24h.Int64 != 60 {
		t.Fatalf("avg latency = %#v, want 60", row.AvgLatencyMS24h)
	}
	if !row.PromptTokens24h.Valid || row.PromptTokens24h.Int64 != 17 {
		t.Fatalf("prompt tokens = %#v, want 17", row.PromptTokens24h)
	}
	if !row.CompletionTokens24h.Valid || row.CompletionTokens24h.Int64 != 25 {
		t.Fatalf("completion tokens = %#v, want 25", row.CompletionTokens24h)
	}
	if !row.EstimatedCost24h.Valid || math.Abs(row.EstimatedCost24h.Float64-0.4) > 0.000001 {
		t.Fatalf("estimated cost = %#v, want 0.4", row.EstimatedCost24h)
	}
	if !row.LastRoutedAt.Valid || !row.LastRoutedAt.Time.Equal(lastCreatedAt) {
		t.Fatalf("last routed at = %#v, want %v", row.LastRoutedAt, lastCreatedAt)
	}
	if !row.LastStatusCode.Valid || row.LastStatusCode.Int64 != 201 {
		t.Fatalf("last status = %#v, want 201", row.LastStatusCode)
	}
	if !row.LastSuccess.Valid || !row.LastSuccess.Bool {
		t.Fatalf("last success = %#v, want true", row.LastSuccess)
	}
	if !row.LastSiteName.Valid || row.LastSiteName.String != "Winning Site" {
		t.Fatalf("last site = %#v, want Winning Site", row.LastSiteName)
	}
}

func TestModelAvailableKeyCountDeduplicatesCredentialRowsAndIgnoresDisabledMappings(t *testing.T) {
	siteID := uuid.New()
	siteModelID := uuid.New()
	credentialID := uuid.New()
	disabledMappingCredentialID := uuid.New()

	count := modelAvailableKeyCount(
		SiteModel{ID: siteModelID, SiteID: siteID, Status: "active"},
		Site{ID: siteID, SiteType: "newapi", Status: "active", Enabled: true},
		[]SiteAPIKeyModel{
			{SiteCredentialID: credentialID, SiteModelID: uuid.NullUUID{UUID: siteModelID, Valid: true}, Available: true, Enabled: true},
			{SiteCredentialID: credentialID, SiteModelID: uuid.NullUUID{UUID: siteModelID, Valid: true}, Available: true, Enabled: true},
			{SiteCredentialID: disabledMappingCredentialID, SiteModelID: uuid.NullUUID{UUID: siteModelID, Valid: true}, Available: true, Enabled: false},
		},
		map[uuid.UUID]SiteCredential{
			credentialID:                {ID: credentialID, SiteID: siteID, CredentialType: "api_key:1", Meta: JSON(`{}`)},
			disabledMappingCredentialID: {ID: disabledMappingCredentialID, SiteID: siteID, CredentialType: "api_key:2", Meta: JSON(`{}`)},
		},
		nil,
		nil,
	)

	if count != 1 {
		t.Fatalf("expected one deduplicated available key, got %d", count)
	}
}

func TestPricingRankPrefersRequestedThenDefaultThenCheapest(t *testing.T) {
	requested := SiteModelPricing{GroupName: "vip", InputValue: sql.NullFloat64{Float64: 10, Valid: true}}
	defaultGroup := SiteModelPricing{GroupName: "default", InputValue: sql.NullFloat64{Float64: 1, Valid: true}}
	cheapOther := SiteModelPricing{GroupName: "other", InputValue: sql.NullFloat64{Float64: 0.1, Valid: true}}

	if !(pricingRank(requested, "vip") < pricingRank(defaultGroup, "vip")) {
		t.Fatal("requested group should outrank default group")
	}
	if !(pricingRank(defaultGroup, "vip") < pricingRank(cheapOther, "vip")) {
		t.Fatal("default group should outrank cheaper non-default groups")
	}
}

func TestCollectSupportedEndpointTypesReadsSiteModelCapabilities(t *testing.T) {
	model := SiteModel{
		Capabilities: JSON(`{
			"supported_endpoint_types": ["openai-response", "openai", "openai-response"]
		}`),
	}

	got := collectSupportedEndpointTypes(model)
	if len(got) != 2 || got[0] != "openai-response" || got[1] != "openai" {
		t.Fatalf("unexpected endpoint types: %#v", got)
	}
}

func TestNormalizeRequestLogPagination(t *testing.T) {
	page, pageSize := normalizeRequestLogPagination(0, 0)
	if page != 1 || pageSize != 50 {
		t.Fatalf("expected default page/pageSize, got %d/%d", page, pageSize)
	}

	page, pageSize = normalizeRequestLogPagination(3, 500)
	if page != 3 || pageSize != 200 {
		t.Fatalf("expected capped pageSize, got %d/%d", page, pageSize)
	}
}

func TestRouteCooldownNullableUUIDExpr(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	assertEqClause(t, nullableUUIDExpr("site_model_id", uuid.NullUUID{UUID: id, Valid: true}), "site_model_id", id)
	assertEqClause(t, nullableUUIDExpr("site_model_id", uuid.NullUUID{}), "site_model_id", nil)
}

func assertEqClause(t *testing.T, expr clause.Expression, column string, value any) {
	t.Helper()
	eq, ok := expr.(clause.Eq)
	if !ok {
		t.Fatalf("expected clause.Eq, got %T", expr)
	}
	if columnName(eq.Column) != column || eq.Value != value {
		t.Fatalf("unexpected eq clause: %#v", eq)
	}
}

func assertNeqClause(t *testing.T, expr clause.Expression, column string, value any) {
	t.Helper()
	neq, ok := expr.(clause.Neq)
	if !ok {
		t.Fatalf("expected clause.Neq, got %T", expr)
	}
	if columnName(neq.Column) != column || neq.Value != value {
		t.Fatalf("unexpected neq clause: %#v", neq)
	}
}

func assertGteClause(t *testing.T, expr clause.Expression, column string, value any) {
	t.Helper()
	gte, ok := expr.(clause.Gte)
	if !ok {
		t.Fatalf("expected clause.Gte, got %T", expr)
	}
	if columnName(gte.Column) != column || gte.Value != value {
		t.Fatalf("unexpected gte clause: %#v", gte)
	}
}

func assertLteClause(t *testing.T, expr clause.Expression, column string, value any) {
	t.Helper()
	lte, ok := expr.(clause.Lte)
	if !ok {
		t.Fatalf("expected clause.Lte, got %T", expr)
	}
	if columnName(lte.Column) != column || lte.Value != value {
		t.Fatalf("unexpected lte clause: %#v", lte)
	}
}

func columnName(column any) string {
	if value, ok := column.(clause.Column); ok {
		return value.Name
	}
	return ""
}
