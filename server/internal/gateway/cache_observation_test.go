package gateway

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	routeengine "xlyra/server/internal/router"
	"xlyra/server/internal/store"
)

func TestCacheObservationUsesOpaqueStableHashes(t *testing.T) {
	t.Parallel()

	handler := Handler{cacheObservationKey: []byte("cache-observation-test-key")}
	apiKeyID := uuid.New()
	canonicalModelID := uuid.New()
	request := cacheObservationChatRequest(t, `{
		"model": "alias-model",
		"temperature": 0.8,
		"max_completion_tokens": 256,
		"tools": [{"type":"function","function":{"name":"lookup"}}],
		"messages": [
			{"role":"system","content":"private system instruction"},
			{"role":"user","content":"private user prompt"}
		]
	}`)
	request.DownstreamHeaders.Set(codexGatewaySessionHeader, "session-with-private-value")

	ctx := handler.withCacheObservation(context.Background(), apiKeyID, canonicalModelID, request)
	metadata := map[string]any{}
	handler.appendCacheObservationMetadata(ctx, metadata, routeengine.Candidate{
		Site:  routeengine.CandidateSite{ID: uuid.New(), SiteType: "openai"},
		Model: routeengine.CandidateModel{SiteModelID: uuid.New(), UpstreamName: "gpt-test"},
	}, gatewayAttemptResult{upstreamProtocol: "openai_chat_completions"})

	observation, ok := metadata["cache_observation"].(map[string]any)
	if !ok {
		t.Fatalf("cache observation metadata = %#v", metadata)
	}
	for _, key := range []string{"prefix_hash", "root_prefix_hash", "session_hash", "cache_fingerprint", "cache_domain_hash"} {
		if value, ok := observation[key].(string); !ok || value == "" {
			t.Fatalf("cache observation %s = %#v, want non-empty opaque hash", key, observation[key])
		}
	}
	if lineage, ok := observation["prefix_lineage"].([]string); !ok || len(lineage) != 2 {
		t.Fatalf("cache observation prefix lineage = %#v, want root and message boundaries", observation["prefix_lineage"])
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	for _, secret := range []string{"private system instruction", "private user prompt", "session-with-private-value"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("cache observation leaked raw value %q: %s", secret, encoded)
		}
	}

	changedSampling := cacheObservationChatRequest(t, `{
		"model": "alias-model",
		"temperature": 0.1,
		"max_completion_tokens": 1024,
		"tools": [{"type":"function","function":{"name":"lookup"}}],
		"messages": [
			{"role":"system","content":"private system instruction"},
			{"role":"user","content":"private user prompt"}
		]
	}`)
	changedSampling.DownstreamHeaders.Set(codexGatewaySessionHeader, "session-with-private-value")
	changedCtx := handler.withCacheObservation(context.Background(), apiKeyID, canonicalModelID, changedSampling)
	changedMetadata := map[string]any{}
	handler.appendCacheObservationMetadata(changedCtx, changedMetadata, routeengine.Candidate{
		Site:  routeengine.CandidateSite{ID: uuid.New(), SiteType: "openai"},
		Model: routeengine.CandidateModel{SiteModelID: uuid.New(), UpstreamName: "gpt-test"},
	}, gatewayAttemptResult{upstreamProtocol: "openai_chat_completions"})
	changedObservation := changedMetadata["cache_observation"].(map[string]any)
	if observation["prefix_hash"] != changedObservation["prefix_hash"] {
		t.Fatalf("sampling-only changes split prefix lineage: before=%#v after=%#v", observation, changedObservation)
	}
}

func TestChatCompletionsEndpointCapturesConversationHeader(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest("POST", gatewayEndpointChatCompletions, strings.NewReader(`{"model":"test-model","messages":[{"role":"user","content":"hi"}]}`))
	request.Header.Set(codexGatewaySessionHeader, "native-session")
	decoded, failure := (chatCompletionsEndpointAdapter{}).DecodeRequest(request)
	if failure != nil {
		t.Fatalf("DecodeRequest failure = %#v", failure)
	}
	if got := decoded.DownstreamHeaders.Get(codexGatewaySessionHeader); got != "native-session" {
		t.Fatalf("Session-Id = %q, want native-session", got)
	}
}

func TestCacheObservationLineageTracksAppendOnlyConversation(t *testing.T) {
	t.Parallel()

	handler := Handler{cacheObservationKey: []byte("cache-observation-test-key")}
	apiKeyID := uuid.New()
	canonicalModelID := uuid.New()
	firstRequest := cacheObservationRequest([]canonicalMessage{{
		Type:       "message",
		Role:       "user",
		RawContent: "first user message",
		Content:    []canonicalContentPart{{Type: "input_text", Text: "first user message"}},
	}})
	first, ok := cacheObservationFromContext(handler.withCacheObservation(context.Background(), apiKeyID, canonicalModelID, firstRequest))
	if !ok {
		t.Fatal("expected first cache observation")
	}
	if first.PrefixHash == "" || len(first.PrefixLineage) != 2 {
		t.Fatalf("first observation = %#v, want root and first message boundaries", first)
	}

	secondRequest := cacheObservationRequest([]canonicalMessage{
		firstRequest.Canonical.Messages[0],
		{
			Type:       "message",
			Role:       "assistant",
			RawContent: "first assistant message",
			Content:    []canonicalContentPart{{Type: "output_text", Text: "first assistant message"}},
		},
		{
			Type:       "message",
			Role:       "user",
			RawContent: "second user message",
			Content:    []canonicalContentPart{{Type: "input_text", Text: "second user message"}},
		},
	})
	second, ok := cacheObservationFromContext(handler.withCacheObservation(context.Background(), apiKeyID, canonicalModelID, secondRequest))
	if !ok {
		t.Fatal("expected second cache observation")
	}
	if !cacheObservationContains(second.PrefixLineage, first.PrefixHash) {
		t.Fatalf("append-only lineage = %#v, want prior request boundary %q", second.PrefixLineage, first.PrefixHash)
	}

	changedTools := secondRequest
	changedCanonical := *secondRequest.Canonical
	changedCanonical.Tools = []canonicalTool{
		{Type: "function", Name: "second", Parameters: map[string]any{"type": "object"}},
		{Type: "function", Name: "first", Parameters: map[string]any{"type": "object"}},
	}
	changedTools.Canonical = &changedCanonical
	changed, ok := cacheObservationFromContext(handler.withCacheObservation(context.Background(), apiKeyID, canonicalModelID, changedTools))
	if !ok {
		t.Fatal("expected changed cache observation")
	}
	if changed.RootPrefixHash == second.RootPrefixHash {
		t.Fatalf("tool order change retained root prefix: before=%q after=%q", second.RootPrefixHash, changed.RootPrefixHash)
	}

	differentTenant, ok := cacheObservationFromContext(handler.withCacheObservation(context.Background(), uuid.New(), canonicalModelID, secondRequest))
	if !ok {
		t.Fatal("expected tenant-isolated cache observation")
	}
	if differentTenant.PrefixHash == second.PrefixHash {
		t.Fatalf("tenant isolation failed: original=%#v other=%#v", second, differentTenant)
	}
}

func TestCacheObservationSeparatesCacheDirectivesFromSampling(t *testing.T) {
	t.Parallel()

	handler := Handler{cacheObservationKey: []byte("cache-observation-test-key")}
	apiKeyID := uuid.New()
	canonicalModelID := uuid.New()
	candidate := routeengine.Candidate{
		Site:  routeengine.CandidateSite{ID: uuid.New(), SiteType: "anthropic"},
		Model: routeengine.CandidateModel{SiteModelID: uuid.New(), UpstreamName: "claude-test"},
	}
	request := cacheObservationRequest([]canonicalMessage{{
		Type:       "message",
		Role:       "user",
		RawContent: "private user prompt",
		Content:    []canonicalContentPart{{Type: "input_text", Text: "private user prompt"}},
	}})
	request.Canonical.RawSystem = []any{map[string]any{
		"type": "text",
		"text": "system instruction",
		"cache_control": map[string]any{
			"type": "ephemeral",
			"ttl":  "5m",
		},
	}}
	request.Canonical.Params = map[string]any{"temperature": 0.8}

	firstCtx := handler.withCacheObservation(context.Background(), apiKeyID, canonicalModelID, request)
	firstMetadata := map[string]any{}
	handler.appendCacheObservationMetadata(firstCtx, firstMetadata, candidate, gatewayAttemptResult{upstreamProtocol: "anthropic_messages"})
	first := firstMetadata["cache_observation"].(map[string]any)

	changed := request
	changedCanonical := *request.Canonical
	changedCanonical.RawSystem = []any{map[string]any{
		"type": "text",
		"text": "system instruction",
		"cache_control": map[string]any{
			"type": "ephemeral",
			"ttl":  "1h",
		},
	}}
	changedCanonical.Params = map[string]any{"temperature": 0.1}
	changed.Canonical = &changedCanonical
	changedCtx := handler.withCacheObservation(context.Background(), apiKeyID, canonicalModelID, changed)
	changedMetadata := map[string]any{}
	handler.appendCacheObservationMetadata(changedCtx, changedMetadata, candidate, gatewayAttemptResult{upstreamProtocol: "anthropic_messages"})
	changedObservation := changedMetadata["cache_observation"].(map[string]any)

	if first["prefix_hash"] != changedObservation["prefix_hash"] {
		t.Fatalf("sampling or cache directives split generic prefix: before=%q after=%q", first["prefix_hash"], changedObservation["prefix_hash"])
	}
	if first["cache_fingerprint"] == changedObservation["cache_fingerprint"] {
		t.Fatalf("cache directive change retained candidate fingerprint: before=%q after=%q", first["cache_fingerprint"], changedObservation["cache_fingerprint"])
	}
}

func TestCacheShadowAffinityFindsLongestPriorBoundary(t *testing.T) {
	t.Parallel()

	handler := Handler{cacheObservationKey: []byte("cache-observation-test-key")}
	apiKeyID := uuid.New()
	canonicalModelID := uuid.New()
	firstRequest := cacheObservationRequest([]canonicalMessage{{
		Type:       "message",
		Role:       "user",
		RawContent: "first user message",
		Content:    []canonicalContentPart{{Type: "input_text", Text: "first user message"}},
	}})
	first, ok := cacheObservationFromContext(handler.withCacheObservation(context.Background(), apiKeyID, canonicalModelID, firstRequest))
	if !ok {
		t.Fatal("expected first cache observation")
	}
	currentRequest := cacheObservationRequest([]canonicalMessage{
		firstRequest.Canonical.Messages[0],
		{
			Type:       "message",
			Role:       "assistant",
			RawContent: "first assistant message",
			Content:    []canonicalContentPart{{Type: "output_text", Text: "first assistant message"}},
		},
	})
	current, ok := cacheObservationFromContext(handler.withCacheObservation(context.Background(), apiKeyID, canonicalModelID, currentRequest))
	if !ok {
		t.Fatal("expected current cache observation")
	}
	metadata, err := json.Marshal(map[string]any{"cache_observation": map[string]any{
		"prefix_hash":       first.PrefixHash,
		"cache_domain_hash": "opaque-domain",
		"cache_fingerprint": "opaque-fingerprint",
	}})
	if err != nil {
		t.Fatalf("marshal request log metadata: %v", err)
	}
	affinity := cacheShadowAffinityFromRequestLogs(handler.cacheObservationKey, current, nil, []store.RequestLogCacheObservation{{
		Success:   true,
		CreatedAt: time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC),
		Metadata:  store.JSON(metadata),
	}}, time.Date(2026, 8, 14, 10, 1, 0, 0, time.UTC))
	if !affinity.Eligible || !affinity.Matched || affinity.MatchKind != "prefix" || affinity.MatchedPrefixHash != first.PrefixHash ||
		affinity.MatchedCacheDomain != "opaque-domain" || affinity.MatchedFingerprint != "opaque-fingerprint" || affinity.MatchedLineageDepth != 1 {
		t.Fatalf("shadow affinity = %#v, want longest prior prefix match", affinity)
	}
}

func TestCacheShadowAffinityMarksUnmatchedLineageEligible(t *testing.T) {
	t.Parallel()

	handler := Handler{cacheObservationKey: []byte("cache-observation-test-key")}
	observation, ok := cacheObservationFromContext(handler.withCacheObservation(context.Background(), uuid.New(), uuid.New(), cacheObservationRequest([]canonicalMessage{{
		Type:       "message",
		Role:       "user",
		RawContent: "unmatched message",
		Content:    []canonicalContentPart{{Type: "input_text", Text: "unmatched message"}},
	}})))
	if !ok {
		t.Fatal("expected cache observation")
	}
	affinity := cacheShadowAffinityFromRequestLogs(handler.cacheObservationKey, observation, nil, nil, time.Now())
	if !affinity.Eligible || affinity.Matched {
		t.Fatalf("unmatched shadow affinity = %#v, want eligible without match", affinity)
	}
}

func TestCacheShadowAffinityRequiresCompatibleHealthyCandidate(t *testing.T) {
	t.Parallel()

	key := []byte("cache-observation-test-key")
	handler := Handler{cacheObservationKey: key}
	apiKeyID := uuid.New()
	canonicalModelID := uuid.New()
	candidate := routeengine.Candidate{
		Rank: 1,
		Site: routeengine.CandidateSite{
			ID:       uuid.New(),
			SiteType: "openai",
		},
		Model: routeengine.CandidateModel{
			SiteModelID:  uuid.New(),
			UpstreamName: "gpt-test",
		},
	}
	credentialID := uuid.New()
	candidate.Credential.ID = &credentialID
	firstRequest := cacheObservationRequest([]canonicalMessage{{
		Type:       "message",
		Role:       "user",
		RawContent: "first user message",
		Content:    []canonicalContentPart{{Type: "input_text", Text: "first user message"}},
	}})
	first, ok := cacheObservationFromContext(handler.withCacheObservation(context.Background(), apiKeyID, canonicalModelID, firstRequest))
	if !ok {
		t.Fatal("expected first cache observation")
	}
	firstMetadata := map[string]any{}
	handler.appendCacheObservationMetadata(context.WithValue(context.Background(), cacheObservationContextKey{}, first), firstMetadata, candidate, gatewayAttemptResult{
		credentialID:     credentialID,
		upstreamProtocol: "openai_chat_completions",
	})
	encoded, err := json.Marshal(firstMetadata)
	if err != nil {
		t.Fatalf("marshal first metadata: %v", err)
	}

	currentRequest := cacheObservationRequest([]canonicalMessage{
		firstRequest.Canonical.Messages[0],
		{
			Type:       "message",
			Role:       "assistant",
			RawContent: "assistant response",
			Content:    []canonicalContentPart{{Type: "output_text", Text: "assistant response"}},
		},
	})
	current, ok := cacheObservationFromContext(handler.withCacheObservation(context.Background(), apiKeyID, canonicalModelID, currentRequest))
	if !ok {
		t.Fatal("expected current cache observation")
	}
	descriptor := cacheShadowCandidate{
		Candidate:        candidate,
		UpstreamProtocol: "openai_chat_completions",
		CacheDomainHash:  cacheObservationCacheDomainHash(key, candidate.Site.ID, uuid.Nil, ""),
	}
	logs := []store.RequestLogCacheObservation{{
		Success:   true,
		CreatedAt: time.Now().Add(-time.Minute),
		Metadata:  store.JSON(encoded),
	}}
	affinity := cacheShadowAffinityFromRequestLogs(key, current, []cacheShadowCandidate{descriptor}, logs, time.Now())
	if !affinity.Matched || !affinity.Routable || affinity.MatchedCandidateRank != candidate.Rank {
		t.Fatalf("compatible affinity = %#v, want routable candidate", affinity)
	}

	descriptor.UpstreamProtocol = "anthropic_messages"
	affinity = cacheShadowAffinityFromRequestLogs(key, current, []cacheShadowCandidate{descriptor}, logs, time.Now())
	if !affinity.Matched || affinity.Routable {
		t.Fatalf("protocol-incompatible affinity = %#v, want unmatched candidate", affinity)
	}
}

func TestCacheShadowAffinitySkipsExpiredObservation(t *testing.T) {
	t.Parallel()

	observation := cacheObservation{PrefixHash: "current", PrefixLineage: []string{"root", "previous", "current"}}
	metadata, err := json.Marshal(map[string]any{"cache_observation": map[string]any{
		"prefix_hash":       "previous",
		"cache_domain_hash": "domain",
		"cache_fingerprint": "fingerprint",
		"expires_at":        time.Now().Add(-time.Minute),
	}})
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	affinity := cacheShadowAffinityFromRequestLogs([]byte("cache-observation-test-key"), observation, nil, []store.RequestLogCacheObservation{{
		Success:   true,
		CreatedAt: time.Now().Add(-2 * time.Minute),
		Metadata:  store.JSON(metadata),
	}}, time.Now())
	if affinity.Matched {
		t.Fatalf("expired affinity = %#v, want no match", affinity)
	}
}

func TestCacheObservationConfiguredDomainIsProtocolIndependent(t *testing.T) {
	t.Parallel()

	key := []byte("cache-observation-test-key")
	configured := cacheObservationCacheDomainHash(key, uuid.New(), uuid.New(), "shared-project")
	otherConfigured := cacheObservationCacheDomainHash(key, uuid.New(), uuid.New(), "shared-project")
	if configured != otherConfigured {
		t.Fatalf("configured cache domain hashes differ: %q != %q", configured, otherConfigured)
	}
	defaultLeft := cacheObservationCacheDomainHash(key, uuid.New(), uuid.New(), "")
	defaultRight := cacheObservationCacheDomainHash(key, uuid.New(), uuid.New(), "")
	if defaultLeft == defaultRight {
		t.Fatalf("default credential cache domains unexpectedly merged: %q", defaultLeft)
	}
}

func TestCacheObservationInfersProtocolFromCandidateCapabilities(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		request   gatewayRequest
		candidate routeengine.Candidate
		want      string
	}{
		{
			name:    "responses through chat completions",
			request: gatewayRequest{DownstreamPath: gatewayEndpointResponses},
			candidate: routeengine.Candidate{
				Site:  routeengine.CandidateSite{SiteType: "newapi"},
				Model: routeengine.CandidateModel{UpstreamName: "gpt-test", SupportedEndpointTypes: []string{"openai"}},
			},
			want: "openai_chat_completions_to_responses",
		},
		{
			name:    "responses through responses",
			request: gatewayRequest{DownstreamPath: gatewayEndpointResponses},
			candidate: routeengine.Candidate{
				Site:  routeengine.CandidateSite{SiteType: "newapi"},
				Model: routeengine.CandidateModel{UpstreamName: "gpt-test", SupportedEndpointTypes: []string{"openai", "openai-response"}},
			},
			want: "openai_responses",
		},
		{
			name:    "messages through chat completions",
			request: gatewayRequest{DownstreamPath: gatewayEndpointMessages},
			candidate: routeengine.Candidate{
				Site:  routeengine.CandidateSite{SiteType: "newapi"},
				Model: routeengine.CandidateModel{UpstreamName: "gpt-test", SupportedEndpointTypes: []string{"openai"}},
			},
			want: "openai_chat_completions_to_messages",
		},
		{
			name:    "provider anthropic alternate",
			request: gatewayRequest{DownstreamPath: gatewayEndpointResponses},
			candidate: routeengine.Candidate{
				Site:  routeengine.CandidateSite{SiteType: "deepseek"},
				Model: routeengine.CandidateModel{UpstreamName: "deepseek-v4-pro", SupportedEndpointTypes: []string{"anthropic-messages"}},
			},
			want: "deepseek_anthropic_messages_to_responses",
		},
		{
			name:    "chat completions prefers matching native endpoint",
			request: gatewayRequest{DownstreamPath: gatewayEndpointChatCompletions},
			candidate: routeengine.Candidate{
				Site:  routeengine.CandidateSite{SiteType: "deepseek"},
				Model: routeengine.CandidateModel{UpstreamName: "deepseek-v4-pro", SupportedEndpointTypes: []string{"openai", "openai-response", "anthropic-messages"}},
			},
			want: "openai_chat_completions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := cacheObservationInferUpstreamProtocol(tt.candidate, tt.request); got != tt.want {
				t.Fatalf("cacheObservationInferUpstreamProtocol() = %q, want %q", got, tt.want)
			}
		})
	}
}

func cacheObservationChatRequest(t *testing.T, body string) gatewayRequest {
	t.Helper()

	request := httptest.NewRequest("POST", gatewayEndpointChatCompletions, strings.NewReader(body))
	decoded, failure := (chatCompletionsEndpointAdapter{}).DecodeRequest(request)
	if failure != nil {
		t.Fatalf("DecodeRequest failure = %#v", failure)
	}
	return decoded
}

func cacheObservationRequest(messages []canonicalMessage) gatewayRequest {
	return gatewayRequest{
		DownstreamPath: gatewayEndpointChatCompletions,
		Canonical: &canonicalRequest{
			SourceProtocol: canonicalProtocolOpenAIChat,
			Instructions:   "system instruction",
			Tools: []canonicalTool{
				{Type: "function", Name: "first", Parameters: map[string]any{"type": "object"}},
				{Type: "function", Name: "second", Parameters: map[string]any{"type": "object"}},
			},
			Messages: messages,
		},
	}
}

func cacheObservationContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
