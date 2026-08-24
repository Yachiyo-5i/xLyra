package gateway

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"xlyra/server/internal/adapter"
	"xlyra/server/internal/config"
	routeengine "xlyra/server/internal/router"
	sitepkg "xlyra/server/internal/site"
	"xlyra/server/internal/store"
	"xlyra/server/internal/upstream"
)

const codexGatewayUserAgent = adapter.CodexUserAgent

func isCodexSite(siteType string) bool {
	return strings.EqualFold(strings.TrimSpace(siteType), "codex")
}

func applyCodexGatewayHeaders(req *http.Request, accountID string, stream bool) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", codexGatewayUserAgent)
	req.Header.Set("Connection", "Keep-Alive")
	req.Header.Set("Originator", codexOriginator)
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}
	if strings.TrimSpace(accountID) != "" {
		req.Header.Set("Chatgpt-Account-Id", strings.TrimSpace(accountID))
	}
}

const codexResponsesLiteHeader = "X-OpenAI-Internal-Codex-Responses-Lite"

const codexRemoteCompactionV2Feature = "remote_compaction_v2"

func codexResponsesLiteRequested(request gatewayRequest) bool {
	if request.DownstreamHeaders == nil {
		return false
	}
	if strings.TrimSpace(request.DownstreamPath) != gatewayEndpointResponses {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(request.DownstreamHeaders.Get(codexResponsesLiteHeader)), "true")
}

func applyCodexResponsesLitePolicy(payload map[string]any, request gatewayRequest) map[string]any {
	if payload == nil {
		return payload
	}
	if codexResponsesLiteRequested(request) {
		payload["parallel_tool_calls"] = false
	}
	return payload
}

var codexRemoteCompactionV2Headers = map[string]struct{}{
	"session-id":               {},
	"thread-id":                {},
	"x-oai-attestation":        {},
	"x-client-request-id":      {},
	"x-codex-beta-features":    {},
	"x-codex-installation-id":  {},
	"x-codex-parent-thread-id": {},
	"x-codex-turn-metadata":    {},
	"x-codex-turn-state":       {},
	"x-codex-window-id":        {},
	"x-openai-subagent":        {},
}

var downstreamPassthroughBlockedHeaders = map[string]struct{}{
	"accept":              {},
	"accept-encoding":     {},
	"authorization":       {},
	"connection":          {},
	"content-length":      {},
	"content-type":        {},
	"cookie":              {},
	"host":                {},
	"keep-alive":          {},
	"proxy-authenticate":  {},
	"proxy-authorization": {},
	"te":                  {},
	"trailer":             {},
	"transfer-encoding":   {},
	"upgrade":             {},
	"x-api-key":           {},
}

func applyDownstreamPassthroughHeaders(req *http.Request, request gatewayRequest, candidate routeengine.Candidate) {
	if req == nil || request.DownstreamHeaders == nil {
		return
	}
	if !gatewayEndpointSupportsDownstreamHeaderPassthrough(request.DownstreamPath) {
		return
	}
	remoteCompactionV2 := isCodexRemoteCompactionV2Request(request, candidate)
	codexSite := isCodexSite(candidate.Site.SiteType)
	if codexSite && !remoteCompactionV2 && !codexResponsesLiteRequested(request) {
		return
	}
	for key, values := range request.DownstreamHeaders {
		if !downstreamHeaderPassthroughAllowed(key, remoteCompactionV2, codexSite) {
			continue
		}
		req.Header.Del(key)
		for _, value := range values {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			req.Header.Add(key, value)
		}
	}
}

func isCodexRemoteCompactionV2Request(request gatewayRequest, candidate routeengine.Candidate) bool {
	if request.DownstreamPath != gatewayEndpointResponses {
		return false
	}
	if !codexRemoteCompactionV2Model(request, candidate) {
		return false
	}
	if !codexBetaFeaturesContains(request.DownstreamHeaders, codexRemoteCompactionV2Feature) {
		return false
	}
	return payloadInputContainsType(request.Payload, "compaction_trigger")
}

func codexRemoteCompactionV2Model(request gatewayRequest, candidate routeengine.Candidate) bool {
	for _, model := range []string{
		candidate.Model.UpstreamName,
		candidate.Model.DisplayName,
		request.RequestedModel,
		stringFromPayloadModel(request.Payload),
	} {
		if isGPTModelName(model) {
			return true
		}
	}
	return false
}

func gatewayEndpointSupportsDownstreamHeaderPassthrough(path string) bool {
	switch strings.TrimSpace(path) {
	case gatewayEndpointResponses, gatewayEndpointMessages:
		return true
	default:
		return false
	}
}

func downstreamHeaderPassthroughAllowed(key string, remoteCompactionV2 bool, codexSite bool) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" {
		return false
	}
	if _, blocked := downstreamPassthroughBlockedHeaders[key]; blocked {
		return false
	}
	if codexSite {
		if key == strings.ToLower(codexResponsesLiteHeader) {
			return true
		}
		if _, ok := codexRemoteCompactionV2Headers[key]; ok {
			return remoteCompactionV2
		}
		return false
	}
	return true
}

func codexBetaFeaturesContains(headers http.Header, feature string) bool {
	feature = strings.TrimSpace(feature)
	if headers == nil || feature == "" {
		return false
	}
	for _, value := range headers.Values("X-Codex-Beta-Features") {
		for _, item := range strings.Split(value, ",") {
			if strings.TrimSpace(item) == feature {
				return true
			}
		}
	}
	return false
}

func payloadInputContainsType(payload map[string]any, itemType string) bool {
	itemType = strings.TrimSpace(itemType)
	if payload == nil || itemType == "" {
		return false
	}
	return payloadContainsTypedItem(payload["input"], itemType)
}

func payloadContainsTypedItem(value any, itemType string) bool {
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			if payloadContainsTypedItem(item, itemType) {
				return true
			}
		}
	case []map[string]any:
		for _, item := range typed {
			if payloadContainsTypedItem(item, itemType) {
				return true
			}
		}
	case map[string]any:
		if strings.TrimSpace(anyString(typed["type"])) == itemType {
			return true
		}
		for _, nested := range []string{"content", "items"} {
			if payloadContainsTypedItem(typed[nested], itemType) {
				return true
			}
		}
	}
	return false
}

func classifyGatewayUpstreamError(candidate routeengine.Candidate, result gatewayAttemptResult, body []byte, now time.Time) gatewayAttemptResult {
	return classifyGatewayUpstreamErrorWithTimeZone(candidate, result, body, now, config.ResolveTimeZone())
}

func classifyGatewayUpstreamErrorWithTimeZone(candidate routeengine.Candidate, result gatewayAttemptResult, body []byte, now time.Time, timeZone config.TimeZone) gatewayAttemptResult {
	if classified, ok := classifyOpenCodeGoUsageLimit(candidate, result, body, now); ok {
		return classified
	}
	classified, ok := codexUpstreamError{}, false
	if isCodexSite(candidate.Site.SiteType) {
		classified, ok = classifyCodexUpstreamError(result.upstreamStatusCode, body, now)
	}
	if isAntigravitySite(candidate.Site.SiteType) {
		classified, ok = classifyAntigravityUpstreamError(result.upstreamStatusCode, body, now)
	}
	if !ok {
		failure := upstream.ClassifyResponseAt(result.upstreamStatusCode, nil, body, now)
		if failure.SubscriptionLimited() {
			resetAt, retryAfterSeconds := subscriptionLimitResetAt(result, failure, now, timeZone)
			classified = codexUpstreamError{
				StatusCode:        result.statusCode,
				ErrorType:         "upstream_subscription_limit_exceeded",
				ErrorMessage:      nonEmptyString(failure.Message, result.errorMessage),
				CooldownReason:    store.CooldownReasonUpstreamSubscriptionLimitExceeded,
				CooldownScope:     "credential",
				CooldownDuration:  resetAt.Sub(now),
				RetryAfterSeconds: retryAfterSeconds,
				CooldownMetadata: map[string]any{
					"limit_window":    failure.LimitWindow,
					"upstream_code":   failure.Code,
					"upstream_reason": failure.LimitReason,
					"reset_at":        resetAt.UTC().Format(time.RFC3339),
				},
			}
			ok = true
		} else if failure.Limited() {
			retryAfterSeconds := failure.RetryAfterSeconds
			if retryAfterSeconds <= 0 {
				retryAfterSeconds = result.retryAfterSeconds
			}
			duration := time.Duration(retryAfterSeconds) * time.Second
			classified = codexUpstreamError{
				StatusCode:        result.statusCode,
				ErrorType:         "upstream_credential_limited",
				ErrorMessage:      nonEmptyString(failure.Message, result.errorMessage),
				CooldownReason:    store.CooldownReasonUpstreamCredentialLimited,
				CooldownScope:     "credential",
				CooldownDuration:  duration,
				RetryAfterSeconds: retryAfterSeconds,
			}
			ok = true
		} else if failure.CredentialInvalid() {
			classified = codexUpstreamError{
				StatusCode:       result.statusCode,
				ErrorType:        "upstream_credential_invalid",
				ErrorMessage:     nonEmptyString(failure.Message, result.errorMessage),
				CooldownReason:   store.CooldownReasonUpstreamCredentialUnauthorized,
				CooldownScope:    "credential",
				CooldownDuration: credentialUnauthorizedCooldownDuration,
			}
			ok = true
		}
	}
	if !ok {
		return result
	}
	result.statusCode = classified.StatusCode
	result.errorType = classified.ErrorType
	result.errorMessage = classified.ErrorMessage
	result.cooldownReason = classified.CooldownReason
	result.cooldownScope = classified.CooldownScope
	result.cooldownDuration = classified.CooldownDuration
	result.cooldownMetadata = classified.CooldownMetadata
	result.retryAfterSeconds = classified.RetryAfterSeconds
	return result
}

func subscriptionLimitResetAt(result gatewayAttemptResult, failure upstream.Failure, now time.Time, timeZone config.TimeZone) (time.Time, int64) {
	if now.IsZero() {
		now = time.Now()
	}
	if result.retryAfterSeconds > 0 {
		resetAt := now.Add(time.Duration(result.retryAfterSeconds) * time.Second)
		return resetAt, result.retryAfterSeconds
	}
	if failure.HasRetryAfter && failure.RetryAfterSeconds > 0 {
		resetAt := now.Add(time.Duration(failure.RetryAfterSeconds) * time.Second)
		return resetAt, failure.RetryAfterSeconds
	}
	if failure.ResetAt.After(now) {
		return failure.ResetAt, int64(math.Ceil(failure.ResetAt.Sub(now).Seconds()))
	}
	if resetAt, ok := sitepkg.CredentialQuotaProbeResetAt(result.credentialMeta, failure.LimitWindow, now); ok {
		return resetAt, int64(math.Ceil(resetAt.Sub(now).Seconds()))
	}
	resetAt := subscriptionLimitCalendarResetAt(now, failure.LimitWindow, timeZone)
	return resetAt, int64(math.Ceil(resetAt.Sub(now).Seconds()))
}

func subscriptionLimitCalendarResetAt(now time.Time, window string, timeZone config.TimeZone) time.Time {
	switch strings.ToLower(strings.TrimSpace(window)) {
	case "weekly":
		return timeZone.StartOfWeek(now).AddDate(0, 0, 7)
	case "monthly":
		start := timeZone.StartOfDay(now)
		year, month, _ := start.Date()
		return time.Date(year, month+1, 1, 0, 0, 0, 0, start.Location())
	default:
		return timeZone.StartOfDay(now).AddDate(0, 0, 1)
	}
}

func applyCodexRequestPolicy(payload map[string]any, candidate routeengine.Candidate) map[string]any {
	return applyCodexRequestPolicyWithCallIDMappingScope(payload, candidate, "")
}

func applyCodexRequestPolicyForRequest(payload map[string]any, candidate routeengine.Candidate, request gatewayRequest) map[string]any {
	return applyCodexRequestPolicyWithCallIDMappingScope(payload, candidate, codexCallIDMappingScope(request))
}

func applyCodexRequestPolicyWithCallIDMappingScope(payload map[string]any, candidate routeengine.Candidate, callIDMappingScope string) map[string]any {
	if payload == nil {
		return payload
	}
	model := strings.TrimSpace(anyString(payload["model"]))
	if model == "" {
		model = candidate.Model.UpstreamName
	}
	payload["model"] = model
	if _, ok := payload["instructions"]; !ok {
		payload["instructions"] = ""
	}

	// Codex accepts a Responses-shaped body but rejects several generic
	// OpenAI fields; keep the deny/force list in protocol_specs.json.
	codexCandidate := candidate
	if strings.TrimSpace(codexCandidate.Site.SiteType) == "" {
		codexCandidate.Site.SiteType = "codex"
	}
	applyRequestPolicyForCandidate(payload, canonicalProtocolCodexResponses, codexCandidate)
	normalizeCodexResponsesInputItems(payload)
	normalizeCodexResponsesInput(payload)
	normalizeCodexResponsesCallIDs(payload, callIDMappingScope)
	convertCodexSystemInputRoles(payload)
	normalizeCodexBuiltinTools(payload)
	return payload
}

const codexResponsesMaxCallIDLength = 64

const (
	codexCallIDCollisionCacheTTL        = time.Hour
	codexCallIDCollisionCacheMaxEntries = 1024
)

type codexCallIDCollisionCacheKey struct {
	Scope      string
	OriginalID string
}

type codexCallIDCollisionCacheEntry struct {
	MappedID  string
	ExpiresAt time.Time
}

type codexCallIDCollisionCache struct {
	mu      sync.Mutex
	entries map[codexCallIDCollisionCacheKey]codexCallIDCollisionCacheEntry
}

var codexCallIDCollisions = newCodexCallIDCollisionCache()

func newCodexCallIDCollisionCache() *codexCallIDCollisionCache {
	return &codexCallIDCollisionCache{entries: map[codexCallIDCollisionCacheKey]codexCallIDCollisionCacheEntry{}}
}

func (c *codexCallIDCollisionCache) lookup(scope string, originalID string, now time.Time) (string, bool) {
	if c == nil || scope == "" {
		return "", false
	}
	key := codexCallIDCollisionCacheKey{Scope: scope, OriginalID: originalID}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return "", false
	}
	if !entry.ExpiresAt.After(now) {
		delete(c.entries, key)
		return "", false
	}
	entry.ExpiresAt = now.Add(codexCallIDCollisionCacheTTL)
	c.entries[key] = entry
	return entry.MappedID, true
}

func (c *codexCallIDCollisionCache) remember(scope string, originalID string, mappedID string, now time.Time) {
	if c == nil || scope == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, entry := range c.entries {
		if !entry.ExpiresAt.After(now) {
			delete(c.entries, key)
		}
	}
	key := codexCallIDCollisionCacheKey{Scope: scope, OriginalID: originalID}
	if _, exists := c.entries[key]; !exists && len(c.entries) >= codexCallIDCollisionCacheMaxEntries {
		var oldestKey codexCallIDCollisionCacheKey
		var oldestExpiry time.Time
		for key, entry := range c.entries {
			if oldestExpiry.IsZero() || entry.ExpiresAt.Before(oldestExpiry) {
				oldestKey = key
				oldestExpiry = entry.ExpiresAt
			}
		}
		delete(c.entries, oldestKey)
	}
	c.entries[key] = codexCallIDCollisionCacheEntry{
		MappedID:  mappedID,
		ExpiresAt: now.Add(codexCallIDCollisionCacheTTL),
	}
}

func codexCallIDMappingScope(request gatewayRequest) string {
	conversationID := gatewayRequestConversationHint(request)
	if conversationID == "" {
		return ""
	}
	callerID := strings.TrimSpace(request.DownstreamHeaders.Get("Authorization"))
	digest := sha256.Sum256([]byte(callerID + "\x00" + conversationID))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func normalizeCodexResponsesCallIDs(payload map[string]any, scope string) {
	input, _ := payload["input"].([]any)
	occupied := map[string]struct{}{}
	longCallIDs := map[string]struct{}{}
	for _, rawItem := range input {
		item, _ := rawItem.(map[string]any)
		callID, ok := item["call_id"].(string)
		if !ok {
			continue
		}
		if len(callID) <= codexResponsesMaxCallIDLength {
			occupied[callID] = struct{}{}
			continue
		}
		longCallIDs[callID] = struct{}{}
	}
	if len(longCallIDs) == 0 {
		return
	}
	ids := make([]string, 0, len(longCallIDs))
	for callID := range longCallIDs {
		ids = append(ids, callID)
	}
	sort.Strings(ids)
	now := time.Now()
	mappedIDs := make(map[string]string, len(ids))
	for _, callID := range ids {
		mappedID, cached := codexCallIDCollisions.lookup(scope, callID, now)
		if cached {
			if _, exists := occupied[mappedID]; !exists {
				occupied[mappedID] = struct{}{}
				mappedIDs[callID] = mappedID
				continue
			}
		}
		startAttempt := 0
		if cached {
			startAttempt = 1
		}
		for attempt := startAttempt; ; attempt++ {
			candidate := codexShortCallID(callID, attempt)
			if _, exists := occupied[candidate]; exists {
				continue
			}
			mappedID = candidate
			if attempt > 0 {
				codexCallIDCollisions.remember(scope, callID, mappedID, now)
			}
			break
		}
		occupied[mappedID] = struct{}{}
		mappedIDs[callID] = mappedID
	}
	for _, rawItem := range input {
		item, _ := rawItem.(map[string]any)
		callID, _ := item["call_id"].(string)
		if mappedID, ok := mappedIDs[callID]; ok {
			item["call_id"] = mappedID
		}
	}
}

func codexShortCallID(callID string, attempt int) string {
	value := callID
	if attempt > 0 {
		value += "\x00" + strconv.Itoa(attempt)
	}
	digest := sha256.Sum256([]byte(value))
	return "call_" + base64.RawURLEncoding.EncodeToString(digest[:])
}

func normalizeCodexResponsesInputItems(payload map[string]any) {
	input, ok := payload["input"].([]any)
	if !ok {
		return
	}
	input = cloneSpecSlice(input)
	normalized := make([]any, 0, len(input))
	for _, rawItem := range input {
		item, ok := rawItem.(map[string]any)
		if !ok {
			normalized = append(normalized, rawItem)
			continue
		}
		itemType := strings.TrimSpace(anyString(item["type"]))
		if itemType == "" && strings.TrimSpace(anyString(item["role"])) != "" {
			itemType = "message"
		}
		itemID := strings.TrimSpace(anyString(item["id"]))
		if itemType == "reasoning" {
			if strings.TrimSpace(anyString(item["encrypted_content"])) == "" {
				continue
			}
			if itemID != "" && !responsesItemIDMatchesType(itemType, itemID) {
				delete(item, "id")
			} else if itemID != "" {
				item["id"] = itemID
			}
			normalized = append(normalized, item)
			continue
		}
		if itemType == "compaction_trigger" || (itemID != "" && !responsesItemIDMatchesType(itemType, itemID)) {
			delete(item, "id")
		} else if itemID != "" {
			item["id"] = itemID
		}
		normalized = append(normalized, item)
	}
	payload["input"] = normalized
}

func normalizeCodexResponsesInput(payload map[string]any) {
	switch input := payload["input"].(type) {
	case string:
		text := strings.TrimSpace(input)
		if text == "" {
			payload["input"] = []any{}
			return
		}
		payload["input"] = []any{
			map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{
					map[string]any{
						"type": "input_text",
						"text": text,
					},
				},
			},
		}
	case []any:
		for _, item := range input {
			message, _ := item.(map[string]any)
			itemType := strings.TrimSpace(anyString(message["type"]))
			role := strings.TrimSpace(anyString(message["role"]))
			if itemType == "" && role != "" {
				itemType = "message"
				message["type"] = "message"
			}
			if itemType != "message" {
				continue
			}
			switch content := message["content"].(type) {
			case string:
				message["content"] = []any{
					map[string]any{
						"type": codexContentTypeForRole(role),
						"text": content,
					},
				}
			case []any:
				normalizeCodexResponsesContentParts(content, role)
			}
		}
	}
}

func normalizeCodexResponsesContentParts(content []any, role string) {
	wantType := codexContentTypeForRole(role)
	for _, rawPart := range content {
		part, _ := rawPart.(map[string]any)
		delete(part, "prompt_cache_breakpoint")
		partType := strings.TrimSpace(anyString(part["type"]))
		if partType == "text" || partType == "input_text" || partType == "output_text" {
			part["type"] = wantType
		}
	}
}

func codexContentTypeForRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "assistant":
		return "output_text"
	default:
		return "input_text"
	}
}

func convertCodexSystemInputRoles(payload map[string]any) {
	input, _ := payload["input"].([]any)
	for _, item := range input {
		message, _ := item.(map[string]any)
		if strings.EqualFold(strings.TrimSpace(anyString(message["role"])), "system") {
			message["role"] = "developer"
		}
	}
}

func normalizeCodexBuiltinTools(payload map[string]any) {
	if tools, ok := payload["tools"].([]any); ok {
		for _, item := range tools {
			tool, _ := item.(map[string]any)
			normalizeCodexBuiltinTool(tool)
		}
	}
	if choice, ok := payload["tool_choice"].(map[string]any); ok {
		normalizeCodexBuiltinTool(choice)
		if tools, ok := choice["tools"].([]any); ok {
			for _, item := range tools {
				tool, _ := item.(map[string]any)
				normalizeCodexBuiltinTool(tool)
			}
		}
	}
}

func normalizeCodexBuiltinTool(tool map[string]any) {
	if len(tool) == 0 {
		return
	}
	toolType := strings.TrimSpace(anyString(tool["type"]))
	switch toolType {
	case "web_search_preview", "web_search_preview_2025_03_11":
		tool["type"] = "web_search"
	case "computer_use_preview":
		tool["type"] = "computer_use"
	}
}

type codexUpstreamError struct {
	StatusCode        int
	ErrorType         string
	ErrorMessage      string
	CooldownReason    string
	CooldownScope     string
	CooldownDuration  time.Duration
	CooldownMetadata  map[string]any
	RetryAfterSeconds int64
}

func classifyCodexUpstreamError(statusCode int, body []byte, now time.Time) (codexUpstreamError, bool) {
	if isCodexModelCapacityError(body) {
		return codexUpstreamError{
			StatusCode:       http.StatusTooManyRequests,
			ErrorType:        "codex_model_capacity",
			ErrorMessage:     codexErrorMessage(body, "Codex selected model is at capacity"),
			CooldownReason:   "codex_model_capacity",
			CooldownScope:    "model",
			CooldownDuration: 2 * time.Minute,
		}, true
	}

	if statusCode == http.StatusTooManyRequests && codexErrorType(body) == "usage_limit_reached" {
		duration, seconds := parseCodexRetryAfter(body, now)
		if duration <= 0 {
			duration = 5 * time.Minute
			seconds = int64(duration.Seconds())
		}
		return codexUpstreamError{
			StatusCode:        http.StatusTooManyRequests,
			ErrorType:         "codex_usage_limit_reached",
			ErrorMessage:      codexErrorMessage(body, "Codex usage limit reached"),
			CooldownReason:    store.CooldownReasonCodexUsageLimitReached,
			CooldownScope:     "credential",
			CooldownDuration:  duration,
			RetryAfterSeconds: seconds,
		}, true
	}

	return codexUpstreamError{}, false
}

func isCodexModelCapacityError(body []byte) bool {
	if len(body) == 0 {
		return false
	}
	for _, candidate := range []string{
		codexErrorMessage(body, ""),
		string(body),
	} {
		lower := strings.ToLower(strings.TrimSpace(candidate))
		if strings.Contains(lower, "selected model is at capacity") ||
			strings.Contains(lower, "model is at capacity. please try a different model") {
			return true
		}
	}
	return false
}

func parseCodexRetryAfter(body []byte, now time.Time) (time.Duration, int64) {
	if len(body) == 0 {
		return 0, 0
	}
	payload := map[string]any{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return 0, 0
	}
	errorPayload, _ := payload["error"].(map[string]any)
	if resetsAt, ok := int64FromAny(errorPayload["resets_at"]); ok && resetsAt > 0 {
		resetAtTime := time.Unix(resetsAt, 0)
		if resetAtTime.After(now) {
			duration := resetAtTime.Sub(now)
			return duration, int64(duration.Seconds())
		}
	}
	if resetsInSeconds, ok := int64FromAny(errorPayload["resets_in_seconds"]); ok && resetsInSeconds > 0 {
		return time.Duration(resetsInSeconds) * time.Second, resetsInSeconds
	}
	return 0, 0
}

func int64FromAny(value any) (int64, bool) {
	switch typed := value.(type) {
	case int:
		return int64(typed), true
	case int64:
		return typed, true
	case int32:
		return int64(typed), true
	case float64:
		return int64(typed), true
	case float32:
		return int64(typed), true
	case json.Number:
		parsed, err := typed.Int64()
		return parsed, err == nil
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func codexErrorType(body []byte) string {
	payload := map[string]any{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	errorPayload, _ := payload["error"].(map[string]any)
	return strings.TrimSpace(anyString(errorPayload["type"]))
}

func codexErrorMessage(body []byte, fallback string) string {
	payload := map[string]any{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return fallback
	}
	errorPayload, _ := payload["error"].(map[string]any)
	for _, value := range []any{errorPayload["message"], payload["message"], payload["detail"]} {
		text := strings.TrimSpace(anyString(value))
		if text != "" {
			return text
		}
	}
	return fallback
}

func gatewayCredentialMetaString(credential store.SiteCredential, key string) string {
	meta := map[string]any{}
	if len(credential.Meta) > 0 {
		_ = json.Unmarshal(credential.Meta, &meta)
	}
	value, _ := meta[key].(string)
	return strings.TrimSpace(value)
}
