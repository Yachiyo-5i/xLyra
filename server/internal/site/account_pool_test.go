package site

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"xlyra/server/internal/adapter"
	"xlyra/server/internal/store"
)

func TestGrokUpdateDoesNotRecalculateQuotaSummary(t *testing.T) {
	t.Parallel()

	f := newAccountPoolFixture(t, accountPoolGrok)
	id := f.addAccount(freshAccountBundle("token-a"), map[string]any{"enabled": true, "tier": "starter"})
	f.addQuotaState(id, 40)
	enabled := true
	if _, err := f.service.UpdateGrokAccount(context.Background(), f.siteID, id, GrokAccountUpdate{Enabled: &enabled}); err != nil {
		t.Fatalf("UpdateGrokAccount() error = %v\n%s", err, f.traceText())
	}
	assertQuotaSentinelUnchanged(t, f)
}

func TestGrokDeleteDoesNotRecalculateQuotaSummary(t *testing.T) {
	t.Parallel()

	f := newAccountPoolFixture(t, accountPoolGrok)
	id := f.addAccount(freshAccountBundle("token-a"), map[string]any{"enabled": true, "tier": "starter"})
	f.addQuotaState(id, 40)
	if err := f.service.DeleteGrokAccount(context.Background(), f.siteID, id); err != nil {
		t.Fatalf("DeleteGrokAccount() error = %v\n%s", err, f.traceText())
	}
	assertQuotaSentinelUnchanged(t, f)
}

func TestRefreshGrokAccountRereadMissUsesSummaryTier(t *testing.T) {
	t.Parallel()

	f := newAccountPoolFixture(t, accountPoolGrok)
	f.failReread = true
	f.summary = adapter.APIKeySummary{Usage: map[string]any{
		"tier": "premium_plus", "email": "fresh@example.com", "remaining": int64(10),
	}}
	id := f.addAccount(freshAccountBundle("good-token"), map[string]any{"enabled": true, "tier": "starter"})
	account, err := f.service.RefreshGrokAccount(context.Background(), f.siteID, id)
	if err != nil {
		t.Fatalf("RefreshGrokAccount() error = %v\n%s", err, f.traceText())
	}
	meta := accountMetaMap(f.credentials[id].Meta)
	if meta["email"] != "fresh@example.com" {
		t.Fatalf("persisted email = %#v, want summary email so the reread miss is observable", meta["email"])
	}
	if account.Tier != "premium_plus" {
		t.Fatalf("tier = %q, want premium_plus from this refresh summary", account.Tier)
	}
	if account.MaskedToken == "fresh@example.com" {
		t.Fatal("reread miss applied the persisted email; only tier is filled from the summary")
	}
}

func TestRefreshGrokAccountTokenFailureMarksReauthRequired(t *testing.T) {
	t.Parallel()
	assertAccountRefreshTokenFailure(t, accountPoolGrok)
}

func TestRefreshGrokAccountSummarizeFailureMarksStale(t *testing.T) {
	t.Parallel()
	assertAccountRefreshSummarizeFailure(t, accountPoolGrok)
}

func TestRefreshGrokAccountsMixedBatchKeepsCredentialCorrespondence(t *testing.T) {
	t.Parallel()

	f := newAccountPoolFixture(t, accountPoolGrok)
	f.failToken = "bad-token"
	f.summarizeErr = errors.New("grok upstream returned status 500")
	f.summary = adapter.APIKeySummary{Usage: map[string]any{"tier": "premium", "remaining": int64(3)}, Models: []adapter.Model{{UpstreamName: "grok-4"}}}
	goodID := f.addTypedAccount("00000000-0000-4000-8000-000000000001", freshAccountBundle("good-token"), map[string]any{"enabled": true, "tier": "starter"})
	badID := f.addTypedAccount("ffffffff-ffff-4fff-8fff-ffffffffffff", freshAccountBundle("bad-token"), map[string]any{"enabled": true, "tier": "starter"})
	results, err := f.service.RefreshGrokAccounts(context.Background(), f.siteID)
	if err != nil {
		t.Fatalf("RefreshGrokAccounts() error = %v\n%s", err, f.traceText())
	}
	if len(results) != 2 || results[0].CredentialID != goodID || results[1].CredentialID != badID {
		t.Fatalf("results = %#v, want good then bad", results)
	}
	if results[0].Account == nil || results[0].Account.CredentialID != goodID || results[0].Error != "" {
		t.Fatalf("good result = %#v", results[0])
	}
	if results[1].Account != nil || !strings.Contains(results[1].Error, "status 500") {
		t.Fatalf("bad result = %#v", results[1])
	}
}

func assertAccountRefreshTokenFailure(t *testing.T, kind accountPoolKind) {
	t.Helper()

	f := newAccountPoolFixture(t, kind)
	id := f.addAccount(expiredAccountBundle(), map[string]any{"enabled": true})
	f.addQuotaState(id, 40)
	err := f.refreshAccount(id)
	if err == nil || !strings.Contains(err.Error(), kind.tokenErrorPrefix()) {
		t.Fatalf("refresh error = %v, want token failure\n%s", err, f.traceText())
	}
	state := f.states[id]
	meta := accountMetaMap(f.credentials[id].Meta)
	if state.SyncStatus != "reauth_required" || state.Enabled || boolFromMeta(meta, "enabled", true) || !boolFromMeta(meta, "reauth_required", false) {
		t.Fatalf("state=%#v meta=%#v, want reauth_required and disabled", state, meta)
	}
}

func assertAccountRefreshSummarizeFailure(t *testing.T, kind accountPoolKind) {
	t.Helper()

	f := newAccountPoolFixture(t, kind)
	f.summarizeErr = fmt.Errorf("%s upstream returned status 500", kind)
	id := f.addAccount(freshAccountBundle("good-token"), map[string]any{"enabled": true})
	f.addQuotaState(id, 40)
	err := f.refreshAccount(id)
	if err == nil || !strings.Contains(err.Error(), "status 500") {
		t.Fatalf("refresh error = %v, want summarize failure\n%s", err, f.traceText())
	}
	state := f.states[id]
	meta := accountMetaMap(f.credentials[id].Meta)
	if state.SyncStatus != "stale" || !state.Enabled || !boolFromMeta(meta, "enabled", true) || boolFromMeta(meta, "reauth_required", false) {
		t.Fatalf("state=%#v meta=%#v, want stale and still enabled", state, meta)
	}
}

func assertQuotaSentinelUnchanged(t *testing.T, f *accountPoolFixture) {
	t.Helper()

	summary, ok := f.quotaSummary()
	if !ok || summary["probe_type"] != "sentinel" || jsonNumber(summary["remaining_min"]) != 999 || f.siteSaves != 0 {
		t.Fatalf("quota summary = %#v ok=%v siteSaves=%d, want untouched sentinel", summary, ok, f.siteSaves)
	}
}

type accountPoolKind string

const (
	accountPoolGrok accountPoolKind = "grok"
)

func (k accountPoolKind) siteType() string {
	return "grok"
}

func (k accountPoolKind) credentialPrefix() string {
	return grokCredentialPrefix
}

func (k accountPoolKind) tokenErrorPrefix() string {
	return "grok account token"
}

func freshAccountBundle(token string) string {
	return fmt.Sprintf(`{"access_token":%q,"refresh_token":"refresh-token","expires_at":9999999999}`, token)
}

func expiredAccountBundle() string {
	return `{"access_token":"expired-token","refresh_token":"","expires_at":1}`
}

type accountPoolFixture struct {
	t                *testing.T
	kind             accountPoolKind
	service          *Service
	siteID           uuid.UUID
	site             store.Site
	credentials      map[uuid.UUID]store.SiteCredential
	states           map[uuid.UUID]store.SiteAPIKeyState
	apiModels        []store.SiteAPIKeyModel
	siteModels       []store.SiteModel
	siteState        store.SiteState
	hasSiteState     bool
	siteStateWritten bool
	siteSaves        int
	failReread       bool
	failToken        string
	summarizeErr     error
	summary          adapter.APIKeySummary
	trace            []string
}

func newAccountPoolFixture(t *testing.T, kind accountPoolKind) *accountPoolFixture {
	t.Helper()

	f := &accountPoolFixture{
		t:           t,
		kind:        kind,
		siteID:      uuid.New(),
		credentials: map[uuid.UUID]store.SiteCredential{},
		states:      map[uuid.UUID]store.SiteAPIKeyState{},
	}
	f.site = store.Site{
		ID:       f.siteID,
		Name:     string(kind),
		SiteType: kind.siteType(),
		BaseURL:  "https://example.test",
		Enabled:  true,
		Meta: siteJSONMeta(t, map[string]any{
			"quota_probe_summary": map[string]any{"probe_type": "sentinel", "remaining_min": 999},
		}),
	}
	db, _ := grokTransactionalGorm(t, siteGormCallbacks{
		query:          f.query,
		create:         f.create,
		update:         f.update,
		deleteCallback: f.delete,
	})
	f.service = NewService(siteStoreWithGorm(t, db), siteTestMasterKey)
	registry := adapter.NewRegistry()
	registry.Register(accountPoolProbeModule{f: f})
	f.service.adapters = registry
	return f
}

func (f *accountPoolFixture) addAccount(secret string, meta map[string]any) uuid.UUID {
	f.t.Helper()
	return f.addTypedAccount(uuid.NewString(), secret, meta)
}

func (f *accountPoolFixture) addTypedAccount(typeID string, secret string, meta map[string]any) uuid.UUID {
	f.t.Helper()
	return f.addCredential(f.kind.credentialPrefix()+typeID, secret, meta)
}

func (f *accountPoolFixture) addCredential(credentialType string, secret string, meta map[string]any) uuid.UUID {
	f.t.Helper()

	id := uuid.New()
	f.credentials[id] = siteEncryptedCredential(f.t, id, f.siteID, credentialType, secret, siteJSONMeta(f.t, meta))
	return id
}

func (f *accountPoolFixture) addQuotaState(id uuid.UUID, remain int64) {
	f.states[id] = store.SiteAPIKeyState{
		SiteCredentialID: id,
		SiteID:           f.siteID,
		Enabled:          true,
		RemainQuota:      sql.NullInt64{Int64: remain, Valid: true},
		UsedQuota:        sql.NullInt64{Int64: 1, Valid: true},
		SyncStatus:       "synced",
		Usage:            store.JSON(`{"limit":100,"tier":"pro","quota_status":"ok","unit":"percent"}`),
	}
}

func (f *accountPoolFixture) refreshAccount(id uuid.UUID) error {
	f.t.Helper()

	_, err := f.service.RefreshGrokAccount(context.Background(), f.siteID, id)
	return err
}

func (f *accountPoolFixture) quotaSummary() (map[string]any, bool) {
	meta := accountMetaMap(f.site.Meta)
	summary, ok := meta[siteMetaQuotaProbeSummary].(map[string]any)
	return summary, ok && summary != nil
}

func (f *accountPoolFixture) traceText() string {
	return strings.Join(f.trace, "\n")
}

func (f *accountPoolFixture) note(format string, args ...any) {
	f.trace = append(f.trace, fmt.Sprintf(format, args...))
}

func (f *accountPoolFixture) rows(tx *gorm.DB, n int) {
	tx.RowsAffected = int64(n)
	tx.Statement.RowsAffected = int64(n)
}

func (f *accountPoolFixture) query(tx *gorm.DB) {
	switch dest := tx.Statement.Dest.(type) {
	case *store.Site:
		*dest = f.site
		f.rows(tx, 1)
	case *store.SiteCredential:
		f.note("get credential %v", clauseUUIDs(tx))
		if f.failReread && f.siteStateWritten {
			tx.AddError(errors.New("credential reread miss"))
			return
		}
		id := f.credentialIDFrom(tx)
		cred, ok := f.credentials[id]
		if !ok {
			tx.AddError(gorm.ErrRecordNotFound)
			return
		}
		*dest = cred
		f.rows(tx, 1)
	case *[]store.SiteCredential:
		values := make([]store.SiteCredential, 0, len(f.credentials))
		for _, cred := range f.credentials {
			values = append(values, cred)
		}
		*dest = values
		f.rows(tx, len(values))
	case *store.SiteAPIKeyState:
		id := f.credentialIDFrom(tx)
		state, ok := f.states[id]
		if !ok {
			tx.AddError(gorm.ErrRecordNotFound)
			return
		}
		*dest = state
		f.rows(tx, 1)
	case *[]store.SiteAPIKeyState:
		values := make([]store.SiteAPIKeyState, 0, len(f.states))
		for _, state := range f.states {
			values = append(values, state)
		}
		*dest = values
		f.rows(tx, len(values))
	case *store.SiteAPIKeyModel:
		if model, ok := f.findAPIModel(tx); ok {
			*dest = model
			f.rows(tx, 1)
			return
		}
		tx.AddError(gorm.ErrRecordNotFound)
	case *[]store.SiteAPIKeyModel:
		*dest = append([]store.SiteAPIKeyModel(nil), f.apiModels...)
		f.rows(tx, len(f.apiModels))
	case *store.SiteModel:
		if model, ok := f.findSiteModel(tx); ok {
			*dest = model
			f.rows(tx, 1)
			return
		}
		tx.AddError(gorm.ErrRecordNotFound)
	case *[]store.SiteModel:
		*dest = append([]store.SiteModel(nil), f.siteModels...)
		f.rows(tx, len(f.siteModels))
	case *store.SiteState:
		if !f.hasSiteState {
			tx.AddError(gorm.ErrRecordNotFound)
			return
		}
		*dest = f.siteState
		f.rows(tx, 1)
	default:
		f.note("query %T", tx.Statement.Dest)
		tx.AddError(fmt.Errorf("unhandled query %T", tx.Statement.Dest))
	}
}

func (f *accountPoolFixture) create(tx *gorm.DB) {
	switch dest := tx.Statement.Dest.(type) {
	case *store.SiteAPIKeyState:
		f.states[dest.SiteCredentialID] = *dest
	case *store.SiteAPIKeyModel:
		if dest.ID == uuid.Nil {
			dest.ID = uuid.New()
		}
		f.apiModels = append(f.apiModels, *dest)
	case *store.SiteModel:
		if dest.ID == uuid.Nil {
			dest.ID = uuid.New()
		}
		f.siteModels = append(f.siteModels, *dest)
	case *store.SiteState:
		f.siteState = *dest
		f.hasSiteState = true
		f.siteStateWritten = true
	default:
		f.note("create %T", tx.Statement.Dest)
		tx.AddError(fmt.Errorf("unhandled create %T", tx.Statement.Dest))
		return
	}
	f.rows(tx, 1)
}

func (f *accountPoolFixture) update(tx *gorm.DB) {
	switch dest := tx.Statement.Dest.(type) {
	case *store.Site:
		f.site = *dest
		f.siteSaves++
	case *store.SiteCredential:
		if dest.ID != uuid.Nil {
			f.credentials[dest.ID] = *dest
		}
	case *store.SiteAPIKeyState:
		if dest.SiteCredentialID != uuid.Nil {
			f.states[dest.SiteCredentialID] = *dest
		}
	case *store.SiteAPIKeyModel:
		f.replaceAPIModel(*dest)
	case *store.SiteModel:
		f.replaceSiteModel(*dest)
	case *store.SiteState:
		f.siteState = *dest
		f.hasSiteState = true
		f.siteStateWritten = true
	case map[string]any:
		if meta, ok := jsonValue(dest["meta"]); ok {
			id := f.credentialIDFrom(tx)
			if id == uuid.Nil {
				tx.AddError(fmt.Errorf("credential meta update missing id"))
				return
			}
			cred := f.credentials[id]
			cred.Meta = meta
			f.credentials[id] = cred
			break
		}
		f.note("update map keys %v", mapKeys(dest))
	default:
		f.note("update %T", tx.Statement.Dest)
		tx.AddError(fmt.Errorf("unhandled update %T", tx.Statement.Dest))
		return
	}
	f.rows(tx, 1)
}

func (f *accountPoolFixture) delete(tx *gorm.DB) {
	id := f.credentialIDFrom(tx)
	f.note("delete model=%T dest=%T id=%s", tx.Statement.Model, tx.Statement.Dest, id)
	switch tx.Statement.Model.(type) {
	case *store.SiteCredential:
		delete(f.credentials, id)
	case *store.SiteAPIKeyState:
		delete(f.states, id)
	case *store.SiteAPIKeyModel:
		kept := f.apiModels[:0]
		for _, model := range f.apiModels {
			if model.SiteCredentialID != id {
				kept = append(kept, model)
			}
		}
		f.apiModels = kept
	}
	f.rows(tx, 1)
}

func (f *accountPoolFixture) findAPIModel(tx *gorm.DB) (store.SiteAPIKeyModel, bool) {
	id := f.credentialIDFrom(tx)
	_, texts := clauseValues(tx)
	for _, model := range f.apiModels {
		if id != uuid.Nil && model.SiteCredentialID != id {
			continue
		}
		if len(texts) == 0 || containsString(texts, model.UpstreamModelName) {
			return model, true
		}
	}
	return store.SiteAPIKeyModel{}, false
}

func (f *accountPoolFixture) findSiteModel(tx *gorm.DB) (store.SiteModel, bool) {
	_, texts := clauseValues(tx)
	for _, model := range f.siteModels {
		if len(texts) == 0 || containsString(texts, model.UpstreamName) {
			return model, true
		}
	}
	return store.SiteModel{}, false
}

func (f *accountPoolFixture) replaceAPIModel(model store.SiteAPIKeyModel) {
	for i := range f.apiModels {
		if model.ID != uuid.Nil && f.apiModels[i].ID == model.ID {
			f.apiModels[i] = model
			return
		}
		if f.apiModels[i].SiteCredentialID == model.SiteCredentialID && f.apiModels[i].UpstreamModelName == model.UpstreamModelName {
			f.apiModels[i] = model
			return
		}
	}
	f.apiModels = append(f.apiModels, model)
}

func (f *accountPoolFixture) replaceSiteModel(model store.SiteModel) {
	for i := range f.siteModels {
		if model.ID != uuid.Nil && f.siteModels[i].ID == model.ID {
			f.siteModels[i] = model
			return
		}
		if f.siteModels[i].UpstreamName == model.UpstreamName {
			f.siteModels[i] = model
			return
		}
	}
	f.siteModels = append(f.siteModels, model)
}

func (f *accountPoolFixture) credentialIDFrom(tx *gorm.DB) uuid.UUID {
	ids, _ := clauseValues(tx)
	for _, id := range ids {
		if _, ok := f.credentials[id]; ok {
			return id
		}
	}
	return uuid.Nil
}

func jsonNumber(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case json.Number:
		number, _ := typed.Float64()
		return number
	default:
		return 0
	}
}

func jsonValue(value any) (store.JSON, bool) {
	switch typed := value.(type) {
	case store.JSON:
		return typed, true
	case []byte:
		return store.JSON(typed), true
	case string:
		return store.JSON(typed), true
	default:
		return nil, false
	}
}

func mapKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func clauseUUIDs(tx *gorm.DB) []uuid.UUID {
	ids, _ := clauseValues(tx)
	return ids
}

func clauseValues(tx *gorm.DB) ([]uuid.UUID, []string) {
	var ids []uuid.UUID
	var texts []string
	if tx.Statement != nil {
		if where, ok := tx.Statement.Clauses["WHERE"].Expression.(clause.Where); ok {
			for _, expr := range where.Exprs {
				collectClauseValues(expr, &ids, &texts)
			}
		}
		for _, value := range tx.Statement.Vars {
			collectClauseValue(value, &ids, &texts)
		}
	}
	return ids, texts
}

func collectClauseValues(expr clause.Expression, ids *[]uuid.UUID, texts *[]string) {
	if expr == nil {
		return
	}
	switch typed := expr.(type) {
	case clause.Where:
		for _, item := range typed.Exprs {
			collectClauseValues(item, ids, texts)
		}
	case clause.AndConditions:
		for _, item := range typed.Exprs {
			collectClauseValues(item, ids, texts)
		}
	case clause.OrConditions:
		for _, item := range typed.Exprs {
			collectClauseValues(item, ids, texts)
		}
	case clause.Eq:
		collectClauseValue(typed.Value, ids, texts)
	case clause.IN:
		for _, value := range typed.Values {
			collectClauseValue(value, ids, texts)
		}
	case clause.Expr:
		for _, value := range typed.Vars {
			collectClauseValue(value, ids, texts)
		}
	case clause.NamedExpr:
		for _, value := range typed.Vars {
			collectClauseValue(value, ids, texts)
		}
	}
}

func collectClauseValue(value any, ids *[]uuid.UUID, texts *[]string) {
	switch typed := value.(type) {
	case uuid.UUID:
		*ids = append(*ids, typed)
	case *uuid.UUID:
		if typed != nil {
			*ids = append(*ids, *typed)
		}
	case string:
		if id, err := uuid.Parse(typed); err == nil {
			*ids = append(*ids, id)
			return
		}
		if typed != "" {
			*texts = append(*texts, typed)
		}
	}
}

type accountPoolProbeModule struct {
	f *accountPoolFixture
}

func (m accountPoolProbeModule) SiteTypes() []string {
	return []string{m.f.kind.siteType()}
}

func (m accountPoolProbeModule) SummarizeAPIKey(_ context.Context, _ adapter.SiteConfig, token string) (adapter.APIKeySummary, error) {
	if m.f.summarizeErr != nil && (m.f.failToken == "" || token == m.f.failToken) {
		return adapter.APIKeySummary{}, m.f.summarizeErr
	}
	return m.f.summary, nil
}
