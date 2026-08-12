package gateway

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"xlyra/server/internal/httpx"
	routeengine "xlyra/server/internal/router"
)

type modelMappingContextKey struct{}
type retryAfterContextKey struct{}
type reasoningEffortContextKey struct{}

type modelMappingInfo struct {
	OriginalModel string
	MappedModel   string
	Mode          string
}

func withModelMapping(ctx context.Context, original, mapped, mode string) context.Context {
	return context.WithValue(ctx, modelMappingContextKey{}, modelMappingInfo{
		OriginalModel: original,
		MappedModel:   mapped,
		Mode:          mode,
	})
}

func modelMappingFromContext(ctx context.Context) (modelMappingInfo, bool) {
	v, ok := ctx.Value(modelMappingContextKey{}).(modelMappingInfo)
	return v, ok
}

func withRetryAfter(ctx context.Context, seconds int64) context.Context {
	if seconds <= 0 {
		return ctx
	}
	return context.WithValue(ctx, retryAfterContextKey{}, seconds)
}

func retryAfterFromContext(ctx context.Context) (int64, bool) {
	v, ok := ctx.Value(retryAfterContextKey{}).(int64)
	return v, ok && v > 0
}

func withReasoningEffort(ctx context.Context, effort string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	effort = strings.TrimSpace(effort)
	if effort == "" {
		return ctx
	}
	return context.WithValue(ctx, reasoningEffortContextKey{}, effort)
}

func reasoningEffortFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	effort, ok := ctx.Value(reasoningEffortContextKey{}).(string)
	effort = strings.TrimSpace(effort)
	return effort, ok && effort != ""
}

func reasoningEffortFromPayload(payload map[string]any) string {
	if reasoning, ok := payload["reasoning"].(map[string]any); ok {
		if effort, ok := reasoning["effort"].(string); ok {
			if effort = strings.TrimSpace(effort); effort != "" {
				return effort
			}
		}
	}
	if effort, ok := payload["reasoning_effort"].(string); ok {
		return strings.TrimSpace(effort)
	}
	return ""
}

const (
	gatewayEndpointChatCompletions   = "/v1/chat/completions"
	gatewayEndpointModels            = "/v1/models"
	gatewayEndpointImagesGenerations = "/v1/images/generations"
	gatewayEndpointImagesEdits       = "/v1/images/edits"
	gatewayEndpointEmbeddings        = "/v1/embeddings"
	gatewayEndpointAudioSpeech       = "/v1/audio/speech"
)

func attemptMetadata(
	ctx context.Context,
	attemptRequestID string,
	parentRequestID string,
	apiKeyID uuid.UUID,
	canonicalModelID uuid.UUID,
	candidate routeengine.Candidate,
	result gatewayAttemptResult,
) map[string]any {
	scope := "gateway"
	if result.diagnostic {
		scope = "site_model_test"
	}
	costCalculation := costCalculationMetadata(gatewayUsage{
		PromptTokens:               result.promptTokens,
		CompletionTokens:           result.completionTokens,
		TotalTokens:                result.promptTokens + result.completionTokens + result.audioOutputTokens,
		ImageCount:                 result.imageCount,
		CachedPromptTokens:         result.cachedPromptTokens,
		CacheWriteTokens:           result.cacheWriteTokens,
		CacheCreationInputTokens:   result.cacheCreationInputTokens,
		CacheCreation5mInputTokens: result.cacheCreation5mInputTokens,
		CacheCreation1hInputTokens: result.cacheCreation1hInputTokens,
		AudioOutputTokens:          result.audioOutputTokens,
	}, result.pricing, result.estimatedCost, billingAdjustment{
		ServiceTier: result.serviceTier,
		Mode:        result.billingMode,
		Multiplier:  result.costMultiplier,
		Reason:      result.multiplierReason,
	}, result.baseEstimatedCost)
	credentialMultiplier := result.credentialCostMultiplier
	if credentialMultiplier <= 0 {
		credentialMultiplier = 1
	}
	serviceTierMultiplier := 1.0
	if result.billingMode == "fast" && result.costMultiplier > 1 {
		serviceTierMultiplier = result.costMultiplier
	}
	costCalculation["base_estimated_cost"] = float64PtrValue(result.baseEstimatedCost)
	costCalculation["credential_upstream_cost_multiplier"] = credentialMultiplier
	costCalculation["service_tier_multiplier"] = serviceTierMultiplier
	meta := map[string]any{
		"scope":                               scope,
		"endpoint":                            emptyToNil(result.downstreamPath),
		"request_id":                          attemptRequestID,
		"parent_request_id":                   parentRequestID,
		"api_key_id":                          nullableCredentialID(apiKeyID),
		"site_id":                             nullableCredentialID(candidate.Site.ID),
		"site_model_id":                       nullableCredentialID(candidate.Model.SiteModelID),
		"canonical_model_id":                  nullableCredentialID(canonicalModelID),
		"attempt":                             result.attempt,
		"credential_attempt":                  result.credentialAttempt,
		"credential_total":                    result.credentialTotal,
		"site_name":                           candidate.Site.Name,
		"site_slug":                           candidate.Site.Slug,
		"site_type":                           candidate.Site.SiteType,
		"site_base_url":                       candidate.Site.BaseURL,
		"upstream_model":                      candidate.Model.UpstreamName,
		"site_model_display_name":             candidate.Model.DisplayName,
		"canonical_model":                     candidate.Model.DisplayName,
		"stream":                              result.stream,
		"response_mode":                       responseModeLabel(result.stream),
		"downstream_transport":                "http",
		"upstream_transport":                  upstreamTransportLabel(result.stream),
		"stream_started":                      result.responseStarted,
		"stream_completed":                    result.streamCompleted,
		"stream_incomplete":                   streamIncomplete(result),
		"stream_received_done":                result.streamReceivedDone,
		"stream_end_reason":                   emptyToNil(result.streamEndReason),
		"stream_failure_scope":                streamFailureScope(result.streamEndReason),
		"protocol_conversion":                 protocolConversionMetadata(result),
		"pricing_group":                       result.pricingGroup,
		"currency":                            result.currency,
		"status_code":                         result.statusCode,
		"upstream_status_code":                result.upstreamStatusCode,
		"no_upstream_response":                upstreamNoResponseFailure(result),
		"error_code":                          emptyToNil(result.errorType),
		"error_type":                          emptyToNil(result.errorType),
		"credential_id":                       nullableCredentialID(result.credentialID),
		"credential_name":                     emptyToNil(result.credentialName),
		"credential_masked":                   emptyToNil(result.credentialMasked),
		"credential_routing_priority":         result.credentialPriority,
		"credential_upstream_cost_multiplier": credentialMultiplier,
		"credential": map[string]any{
			"id":                       nullableCredentialID(result.credentialID),
			"name":                     emptyToNil(result.credentialName),
			"masked_secret":            emptyToNil(result.credentialMasked),
			"routing_priority":         result.credentialPriority,
			"group_name":               emptyToNil(result.pricingGroup),
			"upstream_cost_multiplier": credentialMultiplier,
		},
		"downstream_path":    emptyToNil(result.downstreamPath),
		"upstream_path":      emptyToNil(result.upstreamPath),
		"upstream_protocol":  emptyToNil(result.upstreamProtocol),
		"upstream_url":       emptyToNil(result.upstreamURL),
		"first_byte_latency": zeroInt64ToNil(result.firstByteLatencyMS),
		"image_count":        zeroIntToNil(result.imageCount),
		"service_tier":       emptyToNil(result.serviceTier),
		"billing_mode":       emptyToNil(result.billingMode),
		"pricing":            pricingMetadata(result.pricing),
		"cost_calculation":   costCalculation,
	}
	if effort, ok := reasoningEffortFromContext(ctx); ok {
		meta["reasoning_effort"] = effort
	}
	if result.diagnostic {
		meta["test"] = true
		meta["downstream_api_key"] = nil
	}
	if mapping, ok := modelMappingFromContext(ctx); ok {
		meta["original_model"] = mapping.OriginalModel
		meta["mapped_model"] = mapping.MappedModel
		meta["mapping_mode"] = emptyToNil(mapping.Mode)
	}
	if result.rateLimit != nil {
		meta["rate_limit"] = result.rateLimit.Metadata(rateLimitTokenCount(result))
	} else if rateLimit, ok := rateLimitMetadataFromContext(ctx); ok {
		meta["rate_limit"] = rateLimit
	}
	applyResponsesWebSocketMetadata(meta, ctx)
	return meta
}

func streamIncomplete(result gatewayAttemptResult) bool {
	return result.stream && result.responseStarted && streamEndedIncomplete(result.streamCompleted, result.streamEndReason)
}

func streamEndedIncomplete(streamCompleted bool, endReason string) bool {
	reason := strings.TrimSpace(endReason)
	if streamCompleted || reason == "" || reason == "done" || reason == "response_incomplete" {
		return false
	}
	return true
}

func streamFailureScope(endReason string) any {
	switch endReason {
	case "":
		return nil
	case "downstream_client_cancelled", "downstream_stream_write_failed":
		return "downstream"
	case "upstream_stream_error", "tool_call_arguments_invalid_json", "upstream_stream_read_failed", "upstream_stream_parse_failed", "upstream_stream_incomplete", "upstream_stream_eof", "upstream_stream_empty", "upstream_stream_missing_body", "upstream_stream_preoutput_too_large":
		return "upstream"
	default:
		return nil
	}
}

func protocolConversionMetadata(result gatewayAttemptResult) any {
	if !result.stream {
		return nil
	}
	downstreamProtocol := downstreamProtocolFromPath(result.downstreamPath)
	upstreamProtocol := strings.TrimSpace(result.upstreamProtocol)
	return map[string]any{
		"mode":                streamConversionMode(downstreamProtocol, upstreamProtocol),
		"downstream_protocol": emptyToNil(downstreamProtocol),
		"upstream_protocol":   emptyToNil(upstreamProtocol),
	}
}

func downstreamProtocolFromPath(path string) string {
	switch strings.TrimSpace(path) {
	case gatewayEndpointChatCompletions:
		return string(canonicalProtocolOpenAIChat)
	case gatewayEndpointResponses:
		return string(canonicalProtocolOpenAIResponses)
	case gatewayEndpointMessages:
		return string(canonicalProtocolAnthropicMessages)
	case gatewayEndpointImagesGenerations, gatewayEndpointImagesEdits:
		return string(canonicalProtocolOpenAIImages)
	default:
		return ""
	}
}

func streamConversionMode(downstreamProtocol string, upstreamProtocol string) string {
	if downstreamProtocol == "" || upstreamProtocol == "" {
		return "unknown"
	}
	if downstreamProtocol == upstreamProtocol {
		return "passthrough"
	}
	return "canonical"
}

func requestFailureMetadata(
	ctx context.Context,
	requestID string,
	apiKeyID uuid.UUID,
	statusCode int,
	errorType string,
	errorMessage string,
	requestedModel string,
	stream bool,
	stage string,
	endpoint string,
) map[string]any {
	if strings.TrimSpace(endpoint) == "" {
		endpoint = gatewayEndpointChatCompletions
	}
	meta := map[string]any{
		"scope":                "gateway",
		"failure_scope":        "gateway",
		"endpoint":             endpoint,
		"request_id":           requestID,
		"parent_request_id":    requestID,
		"api_key_id":           nullableCredentialID(apiKeyID),
		"attempt":              0,
		"stage":                emptyToNil(stage),
		"requested_model":      emptyToNil(requestedModel),
		"stream":               stream,
		"response_mode":        responseModeLabel(stream),
		"downstream_transport": "http",
		"upstream_transport":   nil,
		"downstream_path":      endpoint,
		"status_code":          statusCode,
		"error_code":           emptyToNil(errorType),
		"error_type":           emptyToNil(errorType),
		"error_response":       httpx.ErrorPayload(errorType, errorMessage, ""),
	}
	if mapping, ok := modelMappingFromContext(ctx); ok {
		meta["original_model"] = mapping.OriginalModel
		meta["mapped_model"] = mapping.MappedModel
		meta["mapping_mode"] = emptyToNil(mapping.Mode)
	}
	if retryAfter, ok := retryAfterFromContext(ctx); ok {
		meta["retry_after_seconds"] = retryAfter
	}
	if rateLimit, ok := rateLimitMetadataFromContext(ctx); ok {
		meta["rate_limit"] = rateLimit
	}
	if effort, ok := reasoningEffortFromContext(ctx); ok {
		meta["reasoning_effort"] = effort
	}
	applyResponsesWebSocketMetadata(meta, ctx)
	return meta
}

func upstreamTransportLabel(stream bool) string {
	if stream {
		return "http_sse"
	}
	return "http"
}

func applyResponsesWebSocketMetadata(meta map[string]any, ctx context.Context) {
	metadata, ok := responsesWebSocketMetadataFromContext(ctx)
	if !ok {
		return
	}
	meta["downstream_transport"] = "websocket"
	meta["websocket_connection_id"] = metadata.ConnectionID
	meta["websocket_turn_index"] = metadata.TurnIndex
	meta["websocket_connection_reused"] = metadata.ConnectionReused
	meta["websocket_state_mode"] = emptyToNil(metadata.StateMode)
	meta["websocket_had_previous_response_id"] = metadata.HadPrevious
	meta["websocket_connection_age_ms"] = metadata.ConnectionAgeMS
}
