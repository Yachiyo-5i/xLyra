package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"xlyra/server/internal/ratelimit"
	routeengine "xlyra/server/internal/router"
	sitepkg "xlyra/server/internal/site"
	"xlyra/server/internal/store"
)

type bridgeContext struct {
	cfg         store.APIKeyImageBridge
	writer      *bridgeStreamWriter
	imageCalls  int
	parentLogID uuid.UUID
}

func newBridgeContext(cfg store.APIKeyImageBridge) *bridgeContext {
	return &bridgeContext{cfg: cfg}
}

func bridgeContextForRequest(apiKey store.APIKey, request gatewayRequest, intent codexImageGenerationIntent) (*bridgeContext, string) {
	if request.DownstreamPath != gatewayEndpointResponses {
		return nil, "non_responses_endpoint"
	}
	if !request.Stream {
		return nil, "non_streaming_request"
	}
	if intent != codexImageIntentToolAdvertised && intent != codexImageIntentExplicit {
		return nil, "no_image_intent"
	}
	if !codexPayloadHasImageGenerationTool(request.Payload) {
		return nil, "no_image_generation_tool"
	}
	if strings.TrimSpace(anyString(request.Payload["previous_response_id"])) != "" {
		return nil, "stateful_previous_response_id"
	}
	cfg, ok := apiKey.ImageBridge()
	if !ok {
		return nil, "key_bridge_not_configured"
	}
	return newBridgeContext(cfg), ""
}

func bridgeRescueEligible(result gatewayAttemptResult) bool {
	if result.success || result.responseStarted {
		return false
	}
	haystack := strings.ToLower(result.errorMessage + " " + string(result.body))
	for _, marker := range []string{
		"image generation is not enabled",
		"image generation is not supported",
		"images api is not supported",
	} {
		if strings.Contains(haystack, marker) {
			return true
		}
	}
	return false
}

func candidateRequiresImageBridge(candidate routeengine.Candidate, protocol gatewayProtocolAdapter) bool {
	switch protocol.(type) {
	case codexProtocolAdapter, openAIResponsesProtocolAdapter:
		cfg := &sitepkg.GatewayConfig{
			ResponsesToolPolicy:    candidate.Site.ResponsesToolPolicy,
			DisabledResponsesTools: candidate.Site.DisabledResponsesTools,
		}
		return sitepkg.ResponsesToolDisabled(cfg, sitepkg.ResponsesHostedToolImageGeneration)
	default:
		return true
	}
}

func (bc *bridgeContext) Close() {
	if bc != nil && bc.writer != nil {
		bc.writer.Close()
	}
}

func (bc *bridgeContext) FailAll(code string, message string) bool {
	if bc == nil || bc.writer == nil {
		return false
	}
	return bc.writer.FailAll(code, message)
}

func (bc *bridgeContext) ensureWriter(w http.ResponseWriter, model string) *bridgeStreamWriter {
	if bc.writer == nil {
		bc.writer = newBridgeStreamWriter(w, model)
		bc.writer.StartEnvelope()
	}
	return bc.writer
}

const bridgeMaxExtraRounds = 2

func (h Handler) forwardBridgedResponses(
	ctx context.Context,
	w http.ResponseWriter,
	requestID string,
	attempt int,
	apiKeyID uuid.UUID,
	canonicalModelID uuid.UUID,
	candidate routeengine.Candidate,
	request gatewayRequest,
	reservation *ratelimit.Reservation,
	resolver upstreamProtocolResolver,
	bc *bridgeContext,
) gatewayAttemptResult {
	rewritten, spec, ok := rewriteImageToolForBridge(request)
	if !ok {
		return h.forwardGatewayRequestResolved(ctx, w, requestID, attempt, apiKeyID, canonicalModelID, candidate, request, reservation, resolver)
	}

	writer := bc.ensureWriter(w, request.RequestedModel)
	inputItems := responsesInputItemsForBridge(rewritten.Payload)
	roundRequest := rewritten
	maxRounds := bc.cfg.MaxCalls + bridgeMaxExtraRounds

	aggPromptTokens := 0
	aggCompletionTokens := 0
	var lastResult gatewayAttemptResult

	for round := 1; round <= maxRounds; round++ {
		if round > 1 {
			payload := clonePayload(roundRequest.Payload)
			payload["input"] = inputItems
			payload["tool_choice"] = "auto"
			roundRequest.Payload = payload
			if canonical, err := canonicalRequestFromOpenAIResponsesPayload(payload, roundRequest.RequestedModel); err == nil {
				roundRequest.Canonical = &canonical
			}
		}

		roundCtx := ctx
		roundReservation := reservation
		if round > 1 {
			roundCtx = withBridgeRecording(ctx, bc.parentLogID)
			roundReservation = nil
		}

		if h.logger != nil {
			toolsJSON, _ := json.Marshal(roundRequest.Payload["tools"])
			choiceJSON, _ := json.Marshal(roundRequest.Payload["tool_choice"])
			h.logger.DebugContext(ctx, "image bridge round start",
				"scope", "gateway", "request_id", requestID, "round", round,
				"site", candidate.Site.Slug, "tools", string(toolsJSON), "tool_choice", string(choiceJSON))
		}

		writer.beginRound()
		result := h.forwardGatewayRequestResolved(roundCtx, writer, requestID, attempt, apiKeyID, canonicalModelID, candidate, roundRequest, roundReservation, resolver)
		lastResult = result
		aggPromptTokens += result.promptTokens
		aggCompletionTokens += result.completionTokens
		if round == 1 && result.requestLogID != uuid.Nil {
			bc.parentLogID = result.requestLogID
		}

		calls, replayItems := writer.takeRound()

		if !result.success && h.logger != nil {
			h.logger.WarnContext(ctx, "image bridge round failed",
				"scope", "gateway", "request_id", requestID, "round", round,
				"site", candidate.Site.Slug, "error_type", result.errorType, "error", result.errorMessage,
				"latency_ms", result.latencyMS, "captured_calls", len(calls), "content_flushed", writer.ContentFlushed())
		}

		if !result.success {
			if result.errorType == "downstream_client_cancelled" {
				break
			}
			if round > 1 && writer.ContentFlushed() {
				writer.CompleteGracefully()
				lastResult.success = true
				lastResult.statusCode = http.StatusOK
			}
			break
		}

		if len(calls) == 0 {
			break
		}

		if len(replayItems) == 0 {
			replayItems = bridgeReplayItemsFromCalls(calls)
		}
		inputItems = append(inputItems, replayItems...)

		bridgeCtx := withBridgeRecording(ctx, bc.parentLogID)
		for _, call := range calls {
			var outcome bridgeImageOutcome
			if bc.imageCalls >= bc.cfg.MaxCalls {
				outcome = bridgeImageOutcome{ErrorMessage: "the image generation limit for this request was reached"}
			} else {
				bc.imageCalls++
				itemID, index := writer.InjectImageStart()
				outcome = h.executeBridgeImageGeneration(bridgeCtx, requestID, apiKeyID, bc.cfg, call.Prompt(), spec)
				writer.InjectImageResult(itemID, index, spec, outcome)
			}
			inputItems = append(inputItems, bridgeFunctionCallOutput(call, outcome))
		}

		if bridgeReplayHasUserFunctionCall(replayItems) {
			writer.CompleteGracefully()
			break
		}
	}

	if lastResult.success && !writer.Finished() {
		writer.CompleteGracefully()
	}

	combined := lastResult
	combined.promptTokens = aggPromptTokens
	combined.completionTokens = aggCompletionTokens
	combined.responseStarted = writer.ContentFlushed() || writer.Finished()
	return combined
}

func (h Handler) forwardGatewayRequestResolved(
	ctx context.Context,
	w http.ResponseWriter,
	requestID string,
	attempt int,
	apiKeyID uuid.UUID,
	canonicalModelID uuid.UUID,
	candidate routeengine.Candidate,
	request gatewayRequest,
	reservation *ratelimit.Reservation,
	resolver upstreamProtocolResolver,
) gatewayAttemptResult {
	protocol, err := resolver.Resolve(ctx, request, candidate)
	if err != nil {
		return gatewayAttemptResult{
			attempt:        attempt,
			statusCode:     http.StatusBadGateway,
			errorType:      "protocol_resolution_failed",
			errorMessage:   err.Error(),
			currency:       "USD",
			stream:         request.Stream,
			downstreamPath: request.DownstreamPath,
			rateLimit:      reservation,
			diagnostic:     request.Diagnostic,
		}
	}
	return h.forwardGatewayRequest(ctx, w, requestID, attempt, apiKeyID, canonicalModelID, candidate, request, reservation, protocol)
}

func bridgeReplayItemsFromCalls(calls []bridgeFunctionCall) []any {
	items := make([]any, 0, len(calls))
	for _, call := range calls {
		items = append(items, map[string]any{
			"type":      "function_call",
			"call_id":   call.CallID,
			"name":      bridgeImageFunctionName,
			"arguments": call.Arguments,
		})
	}
	return items
}
