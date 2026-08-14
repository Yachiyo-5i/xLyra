package gateway

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

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
}

type cacheShadowAffinity struct {
	Eligible            bool
	Matched             bool
	MatchKind           string
	MatchedPrefixHash   string
	MatchedCacheDomain  string
	MatchedFingerprint  string
	MatchedAt           time.Time
	MatchedLineageDepth int
}

const (
	cacheObservationVersion         = 1
	cacheObservationMaxLineageDepth = 128
	cacheObservationHistoryLimit    = 64
)

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
	if len(h.cacheObservationKey) == 0 || apiKeyID == uuid.Nil || canonicalModelID == uuid.Nil {
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

func (h Handler) withCacheShadowAffinity(ctx context.Context, apiKeyID uuid.UUID, canonicalModelID uuid.UUID) context.Context {
	observation, ok := cacheObservationFromContext(ctx)
	if !ok || h.db == nil {
		return ctx
	}
	affinity := cacheShadowAffinity{Eligible: observation.PrefixHash != "" || observation.SessionHash != ""}
	if !affinity.Eligible {
		return ctx
	}
	logs, err := store.NewRequestLogRepository(h.db.DB()).ListRecentByAPIKeyAndCanonicalModel(ctx, apiKeyID, canonicalModelID, cacheObservationHistoryLimit)
	if err != nil {
		if h.logger != nil {
			h.logger.DebugContext(ctx, "cache shadow affinity lookup failed", "scope", "gateway", "error", err)
		}
		return context.WithValue(ctx, cacheShadowAffinityContextKey{}, affinity)
	}
	affinity = cacheShadowAffinityFromRequestLogs(observation, logs)
	return context.WithValue(ctx, cacheShadowAffinityContextKey{}, affinity)
}

func cacheObservationFromRequest(key []byte, apiKeyID uuid.UUID, canonicalModelID uuid.UUID, request gatewayRequest) cacheObservation {
	protocol := string(downstreamCanonicalProtocol(request.DownstreamPath))
	if protocol == "" {
		protocol = strings.TrimSpace(request.DownstreamPath)
	}
	observation := cacheObservation{
		DownstreamProtocol: protocol,
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

func cacheShadowAffinityFromRequestLogs(observation cacheObservation, logs []store.RequestLog) cacheShadowAffinity {
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
			if !ok || previous.SessionHash != observation.SessionHash {
				continue
			}
			return cacheShadowAffinityFromPrevious("session", previous, log.CreatedAt, 0)
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
		if !ok {
			continue
		}
		position, matches := lineagePositions[previous.PrefixHash]
		if !matches || position <= bestPosition {
			continue
		}
		bestPosition = position
		best = cacheShadowAffinityFromPrevious("prefix", previous, log.CreatedAt, position)
	}
	if bestPosition < 0 {
		return affinity
	}
	return best
}

type cacheObservationRequestLog struct {
	PrefixHash       string `json:"prefix_hash"`
	SessionHash      string `json:"session_hash"`
	CacheDomainHash  string `json:"cache_domain_hash"`
	CacheFingerprint string `json:"cache_fingerprint"`
}

func cacheObservationFromRequestLog(log store.RequestLog) (cacheObservationRequestLog, bool) {
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

func cacheShadowAffinityFromPrevious(kind string, previous cacheObservationRequestLog, matchedAt time.Time, lineageDepth int) cacheShadowAffinity {
	return cacheShadowAffinity{
		Eligible:            true,
		Matched:             true,
		MatchKind:           kind,
		MatchedPrefixHash:   previous.PrefixHash,
		MatchedCacheDomain:  previous.CacheDomainHash,
		MatchedFingerprint:  previous.CacheFingerprint,
		MatchedAt:           matchedAt,
		MatchedLineageDepth: lineageDepth,
	}
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
	cacheFingerprint := cacheObservationHMAC(h.cacheObservationKey, "candidate", lineageKey, candidate.Site.ID.String(), candidate.Model.SiteModelID.String(), candidate.Site.SiteType, candidate.Model.UpstreamName, upstreamProtocol)
	credentialID := result.credentialID
	if credentialID == uuid.Nil && candidate.Credential.ID != nil {
		credentialID = *candidate.Credential.ID
	}
	cacheDomainHash := cacheObservationHMAC(h.cacheObservationKey, "domain", candidate.Site.ID.String(), credentialID.String(), upstreamProtocol)
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
			"selected_domain_matches":   shadowAffinity.Matched && shadowAffinity.MatchedCacheDomain == cacheDomainHash,
		}
	}
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
		"cache_fingerprint":     cacheFingerprint,
		"cache_domain_hash":     cacheDomainHash,
		"route_changed":         result.attempt > 1,
		"candidate_rank":        candidate.Rank,
		"candidate_was_cooling": candidate.Cooling,
		"cache_read_tokens":     result.cachedPromptTokens,
		"shadow_affinity":       shadowMetadata,
	}
}
