package site

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"xlyra/server/internal/adapter"
	"xlyra/server/internal/catalog"
	"xlyra/server/internal/store"
)

const poolPricingSiteType = "acme.example/pool"

func TestPluginAccountPoolRefreshCanonicalPricing(t *testing.T) {
	const (
		pricedName    = "acme-pool-priced"
		unmatchedName = "acme-pool-unmatched"
		inputPrice    = 1.25
		outputPrice   = 2.5
	)
	st := openPluginAccountStore(t)
	ctx := context.Background()
	canonical := createPoolCanonical(t, st, pricedName, inputPrice, outputPrice)
	runtime := &poolPricingRuntime{models: []adapter.Model{
		{UpstreamName: pricedName, DisplayName: pricedName},
		{UpstreamName: unmatchedName, DisplayName: unmatchedName},
	}}
	svc := newPoolPricingService(t, st, runtime)
	site := createTypedSite(t, st, poolPricingSiteType)
	insertPoolPricingAccount(t, svc, site.ID)

	refreshed, err := svc.RefreshState(ctx, site.ID)
	if err != nil {
		t.Fatal(err)
	}
	models, err := store.NewSiteModelRepository(st.DB()).ListBySite(ctx, site.ID)
	if err != nil {
		t.Fatal(err)
	}
	priced := poolSiteModelByName(t, models, pricedName)
	unmatched := poolSiteModelByName(t, models, unmatchedName)
	if unmatched.Status == "" || unmatched.Status == "unavailable" {
		t.Fatalf("unmatched model status = %q", unmatched.Status)
	}
	if !priced.CanonicalID.Valid || priced.CanonicalID.UUID != canonical.ID {
		t.Fatalf("priced canonical = %#v, want %s", priced.CanonicalID, canonical.ID)
	}
	rows, err := store.NewSiteModelPricingRepository(st.DB()).ListBySite(ctx, site.ID)
	if err != nil {
		t.Fatal(err)
	}
	row, ok := poolPricingByName(rows, pricedName)
	if !ok || row.PricingSource != "canonical_fallback" || !row.Available || !row.InputValue.Valid || row.InputValue.Float64 != inputPrice || !row.OutputValue.Valid || row.OutputValue.Float64 != outputPrice {
		t.Fatalf("priced row = %#v ok=%v", row, ok)
	}
	if _, found := poolPricingByName(rows, unmatchedName); found {
		t.Fatalf("unmatched model has a pricing row: %#v", rows)
	}

	state, err := store.NewSiteStateRepository(st.DB()).GetBySite(ctx, site.ID)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.State.SyncStatus != state.SyncStatus || refreshed.State.SyncMessage.String != state.SyncMessage.String {
		t.Fatalf("returned state status=%s message=%q, stored status=%s message=%q", refreshed.State.SyncStatus, refreshed.State.SyncMessage.String, state.SyncStatus, state.SyncMessage.String)
	}
	if state.SyncStatus != "partial" || !state.ValidationOK.Valid || !state.ValidationOK.Bool {
		t.Fatalf("pricing gap status=%s validation=%v message=%q", state.SyncStatus, state.ValidationOK, state.SyncMessage.String)
	}
	if !strings.Contains(state.SyncMessage.String, "have no pricing") || !strings.Contains(state.SyncMessage.String, unmatchedName) || strings.Contains(state.SyncMessage.String, pricedName) {
		t.Fatalf("pricing gap message = %q", state.SyncMessage.String)
	}
}

func TestPluginAccountPoolRefreshKeepsManualAndUpstreamPricing(t *testing.T) {
	const (
		manualName   = "acme-pool-manual"
		upstreamName = "acme-pool-upstream"
		inputPrice   = 1.25
		outputPrice  = 2.5
		manualInput  = 9.5
		manualOutput = 8.5
	)
	st := openPluginAccountStore(t)
	ctx := context.Background()
	createPoolCanonical(t, st, manualName, inputPrice, outputPrice)
	createPoolCanonical(t, st, upstreamName, inputPrice, outputPrice)
	runtime := &poolPricingRuntime{models: []adapter.Model{
		{UpstreamName: manualName, DisplayName: manualName},
		{UpstreamName: upstreamName, DisplayName: upstreamName},
	}}
	svc := newPoolPricingService(t, st, runtime)
	site := createTypedSite(t, st, poolPricingSiteType)
	insertPoolPricingAccount(t, svc, site.ID)
	manualModel := insertPoolSiteModel(t, st, site.ID, manualName)
	upstreamModel := insertPoolSiteModel(t, st, site.ID, upstreamName)
	manualBefore := seedPoolPricing(t, st, site.ID, manualModel, "manual", true, manualInput, manualOutput)
	upstreamBefore := seedPoolPricing(t, st, site.ID, upstreamModel, "plugin", false, inputPrice, outputPrice)

	refreshed, err := svc.RefreshState(ctx, site.ID)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.State.SyncStatus == "failed" || strings.Contains(refreshed.State.SyncMessage.String, "have no pricing") {
		t.Fatalf("priced refresh status=%s message=%q", refreshed.State.SyncStatus, refreshed.State.SyncMessage.String)
	}
	rows, err := store.NewSiteModelPricingRepository(st.DB()).ListBySite(ctx, site.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("pricing rows = %#v", rows)
	}
	manualAfter, ok := poolPricingByName(rows, manualName)
	if !ok || !samePoolPricing(manualBefore, manualAfter) {
		t.Fatalf("manual pricing changed from %#v to %#v", manualBefore, manualAfter)
	}
	upstreamAfter, ok := poolPricingByName(rows, upstreamName)
	if !ok || !samePoolPricing(upstreamBefore, upstreamAfter) {
		t.Fatalf("upstream pricing changed from %#v to %#v", upstreamBefore, upstreamAfter)
	}
}

func TestPluginAccountPoolRefreshZeroAccountsStaysSynced(t *testing.T) {
	st := openPluginAccountStore(t)
	ctx := context.Background()
	runtime := &poolPricingRuntime{models: []adapter.Model{
		{UpstreamName: "acme-pool-priced", DisplayName: "acme-pool-priced"},
	}}
	svc := newPoolPricingService(t, st, runtime)
	site := createTypedSite(t, st, poolPricingSiteType)

	refreshed, err := svc.RefreshState(ctx, site.ID)
	if err != nil {
		t.Fatal(err)
	}
	state, err := store.NewSiteStateRepository(st.DB()).GetBySite(ctx, site.ID)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.State.SyncStatus != "synced" || state.SyncStatus != "synced" || refreshed.State.ModelCount != 0 || state.ModelCount != 0 {
		t.Fatalf("empty refresh returned status=%s models=%d stored status=%s models=%d", refreshed.State.SyncStatus, refreshed.State.ModelCount, state.SyncStatus, state.ModelCount)
	}
	if !state.ValidationOK.Valid || !state.ValidationOK.Bool || state.SyncMessage.Valid || strings.Contains(state.SyncMessage.String, "have no pricing") {
		t.Fatalf("empty refresh validation=%v message=%q", state.ValidationOK, state.SyncMessage.String)
	}
	models, err := store.NewSiteModelRepository(st.DB()).ListBySite(ctx, site.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 0 {
		t.Fatalf("empty refresh stored models %#v", models)
	}
	rows, err := store.NewSiteModelPricingRepository(st.DB()).ListBySite(ctx, site.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("empty refresh stored pricing %#v", rows)
	}
}

func newPoolPricingService(t *testing.T, st *store.Store, runtime PluginAccountRuntime) *Service {
	t.Helper()
	spec := pluginAccountTestSpec()
	spec.ModelsPerAccount = true
	ClearPluginSiteTypes()
	t.Cleanup(ClearPluginSiteTypes)
	if err := SetPluginSiteTypes([]PluginSiteType{{
		Descriptor: SiteTypeDescriptor{SiteType: poolPricingSiteType, DisplayName: "Acme Pool"},
		PluginID:   "acme.example",
		Enabled:    true,
		Hosts:      []string{"api.example.com"},
		Pool:       spec,
	}}); err != nil {
		t.Fatal(err)
	}
	svc := NewService(st, siteTestMasterKey)
	if err := svc.SetPluginAccountRuntimes(map[string]PluginAccountRuntime{poolPricingSiteType: runtime}); err != nil {
		t.Fatal(err)
	}
	return svc
}

func createPoolCanonical(t *testing.T, st *store.Store, upstreamName string, inputPrice, outputPrice float64) store.CanonicalModel {
	t.Helper()
	key := catalog.CanonicalModelKeyFromUpstream(upstreamName)
	if key == "" || key != upstreamName {
		t.Fatalf("canonical key for %q = %q", upstreamName, key)
	}
	model, err := store.NewCanonicalModelRepository(st.DB()).Create(context.Background(), store.UpsertCanonicalModelParams{
		ModelKey:      key,
		DisplayName:   key,
		Provider:      "acme",
		Category:      "chat",
		Status:        "active",
		InputPrice:    sql.NullFloat64{Float64: inputPrice, Valid: true},
		OutputPrice:   sql.NullFloat64{Float64: outputPrice, Valid: true},
		PricingSource: store.CanonicalPricingSourceCatalog,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !model.InputPrice.Valid || model.InputPrice.Float64 != inputPrice {
		t.Fatalf("canonical input price = %#v", model.InputPrice)
	}
	return model
}

func insertPoolPricingAccount(t *testing.T, svc *Service, siteID uuid.UUID) store.SiteCredential {
	t.Helper()
	encoded, err := encodePluginCredential(map[string]string{
		"access_token": "pool-access", "refresh_token": "pool-refresh", "subject": "pool-user", "realm": "intl",
	})
	if err != nil {
		t.Fatal(err)
	}
	encrypted, masked, err := svc.credentials.Encrypt(encoded)
	if err != nil {
		t.Fatal(err)
	}
	meta, err := json.Marshal(map[string]any{"enabled": true, "subject": "pool-user", "realm": "intl"})
	if err != nil {
		t.Fatal(err)
	}
	cred, err := store.NewSiteCredentialRepository(svc.db.DB()).Upsert(context.Background(), store.UpsertSiteCredentialParams{
		SiteID:          siteID,
		CredentialType:  store.NewAccountCredentialType(poolPricingSiteType, uuid.New()),
		EncryptedSecret: encrypted,
		MaskedSecret:    masked,
		Meta:            meta,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.NewSiteAPIKeyStateRepository(svc.db.DB()).Upsert(context.Background(), store.UpsertSiteAPIKeyStateParams{
		SiteCredentialID: cred.ID,
		SiteID:           siteID,
		Name:             "pool-user",
		Enabled:          true,
		SyncStatus:       "synced",
	}); err != nil {
		t.Fatal(err)
	}
	return cred
}

func insertPoolSiteModel(t *testing.T, st *store.Store, siteID uuid.UUID, name string) store.SiteModel {
	t.Helper()
	model, err := store.NewSiteModelRepository(st.DB()).Upsert(context.Background(), store.UpsertSiteModelParams{
		SiteID:       siteID,
		UpstreamName: name,
		DisplayName:  name,
		Capabilities: store.JSON(`{}`),
		Status:       "active",
	})
	if err != nil {
		t.Fatal(err)
	}
	return model
}

func seedPoolPricing(t *testing.T, st *store.Store, siteID uuid.UUID, model store.SiteModel, source string, manual bool, inputPrice, outputPrice float64) store.SiteModelPricing {
	t.Helper()
	row, err := store.NewSiteModelPricingRepository(st.DB()).Upsert(context.Background(), store.UpsertSiteModelPricingParams{
		SiteID:         siteID,
		SiteModelID:    model.ID,
		ModelName:      model.UpstreamName,
		GroupName:      "default",
		BillingType:    "tokens",
		Currency:       "USD",
		GroupRatio:     1,
		InputValue:     inputPrice,
		OutputValue:    outputPrice,
		PricingSource:  source,
		ManualOverride: manual,
		Available:      true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func poolSiteModelByName(t *testing.T, models []store.SiteModel, name string) store.SiteModel {
	t.Helper()
	for _, model := range models {
		if model.UpstreamName == name {
			return model
		}
	}
	t.Fatalf("site model %s was not stored", name)
	return store.SiteModel{}
}

func poolPricingByName(rows []store.SiteModelPricing, name string) (store.SiteModelPricing, bool) {
	var found store.SiteModelPricing
	ok := false
	for _, row := range rows {
		if row.ModelName != name {
			continue
		}
		if ok {
			return store.SiteModelPricing{}, false
		}
		found = row
		ok = true
	}
	return found, ok
}

func samePoolPricing(before, after store.SiteModelPricing) bool {
	return before.ID == after.ID &&
		before.PricingSource == after.PricingSource &&
		before.ManualOverride == after.ManualOverride &&
		before.Available == after.Available &&
		before.InputValue == after.InputValue &&
		before.OutputValue == after.OutputValue &&
		before.ModelName == after.ModelName &&
		before.GroupName == after.GroupName
}

type poolPricingRuntime struct {
	models []adapter.Model
}

func (r *poolPricingRuntime) StartLogin(context.Context, map[string]string) (PluginLoginStart, error) {
	return PluginLoginStart{}, errors.New("login is unused")
}

func (r *poolPricingRuntime) PollLogin(context.Context, string) (PluginLoginPoll, error) {
	return PluginLoginPoll{}, errors.New("login is unused")
}

func (r *poolPricingRuntime) Refresh(_ context.Context, fields map[string]string) (map[string]string, error) {
	out := map[string]string{}
	for key, value := range fields {
		out[key] = value
	}
	return out, nil
}

func (r *poolPricingRuntime) Summarize(context.Context, string, map[string]string) (PluginAccountSummary, error) {
	remaining := 12.0
	limit := 20.0
	return PluginAccountSummary{
		Status: "ok",
		Entries: []QuotaProbeEntry{{
			Label: "credits", Remaining: &remaining, Limit: &limit, Unit: "credits",
		}},
	}, nil
}

func (r *poolPricingRuntime) ListModels(context.Context, map[string]string) ([]adapter.Model, error) {
	return append([]adapter.Model(nil), r.models...), nil
}

func (r *poolPricingRuntime) ParseImport([]byte) ([]PluginImportedAccount, error) {
	return nil, errors.New("import is unused")
}
