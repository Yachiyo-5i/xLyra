package gateway

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/catalog"
	"xlyra/server/internal/store"
	"xlyra/server/internal/upstream"
)

const (
	modelsCacheFreshTTL       = 5 * time.Minute
	modelsCacheStaleTTL       = 30 * time.Minute
	modelsCacheRefreshTimeout = 2 * time.Minute
)

type modelsCache struct {
	mu       sync.Mutex
	items    map[uuid.UUID]modelsCacheEntry
	inflight map[uuid.UUID]*modelsCacheCall
	version  uint64
}

type modelsCacheEntry struct {
	payload map[string]any
	cached  time.Time
}

type modelsCacheCall struct {
	done    chan struct{}
	payload map[string]any
	err     error
}

func newModelsCache() *modelsCache {
	return &modelsCache{
		items:    map[uuid.UUID]modelsCacheEntry{},
		inflight: map[uuid.UUID]*modelsCacheCall{},
	}
}

func (c *modelsCache) get(apiKeyID uuid.UUID) (map[string]any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	item, ok := c.items[apiKeyID]
	if !ok {
		return nil, false
	}
	if time.Since(item.cached) > modelsCacheFreshTTL {
		return nil, false
	}
	return cloneModelsPayload(item.payload), true
}

func (c *modelsCache) getStale(apiKeyID uuid.UUID) (map[string]any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	item, ok := c.items[apiKeyID]
	if !ok {
		return nil, false
	}
	if time.Since(item.cached) > modelsCacheStaleTTL {
		delete(c.items, apiKeyID)
		return nil, false
	}
	return cloneModelsPayload(item.payload), true
}

func (c *modelsCache) getOrBuild(ctx context.Context, apiKey store.APIKey, build func(context.Context, store.APIKey) (map[string]any, error)) (map[string]any, error) {
	if payload, ok := c.get(apiKey.ID); ok {
		return payload, nil
	}
	if payload, ok := c.getStale(apiKey.ID); ok {
		c.refreshAsync(apiKey, build)
		return payload, nil
	}

	c.mu.Lock()
	if call, ok := c.inflight[apiKey.ID]; ok {
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-call.done:
			return cloneModelsPayload(call.payload), call.err
		}
	}
	call := &modelsCacheCall{done: make(chan struct{})}
	c.inflight[apiKey.ID] = call
	version := c.version
	c.mu.Unlock()

	payload, err := build(ctx, apiKey)

	c.mu.Lock()
	if err == nil && version == c.version {
		c.items[apiKey.ID] = modelsCacheEntry{payload: cloneModelsPayload(payload), cached: time.Now()}
	}
	call.payload = payload
	call.err = err
	delete(c.inflight, apiKey.ID)
	close(call.done)
	c.mu.Unlock()

	return cloneModelsPayload(payload), err
}

func (c *modelsCache) refreshAsync(apiKey store.APIKey, build func(context.Context, store.APIKey) (map[string]any, error)) {
	c.mu.Lock()
	if _, ok := c.inflight[apiKey.ID]; ok {
		c.mu.Unlock()
		return
	}
	call := &modelsCacheCall{done: make(chan struct{})}
	c.inflight[apiKey.ID] = call
	version := c.version
	c.mu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), modelsCacheRefreshTimeout)
		defer cancel()
		payload, err := build(ctx, apiKey)

		c.mu.Lock()
		if err == nil && version == c.version {
			c.items[apiKey.ID] = modelsCacheEntry{payload: cloneModelsPayload(payload), cached: time.Now()}
		}
		call.payload = payload
		call.err = err
		delete(c.inflight, apiKey.ID)
		close(call.done)
		c.mu.Unlock()
	}()
}

func (c *modelsCache) invalidate() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.version++
	c.items = map[uuid.UUID]modelsCacheEntry{}
}

func (c *modelsCache) invalidateKey(apiKeyID uuid.UUID) {
	if c == nil || apiKeyID == uuid.Nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.version++
	delete(c.items, apiKeyID)
}

func cloneModelsPayload(payload map[string]any) map[string]any {
	if payload == nil {
		return nil
	}
	clone := make(map[string]any, len(payload))
	for key, value := range payload {
		if key == "data" {
			if rows, ok := value.([]map[string]any); ok {
				copiedRows := make([]map[string]any, 0, len(rows))
				for _, row := range rows {
					copied := make(map[string]any, len(row))
					for rowKey, rowValue := range row {
						copied[rowKey] = rowValue
					}
					copiedRows = append(copiedRows, copied)
				}
				clone[key] = copiedRows
				continue
			}
		}
		clone[key] = value
	}
	return clone
}

func (h Handler) InvalidateModelsCache() {
	if h.modelsCache != nil {
		h.modelsCache.invalidate()
	}
}

func (h Handler) InvalidateModelsCacheForAPIKey(apiKeyID uuid.UUID) {
	if h.modelsCache != nil {
		h.modelsCache.invalidateKey(apiKeyID)
	}
}

func (h Handler) PrewarmModelsCacheForAPIKey(ctx context.Context, apiKey store.APIKey) {
	if h.db == nil || h.auth == nil || h.modelsCache == nil || apiKey.ID == uuid.Nil {
		return
	}
	if apiKey.Status != "active" {
		return
	}
	now := time.Now()
	if apiKey.ExpiresAt != nil && !apiKey.ExpiresAt.After(now) {
		return
	}
	if apiKey.QuotaExceededAt(now, h.recorder.timeZone) {
		return
	}
	if _, err := h.modelsCache.getOrBuild(ctx, apiKey, h.buildModelsPayloadFast); err != nil && h.logger != nil {
		h.logger.WarnContext(ctx, "prewarm models cache item failed", "scope", "gateway", "api_key_id", apiKey.ID, "error", err)
	}
}

func (h Handler) PrewarmModelsCache(ctx context.Context) {
	if h.db == nil || h.auth == nil || h.modelsCache == nil {
		return
	}
	apiKeys, err := store.NewAPIKeyRepository(h.db.DB()).List(ctx)
	if err != nil {
		if h.logger != nil {
			h.logger.WarnContext(ctx, "prewarm models cache list api keys failed", "scope", "gateway", "error", err)
		}
		return
	}
	count := 0
	now := time.Now()
	for _, apiKey := range apiKeys {
		if apiKey.Status != "active" {
			continue
		}
		if apiKey.ExpiresAt != nil && !apiKey.ExpiresAt.After(now) {
			continue
		}
		if apiKey.QuotaExceededAt(now, h.recorder.timeZone) {
			continue
		}
		if _, err := h.modelsCache.getOrBuild(ctx, apiKey, h.buildModelsPayloadFast); err != nil {
			if h.logger != nil {
				h.logger.WarnContext(ctx, "prewarm models cache item failed", "scope", "gateway", "api_key_id", apiKey.ID, "error", err)
			}
			continue
		}
		count++
	}
	if h.logger != nil {
		h.logger.InfoContext(ctx, "prewarm models cache completed", "scope", "gateway", "count", count)
	}
}

func (h Handler) buildModelsPayloadFast(ctx context.Context, apiKey store.APIKey) (map[string]any, error) {
	access, err := h.auth.ResolveAPIKeyAccessSets(ctx, apiKey.ID)
	if err != nil {
		return nil, err
	}
	if access.APIKey.SitePolicy == "allow_list" && len(access.AllowedSiteIDs) == 0 {
		return emptyModelsPayload(), nil
	}
	if access.APIKey.ModelPolicy == "allow_list" && len(access.AllowedSiteModelIDs) == 0 {
		return emptyModelsPayload(), nil
	}

	var canonicalModels []store.CanonicalModel
	if err := h.db.DB().WithContext(ctx).Find(&canonicalModels).Error; err != nil {
		return nil, err
	}
	siteModels, err := store.NewSiteModelRepository(h.db.DB()).ListAll(ctx)
	if err != nil {
		return nil, err
	}
	sites, err := store.NewSiteRepository(h.db.DB()).List(ctx)
	if err != nil {
		return nil, err
	}
	credentials, err := store.NewSiteCredentialRepository(h.db.DB()).ListAll(ctx)
	if err != nil {
		return nil, err
	}
	keyStates, err := store.NewSiteAPIKeyStateRepository(h.db.DB()).ListAll(ctx)
	if err != nil {
		return nil, err
	}
	apiKeyModels, err := store.NewSiteAPIKeyModelRepository(h.db.DB()).ListAll(ctx)
	if err != nil {
		return nil, err
	}
	cooldowns, err := store.NewRouteCooldownRepository(h.db.DB()).ListActive(ctx, time.Now())
	if err != nil {
		return nil, err
	}

	canonicalByID := map[uuid.UUID]store.CanonicalModel{}
	for _, model := range canonicalModels {
		if model.Status == "active" {
			canonicalByID[model.ID] = model
		}
	}
	sitesByID := map[uuid.UUID]store.Site{}
	for _, site := range sites {
		sitesByID[site.ID] = site
	}
	keyStatesByCredentialID := map[uuid.UUID]store.SiteAPIKeyState{}
	for _, state := range keyStates {
		keyStatesByCredentialID[state.SiteCredentialID] = state
	}
	siteCooldowns, modelCooldowns := gatewayCooldownSets(cooldowns)
	siteCredentialCounts := gatewaySiteCredentialCounts(credentials, keyStatesByCredentialID, cooldowns)
	modelCredentialCounts := gatewayModelCredentialCounts(apiKeyModels, credentials, keyStatesByCredentialID, cooldowns)
	modelCredentialBindingCounts := gatewayModelCredentialBindingCounts(apiKeyModels)
	allowedSites := gatewayUUIDSet(access.AllowedSiteIDs)
	allowedModels := gatewayUUIDSet(access.AllowedSiteModelIDs)

	itemsByModelKey := map[string]map[string]any{}
	endpointTypesByModelKey := map[string]map[string]struct{}{}
	for _, siteModel := range siteModels {
		if siteModel.Status != "active" || !siteModel.CanonicalID.Valid {
			continue
		}
		if access.APIKey.ModelPolicy == "allow_list" {
			if _, ok := allowedModels[siteModel.ID]; !ok {
				continue
			}
		}
		if _, ok := modelCooldowns[siteModel.ID]; ok {
			continue
		}
		canonical, ok := canonicalByID[siteModel.CanonicalID.UUID]
		if !ok {
			continue
		}
		site := sitesByID[siteModel.SiteID]
		if !site.Enabled || site.Status != "active" {
			continue
		}
		if access.APIKey.SitePolicy == "allow_list" {
			if _, ok := allowedSites[site.ID]; !ok {
				continue
			}
		}
		if _, ok := siteCooldowns[site.ID]; ok {
			continue
		}

		modelAvailableKeys := modelCredentialCounts[siteModel.ID]
		availableKeys := modelAvailableKeys
		if site.SiteType != "newapi" && modelCredentialBindingCounts[siteModel.ID] == 0 {
			availableKeys = siteCredentialCounts[site.ID]
		}
		if availableKeys <= 0 {
			continue
		}
		endpointSet, ok := endpointTypesByModelKey[canonical.ModelKey]
		if !ok {
			endpointSet = map[string]struct{}{}
			endpointTypesByModelKey[canonical.ModelKey] = endpointSet
		}
		for _, endpointType := range gatewayModelEndpointTypes(siteModel) {
			endpointSet[endpointType] = struct{}{}
		}
		if _, ok := itemsByModelKey[canonical.ModelKey]; ok {
			continue
		}
		itemsByModelKey[canonical.ModelKey] = canonicalModelPayload(canonical)
	}

	for modelKey, item := range itemsByModelKey {
		applyModelEndpointTypes(item, endpointTypesByModelKey[modelKey])
	}

	data := make([]map[string]any, 0, len(itemsByModelKey))
	for _, model := range canonicalModels {
		item, ok := itemsByModelKey[model.ModelKey]
		if !ok {
			continue
		}
		data = append(data, item)
	}
	data = append(data, modelRuleAliasPayloads(apiKey.ModelRules(), itemsByModelKey)...)
	return map[string]any{
		"object": "list",
		"data":   data,
	}, nil
}

func modelRuleAliasPayloads(rules []store.APIKeyModelRule, itemsByModelKey map[string]map[string]any) []map[string]any {
	if len(rules) == 0 {
		return nil
	}
	listed := make(map[string]struct{}, len(itemsByModelKey))
	itemsByNormalizedKey := make(map[string]map[string]any, len(itemsByModelKey))
	for modelKey, item := range itemsByModelKey {
		normalized := catalog.NormalizeModelKey(modelKey)
		listed[normalized] = struct{}{}
		itemsByNormalizedKey[normalized] = item
	}
	aliases := make([]map[string]any, 0, len(rules))
	for _, rule := range rules {
		if !rule.IsExact() {
			continue
		}
		normalized := catalog.NormalizeModelKey(rule.Pattern)
		if normalized == "" {
			continue
		}
		if _, ok := listed[normalized]; ok {
			continue
		}
		target, ok := itemsByNormalizedKey[catalog.NormalizeModelKey(rule.Target)]
		if !ok {
			continue
		}
		alias := make(map[string]any, len(target))
		for key, value := range target {
			alias[key] = value
		}
		alias["id"] = rule.Pattern
		metadata := map[string]any{"mapped_model": target["id"]}
		if targetMetadata, ok := target["metadata"].(map[string]any); ok {
			for key, value := range targetMetadata {
				metadata[key] = value
			}
			metadata["display_name"] = rule.Pattern
		}
		alias["metadata"] = metadata
		aliases = append(aliases, alias)
		listed[normalized] = struct{}{}
	}
	return aliases
}

func emptyModelsPayload() map[string]any {
	return map[string]any{
		"object": "list",
		"data":   []map[string]any{},
	}
}

func gatewayModelEndpointTypes(model store.SiteModel) []string {
	if len(model.Capabilities) == 0 {
		return nil
	}
	values := map[string]any{}
	if err := json.Unmarshal(model.Capabilities, &values); err != nil {
		return nil
	}
	raw, ok := values["supported_endpoint_types"]
	if !ok {
		return nil
	}
	out := []string{}
	appendType := func(text string) {
		text = normalizeEndpointType(text)
		if text != "" {
			out = append(out, text)
		}
	}
	switch items := raw.(type) {
	case []any:
		for _, item := range items {
			if text, ok := item.(string); ok {
				appendType(text)
			}
		}
	case []string:
		for _, item := range items {
			appendType(item)
		}
	}
	return out
}

func applyModelEndpointTypes(item map[string]any, endpointSet map[string]struct{}) {
	if len(endpointSet) == 0 {
		return
	}
	types := make([]string, 0, len(endpointSet))
	for endpointType := range endpointSet {
		types = append(types, endpointType)
	}
	sort.Strings(types)
	metadata, ok := item["metadata"].(map[string]any)
	if !ok {
		metadata = map[string]any{}
		item["metadata"] = metadata
	}
	metadata["supported_endpoint_types"] = types
	if _, ok := endpointSet["openai-image"]; ok {
		metadata["category"] = "image"
	} else if _, ok := endpointSet["openai-embedding"]; ok {
		metadata["category"] = "embedding"
	} else if _, ok := endpointSet["openai-audio-speech"]; ok {
		metadata["category"] = "audio"
	}
}

func canonicalModelPayload(model store.CanonicalModel) map[string]any {
	return map[string]any{
		"id":       model.ModelKey,
		"object":   "model",
		"created":  model.CreatedAt.Unix(),
		"owned_by": defaultString(model.Provider, "xlyra"),
		"metadata": map[string]any{
			"canonical_model_id": model.ID.String(),
			"display_name":       defaultString(model.DisplayName, model.ModelKey),
			"category":           defaultString(model.Category, "chat"),
		},
	}
}

func gatewayUUIDSet(items []uuid.UUID) map[uuid.UUID]struct{} {
	result := make(map[uuid.UUID]struct{}, len(items))
	for _, item := range items {
		if item == uuid.Nil {
			continue
		}
		result[item] = struct{}{}
	}
	return result
}

func gatewayCooldownSets(items []store.RouteCooldown) (map[uuid.UUID]struct{}, map[uuid.UUID]struct{}) {
	siteCooldowns := map[uuid.UUID]struct{}{}
	modelCooldowns := map[uuid.UUID]struct{}{}
	for _, item := range items {
		if item.SiteCredentialID.Valid {
			continue
		}
		if item.SiteModelID.Valid {
			modelCooldowns[item.SiteModelID.UUID] = struct{}{}
			continue
		}
		siteCooldowns[item.SiteID] = struct{}{}
	}
	return siteCooldowns, modelCooldowns
}

func gatewaySiteCredentialCounts(credentials []store.SiteCredential, states map[uuid.UUID]store.SiteAPIKeyState, cooldowns []store.RouteCooldown) map[uuid.UUID]int {
	counts := map[uuid.UUID]int{}
	for _, credential := range credentials {
		if !gatewayAPIKeyCredentialType(credential.CredentialType) || !gatewayCredentialUsable(credential) || gatewayCredentialCooling(cooldowns, credential.SiteID, uuid.Nil, credential.ID) {
			continue
		}
		state := states[credential.ID]
		if !store.CredentialStateUsableForCredential(credential, state) {
			continue
		}
		counts[credential.SiteID]++
	}
	return counts
}

func gatewayModelCredentialCounts(apiKeyModels []store.SiteAPIKeyModel, credentials []store.SiteCredential, states map[uuid.UUID]store.SiteAPIKeyState, cooldowns []store.RouteCooldown) map[uuid.UUID]int {
	credentialsByID := map[uuid.UUID]store.SiteCredential{}
	for _, credential := range credentials {
		credentialsByID[credential.ID] = credential
	}
	seen := map[uuid.UUID]map[uuid.UUID]struct{}{}
	for _, apiKeyModel := range apiKeyModels {
		if !apiKeyModel.SiteModelID.Valid || !apiKeyModel.Available || !apiKeyModel.Enabled {
			continue
		}
		credential := credentialsByID[apiKeyModel.SiteCredentialID]
		state, hasState := states[credential.ID]
		if credential.ID == uuid.Nil || !gatewayModelCredentialType(credential.CredentialType) || !gatewayCredentialUsable(credential) || !gatewayModelCredentialStateUsable(credential, state, hasState) {
			continue
		}
		if gatewayCredentialCooling(cooldowns, apiKeyModel.SiteID, apiKeyModel.SiteModelID.UUID, credential.ID) {
			continue
		}
		modelID := apiKeyModel.SiteModelID.UUID
		if seen[modelID] == nil {
			seen[modelID] = map[uuid.UUID]struct{}{}
		}
		seen[modelID][credential.ID] = struct{}{}
	}
	counts := map[uuid.UUID]int{}
	for modelID, credentialIDs := range seen {
		counts[modelID] = len(credentialIDs)
	}
	return counts
}

func gatewayModelCredentialBindingCounts(apiKeyModels []store.SiteAPIKeyModel) map[uuid.UUID]int {
	seen := map[uuid.UUID]map[uuid.UUID]struct{}{}
	for _, apiKeyModel := range apiKeyModels {
		if !apiKeyModel.SiteModelID.Valid {
			continue
		}
		modelID := apiKeyModel.SiteModelID.UUID
		if seen[modelID] == nil {
			seen[modelID] = map[uuid.UUID]struct{}{}
		}
		seen[modelID][apiKeyModel.SiteCredentialID] = struct{}{}
	}
	counts := map[uuid.UUID]int{}
	for modelID, credentialIDs := range seen {
		counts[modelID] = len(credentialIDs)
	}
	return counts
}

func gatewayAPIKeyCredentialType(value string) bool {
	return value == "api_key" || strings.HasPrefix(value, "api_key:")
}

func gatewayModelCredentialType(value string) bool {
	return gatewayAPIKeyCredentialType(value) || strings.HasPrefix(value, "grok_sso:")
}

func gatewayModelCredentialStateUsable(credential store.SiteCredential, state store.SiteAPIKeyState, hasState bool) bool {
	if strings.HasPrefix(credential.CredentialType, "grok_sso:") {
		return grokCredentialUsable(credential, state, hasState)
	}
	return store.CredentialStateUsableForCredential(credential, state)
}

func gatewayCredentialUsable(credential store.SiteCredential) bool {
	return store.CredentialUsable(credential)
}

func gatewayCredentialStateUsable(state store.SiteAPIKeyState) bool {
	return store.CredentialStateUsable(state)
}

func gatewayPermanentAuthFailure(message string) bool {
	return upstream.ClassifyMessage(message).CredentialInvalid()
}

func gatewayJSONBoolDefault(data store.JSON, key string, fallback bool) bool {
	if len(data) == 0 {
		return fallback
	}
	meta := map[string]any{}
	if err := json.Unmarshal(data, &meta); err != nil {
		return fallback
	}
	value, ok := meta[key]
	if !ok {
		return fallback
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(typed, "true")
	default:
		return fallback
	}
}

func gatewayCredentialCooling(cooldowns []store.RouteCooldown, siteID uuid.UUID, siteModelID uuid.UUID, credentialID uuid.UUID) bool {
	for _, cooldown := range cooldowns {
		if cooldown.SiteID != siteID || !cooldown.SiteCredentialID.Valid || cooldown.SiteCredentialID.UUID != credentialID {
			continue
		}
		if !cooldown.SiteModelID.Valid || siteModelID == uuid.Nil || cooldown.SiteModelID.UUID == siteModelID {
			return true
		}
	}
	return false
}

func (h Handler) buildModelsPayload(ctx context.Context, apiKey store.APIKey) (map[string]any, error) {
	access, err := h.auth.ResolveAPIKeyAccessSets(ctx, apiKey.ID)
	if err != nil {
		return nil, err
	}
	if access.APIKey.SitePolicy == "allow_list" && len(access.AllowedSiteIDs) == 0 {
		return map[string]any{
			"object": "list",
			"data":   []map[string]any{},
		}, nil
	}
	models, err := store.NewCanonicalModelRepository(h.db.DB()).List(ctx)
	if err != nil {
		return nil, err
	}

	data := make([]map[string]any, 0, len(models))
	for _, model := range models {
		if model.Status != "active" {
			continue
		}
		if access.APIKey.ModelPolicy == "allow_list" && len(access.AllowedSiteModelIDs) == 0 {
			continue
		}
		candidates, err := h.router.Candidates(ctx, routeCandidateQuery(model.ModelKey, access))
		if err != nil || len(candidates.Items) == 0 {
			continue
		}
		data = append(data, map[string]any{
			"id":       model.ModelKey,
			"object":   "model",
			"created":  model.CreatedAt.Unix(),
			"owned_by": defaultString(model.Provider, "xlyra"),
			"metadata": map[string]any{
				"canonical_model_id": model.ID.String(),
				"display_name":       defaultString(model.DisplayName, model.ModelKey),
				"category":           defaultString(model.Category, "chat"),
			},
		})
	}

	return map[string]any{
		"object": "list",
		"data":   data,
	}, nil
}
