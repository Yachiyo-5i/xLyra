package gateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/inflight"
	oauthsvc "xlyra/server/internal/oauth"
	"xlyra/server/internal/ratelimit"
	routeengine "xlyra/server/internal/router"
	"xlyra/server/internal/store"
)

type gatewayAttemptResult struct {
	attempt                  int
	statusCode               int
	upstreamStatusCode       int
	contentType              string
	body                     []byte
	failureResponse          any
	success                  bool
	errorType                string
	errorMessage             string
	latencyMS                int64
	upstreamLatencyMS        int64
	firstByteLatencyMS       int64
	promptTokens             int
	completionTokens         int
	cachedPromptTokens       int
	cacheWriteTokens         int
	cacheCreationInputTokens   int
	cacheCreation5mInputTokens int
	cacheCreation1hInputTokens int
	imageCount               int
	audioOutputTokens        int
	estimatedCost            *float64
	baseEstimatedCost        *float64
	currency                 string
	pricingGroup             string
	pricing                  selectedPricing
	serviceTier              string
	billingMode              string
	costMultiplier           float64
	multiplierReason         string
	stream                   bool
	streamCompleted          bool
	streamReceivedDone       bool
	streamEndReason          string
	responseStarted          bool
	downstreamPath           string
	upstreamPath             string
	upstreamProtocol         string
	upstreamURL              string
	credentialID             uuid.UUID
	credentialName           string
	credentialMasked         string
	credentialPriority       float64
	credentialCostMultiplier float64
	credentialAttempt        int
	credentialTotal          int
	cooldownReason           string
	cooldownScope            string
	cooldownDuration         time.Duration
	cooldownMetadata         map[string]any
	retryAfterSeconds        int64
	rateLimit                *ratelimit.Reservation
	diagnostic               bool
	requestLogID             uuid.UUID
}

func (h Handler) forwardGatewayRequest(
	ctx context.Context,
	w http.ResponseWriter,
	requestID string,
	attempt int,
	apiKeyID uuid.UUID,
	canonicalModelID uuid.UUID,
	candidate routeengine.Candidate,
	request gatewayRequest,
	rateLimit *ratelimit.Reservation,
	protocol gatewayProtocolAdapter,
) gatewayAttemptResult {
	inflight.RouteRequest(requestID, inflight.Route{
		SiteID:   candidate.Site.ID.String(),
		SiteName: candidate.Site.Name,
		SiteType: candidate.Site.SiteType,
		Attempt:  attempt,
	})
	credentials, err := h.gatewayCredentials(ctx, candidate)
	var grokCandidates []grokCredentialCandidate
	if err == nil && isGrokSite(candidate.Site.SiteType) {
		states, stateErr := store.NewSiteAPIKeyStateRepository(h.db.DB()).ListBySite(ctx, candidate.Site.ID)
		if stateErr != nil {
			err = stateErr
		} else {
			grokCandidates = grokCredentialCandidates(credentials, states)
			if len(grokCandidates) == 0 {
				err = gorm.ErrRecordNotFound
			} else {
				credentials = make([]store.GatewayCredential, 0, len(grokCandidates))
				for _, item := range grokCandidates {
					credentials = append(credentials, item.Credential)
				}
			}
		}
	}
	if err != nil {
		startedAt := time.Now()
		result := gatewayAttemptResult{
			attempt:          attempt,
			currency:         "USD",
			stream:           request.Stream,
			downstreamPath:   request.DownstreamPath,
			upstreamPath:     request.DownstreamPath,
			upstreamProtocol: protocol.ProtocolName(),
			rateLimit:        rateLimit,
			diagnostic:       request.Diagnostic,
		}
		result.statusCode = http.StatusBadGateway
		result.errorType = "upstream_credential_unavailable"
		result.errorMessage = err.Error()
		result.latencyMS = time.Since(startedAt).Milliseconds()
		result.requestLogID = h.recordAttempt(ctx, requestID, apiKeyID, canonicalModelID, candidate, result, nil)
		return result
	}

	var lastResult gatewayAttemptResult
	for credentialIndex, selectedCredential := range credentials {
		var releaseCredentialSelection func()
		if isGrokSite(candidate.Site.SiteType) {
			selector := h.credentialSelector
			if selector == nil {
				selector = newCredentialWindowSelector(time.Minute)
			}
			selected, release, ok := selector.selectCredential(grokCandidates)
			if !ok {
				break
			}
			selectedCredential = selected.Credential
			releaseCredentialSelection = release
			for index := range grokCandidates {
				if grokCandidates[index].Credential.Credential.ID == selectedCredential.Credential.ID {
					grokCandidates = append(grokCandidates[:index], grokCandidates[index+1:]...)
					break
				}
			}
			defer releaseCredentialSelection()
		}
		startedAt := time.Now()
		result := gatewayAttemptResult{
			attempt:                  attempt,
			currency:                 "USD",
			stream:                   request.Stream,
			downstreamPath:           request.DownstreamPath,
			upstreamProtocol:         protocol.ProtocolName(),
			credentialID:             selectedCredential.Credential.ID,
			credentialName:           store.SiteCredentialDisplayName(selectedCredential.Credential, selectedCredential.State),
			credentialMasked:         selectedCredential.Credential.MaskedSecret,
			credentialPriority:       store.SiteCredentialRoutingPriority(selectedCredential.Credential),
			credentialCostMultiplier: store.SiteCredentialUpstreamCostMultiplier(selectedCredential.Credential),
			credentialAttempt:        credentialIndex + 1,
			credentialTotal:          len(credentials),
			rateLimit:                rateLimit,
			diagnostic:               request.Diagnostic,
		}

		selectedPricing := h.gatewayPricing(ctx, candidate, selectedCredential.GroupName.String)
		result.currency = selectedPricing.Currency
		result.pricingGroup = selectedPricing.GroupName
		result.pricing = selectedPricing

		upstreamKey, err := h.credentials.Decrypt(selectedCredential.Credential.EncryptedSecret)
		if err != nil {
			result.statusCode = http.StatusBadGateway
			result.errorType = "upstream_credential_decrypt_failed"
			result.errorMessage = err.Error()
			result.latencyMS = time.Since(startedAt).Milliseconds()
			result.requestLogID = h.recordAttempt(ctx, requestID, apiKeyID, canonicalModelID, candidate, result, nil)
			lastResult = result
			if credentialIndex < len(credentials)-1 {
				if releaseCredentialSelection != nil {
					releaseCredentialSelection()
				}
				continue
			}
			return result
		}
		accountID := gatewayCredentialMetaString(selectedCredential.Credential, "account_id")
		if isCodexSite(candidate.Site.SiteType) {
			connection, err := h.oauth.EnsureCodexConnectionFresh(ctx, candidate.Site.ID)
			if err != nil {
				result.statusCode = http.StatusBadGateway
				result.errorType = "codex_oauth_refresh_failed"
				result.errorMessage = err.Error()
				result.latencyMS = time.Since(startedAt).Milliseconds()
				result.requestLogID = h.recordAttempt(ctx, requestID, apiKeyID, canonicalModelID, candidate, result, nil)
				return result
			}
			upstreamKey = connection.AccessToken
			accountID = connection.AccountID
		}
		if isAntigravitySite(candidate.Site.SiteType) {
			connection, err := h.oauth.EnsureAntigravityConnectionFresh(ctx, candidate.Site.ID)
			if err != nil {
				result.statusCode = http.StatusBadGateway
				result.errorType = "antigravity_oauth_refresh_failed"
				result.errorMessage = err.Error()
				result.latencyMS = time.Since(startedAt).Milliseconds()
				result.requestLogID = h.recordAttempt(ctx, requestID, apiKeyID, canonicalModelID, candidate, result, nil)
				return result
			}
			upstreamKey = connection.AccessToken
			accountID = connection.AccountID
		}
		if isClaudeCodeSite(candidate.Site.SiteType) {
			connection, err := h.oauth.EnsureClaudeCodeConnectionFresh(ctx, candidate.Site.ID)
			if err != nil {
				result.statusCode = http.StatusBadGateway
				result.errorType = "claude_code_oauth_refresh_failed"
				result.errorMessage = err.Error()
				result.latencyMS = time.Since(startedAt).Milliseconds()
				result.requestLogID = h.recordAttempt(ctx, requestID, apiKeyID, canonicalModelID, candidate, result, nil)
				return result
			}
			upstreamKey = connection.AccessToken
			accountID = connection.AccountID
		}
		if isGrokSite(candidate.Site.SiteType) {
			accessToken, refreshErr := h.oauth.EnsureGrokAccessToken(ctx, selectedCredential.Credential.ID)
			if refreshErr != nil {
				if errors.Is(refreshErr, oauthsvc.ErrGrokReauthorizationRequired) {
					h.markGrokCredentialReauthRequired(ctx, selectedCredential.Credential, refreshErr.Error())
				}
				result.statusCode = http.StatusBadGateway
				result.errorType = "grok_oauth_refresh_failed"
				result.errorMessage = refreshErr.Error()
				result.latencyMS = time.Since(startedAt).Milliseconds()
				result.requestLogID = h.recordAttempt(ctx, requestID, apiKeyID, canonicalModelID, candidate, result, nil)
				lastResult = result
				if credentialIndex < len(credentials)-1 {
					if releaseCredentialSelection != nil {
						releaseCredentialSelection()
					}
					continue
				}
				return result
			}
			upstreamKey = accessToken
		}

		payload, err := protocol.BuildUpstreamPayload(request, candidate)
		if err != nil {
			result.statusCode = http.StatusBadRequest
			result.contentType = "application/json"
			result.body = gatewayRequestFailureBody("request_encode_failed", err.Error(), requestID)
			result.errorType = "request_encode_failed"
			result.errorMessage = err.Error()
			result.latencyMS = time.Since(startedAt).Milliseconds()
			result.requestLogID = h.recordAttempt(ctx, requestID, apiKeyID, canonicalModelID, candidate, result, nil)
			return result
		}
		result = applyUpstreamBillingMetadata(result, payload, candidate)
		if validationErr := validatePayloadParams(payload, protocol.ProtocolName(), candidate); validationErr != nil {
			result.statusCode = http.StatusBadRequest
			result.errorType = "request_param_validation_failed"
			result.errorMessage = validationErr.Error()
			result.latencyMS = time.Since(startedAt).Milliseconds()
			result.requestLogID = h.recordAttempt(ctx, requestID, apiKeyID, canonicalModelID, candidate, result, nil)
			return result
		}
		upstreamStream := request.Stream
		if value, ok := payload["stream"].(bool); ok && value {
			upstreamStream = true
		}
		if format, ok := payload["stream_format"].(string); ok && strings.EqualFold(strings.TrimSpace(format), "sse") {
			upstreamStream = true
		}
		upstreamProfileRequest := upstreamClientProfileRequest{
			Stream:          upstreamStream,
			ImageGeneration: isCodexImageGenerationRequest(candidate, request, payload) || (isGrokSite(candidate.Site.SiteType) && request.DownstreamPath == gatewayEndpointImagesGenerations),
		}

		upstreamClient, siteConfig, requestHeaders, err := h.upstreamClientForSite(ctx, candidate.Site.ID, upstreamProfileRequest)
		if err != nil {
			result.statusCode = http.StatusBadGateway
			result.errorType = "upstream_client_unavailable"
			result.errorMessage = err.Error()
			result.latencyMS = time.Since(startedAt).Milliseconds()
			result.requestLogID = h.recordAttempt(ctx, requestID, apiKeyID, canonicalModelID, candidate, result, nil)
			return result
		}
		claudeCodeSessionID := applyGatewayClientImpersonationPayload(payload, siteConfig, protocol, request, candidate)
		body, contentType, err := buildUpstreamRequestBody(protocol, request, payload)
		if err != nil {
			result.statusCode = http.StatusBadRequest
			result.errorType = "request_encode_failed"
			result.errorMessage = err.Error()
			result.latencyMS = time.Since(startedAt).Milliseconds()
			result.requestLogID = h.recordAttempt(ctx, requestID, apiKeyID, canonicalModelID, candidate, result, nil)
			return result
		}

		endpoint := protocol.UpstreamPath(candidate.Site.BaseURL)
		result.upstreamPath = endpointPath(endpoint)
		result.upstreamURL = endpoint
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			result.statusCode = http.StatusBadGateway
			result.errorType = "upstream_request_build_failed"
			result.errorMessage = err.Error()
			result.latencyMS = time.Since(startedAt).Milliseconds()
			result.requestLogID = h.recordAttempt(ctx, requestID, apiKeyID, canonicalModelID, candidate, result, nil)
			return result
		}
		req.Header.Set("Authorization", "Bearer "+upstreamKey)
		req.Header.Set("Content-Type", contentType)
		if upstreamStream {
			req.Header.Set("Accept", "text/event-stream")
		} else {
			req.Header.Set("Accept", "application/json")
		}
		if isCodexSite(candidate.Site.SiteType) {
			applyCodexGatewayHeaders(req, accountID, upstreamStream)
		}
		if isAntigravitySite(candidate.Site.SiteType) {
			applyAntigravityGatewayHeaders(req)
		}
		if isGoogleSite(candidate.Site.SiteType) {
			applyGoogleGatewayHeaders(req, upstreamKey)
		}
		if headerAdapter, ok := protocol.(gatewayProtocolHeaderAdapter); ok {
			headerAdapter.ApplyUpstreamHeaders(req, upstreamKey, accountID, upstreamStream)
		}
		applyDownstreamPassthroughHeaders(req, request, candidate)
		applyGatewayClientImpersonationHeaders(req, siteConfig, protocol, request, candidate, claudeCodeSessionID)
		for k, v := range requestHeaders {
			req.Header.Set(k, v)
		}
		if isGrokSite(candidate.Site.SiteType) {
			applyGrokGatewayHeaders(req, upstreamKey, candidate.Model.UpstreamName)
		}
		if isClaudeCodeSite(candidate.Site.SiteType) {
			applyClaudeCodeOAuthGatewayHeaders(req, upstreamKey)
		}

		releaseConcurrency, err := h.acquireUpstreamConcurrency(ctx, candidate, selectedCredential.Credential.ID, siteConfig)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				result.statusCode = transportFailureStatusCode(err)
				result.errorType = transportErrorType(err)
			} else if errors.Is(err, context.DeadlineExceeded) {
				result.statusCode = http.StatusServiceUnavailable
				result.errorType = "upstream_concurrency_wait_timeout"
			} else {
				result.statusCode = http.StatusServiceUnavailable
				result.errorType = "upstream_concurrency_acquire_failed"
				if errors.Is(err, errSiteConcurrencyLimited) {
					result.errorType = "site_concurrency_limited"
				}
				if errors.Is(err, errModelConcurrencyLimited) {
					result.errorType = "model_concurrency_limited"
				}
				if errors.Is(err, errCredentialConcurrencyLimited) {
					result.errorType = "credential_concurrency_limited"
				}
			}
			result.errorMessage = err.Error()
			result.latencyMS = time.Since(startedAt).Milliseconds()
			result.requestLogID = h.recordAttempt(ctx, requestID, apiKeyID, canonicalModelID, candidate, result, nil)
			if errors.Is(err, errCredentialConcurrencyLimited) && credentialIndex < len(credentials)-1 {
				if releaseCredentialSelection != nil {
					releaseCredentialSelection()
				}
				lastResult = result
				continue
			}
			return result
		}

		upstreamStartedAt := time.Now()
		var resp *http.Response
		resp, err = upstreamClient.Do(req)
		result.upstreamLatencyMS = time.Since(upstreamStartedAt).Milliseconds()
		if err != nil {
			releaseConcurrency()
			result.statusCode = transportFailureStatusCode(err)
			result.errorType = transportErrorType(err)
			result.errorMessage = err.Error()
			result.latencyMS = time.Since(startedAt).Milliseconds()
			result.requestLogID = h.recordAttempt(ctx, requestID, apiKeyID, canonicalModelID, candidate, result, nil)
			if releaseCredentialSelection != nil {
				releaseCredentialSelection()
			}
			if isGrokSite(candidate.Site.SiteType) && credentialIndex < len(credentials)-1 {
				lastResult = result
				continue
			}
			return result
		}
		result.statusCode = resp.StatusCode
		result.upstreamStatusCode = resp.StatusCode
		result.contentType = resp.Header.Get("Content-Type")
		inflight.MarkResponding(requestID)

		if request.Stream {
			result = h.handleStreamResponse(ctx, w, requestID, apiKeyID, canonicalModelID, candidate, protocol, resp, result, startedAt)
			if isGrokSite(candidate.Site.SiteType) {
				h.markGrokCredentialSyncFailed(ctx, selectedCredential.Credential, result)
			}
			if !request.Diagnostic {
				h.markCredentialModelUnavailableAfterNotFound(ctx, candidate, selectedCredential.Credential, result)
			}
			releaseConcurrency()
			if releaseCredentialSelection != nil {
				releaseCredentialSelection()
			}
			if !result.success && !result.responseStarted && credentialIndex < len(credentials)-1 && shouldTryNextCredential(result) {
				if !request.Diagnostic && result.statusCode != http.StatusNotFound {
					h.cooldownAfterFailure(ctx, candidate, result)
				}
				lastResult = result
				continue
			}
			return result
		}

		result = h.handleBufferedResponse(ctx, requestID, apiKeyID, canonicalModelID, candidate, protocol, resp, result, startedAt)
		if isGrokSite(candidate.Site.SiteType) {
			h.markGrokCredentialSyncFailed(ctx, selectedCredential.Credential, result)
		}
		if !request.Diagnostic {
			h.markCredentialModelUnavailableAfterNotFound(ctx, candidate, selectedCredential.Credential, result)
		}
		releaseConcurrency()
		if releaseCredentialSelection != nil {
			releaseCredentialSelection()
		}
		if result.success {
			return result
		}
		lastResult = result
		if !shouldTryNextCredential(result) {
			return result
		}
		if credentialIndex < len(credentials)-1 && !request.Diagnostic && result.statusCode != http.StatusNotFound {
			h.cooldownAfterFailure(ctx, candidate, result)
		}
	}

	return lastResult
}

func buildUpstreamRequestBody(protocol gatewayProtocolAdapter, request gatewayRequest, payload map[string]any) ([]byte, string, error) {
	if bodyAdapter, ok := protocol.(gatewayProtocolBodyAdapter); ok {
		body, contentType, err := bodyAdapter.BuildUpstreamBody(request, payload)
		if strings.TrimSpace(contentType) == "" {
			contentType = "application/json"
		}
		return body, contentType, err
	}
	body, err := json.Marshal(payload)
	return body, "application/json", err
}

func (h Handler) handleBufferedResponse(
	ctx context.Context,
	requestID string,
	apiKeyID uuid.UUID,
	canonicalModelID uuid.UUID,
	candidate routeengine.Candidate,
	protocol gatewayProtocolAdapter,
	resp *http.Response,
	result gatewayAttemptResult,
	startedAt time.Time,
) gatewayAttemptResult {
	defer func() {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
	}()

	result.statusCode = resp.StatusCode
	result.upstreamStatusCode = resp.StatusCode
	result.latencyMS = time.Since(startedAt).Milliseconds()
	contentType := resp.Header.Get("Content-Type")
	result.contentType = contentType
	var responseBody []byte
	var readErr error
	bufferedBody := bufio.NewReaderSize(resp.Body, 64*1024)
	if !protocolUsesRawBufferedResponse(protocol) && shouldBufferAsResponsesStream(contentType, bufferedBody) {
		responseBody, readErr = readBufferedResponsesStreamBody(bufferedBody)
	} else {
		readLimit := int64(16 << 20)
		if protocolUsesRawBufferedResponse(protocol) {
			readLimit = audioSpeechMaxBytes
		}
		responseBody, readErr = readResponseBodyWithLimit(bufferedBody, readLimit)
	}
	if readErr != nil {
		var semanticFailure *upstreamSemanticFailure
		if errors.As(readErr, &semanticFailure) {
			return h.finishBufferedSemanticFailure(ctx, requestID, apiKeyID, canonicalModelID, candidate, result, semanticFailure)
		}
		result.success = false
		result.errorType = "upstream_response_read_failed"
		result.errorMessage = readErr.Error()
		result.requestLogID = h.recordAttempt(ctx, requestID, apiKeyID, canonicalModelID, candidate, result, nil)
		return result
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if semanticFailure, ok := semanticFailureFromJSON(responseBody); ok {
			return h.finishBufferedSemanticFailure(ctx, requestID, apiKeyID, canonicalModelID, candidate, result, semanticFailure)
		}
	}

	transformed, transformErr := protocol.TransformBufferedResponse(resp.StatusCode, resp.Header, responseBody)
	if transformErr != nil {
		result.statusCode = http.StatusBadGateway
		result.errorType = "upstream_response_transform_failed"
		result.errorMessage = transformErr.Error()
		result.failureResponse = diagnosticFailureResponse(responseBody)
		result.requestLogID = h.recordAttempt(ctx, requestID, apiKeyID, canonicalModelID, candidate, result, upstreamResponseForMetadata(false, responseBody))
		return result
	}
	result.body = transformed.Body
	result.contentType = transformed.ContentType
	result.statusCode = transformed.StatusCode
	result.success = transformed.StatusCode >= 200 && transformed.StatusCode < 300
	if result.success {
		result.promptTokens = transformed.Usage.PromptTokens
		result.completionTokens = transformed.Usage.CompletionTokens
		result.cachedPromptTokens         = transformed.Usage.CachedPromptTokens
		result.cacheWriteTokens           = transformed.Usage.CacheWriteTokens
		result.cacheCreationInputTokens   = transformed.Usage.CacheCreationInputTokens
		result.cacheCreation5mInputTokens = transformed.Usage.CacheCreation5mInputTokens
		result.cacheCreation1hInputTokens = transformed.Usage.CacheCreation1hInputTokens
		result.imageCount = transformed.Usage.ImageCount
		result.audioOutputTokens = transformed.Usage.AudioOutputTokens
		result.pricing = applyLongContextPricing(transformed.Usage, result.pricing)
		result.estimatedCost = estimateCost(transformed.Usage, result.pricing)
		result = applyEstimatedCostBillingAdjustment(result)
	} else {
		result.errorType = "upstream_http_error"
		result.errorMessage = fmt.Sprintf("upstream returned HTTP %d", transformed.StatusCode)
		result = applyRetryAfterHeader(result, resp.Header)
		result = classifyGatewayUpstreamErrorWithTimeZone(candidate, result, responseBody, time.Now(), h.timeZone)
		result.failureResponse = diagnosticFailureResponse(responseBody)
		h.markOAuthConnectionUnavailableOnAuthFailure(ctx, candidate, result)
		if transformed.UpstreamBilled {
			result.imageCount = transformed.Usage.ImageCount
			result.pricing = applyLongContextPricing(transformed.Usage, result.pricing)
			result.estimatedCost = estimateCost(transformed.Usage, result.pricing)
			result = applyEstimatedCostBillingAdjustment(result)
		}
	}

	result.requestLogID = h.recordAttempt(ctx, requestID, apiKeyID, canonicalModelID, candidate, result, upstreamResponseForMetadata(result.success, responseBody))
	return result
}

func (h Handler) finishBufferedSemanticFailure(
	ctx context.Context,
	requestID string,
	apiKeyID uuid.UUID,
	canonicalModelID uuid.UUID,
	candidate routeengine.Candidate,
	result gatewayAttemptResult,
	failure *upstreamSemanticFailure,
) gatewayAttemptResult {
	result.statusCode = http.StatusBadGateway
	result.contentType = "application/json"
	result.errorType = "upstream_response_failed"
	result.errorMessage = failure.Error()
	result.body = failure.Body
	result.failureResponse = diagnosticFailureResponse(failure.Body)
	result = classifyGatewayUpstreamErrorWithTimeZone(candidate, result, failure.Body, time.Now(), h.timeZone)
	h.markOAuthConnectionUnavailableOnAuthFailure(ctx, candidate, result)
	result.requestLogID = h.recordAttempt(ctx, requestID, apiKeyID, canonicalModelID, candidate, result, upstreamResponseForMetadata(false, failure.Body))
	return result
}

func (h Handler) handleStreamResponse(
	ctx context.Context,
	w http.ResponseWriter,
	requestID string,
	apiKeyID uuid.UUID,
	canonicalModelID uuid.UUID,
	candidate routeengine.Candidate,
	protocol gatewayProtocolAdapter,
	resp *http.Response,
	result gatewayAttemptResult,
	startedAt time.Time,
) gatewayAttemptResult {
	defer func() {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
	}()

	result.statusCode = resp.StatusCode
	result.upstreamStatusCode = resp.StatusCode
	result.contentType = resp.Header.Get("Content-Type")
	result.latencyMS = time.Since(startedAt).Milliseconds()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		responseBody, readErr := readResponseBodyWithLimit(resp.Body, 16<<20)
		if readErr != nil {
			result.statusCode = http.StatusBadGateway
			result.errorType = "upstream_response_read_failed"
			result.errorMessage = readErr.Error()
			result.requestLogID = h.recordAttempt(ctx, requestID, apiKeyID, canonicalModelID, candidate, result, nil)
			return result
		}
		result.body = responseBody
		result.errorType = "upstream_http_error"
		result.errorMessage = fmt.Sprintf("upstream returned HTTP %d", resp.StatusCode)
		result = applyRetryAfterHeader(result, resp.Header)
		result = classifyGatewayUpstreamErrorWithTimeZone(candidate, result, responseBody, time.Now(), h.timeZone)
		result.failureResponse = diagnosticFailureResponse(responseBody)
		h.markOAuthConnectionUnavailableOnAuthFailure(ctx, candidate, result)
		if isGrokSite(candidate.Site.SiteType) && result.downstreamPath == gatewayEndpointImagesGenerations && grokUpstreamCharged(responseBody) {
			result.imageCount = 1
			result.estimatedCost = estimateCost(gatewayUsage{ImageCount: 1}, result.pricing)
			result = applyEstimatedCostBillingAdjustment(result)
		}
		result.requestLogID = h.recordAttempt(ctx, requestID, apiKeyID, canonicalModelID, candidate, result, upstreamResponseForMetadata(false, responseBody))
		return result
	}

	if h.exposeRouteSite {
		setRouteSiteHeader(w.Header(), candidate.Site.Name)
	}
	capture, responseStarted, proxyErr := protocol.ProxyStream(ctx, w, resp, startedAt, candidate)
	if proxyErr != nil && streamCompletedAfterReadError(capture, proxyErr) {
		proxyErr = nil
		capture.endReason = "done"
		capture.streamCompleted = true
		capture.sawDone = true
	}
	result.responseStarted = responseStarted
	if responseStarted {
		if writer, ok := w.(downstreamSSELifecycleWriter); ok {
			writer.FinishSSE()
		}
	}
	if !responseStarted {
		w.Header().Del(RouteSiteHeader)
	}
	result.firstByteLatencyMS = capture.firstByteLatency
	result.streamCompleted = capture.streamCompleted
	result.streamReceivedDone = capture.sawDone
	result.streamEndReason = capture.endReason
	result = applyStreamUsage(result, capture.usage)
	result.latencyMS = time.Since(startedAt).Milliseconds()

	if proxyErr != nil {
		var stateCommitErr *responsesWebSocketStateCommitError
		var clientWriteErr *responsesWebSocketClientWriteError
		if errors.As(proxyErr, &stateCommitErr) {
			result.statusCode = http.StatusRequestEntityTooLarge
			result.errorType = "websocket_state_too_large"
			result.errorMessage = proxyErr.Error()
			result = applyStreamUsage(result, stateCommitErr.usage)
		} else if errors.As(proxyErr, &clientWriteErr) {
			if responseStarted {
				result.statusCode = 499
			}
			result.errorType = "downstream_client_cancelled"
			result.errorMessage = proxyErr.Error()
			if clientWriteErr.usage != (gatewayUsage{}) {
				result = applyStreamUsage(result, clientWriteErr.usage)
			}
		} else if streamSucceeded(capture) {
			result.success = true
		} else {
			if responseStarted {
				result.statusCode = 499
			}
			result.errorType = "upstream_stream_failed"
			if errors.Is(proxyErr, context.Canceled) || errors.Is(proxyErr, context.DeadlineExceeded) {
				result.errorType = "downstream_client_cancelled"
			}
			result.errorMessage = proxyErr.Error()
		}
		result.requestLogID = h.recordAttempt(ctx, requestID, apiKeyID, canonicalModelID, candidate, result, streamMetadataEnvelope(capture))
		return result
	}

	if !responseStarted {
		if capture.endReason == "upstream_stream_error" {
			result.statusCode = http.StatusBadGateway
			result.errorType = "upstream_stream_error"
			result.errorMessage = streamErrorMessageFromEndReason(capture.endReason)
			result.requestLogID = h.recordAttempt(ctx, requestID, apiKeyID, canonicalModelID, candidate, result, streamMetadataEnvelope(capture))
			return result
		}
		result.errorType = "upstream_stream_empty"
		result.errorMessage = "upstream stream returned without any data"
		result.requestLogID = h.recordAttempt(ctx, requestID, apiKeyID, canonicalModelID, candidate, result, streamMetadataEnvelope(capture))
		return result
	}

	if streamSucceeded(capture) {
		result.success = true
	} else {
		result.errorType = streamErrorTypeFromEndReason(capture.endReason)
		result.errorMessage = streamErrorMessageFromEndReason(capture.endReason)
	}
	result.requestLogID = h.recordAttempt(ctx, requestID, apiKeyID, canonicalModelID, candidate, result, streamMetadataEnvelope(capture))
	return result
}

func streamCompletedAfterReadError(capture streamCaptureState, err error) bool {
	if err == nil || capture.endReason != "upstream_stream_read_failed" {
		return false
	}
	return capture.streamCompleted || capture.sawDone
}

func applyStreamUsage(result gatewayAttemptResult, usage gatewayUsage) gatewayAttemptResult {
	result.promptTokens               = usage.PromptTokens
	result.completionTokens           = usage.CompletionTokens
	result.cachedPromptTokens         = usage.CachedPromptTokens
	result.cacheWriteTokens           = usage.CacheWriteTokens
	result.cacheCreationInputTokens   = usage.CacheCreationInputTokens
	result.cacheCreation5mInputTokens = usage.CacheCreation5mInputTokens
	result.cacheCreation1hInputTokens = usage.CacheCreation1hInputTokens
	result.imageCount                 = usage.ImageCount
	result.audioOutputTokens          = usage.AudioOutputTokens
	result.pricing = applyLongContextPricing(usage, result.pricing)
	result.estimatedCost = estimateCost(usage, result.pricing)
	return applyEstimatedCostBillingAdjustment(result)
}

func streamSucceeded(capture streamCaptureState) bool {
	switch strings.TrimSpace(capture.endReason) {
	case "upstream_stream_error", "tool_call_arguments_invalid_json", "upstream_stream_incomplete", "upstream_stream_eof", "upstream_stream_read_failed", "upstream_stream_empty":
		return false
	case "response_incomplete":
		return true
	default:
		return capture.streamCompleted
	}
}

func streamErrorTypeFromEndReason(endReason string) string {
	switch strings.TrimSpace(endReason) {
	case "upstream_stream_error", "tool_call_arguments_invalid_json", "upstream_stream_incomplete", "upstream_stream_eof", "upstream_stream_read_failed":
		return strings.TrimSpace(endReason)
	default:
		return "upstream_stream_incomplete"
	}
}

func streamErrorMessageFromEndReason(endReason string) string {
	switch strings.TrimSpace(endReason) {
	case "response_incomplete":
		return "upstream stream ended with response.incomplete"
	case "upstream_stream_error":
		return "upstream stream returned an error event"
	case "tool_call_arguments_invalid_json":
		return "upstream stream returned invalid tool call arguments"
	default:
		return "upstream stream ended before completion"
	}
}

func (h Handler) gatewayCredentials(ctx context.Context, candidate routeengine.Candidate) ([]store.GatewayCredential, error) {
	repo := store.NewGatewayRepository(h.db.DB())
	credentialRecords, err := repo.ListCredentialsForSiteModel(ctx, candidate.Site.ID, candidate.Model.SiteModelID)
	if err == nil && len(credentialRecords) > 0 {
		return credentialRecords, nil
	}
	if err != nil {
		return nil, err
	}
	if candidate.Site.SiteType == "newapi" {
		return nil, gorm.ErrRecordNotFound
	}
	hasBindings, err := repo.HasCredentialBindingsForSiteModel(ctx, candidate.Site.ID, candidate.Model.SiteModelID)
	if err != nil {
		return nil, err
	}
	if hasBindings {
		return nil, gorm.ErrRecordNotFound
	}
	credentialRecord, err := repo.GetFallbackCredentialForSite(ctx, candidate.Site.ID)
	if err != nil {
		return nil, err
	}
	return []store.GatewayCredential{credentialRecord}, nil
}

func (h Handler) markCredentialModelUnavailableAfterNotFound(ctx context.Context, candidate routeengine.Candidate, credential store.SiteCredential, result gatewayAttemptResult) {
	if result.success || result.statusCode != http.StatusNotFound || credential.ID == uuid.Nil || candidate.Model.SiteModelID == uuid.Nil {
		return
	}
	if err := store.NewSiteAPIKeyModelRepository(h.db.DB()).MarkUnavailable(ctx, credential.ID, candidate.Model.SiteModelID); err != nil {
		h.logger.WarnContext(ctx, "failed to mark credential model unavailable", "site_id", candidate.Site.ID, "site_model_id", candidate.Model.SiteModelID, "credential_id", credential.ID, "error", err)
		return
	}
	h.InvalidateModelsCache()
}

func clonePayload(payload map[string]any) map[string]any {
	cloned := make(map[string]any, len(payload))
	for key, value := range payload {
		cloned[key] = value
	}
	return cloned
}

func readResponseBodyWithLimit(reader io.Reader, limit int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("upstream response exceeds %d byte limit", limit)
	}
	return body, nil
}

func gatewayRequestFailureBody(code string, message string, requestID string) []byte {
	body, _ := json.Marshal(map[string]any{
		"error": map[string]any{
			"code":       code,
			"message":    message,
			"request_id": requestID,
		},
	})
	return body
}

func copyUpstreamResponse(w http.ResponseWriter, result gatewayAttemptResult, siteName string, exposeRouteSite bool) {
	if exposeRouteSite {
		setRouteSiteHeader(w.Header(), siteName)
	}
	writeUpstreamBody(w, result.statusCode, result.contentType, result.body)
}

func setRouteSiteHeader(headers http.Header, siteName string) {
	siteName = strings.TrimSpace(strings.NewReplacer("\r", " ", "\n", " ").Replace(siteName))
	if siteName == "" {
		headers.Del(RouteSiteHeader)
		return
	}
	headers.Set(RouteSiteHeader, url.PathEscape(siteName))
}

func writeUpstreamBody(w http.ResponseWriter, status int, contentType string, body []byte) {
	if contentType == "" {
		contentType = "application/json"
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func writeUpstreamFailure(w http.ResponseWriter, result gatewayAttemptResult, requestID string) {
	if result.retryAfterSeconds > 0 {
		w.Header().Set("Retry-After", strconv.FormatInt(result.retryAfterSeconds, 10))
	}
	if writer, ok := w.(downstreamSSEFailureWriter); ok && writer.WriteSSEFailure(downstreamFailureFromAttempt(result, requestID)) {
		return
	}
	writeUpstreamBody(w, result.statusCode, result.contentType, result.body)
}

func downstreamFailureFromAttempt(result gatewayAttemptResult, requestID string) downstreamSSEFailure {
	failure := downstreamSSEFailure{
		Code:              strings.TrimSpace(result.errorType),
		Message:           strings.TrimSpace(result.errorMessage),
		RequestID:         strings.TrimSpace(requestID),
		RetryAfterSeconds: result.retryAfterSeconds,
	}
	var payload map[string]any
	if json.Unmarshal(result.body, &payload) == nil {
		if errorPayload, ok := payload["error"].(map[string]any); ok {
			failure.Code = nonEmptyString(anyString(errorPayload["code"]), anyString(errorPayload["type"]), failure.Code)
			failure.Message = nonEmptyString(anyString(errorPayload["message"]), failure.Message)
			if failure.RetryAfterSeconds <= 0 {
				failure.RetryAfterSeconds = payloadInt64(errorPayload["retry_after_seconds"])
			}
		} else if message, ok := payload["error"].(string); ok {
			failure.Message = nonEmptyString(message, failure.Message)
		} else {
			failure.Code = nonEmptyString(anyString(payload["code"]), failure.Code)
			failure.Message = nonEmptyString(anyString(payload["message"]), failure.Message)
		}
	}
	failure.Code = nonEmptyString(failure.Code, "upstream_http_error")
	failure.Message = nonEmptyString(failure.Message, fmt.Sprintf("upstream returned HTTP %d", result.statusCode))
	return failure
}

func upstreamResponseForMetadata(success bool, body []byte) any {
	if success {
		usage := parseUpstreamResponseUsageForMetadata(body)
		return map[string]any{
			"usage": usage,
		}
	}
	if len(body) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(body, &value); err == nil {
		return value
	}
	text := string(body)
	if len(text) > 4096 {
		text = text[:4096]
	}
	return text
}

func parseUpstreamResponseUsageForMetadata(body []byte) gatewayUsage {
	var root struct {
		Usage map[string]any `json:"usage"`
	}
	if err := json.Unmarshal(body, &root); err != nil || root.Usage == nil {
		return gatewayUsage{}
	}
	if _, ok := root.Usage["cache_read_input_tokens"]; ok {
		return parseAnthropicMessageUsage(body)
	}
	if _, ok := root.Usage["cache_creation_input_tokens"]; ok {
		return parseAnthropicMessageUsage(body)
	}
	if _, ok := root.Usage["input_tokens"]; ok {
		return parseResponsesUsage(body)
	}
	if _, ok := root.Usage["output_tokens"]; ok {
		return parseResponsesUsage(body)
	}
	if _, ok := root.Usage["input_tokens_details"]; ok {
		return parseResponsesUsage(body)
	}
	return parseCompletionUsage(body)
}

func diagnosticFailureResponse(body []byte) any {
	text := strings.TrimSpace(string(body))
	if text == "" {
		return nil
	}
	if len(text) > 4096 {
		return text[:4096]
	}
	return text
}

func streamMetadataEnvelope(capture streamCaptureState) map[string]any {
	return map[string]any{
		"usage":               capture.usage,
		"stream_completed":    capture.streamCompleted,
		"stream_incomplete":   capture.endReason == "upstream_stream_incomplete" || capture.endReason == "upstream_stream_eof",
		"stream_end_reason":   emptyToNil(capture.endReason),
		"stream_error_detail": emptyToNil(capture.errorDetail),
		"first_byte_latency":  zeroInt64ToNil(capture.firstByteLatency),
		"received_done_event": capture.sawDone,
		"annotations":         emptySliceToNil(capture.annotations),
	}
}

func truncatedStreamErrorDetail(data string) string {
	detail := strings.TrimSpace(data)
	if len(detail) > 2048 {
		detail = detail[:2048]
	}
	return detail
}

func emptySliceToNil[T any](items []T) any {
	if len(items) == 0 {
		return nil
	}
	return items
}

func applyRetryAfterHeader(result gatewayAttemptResult, headers http.Header) gatewayAttemptResult {
	if result.retryAfterSeconds > 0 || result.upstreamStatusCode != http.StatusTooManyRequests {
		return result
	}
	raw := strings.TrimSpace(headers.Get("Retry-After"))
	if raw == "" {
		return result
	}
	if seconds, err := strconv.ParseInt(raw, 10, 64); err == nil && seconds > 0 {
		result.retryAfterSeconds = seconds
		return result
	}
	if at, err := http.ParseTime(raw); err == nil {
		if seconds := int64(time.Until(at).Seconds()); seconds > 0 {
			result.retryAfterSeconds = seconds
		}
	}
	return result
}

func transportErrorType(err error) string {
	if errors.Is(err, context.Canceled) {
		return "downstream_client_cancelled"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "upstream_timeout"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "upstream_timeout"
	}
	return "upstream_transport_error"
}

func transportFailureStatusCode(err error) int {
	if errors.Is(err, context.Canceled) {
		return 499
	}
	return http.StatusBadGateway
}
