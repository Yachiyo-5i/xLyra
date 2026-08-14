package gateway

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"xlyra/server/internal/config"
	routeengine "xlyra/server/internal/router"
	"xlyra/server/internal/store"
)

type cacheObservationContextKey struct{}
type cacheShadowAffinityContextKey struct{}

type cacheObservation struct {
	PrefixHash         string
	RootPrefixHash     string
	PrefixLineage      []string
	LineageTruncated   bool
	SessionHash        string
	LineageDepth       int
	DownstreamProtocol string
	CachePolicyHash    string
}

type cacheShadowAffinity struct {
	Eligible             bool
	Matched              bool
	MatchKind            string
	MatchedPrefixHash    string
	MatchedCacheDomain   string
	MatchedFingerprint   string
	MatchedAt            time.Time
	MatchedLineageDepth  int
	Routable             bool
	MatchedCandidateRank int
}

type cacheShadowCandidate struct {
	Candidate        routeengine.Candidate
	UpstreamProtocol string
	CacheDomainHash  string
}

const (
	cacheObservationVersion              = 2
	cacheObservationSerializationVersion = "canonical-v2"
	cacheObservationMaxLineageDepth      = 128
)

type cacheObservationSettings struct {
	Enabled      bool
	HistoryTTL   time.Duration
	HistoryLimit int
}

func (h Handler) cacheObservationSettings() cacheObservationSettings {
	values := config.ReadGeneralConfig(h.confFile).Cache
	return cacheObservationSettings{
		Enabled:      values.Enabled && values.ObservationEnabled,
		HistoryTTL:   time.Duration(values.ObservationTTLMinutes) * time.Minute,
		HistoryLimit: values.ObservationHistoryLimit,
	}
}

func cacheObservationKey(masterKey string) []byte {
	masterKey = strings.TrimSpace(masterKey)
	if masterKey == "" {
		return nil
	}
	sum := sha256.Sum256([]byte("xlyra-cache-observation-v1\x00" + masterKey))
	return sum[:]
}

func (h Handler) withCacheObservation(ctx context.Context, apiKeyID uuid.UUID, canonicalModelID uuid.UUID, request gatewayRequest) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if !h.cacheObservationSettings().Enabled || len(h.cacheObservationKey) == 0 || apiKeyID == uuid.Nil || canonicalModelID == uuid.Nil {
		return ctx
	}
	observation := cacheObservationFromRequest(h.cacheObservationKey, apiKeyID, canonicalModelID, request)
	if observation.PrefixHash == "" && observation.SessionHash == "" {
		return ctx
	}
	return context.WithValue(ctx, cacheObservationContextKey{}, observation)
}

func cacheObservationFromContext(ctx context.Context) (cacheObservation, bool) {
	if ctx == nil {
		return cacheObservation{}, false
	}
	observation, ok := ctx.Value(cacheObservationContextKey{}).(cacheObservation)
	return observation, ok
}

func cacheShadowAffinityFromContext(ctx context.Context) (cacheShadowAffinity, bool) {
	if ctx == nil {
		return cacheShadowAffinity{}, false
	}
	affinity, ok := ctx.Value(cacheShadowAffinityContextKey{}).(cacheShadowAffinity)
	return affinity, ok
}

func (h Handler) withCacheShadowAffinity(ctx context.Context, apiKeyID uuid.UUID, canonicalModelID uuid.UUID, request gatewayRequest, plan routeengine.SelectionPlan, resolver upstreamProtocolResolver) context.Context {
	observation, ok := cacheObservationFromContext(ctx)
	if !ok || h.db == nil {
		return ctx
	}
	settings := h.cacheObservationSettings()
	if !settings.Enabled || settings.HistoryTTL <= 0 || settings.HistoryLimit <= 0 {
		return ctx
	}
	affinity := cacheShadowAffinity{Eligible: observation.PrefixHash != "" || observation.SessionHash != ""}
	if !affinity.Eligible {
		return ctx
	}
	candidates := h.cacheShadowCandidates(ctx, request, plan, resolver)
	if len(candidates) == 0 {
		return context.WithValue(ctx, cacheShadowAffinityContextKey{}, affinity)
	}
	now := time.Now()
	logs, err := store.NewRequestLogRepository(h.db.DB()).ListRecentCacheObservations(ctx, apiKeyID, canonicalModelID, now.Add(-settings.HistoryTTL), settings.HistoryLimit)
	if err != nil {
		if h.logger != nil {
			h.logger.DebugContext(ctx, "cache shadow affinity lookup failed", "scope", "gateway", "error", err)
		}
		return context.WithValue(ctx, cacheShadowAffinityContextKey{}, affinity)
	}
	affinity = cacheShadowAffinityFromRequestLogs(h.cacheObservationKey, observation, candidates, logs, now)
	return context.WithValue(ctx, cacheShadowAffinityContextKey{}, affinity)
}

func (h Handler) cacheShadowCandidates(ctx context.Context, request gatewayRequest, plan routeengine.SelectionPlan, resolver upstreamProtocolResolver) []cacheShadowCandidate {
	if resolver == nil {
		return nil
	}
	items := append([]routeengine.Candidate{plan.Selected}, plan.Failover...)
	result := make([]cacheShadowCandidate, 0, len(items))
	for _, candidate := range items {
		if candidate.Cooling {
			continue
		}
		protocol, err := resolver.Resolve(ctx, request, candidate)
		if err != nil || protocol == nil {
			continue
		}
		credentialID := uuid.Nil
		if candidate.Credential.ID != nil {
			credentialID = *candidate.Credential.ID
		}
		result = append(result, cacheShadowCandidate{
			Candidate:        candidate,
			UpstreamProtocol: strings.TrimSpace(protocol.ProtocolName()),
			CacheDomainHash:  cacheObservationCacheDomainHash(h.cacheObservationKey, candidate.Site.ID, credentialID, candidate.Credential.CacheDomain),
		})
	}
	return result
}

func cacheObservationFromRequest(key []byte, apiKeyID uuid.UUID, canonicalModelID uuid.UUID, request gatewayRequest) cacheObservation {
	protocol := string(downstreamCanonicalProtocol(request.DownstreamPath))
	if protocol == "" {
		protocol = strings.TrimSpace(request.DownstreamPath)
	}
	observation := cacheObservation{
		DownstreamProtocol: protocol,
		CachePolicyHash:    cacheObservationPolicyHash(key, request),
	}
	if root, messages, ok := cacheObservationCanonicalLineage(request); ok {
		rootHash := cacheObservationHMAC(key, "root-prefix", apiKeyID.String(), canonicalModelID.String(), protocol, string(root))
		observation.RootPrefixHash = rootHash
		observation.PrefixLineage = append(observation.PrefixLineage, rootHash)
		previousHash := rootHash
		for _, message := range messages {
			previousHash = cacheObservationHMAC(key, "prefix-boundary", apiKeyID.String(), canonicalModelID.String(), protocol, previousHash, string(message))
			observation.PrefixLineage = append(observation.PrefixLineage, previousHash)
		}
		observation.LineageDepth = len(messages)
		if len(observation.PrefixLineage) > cacheObservationMaxLineageDepth+1 {
			observation.PrefixLineage = append([]string{rootHash}, observation.PrefixLineage[len(observation.PrefixLineage)-cacheObservationMaxLineageDepth:]...)
			observation.LineageTruncated = true
		}
		observation.PrefixHash = previousHash
	}
	if session := gatewayRequestConversationHint(request); session != "" {
		observation.SessionHash = cacheObservationHMAC(key, "session", apiKeyID.String(), canonicalModelID.String(), protocol, session)
	}
	return observation
}

func cacheObservationCanonicalLineage(request gatewayRequest) ([]byte, [][]byte, bool) {
	if request.Canonical == nil || !cacheObservationTextProtocol(request.Canonical.SourceProtocol) {
		return nil, nil, false
	}
	root, err := json.Marshal(struct {
		Instructions string
		Tools        []canonicalTool
		ToolChoice   any
		TextFormat   any
	}{
		Instructions: request.Canonical.Instructions,
		Tools:        request.Canonical.Tools,
		ToolChoice:   request.Canonical.ToolChoice,
		TextFormat:   request.Canonical.TextFormat,
	})
	if err != nil {
		return nil, nil, false
	}
	messages := make([][]byte, 0, len(request.Canonical.Messages))
	for _, message := range request.Canonical.Messages {
		encoded, err := json.Marshal(message)
		if err != nil {
			return nil, nil, false
		}
		messages = append(messages, encoded)
	}
	return root, messages, true
}

func cacheObservationPolicyHash(key []byte, request gatewayRequest) string {
	if request.Canonical == nil {
		return ""
	}
	policy := map[string]any{}
	if directives := cacheObservationDirectives(request.Canonical.RawSystem); directives != nil {
		policy["system"] = directives
	}
	if directives := cacheObservationDirectives(request.Canonical.Tools); directives != nil {
		policy["tools"] = directives
	}
	if directives := cacheObservationDirectives(request.Canonical.Messages); directives != nil {
		policy["messages"] = directives
	}
	if len(request.Canonical.Params) > 0 {
		params := map[string]any{}
		for _, name := range []string{"cache_control", "prompt_cache_key", "prompt_cache_options", "prompt_cache_retention", "cache_ttl"} {
			if value, ok := request.Canonical.Params[name]; ok {
				params[name] = value
			}
		}
		if len(params) > 0 {
			policy["params"] = params
		}
	}
	if len(policy) == 0 {
		return cacheObservationHMAC(key, "cache-policy", "none")
	}
	encoded, err := json.Marshal(policy)
	if err != nil {
		return ""
	}
	return cacheObservationHMAC(key, "cache-policy", string(encoded))
}

func cacheObservationDirectives(value any) any {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var normalized any
	if json.Unmarshal(raw, &normalized) != nil {
		return nil
	}
	return cacheObservationCacheControlFields(normalized)
}

func cacheObservationCacheControlFields(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := map[string]any{}
		for key, item := range typed {
			if key == "cache_control" {
				result[key] = item
				continue
			}
			if nested := cacheObservationCacheControlFields(item); nested != nil {
				result[key] = nested
			}
		}
		if len(result) == 0 {
			return nil
		}
		return result
	case []any:
		result := make([]any, len(typed))
		found := false
		for index, item := range typed {
			if nested := cacheObservationCacheControlFields(item); nested != nil {
				result[index] = nested
				found = true
			}
		}
		if !found {
			return nil
		}
		return result
	default:
		return nil
	}
}

func cacheObservationTextProtocol(protocol canonicalProtocol) bool {
	switch protocol {
	case canonicalProtocolOpenAIChat, canonicalProtocolOpenAIResponses, canonicalProtocolAnthropicMessages, canonicalProtocolCodexResponses:
		return true
	default:
		return false
	}
}

func gatewayRequestConversationHint(request gatewayRequest) string {
	for _, header := range []string{codexGatewayThreadHeader, codexGatewaySessionHeader, "X-Codex-Parent-Thread-Id", claudeCodeGatewaySessionHeader} {
		if value := strings.TrimSpace(request.DownstreamHeaders.Get(header)); value != "" {
			return header + ":" + value
		}
	}
	if value := strings.TrimSpace(anyString(request.Payload["previous_response_id"])); value != "" {
		return "previous_response_id:" + value
	}
	return ""
}

func cacheShadowAffinityFromRequestLogs(key []byte, observation cacheObservation, candidates []cacheShadowCandidate, logs []store.RequestLogCacheObservation, now time.Time) cacheShadowAffinity {
	affinity := cacheShadowAffinity{Eligible: observation.PrefixHash != "" || observation.SessionHash != ""}
	if !affinity.Eligible {
		return affinity
	}
	if observation.SessionHash != "" {
		for _, log := range logs {
			if !log.Success {
				continue
			}
			previous, ok := cacheObservationFromRequestLog(log)
			if !ok || !cacheObservationRequestLogValidAt(previous, now) || previous.SessionHash != observation.SessionHash {
				continue
			}
			return cacheShadowAffinityFromPrevious(key, observation, candidates, "session", previous, log.CreatedAt, 0)
		}
	}
	lineagePositions := make(map[string]int, len(observation.PrefixLineage))
	for index, prefixHash := range observation.PrefixLineage {
		lineagePositions[prefixHash] = index
	}
	bestPosition := -1
	var best cacheShadowAffinity
	for _, log := range logs {
		if !log.Success {
			continue
		}
		previous, ok := cacheObservationFromRequestLog(log)
		if !ok || !cacheObservationRequestLogValidAt(previous, now) {
			continue
		}
		position, matches := lineagePositions[previous.PrefixHash]
		if !matches || position <= bestPosition {
			continue
		}
		bestPosition = position
		best = cacheShadowAffinityFromPrevious(key, observation, candidates, "prefix", previous, log.CreatedAt, position)
	}
	if bestPosition < 0 {
		return affinity
	}
	return best
}

type cacheObservationRequestLog struct {
	PrefixHash       string    `json:"prefix_hash"`
	SessionHash      string    `json:"session_hash"`
	CacheDomainHash  string    `json:"cache_domain_hash"`
	CacheFingerprint string    `json:"cache_fingerprint"`
	ExpiresAt        time.Time `json:"expires_at"`
}

func cacheObservationFromRequestLog(log store.RequestLogCacheObservation) (cacheObservationRequestLog, bool) {
	var metadata struct {
		CacheObservation cacheObservationRequestLog `json:"cache_observation"`
	}
	if err := json.Unmarshal(log.Metadata, &metadata); err != nil {
		return cacheObservationRequestLog{}, false
	}
	previous := metadata.CacheObservation
	if previous.PrefixHash == "" && previous.SessionHash == "" {
		return cacheObservationRequestLog{}, false
	}
	return previous, true
}

func cacheObservationRequestLogValidAt(item cacheObservationRequestLog, now time.Time) bool {
	return item.ExpiresAt.IsZero() || item.ExpiresAt.After(now)
}

func cacheShadowAffinityFromPrevious(key []byte, observation cacheObservation, candidates []cacheShadowCandidate, kind string, previous cacheObservationRequestLog, matchedAt time.Time, lineageDepth int) cacheShadowAffinity {
	affinity := cacheShadowAffinity{
		Eligible:            true,
		Matched:             true,
		MatchKind:           kind,
		MatchedPrefixHash:   previous.PrefixHash,
		MatchedCacheDomain:  previous.CacheDomainHash,
		MatchedFingerprint:  previous.CacheFingerprint,
		MatchedAt:           matchedAt,
		MatchedLineageDepth: lineageDepth,
	}
	lineageKey := previous.PrefixHash
	if lineageKey == "" {
		lineageKey = previous.SessionHash
	}
	for _, candidate := range candidates {
		if candidate.CacheDomainHash != previous.CacheDomainHash {
			continue
		}
		fingerprint := cacheObservationCandidateFingerprint(key, observation, lineageKey, candidate.Candidate, candidate.UpstreamProtocol)
		if fingerprint != previous.CacheFingerprint {
			continue
		}
		affinity.Routable = true
		affinity.MatchedCandidateRank = candidate.Candidate.Rank
		return affinity
	}
	return affinity
}

func cacheShadowAffinityMatchedAt(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

func cacheObservationHMAC(key []byte, domain string, values ...string) string {
	if len(key) == 0 {
		return ""
	}
	mac := hmac.New(sha256.New, key)
	writeCacheObservationHMACValue(mac, domain)
	for _, value := range values {
		writeCacheObservationHMACValue(mac, value)
	}
	return hex.EncodeToString(mac.Sum(nil))
}

func writeCacheObservationHMACValue(mac hashWriter, value string) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(value)))
	_, _ = mac.Write(size[:])
	_, _ = mac.Write([]byte(value))
}

type hashWriter interface {
	Write([]byte) (int, error)
}

func (h Handler) appendCacheObservationMetadata(ctx context.Context, metadata map[string]any, candidate routeengine.Candidate, result gatewayAttemptResult) {
	if metadata == nil || len(h.cacheObservationKey) == 0 {
		return
	}
	observation, ok := cacheObservationFromContext(ctx)
	if !ok {
		return
	}
	upstreamProtocol := strings.TrimSpace(result.upstreamProtocol)
	lineageKey := observation.PrefixHash
	if lineageKey == "" {
		lineageKey = observation.SessionHash
	}
	cacheFingerprint := cacheObservationCandidateFingerprint(h.cacheObservationKey, observation, lineageKey, candidate, upstreamProtocol)
	credentialID := result.credentialID
	if credentialID == uuid.Nil && candidate.Credential.ID != nil {
		credentialID = *candidate.Credential.ID
	}
	cacheDomain := strings.TrimSpace(result.cacheDomain)
	if cacheDomain == "" {
		cacheDomain = candidate.Credential.CacheDomain
	}
	cacheDomainHash := cacheObservationCacheDomainHash(h.cacheObservationKey, candidate.Site.ID, credentialID, cacheDomain)
	shadowAffinity, shadowAffinityKnown := cacheShadowAffinityFromContext(ctx)
	shadowMetadata := map[string]any{"eligible": false}
	if shadowAffinityKnown {
		shadowMetadata = map[string]any{
			"eligible":                  shadowAffinity.Eligible,
			"matched":                   shadowAffinity.Matched,
			"match_kind":                emptyToNil(shadowAffinity.MatchKind),
			"matched_prefix_hash":       emptyToNil(shadowAffinity.MatchedPrefixHash),
			"matched_cache_domain":      emptyToNil(shadowAffinity.MatchedCacheDomain),
			"matched_cache_fingerprint": emptyToNil(shadowAffinity.MatchedFingerprint),
			"matched_at":                cacheShadowAffinityMatchedAt(shadowAffinity.MatchedAt),
			"matched_lineage_depth":     shadowAffinity.MatchedLineageDepth,
			"routable":                  shadowAffinity.Routable,
			"matched_candidate_rank":    shadowAffinity.MatchedCandidateRank,
			"selected_domain_matches":   shadowAffinity.Routable && shadowAffinity.MatchedCacheDomain == cacheDomainHash,
		}
	}
	settings := h.cacheObservationSettings()
	expiresAt := time.Now().Add(settings.HistoryTTL).UTC()
	routeChanged := shadowAffinityKnown && shadowAffinity.Routable && shadowAffinity.MatchedCacheDomain != cacheDomainHash
	metadata["cache_observation"] = map[string]any{
		"version":               cacheObservationVersion,
		"prefix_hash":           observation.PrefixHash,
		"root_prefix_hash":      emptyToNil(observation.RootPrefixHash),
		"prefix_lineage":        observation.PrefixLineage,
		"lineage_truncated":     observation.LineageTruncated,
		"session_hash":          emptyToNil(observation.SessionHash),
		"lineage_depth":         observation.LineageDepth,
		"downstream_protocol":   emptyToNil(observation.DownstreamProtocol),
		"upstream_protocol":     emptyToNil(upstreamProtocol),
		"serialization_version": cacheObservationSerializationVersion,
		"cache_policy_hash":     emptyToNil(observation.CachePolicyHash),
		"cache_fingerprint":     cacheFingerprint,
		"cache_domain_hash":     cacheDomainHash,
		"expires_at":            expiresAt,
		"route_changed":         routeChanged,
		"attempt_failover":      result.attempt > 1,
		"candidate_rank":        candidate.Rank,
		"candidate_was_cooling": candidate.Cooling,
		"cache_read_tokens":     result.cachedPromptTokens,
		"shadow_affinity":       shadowMetadata,
	}
}

func cacheObservationCandidateFingerprint(key []byte, observation cacheObservation, lineageKey string, candidate routeengine.Candidate, upstreamProtocol string) string {
	return cacheObservationHMAC(
		key,
		"candidate",
		strconv.Itoa(cacheObservationVersion),
		cacheObservationSerializationVersion,
		lineageKey,
		observation.CachePolicyHash,
		observation.DownstreamProtocol,
		candidate.Site.SiteType,
		candidate.Model.UpstreamName,
		strings.TrimSpace(upstreamProtocol),
	)
}

func cacheObservationCacheDomainHash(key []byte, siteID uuid.UUID, credentialID uuid.UUID, configuredDomain string) string {
	if configuredDomain = strings.TrimSpace(configuredDomain); configuredDomain != "" {
		return cacheObservationHMAC(key, "domain", "configured", configuredDomain)
	}
	return cacheObservationHMAC(key, "domain", "credential", siteID.String(), credentialID.String())
}
