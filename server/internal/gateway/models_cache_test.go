package gateway

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/auth"
	"xlyra/server/internal/store"
)

func TestModelsCacheCachesAndInvalidates(t *testing.T) {
	cache := newModelsCache()
	apiKey := store.APIKey{ID: uuid.New()}
	calls := 0
	build := func(context.Context, store.APIKey) (map[string]any, error) {
		calls++
		return map[string]any{
			"object": "list",
			"data": []map[string]any{
				{"id": "model-a"},
			},
		}, nil
	}

	first, err := cache.getOrBuild(context.Background(), apiKey, build)
	if err != nil {
		t.Fatalf("first getOrBuild returned error: %v", err)
	}
	first["object"] = "mutated"

	second, err := cache.getOrBuild(context.Background(), apiKey, build)
	if err != nil {
		t.Fatalf("second getOrBuild returned error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("builder calls = %d, want 1", calls)
	}
	if second["object"] != "list" {
		t.Fatalf("cached payload was mutated: %#v", second)
	}

	cache.invalidate()
	if _, err := cache.getOrBuild(context.Background(), apiKey, build); err != nil {
		t.Fatalf("getOrBuild after invalidate returned error: %v", err)
	}
	if calls != 2 {
		t.Fatalf("builder calls after invalidate = %d, want 2", calls)
	}
}

func TestModelsCacheInvalidateDuringBuildDoesNotStoreStalePayload(t *testing.T) {
	cache := newModelsCache()
	apiKey := store.APIKey{ID: uuid.New()}
	releaseBuild := make(chan struct{})
	buildStarted := make(chan struct{})
	calls := 0

	build := func(context.Context, store.APIKey) (map[string]any, error) {
		calls++
		if calls == 1 {
			close(buildStarted)
			<-releaseBuild
			return map[string]any{"object": "stale", "data": []map[string]any{}}, nil
		}
		return map[string]any{"object": "fresh", "data": []map[string]any{}}, nil
	}

	done := make(chan error)
	go func() {
		_, err := cache.getOrBuild(context.Background(), apiKey, build)
		done <- err
	}()
	<-buildStarted
	cache.invalidate()
	close(releaseBuild)
	if err := <-done; err != nil {
		t.Fatalf("first getOrBuild returned error: %v", err)
	}

	payload, err := cache.getOrBuild(context.Background(), apiKey, build)
	if err != nil {
		t.Fatalf("second getOrBuild returned error: %v", err)
	}
	if calls != 2 {
		t.Fatalf("builder calls = %d, want 2", calls)
	}
	if payload["object"] != "fresh" {
		t.Fatalf("payload = %#v, want fresh", payload)
	}
}

func TestModelsPayloadETagIsStable(t *testing.T) {
	left := map[string]any{
		"object": "list",
		"data": []map[string]any{
			{"id": "model-a", "object": "model"},
		},
	}
	right := map[string]any{
		"data": []map[string]any{
			{"object": "model", "id": "model-a"},
		},
		"object": "list",
	}

	leftETag := modelsPayloadETag(left)
	rightETag := modelsPayloadETag(right)
	if leftETag == "" {
		t.Fatal("modelsPayloadETag returned empty")
	}
	if leftETag != rightETag {
		t.Fatalf("etag mismatch: %q != %q", leftETag, rightETag)
	}
}

func TestModelsETagMatches(t *testing.T) {
	etag := `"models-sha256-test"`
	for _, values := range [][]string{
		{etag},
		{`W/` + etag},
		{`"other", ` + etag},
		{"*"},
	} {
		if !modelsETagMatches(values, etag) {
			t.Fatalf("modelsETagMatches(%v, %q) = false, want true", values, etag)
		}
	}
	if modelsETagMatches([]string{`"other"`}, etag) {
		t.Fatal("modelsETagMatches returned true for non-matching etag")
	}
}

func TestModelsCacheInvalidateKeyOnlyRemovesTargetAPIKey(t *testing.T) {
	cache := newModelsCache()
	firstKey := store.APIKey{ID: uuid.New()}
	secondKey := store.APIKey{ID: uuid.New()}
	calls := map[uuid.UUID]int{}
	build := func(_ context.Context, apiKey store.APIKey) (map[string]any, error) {
		calls[apiKey.ID]++
		return map[string]any{
			"object": "list",
			"data": []map[string]any{
				{"id": apiKey.ID.String()},
			},
		}, nil
	}

	if _, err := cache.getOrBuild(context.Background(), firstKey, build); err != nil {
		t.Fatalf("first key build: %v", err)
	}
	if _, err := cache.getOrBuild(context.Background(), secondKey, build); err != nil {
		t.Fatalf("second key build: %v", err)
	}

	cache.invalidateKey(firstKey.ID)

	if _, err := cache.getOrBuild(context.Background(), firstKey, build); err != nil {
		t.Fatalf("first key rebuild: %v", err)
	}
	if _, err := cache.getOrBuild(context.Background(), secondKey, build); err != nil {
		t.Fatalf("second key cached read: %v", err)
	}
	if calls[firstKey.ID] != 2 {
		t.Fatalf("first key build calls = %d, want 2", calls[firstKey.ID])
	}
	if calls[secondKey.ID] != 1 {
		t.Fatalf("second key build calls = %d, want 1", calls[secondKey.ID])
	}

	cache.invalidateKey(uuid.Nil)
	if _, err := cache.getOrBuild(context.Background(), secondKey, build); err != nil {
		t.Fatalf("second key cached read after nil invalidation: %v", err)
	}
	if calls[secondKey.ID] != 1 {
		t.Fatalf("nil invalidation should not clear second key, calls = %d", calls[secondKey.ID])
	}
}

func TestCloneModelsPayloadCopiesDataRows(t *testing.T) {
	t.Parallel()

	original := map[string]any{
		"object": "list",
		"data": []map[string]any{
			{"id": "gpt-5.4", "object": "model"},
		},
	}
	clone := cloneModelsPayload(original)
	clone["object"] = "mutated"
	cloneRows := clone["data"].([]map[string]any)
	cloneRows[0]["id"] = "mutated"

	if original["object"] != "list" {
		t.Fatalf("top-level payload mutated: %#v", original)
	}
	originalRows := original["data"].([]map[string]any)
	if originalRows[0]["id"] != "gpt-5.4" {
		t.Fatalf("data row mutated: %#v", originalRows[0])
	}
	if cloneModelsPayload(nil) != nil {
		t.Fatal("nil payload should clone to nil")
	}
}

func TestModelsPayloadHelpersUseCanonicalMetadataAndDefaults(t *testing.T) {
	t.Parallel()

	modelID := uuid.New()
	createdAt := time.Date(2026, 6, 22, 10, 30, 0, 0, time.UTC)
	item := canonicalModelPayload(store.CanonicalModel{
		ID:          modelID,
		ModelKey:    "gpt-5.4",
		DisplayName: "GPT 5.4",
		Provider:    "openai",
		Category:    "chat",
		CreatedAt:   createdAt,
	})
	if item["id"] != "gpt-5.4" || item["object"] != "model" || item["created"] != createdAt.Unix() || item["owned_by"] != "openai" {
		t.Fatalf("unexpected canonical payload: %#v", item)
	}
	metadata := item["metadata"].(map[string]any)
	if metadata["canonical_model_id"] != modelID.String() || metadata["display_name"] != "GPT 5.4" || metadata["category"] != "chat" {
		t.Fatalf("unexpected metadata: %#v", metadata)
	}

	fallback := canonicalModelPayload(store.CanonicalModel{ID: modelID, ModelKey: "vendor-model"})
	fallbackMetadata := fallback["metadata"].(map[string]any)
	if fallback["owned_by"] != "xlyra" || fallbackMetadata["display_name"] != "vendor-model" || fallbackMetadata["category"] != "chat" {
		t.Fatalf("unexpected fallback payload: %#v", fallback)
	}

	empty := emptyModelsPayload()
	if empty["object"] != "list" {
		t.Fatalf("empty payload object = %#v, want list", empty["object"])
	}
	if rows, ok := empty["data"].([]map[string]any); !ok || len(rows) != 0 {
		t.Fatalf("empty payload data = %#v, want empty rows", empty["data"])
	}
}

func TestRouteCandidateQueryCarriesAllowedAccessSets(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	siteModelID := uuid.New()
	query := routeCandidateQuery("gpt-5.4", auth.APIKeyAccessSets{
		AllowedSiteIDs:      []uuid.UUID{siteID},
		AllowedSiteModelIDs: []uuid.UUID{siteModelID},
	})
	if query.ModelKey != "gpt-5.4" {
		t.Fatalf("ModelKey = %q, want gpt-5.4", query.ModelKey)
	}
	if len(query.AllowedSiteIDs) != 1 || query.AllowedSiteIDs[0] != siteID {
		t.Fatalf("AllowedSiteIDs = %#v", query.AllowedSiteIDs)
	}
	if len(query.AllowedSiteModelIDs) != 1 || query.AllowedSiteModelIDs[0] != siteModelID {
		t.Fatalf("AllowedSiteModelIDs = %#v", query.AllowedSiteModelIDs)
	}
	if defaultString("", "fallback") != "fallback" || defaultString("value", "fallback") != "value" {
		t.Fatal("defaultString did not preserve value/fallback")
	}
}

func TestGatewayCredentialCountsFilterStateMetaAndCooldowns(t *testing.T) {
	siteID := uuid.New()
	otherSiteID := uuid.New()
	modelID := uuid.New()
	okCredentialID := uuid.New()
	disabledCredentialID := uuid.New()
	missingSecretCredentialID := uuid.New()
	permanentFailureCredentialID := uuid.New()
	coolingCredentialID := uuid.New()
	otherModelCredentialID := uuid.New()
	nonAPIKeyCredentialID := uuid.New()

	credentials := []store.SiteCredential{
		{ID: okCredentialID, SiteID: siteID, CredentialType: "api_key", Meta: store.JSON(`{"enabled":true}`)},
		{ID: disabledCredentialID, SiteID: siteID, CredentialType: "api_key:secondary", Meta: store.JSON(`{"enabled":false}`)},
		{ID: missingSecretCredentialID, SiteID: siteID, CredentialType: "api_key", Meta: store.JSON(`{"raw_key_missing":true}`)},
		{ID: permanentFailureCredentialID, SiteID: siteID, CredentialType: "api_key", Meta: store.JSON(`{}`)},
		{ID: coolingCredentialID, SiteID: siteID, CredentialType: "api_key", Meta: store.JSON(`{}`)},
		{ID: otherModelCredentialID, SiteID: siteID, CredentialType: "api_key", Meta: store.JSON(`{}`)},
		{ID: nonAPIKeyCredentialID, SiteID: siteID, CredentialType: "oauth_bearer", Meta: store.JSON(`{}`)},
		{ID: uuid.New(), SiteID: otherSiteID, CredentialType: "api_key", Meta: store.JSON(`{"enabled":"true"}`)},
	}
	states := map[uuid.UUID]store.SiteAPIKeyState{
		permanentFailureCredentialID: {
			SiteCredentialID: permanentFailureCredentialID,
			Enabled:          true,
			SyncStatus:       "failed",
			SyncMessage:      sql.NullString{String: `upstream returned 401: {"error":{"code":"invalid_api_key"}}`, Valid: true},
		},
	}
	cooldowns := []store.RouteCooldown{
		{
			SiteID:           siteID,
			SiteModelID:      uuid.NullUUID{UUID: modelID, Valid: true},
			SiteCredentialID: uuid.NullUUID{UUID: coolingCredentialID, Valid: true},
		},
	}

	siteCounts := gatewaySiteCredentialCounts(credentials, states, cooldowns)
	if siteCounts[siteID] != 2 {
		t.Fatalf("site credential count = %d, want 2", siteCounts[siteID])
	}
	if siteCounts[otherSiteID] != 1 {
		t.Fatalf("other site credential count = %d, want 1", siteCounts[otherSiteID])
	}

	apiKeyModels := []store.SiteAPIKeyModel{
		{SiteID: siteID, SiteCredentialID: okCredentialID, SiteModelID: uuid.NullUUID{UUID: modelID, Valid: true}, Available: true, Enabled: true},
		{SiteID: siteID, SiteCredentialID: okCredentialID, SiteModelID: uuid.NullUUID{UUID: modelID, Valid: true}, Available: true, Enabled: true},
		{SiteID: siteID, SiteCredentialID: disabledCredentialID, SiteModelID: uuid.NullUUID{UUID: modelID, Valid: true}, Available: true, Enabled: true},
		{SiteID: siteID, SiteCredentialID: permanentFailureCredentialID, SiteModelID: uuid.NullUUID{UUID: modelID, Valid: true}, Available: true, Enabled: true},
		{SiteID: siteID, SiteCredentialID: coolingCredentialID, SiteModelID: uuid.NullUUID{UUID: modelID, Valid: true}, Available: true, Enabled: true},
		{SiteID: siteID, SiteCredentialID: otherModelCredentialID, SiteModelID: uuid.NullUUID{UUID: uuid.New(), Valid: true}, Available: true, Enabled: true},
		{SiteID: siteID, SiteCredentialID: okCredentialID, Available: true, Enabled: true},
		{SiteID: siteID, SiteCredentialID: okCredentialID, SiteModelID: uuid.NullUUID{UUID: modelID, Valid: true}, Available: false, Enabled: true},
		{SiteID: siteID, SiteCredentialID: okCredentialID, SiteModelID: uuid.NullUUID{UUID: modelID, Valid: true}, Available: true, Enabled: false},
	}
	modelCounts := gatewayModelCredentialCounts(apiKeyModels, credentials, states, cooldowns)
	if modelCounts[modelID] != 1 {
		t.Fatalf("model credential count = %d, want 1", modelCounts[modelID])
	}
	if len(modelCounts) != 2 {
		t.Fatalf("model counts = %#v, want two model entries", modelCounts)
	}
}

func TestGatewayCredentialHelperPredicates(t *testing.T) {
	t.Parallel()

	if !gatewayAPIKeyCredentialType("api_key") || !gatewayAPIKeyCredentialType("api_key:1") {
		t.Fatal("api_key credential types should be accepted")
	}
	if gatewayAPIKeyCredentialType("oauth_bearer") {
		t.Fatal("oauth_bearer should not be treated as API key credential")
	}
	if !gatewayCredentialUsable(store.SiteCredential{Meta: store.JSON(`{"enabled":"true"}`)}) {
		t.Fatal("string enabled=true should be usable")
	}
	if gatewayCredentialUsable(store.SiteCredential{Meta: store.JSON(`{"enabled":false}`)}) {
		t.Fatal("disabled credential should not be usable")
	}
	if gatewayCredentialUsable(store.SiteCredential{Meta: store.JSON(`{"raw_key_missing":true}`)}) {
		t.Fatal("raw_key_missing credential should not be usable")
	}

	if !gatewayCredentialStateUsable(store.SiteAPIKeyState{}) {
		t.Fatal("missing state should default to usable")
	}
	if gatewayCredentialStateUsable(store.SiteAPIKeyState{SiteCredentialID: uuid.New(), Enabled: false}) {
		t.Fatal("disabled state should not be usable")
	}
	if !gatewayCredentialStateUsable(store.SiteAPIKeyState{
		SiteCredentialID: uuid.New(),
		Enabled:          true,
		SyncStatus:       "failed",
		SyncMessage:      sql.NullString{String: "temporary 500 from upstream", Valid: true},
	}) {
		t.Fatal("temporary failed state should remain usable")
	}
	if gatewayCredentialStateUsable(store.SiteAPIKeyState{
		SiteCredentialID: uuid.New(),
		Enabled:          true,
		SyncStatus:       "failed",
		SyncMessage:      sql.NullString{String: "refresh_token_reused", Valid: true},
	}) {
		t.Fatal("permanent auth failure should not be usable")
	}

	for _, message := range []string{"invalid_grant", "token_invalidated", `upstream returned 401: {"error":{"code":"invalid_api_key"}}`, "api key is disabled"} {
		if !gatewayPermanentAuthFailure(message) {
			t.Fatalf("gatewayPermanentAuthFailure(%q) = false, want true", message)
		}
	}
	for _, message := range []string{
		"temporary upstream 500",
		"upstream returned 401",
		"HTTP 403 forbidden",
		`upstream returned 401: {"error":{"code":"api_key_weekly_quota_exhausted"}}`,
		`upstream returned 429: {"code":"USAGE_LIMIT_EXCEEDED"}`,
	} {
		if gatewayPermanentAuthFailure(message) {
			t.Fatalf("gatewayPermanentAuthFailure(%q) = true, want false", message)
		}
	}

	if !gatewayJSONBoolDefault(store.JSON(`{"enabled":true}`), "enabled", false) {
		t.Fatal("JSON bool true should be read")
	}
	if !gatewayJSONBoolDefault(store.JSON(`{invalid`), "enabled", true) {
		t.Fatal("invalid JSON should use fallback")
	}
	if gatewayJSONBoolDefault(store.JSON(`{"enabled":"false"}`), "enabled", true) {
		t.Fatal("string false should be false")
	}
}

func TestGatewayCooldownSetsAndCredentialCooling(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	modelID := uuid.New()
	credentialID := uuid.New()
	otherCredentialID := uuid.New()

	cooldowns := []store.RouteCooldown{
		{SiteID: siteID},
		{
			SiteID:      siteID,
			SiteModelID: uuid.NullUUID{UUID: modelID, Valid: true},
		},
		{
			SiteID:           siteID,
			SiteModelID:      uuid.NullUUID{UUID: modelID, Valid: true},
			SiteCredentialID: uuid.NullUUID{UUID: credentialID, Valid: true},
		},
	}
	siteCooldowns, modelCooldowns := gatewayCooldownSets(cooldowns)
	if _, ok := siteCooldowns[siteID]; !ok {
		t.Fatal("site cooldown missing")
	}
	if _, ok := modelCooldowns[modelID]; !ok {
		t.Fatal("model cooldown missing")
	}
	if len(siteCooldowns) != 1 || len(modelCooldowns) != 1 {
		t.Fatalf("unexpected cooldown sets: site=%#v model=%#v", siteCooldowns, modelCooldowns)
	}

	if !gatewayCredentialCooling(cooldowns, siteID, modelID, credentialID) {
		t.Fatal("expected credential cooldown for matching model")
	}
	if gatewayCredentialCooling(cooldowns, siteID, modelID, otherCredentialID) {
		t.Fatal("other credential should not be cooling")
	}
	if gatewayCredentialCooling(cooldowns, uuid.New(), modelID, credentialID) {
		t.Fatal("different site should not be cooling")
	}

	allModelCredential := uuid.New()
	allModelCooldowns := []store.RouteCooldown{{
		SiteID:           siteID,
		SiteCredentialID: uuid.NullUUID{UUID: allModelCredential, Valid: true},
	}}
	if !gatewayCredentialCooling(allModelCooldowns, siteID, uuid.Nil, allModelCredential) {
		t.Fatal("credential-wide cooldown should apply without model id")
	}
}

func TestGatewayModelCredentialCountsIncludesModelBoundGrokSSO(t *testing.T) {
	credentialID := uuid.New()
	modelID := uuid.New()
	siteID := uuid.New()
	counts := gatewayModelCredentialCounts(
		[]store.SiteAPIKeyModel{{
			SiteID:           siteID,
			SiteCredentialID: credentialID,
			SiteModelID:      uuid.NullUUID{UUID: modelID, Valid: true},
			Available:        true,
			Enabled:          true,
		}},
		[]store.SiteCredential{{
			ID:             credentialID,
			SiteID:         siteID,
			CredentialType: "grok_sso:account-1",
		}},
		map[uuid.UUID]store.SiteAPIKeyState{},
		nil,
	)

	if counts[modelID] != 1 {
		t.Fatalf("model credential count = %d, want 1", counts[modelID])
	}
	if siteCounts := gatewaySiteCredentialCounts([]store.SiteCredential{{ID: credentialID, SiteID: siteID, CredentialType: "grok_sso:account-1"}}, map[uuid.UUID]store.SiteAPIKeyState{}, nil); siteCounts[siteID] != 0 {
		t.Fatalf("site fallback count = %d, want model-bound Grok credentials excluded", siteCounts[siteID])
	}
}

func TestGatewayModelCredentialCountsExcludesFailedGrokSSO(t *testing.T) {
	credentialID := uuid.New()
	modelID := uuid.New()
	counts := gatewayModelCredentialCounts(
		[]store.SiteAPIKeyModel{{SiteCredentialID: credentialID, SiteModelID: uuid.NullUUID{UUID: modelID, Valid: true}, Available: true, Enabled: true}},
		[]store.SiteCredential{{ID: credentialID, CredentialType: "grok_sso:failed"}},
		map[uuid.UUID]store.SiteAPIKeyState{credentialID: {SiteCredentialID: credentialID, Enabled: true, SyncStatus: "failed"}},
		nil,
	)
	if counts[modelID] != 0 {
		t.Fatalf("failed Grok credential count = %d, want 0", counts[modelID])
	}
}

func TestGatewayModelCredentialBindingCountsIncludeUnusableBindings(t *testing.T) {
	modelID := uuid.New()
	credentialID := uuid.New()
	counts := gatewayModelCredentialBindingCounts([]store.SiteAPIKeyModel{
		{SiteCredentialID: credentialID, SiteModelID: uuid.NullUUID{UUID: modelID, Valid: true}, Available: false, Enabled: false},
		{SiteCredentialID: credentialID, SiteModelID: uuid.NullUUID{UUID: modelID, Valid: true}, Available: true, Enabled: true},
		{SiteCredentialID: uuid.New()},
	})
	if counts[modelID] != 1 {
		t.Fatalf("binding count = %d, want one distinct binding regardless of usability", counts[modelID])
	}
}

func TestGatewayModelEndpointTypesAreNormalizedAndApplied(t *testing.T) {
	t.Parallel()

	types := gatewayModelEndpointTypes(store.SiteModel{
		Capabilities: store.JSON(`{"supported_endpoint_types":[" openai-response ","openai","openai-response",7]}`),
	})
	endpointSet := map[string]struct{}{}
	for _, endpointType := range types {
		endpointSet[endpointType] = struct{}{}
	}
	item := canonicalModelPayload(store.CanonicalModel{ModelKey: "gpt-test"})
	applyModelEndpointTypes(item, endpointSet)

	metadata, ok := item["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("metadata = %#v", item["metadata"])
	}
	got, ok := metadata["supported_endpoint_types"].([]string)
	if !ok || len(got) != 2 || got[0] != "openai" || got[1] != "openai-response" {
		t.Fatalf("supported_endpoint_types = %#v", metadata["supported_endpoint_types"])
	}
}

func TestGatewayModelEndpointTypesOverrideStaleCategory(t *testing.T) {
	t.Parallel()

	item := canonicalModelPayload(store.CanonicalModel{ModelKey: "custom-generator", Category: "chat"})
	applyModelEndpointTypes(item, map[string]struct{}{"openai": {}, "openai-image": {}})
	metadata := item["metadata"].(map[string]any)
	if metadata["category"] != "image" {
		t.Fatalf("category = %v, want image", metadata["category"])
	}
}
