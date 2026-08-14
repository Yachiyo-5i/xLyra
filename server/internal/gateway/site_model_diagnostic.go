package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	routeengine "xlyra/server/internal/router"
	sitepkg "xlyra/server/internal/site"
	"xlyra/server/internal/store"
)

const (
	defaultSiteModelTestPrompt  = "Reply with only: ok"
	defaultSiteModelTestTimeout = 30 * time.Second
	minSiteModelTestTimeout     = time.Second
	maxSiteModelTestTimeout     = 60 * time.Second
	openAISiteModelTestUA       = "curl/8.0"

	siteModelTestProtocolAuto            = "auto"
	siteModelTestProtocolChatCompletions = "chat_completions"
	siteModelTestProtocolResponses       = "responses"
	siteModelTestProtocolMessages        = "messages"
)

type SiteModelTestInput struct {
	SiteID           uuid.UUID
	SiteModelID      uuid.UUID
	SiteCredentialID uuid.UUID
	Prompt           string
	Timeout          time.Duration
	Protocol         string
	Stream           *bool
}

type SiteModelTestResult struct {
	OK           bool                 `json:"ok"`
	Site         SiteModelTestSite    `json:"site"`
	Model        SiteModelTestModel   `json:"model"`
	Request      SiteModelTestRequest `json:"request"`
	Result       SiteModelTestAttempt `json:"result"`
	RequestLogID string               `json:"request_log_id,omitempty"`
}

type SiteModelTestSite struct {
	ID       uuid.UUID `json:"id"`
	Name     string    `json:"name"`
	SiteType string    `json:"site_type"`
}

type SiteModelTestModel struct {
	ID             uuid.UUID `json:"id"`
	UpstreamName   string    `json:"upstream_name"`
	CanonicalModel string    `json:"canonical_model"`
}

type SiteModelTestRequest struct {
	DownstreamPath                   string    `json:"downstream_path"`
	UpstreamPath                     string    `json:"upstream_path"`
	UpstreamProtocol                 string    `json:"upstream_protocol"`
	SiteCredentialID                 uuid.UUID `json:"site_credential_id,omitempty"`
	CredentialName                   string    `json:"credential_name,omitempty"`
	CredentialMasked                 string    `json:"credential_masked"`
	CredentialGroup                  string    `json:"credential_group,omitempty"`
	CredentialRoutingPriority        float64   `json:"credential_routing_priority"`
	CredentialUpstreamCostMultiplier float64   `json:"credential_upstream_cost_multiplier"`
	CredentialAttempt                int       `json:"credential_attempt"`
	CredentialTotal                  int       `json:"credential_total"`
}

type SiteModelTestAttempt struct {
	StatusCode        int      `json:"status_code"`
	LatencyMS         int64    `json:"latency_ms"`
	UpstreamLatencyMS int64    `json:"upstream_latency_ms"`
	PromptTokens      int      `json:"prompt_tokens"`
	CompletionTokens  int      `json:"completion_tokens"`
	TotalTokens       int      `json:"total_tokens"`
	EstimatedCost     *float64 `json:"estimated_cost"`
	Currency          string   `json:"currency"`
	ResponsePreview   string   `json:"response_preview"`
	ErrorType         *string  `json:"error_type"`
	ErrorMessage      *string  `json:"error_message"`
	FailureResponse   any      `json:"failure_response,omitempty"`
}

type SiteModelTestError struct {
	StatusCode int
	Code       string
	Message    string
}

func (e *SiteModelTestError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func (h Handler) TestSiteModel(ctx context.Context, input SiteModelTestInput) (SiteModelTestResult, error) {
	if h.db == nil {
		return SiteModelTestResult{}, siteModelTestError(http.StatusServiceUnavailable, "gateway_unavailable", "gateway service is not available")
	}

	prompt := strings.TrimSpace(input.Prompt)
	if prompt == "" {
		prompt = defaultSiteModelTestPrompt
	}
	timeout := input.Timeout
	if timeout == 0 {
		timeout = defaultSiteModelTestTimeout
	}
	if timeout < minSiteModelTestTimeout || timeout > maxSiteModelTestTimeout {
		return SiteModelTestResult{}, siteModelTestError(http.StatusBadRequest, "invalid_timeout", "timeout_ms must be between 1000 and 60000")
	}
	protocolMode, err := normalizeSiteModelTestProtocol(input.Protocol)
	if err != nil {
		return SiteModelTestResult{}, err
	}
	stream := true
	if input.Stream != nil {
		stream = *input.Stream
	}

	site, err := store.NewSiteRepository(h.db.DB()).GetByID(ctx, input.SiteID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return SiteModelTestResult{}, siteModelTestError(http.StatusNotFound, "site_not_found", "site was not found")
		}
		return SiteModelTestResult{}, fmt.Errorf("get site: %w", err)
	}
	model, err := store.NewSiteModelRepository(h.db.DB()).GetByID(ctx, input.SiteModelID)
	if err != nil || model.SiteID != site.ID {
		if err == nil || errors.Is(err, gorm.ErrRecordNotFound) {
			return SiteModelTestResult{}, siteModelTestError(http.StatusNotFound, "site_model_not_found", "site model was not found")
		}
		return SiteModelTestResult{}, fmt.Errorf("get site model: %w", err)
	}

	base := SiteModelTestResult{
		Site: SiteModelTestSite{
			ID:       site.ID,
			Name:     site.Name,
			SiteType: site.SiteType,
		},
		Model: SiteModelTestModel{
			ID:           model.ID,
			UpstreamName: model.UpstreamName,
		},
	}

	if !model.CanonicalID.Valid {
		return SiteModelTestResult{}, siteModelTestError(http.StatusBadRequest, "canonical_model_required", "site model must be bound to a canonical model before testing")
	}

	canonical, err := store.NewCanonicalModelRepository(h.db.DB()).GetByID(ctx, model.CanonicalID.UUID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return SiteModelTestResult{}, siteModelTestError(http.StatusBadRequest, "canonical_model_required", "bound canonical model was not found")
		}
		return SiteModelTestResult{}, fmt.Errorf("get canonical model: %w", err)
	}
	base.Model.CanonicalModel = canonical.ModelKey

	endpointTypes, err := (openAIProtocolResolver{db: h.db}).supportedEndpointTypesFromCapabilities(ctx, model.ID)
	if err != nil {
		return SiteModelTestResult{}, fmt.Errorf("read site model capabilities: %w", err)
	}
	downstreamPath, err := siteModelTestDownstreamPathForProtocol(endpointTypes, protocolMode)
	if err != nil {
		return SiteModelTestResult{}, err
	}

	request, err := siteModelTestGatewayRequest(downstreamPath, canonical.ModelKey, prompt, stream)
	if err != nil {
		return SiteModelTestResult{}, siteModelTestError(http.StatusBadRequest, "test_request_build_failed", err.Error())
	}
	candidate := routeengine.Candidate{
		Site: routeengine.CandidateSite{
			ID:              site.ID,
			Name:            site.Name,
			Slug:            site.Slug,
			SiteType:        site.SiteType,
			BaseURL:         site.BaseURL,
			RoutingPriority: site.RoutingPriority,
		},
		Model: routeengine.CandidateModel{
			SiteModelID:     model.ID,
			UpstreamName:    model.UpstreamName,
			DisplayName:     canonical.ModelKey,
			MatchSource:     model.MatchSource,
			MatchConfidence: model.MatchConfidence,
		},
	}

	protocol, err := h.siteModelTestProtocolAdapter(ctx, request, candidate, protocolMode)
	if err != nil {
		return SiteModelTestResult{}, fmt.Errorf("resolve test protocol: %w", err)
	}

	testCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	requestID := "site-model-test/" + uuid.NewString()
	result := h.forwardSiteModelTestRequest(testCtx, requestID, canonical.ID, candidate, request, protocol, input.SiteCredentialID)

	response := base
	response.OK = result.success
	response.Request = SiteModelTestRequest{
		DownstreamPath:                   result.downstreamPath,
		UpstreamPath:                     result.upstreamPath,
		UpstreamProtocol:                 result.upstreamProtocol,
		SiteCredentialID:                 result.credentialID,
		CredentialName:                   result.credentialName,
		CredentialMasked:                 result.credentialMasked,
		CredentialGroup:                  result.pricingGroup,
		CredentialRoutingPriority:        result.credentialPriority,
		CredentialUpstreamCostMultiplier: result.credentialCostMultiplier,
		CredentialAttempt:                result.credentialAttempt,
		CredentialTotal:                  result.credentialTotal,
	}
	response.Result = SiteModelTestAttempt{
		StatusCode:        result.statusCode,
		LatencyMS:         result.latencyMS,
		UpstreamLatencyMS: result.upstreamLatencyMS,
		PromptTokens:      result.promptTokens,
		CompletionTokens:  result.completionTokens,
		TotalTokens:       result.promptTokens + result.completionTokens,
		EstimatedCost:     result.estimatedCost,
		Currency:          result.currency,
		ResponsePreview:   siteModelTestResponsePreview(result.body),
		ErrorType:         emptyStringPointer(result.errorType),
		ErrorMessage:      emptyStringPointer(result.errorMessage),
	}
	if !result.success {
		response.Result.FailureResponse = result.failureResponse
		if response.Result.FailureResponse == nil && len(result.body) > 0 {
			response.Result.FailureResponse = diagnosticFailureResponse(result.body)
		}
	}
	if result.requestLogID != uuid.Nil {
		response.RequestLogID = result.requestLogID.String()
	}
	return response, nil
}

func (h Handler) forwardSiteModelTestRequest(
	ctx context.Context,
	requestID string,
	canonicalModelID uuid.UUID,
	candidate routeengine.Candidate,
	request gatewayRequest,
	protocol gatewayProtocolAdapter,
	requestedCredentialID uuid.UUID,
) gatewayAttemptResult {
	startedAt := time.Now()
	result := gatewayAttemptResult{
		attempt:          1,
		currency:         "USD",
		stream:           request.Stream,
		downstreamPath:   request.DownstreamPath,
		upstreamPath:     request.DownstreamPath,
		upstreamProtocol: protocol.ProtocolName(),
		diagnostic:       true,
	}

	upstreamKey, accountID, credentialMasked, credentialID, groupName, errType, err := h.siteModelTestCredential(ctx, candidate, requestedCredentialID)
	if err != nil {
		result.statusCode = http.StatusBadGateway
		result.errorType = errType
		if result.errorType == "" {
			result.errorType = "upstream_credential_unavailable"
		}
		result.errorMessage = err.Error()
		result.latencyMS = time.Since(startedAt).Milliseconds()
		result.requestLogID = h.recordAttempt(ctx, requestID, uuid.Nil, canonicalModelID, candidate, result, nil)
		return result
	}
	result.credentialID = credentialID
	result.credentialMasked = credentialMasked
	result.credentialAttempt = 1
	result.credentialTotal = 1
	if credentialID != uuid.Nil {
		credential, credentialErr := store.NewSiteCredentialRepository(h.db.DB()).GetByID(ctx, credentialID)
		if credentialErr == nil {
			state := store.SiteAPIKeyState{}
			if states, stateErr := store.NewSiteAPIKeyStateRepository(h.db.DB()).ListBySite(ctx, candidate.Site.ID); stateErr == nil {
				for _, item := range states {
					if item.SiteCredentialID == credentialID {
						state = item
						break
					}
				}
			}
			result.credentialName = store.SiteCredentialDisplayName(credential, state)
			result.credentialPriority = store.SiteCredentialRoutingPriority(credential)
			result.credentialCostMultiplier = store.SiteCredentialUpstreamCostMultiplier(credential)
		}
	}

	selectedPricing := h.gatewayPricing(ctx, candidate, groupName)
	result.currency = selectedPricing.Currency
	result.pricingGroup = selectedPricing.GroupName
	result.pricing = selectedPricing

	payload, err := protocol.BuildUpstreamPayload(request, candidate)
	if err != nil {
		result.statusCode = http.StatusBadRequest
		result.errorType = "request_encode_failed"
		result.errorMessage = err.Error()
		result.latencyMS = time.Since(startedAt).Milliseconds()
		result.requestLogID = h.recordAttempt(ctx, requestID, uuid.Nil, canonicalModelID, candidate, result, nil)
		return result
	}
	result = applyUpstreamBillingMetadata(result, payload, candidate)
	upstreamStream := request.Stream
	if value, ok := payload["stream"].(bool); ok && value {
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
		result.requestLogID = h.recordAttempt(ctx, requestID, uuid.Nil, canonicalModelID, candidate, result, nil)
		return result
	}
	claudeCodeSessionID := applyGatewayClientImpersonationPayload(payload, siteConfig, protocol, request, candidate)
	body, err := json.Marshal(payload)
	if err != nil {
		result.statusCode = http.StatusBadRequest
		result.errorType = "request_encode_failed"
		result.errorMessage = err.Error()
		result.latencyMS = time.Since(startedAt).Milliseconds()
		result.requestLogID = h.recordAttempt(ctx, requestID, uuid.Nil, canonicalModelID, candidate, result, nil)
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
		result.requestLogID = h.recordAttempt(ctx, requestID, uuid.Nil, canonicalModelID, candidate, result, nil)
		return result
	}
	req.Header.Set("Authorization", "Bearer "+upstreamKey)
	req.Header.Set("Content-Type", "application/json")
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
	applyOpenAISiteModelTestUserAgent(req, protocol)
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

	upstreamStartedAt := time.Now()
	resp, err := upstreamClient.Do(req)
	result.upstreamLatencyMS = time.Since(upstreamStartedAt).Milliseconds()
	if err != nil {
		result.statusCode = transportFailureStatusCode(err)
		result.errorType = transportErrorType(err)
		result.errorMessage = err.Error()
		result.latencyMS = time.Since(startedAt).Milliseconds()
		result.requestLogID = h.recordAttempt(ctx, requestID, uuid.Nil, canonicalModelID, candidate, result, nil)
		return result
	}
	if upstreamStream {
		return h.handleSiteModelTestStreamResponse(ctx, requestID, canonicalModelID, candidate, protocol, resp, result, startedAt)
	}
	return h.handleBufferedResponse(ctx, requestID, uuid.Nil, canonicalModelID, candidate, protocol, resp, result, startedAt)
}

func (h Handler) siteModelTestCredential(ctx context.Context, candidate routeengine.Candidate, requestedCredentialID uuid.UUID) (string, string, string, uuid.UUID, string, string, error) {
	if isCodexSite(candidate.Site.SiteType) {
		if h.oauth == nil {
			return "", "", "", uuid.Nil, "", "codex_oauth_unavailable", fmt.Errorf("oauth service is not available")
		}
		connection, err := h.oauth.EnsureCodexConnectionFresh(ctx, candidate.Site.ID)
		if err != nil {
			return "", "", "", uuid.Nil, "", "codex_oauth_refresh_failed", err
		}
		return connection.AccessToken, connection.AccountID, connection.Connection.MaskedAccessToken, uuid.Nil, "", "", nil
	}
	if isAntigravitySite(candidate.Site.SiteType) {
		if h.oauth == nil {
			return "", "", "", uuid.Nil, "", "antigravity_oauth_unavailable", fmt.Errorf("oauth service is not available")
		}
		connection, err := h.oauth.EnsureAntigravityConnectionFresh(ctx, candidate.Site.ID)
		if err != nil {
			return "", "", "", uuid.Nil, "", "antigravity_oauth_refresh_failed", err
		}
		return connection.AccessToken, connection.AccountID, connection.Connection.MaskedAccessToken, uuid.Nil, "", "", nil
	}
	if isClaudeCodeSite(candidate.Site.SiteType) {
		if h.oauth == nil {
			return "", "", "", uuid.Nil, "", "claude_code_oauth_unavailable", fmt.Errorf("oauth service is not available")
		}
		connection, err := h.oauth.EnsureClaudeCodeConnectionFresh(ctx, candidate.Site.ID)
		if err != nil {
			return "", "", "", uuid.Nil, "", "claude_code_oauth_refresh_failed", err
		}
		return connection.AccessToken, connection.AccountID, connection.Connection.MaskedAccessToken, uuid.Nil, "", "", nil
	}
	if isGrokSite(candidate.Site.SiteType) {
		if h.oauth == nil {
			return "", "", "", uuid.Nil, "", "grok_oauth_unavailable", fmt.Errorf("oauth service is not available")
		}
		credential, err := h.siteModelTestStoredCredential(ctx, candidate, requestedCredentialID)
		if err != nil {
			return "", "", "", uuid.Nil, "", "upstream_credential_unavailable", err
		}
		accessToken, err := h.oauth.EnsureGrokAccessToken(ctx, credential.Credential.ID)
		if err != nil {
			return "", "", "", credential.Credential.ID, "", "grok_oauth_refresh_failed", err
		}
		return accessToken, "", credential.Credential.MaskedSecret, credential.Credential.ID, credential.GroupName.String, "", nil
	}
	credential, err := h.siteModelTestStoredCredential(ctx, candidate, requestedCredentialID)
	if err != nil {
		return "", "", "", uuid.Nil, "", "upstream_credential_unavailable", err
	}
	upstreamKey, err := h.credentials.Decrypt(credential.Credential.EncryptedSecret)
	if err != nil {
		return "", "", "", credential.Credential.ID, "", "upstream_credential_decrypt_failed", err
	}
	return upstreamKey, gatewayCredentialMetaString(credential.Credential, "account_id"), credential.Credential.MaskedSecret, credential.Credential.ID, credential.GroupName.String, "", nil
}

func (h Handler) siteModelTestStoredCredential(ctx context.Context, candidate routeengine.Candidate, requestedCredentialID uuid.UUID) (store.GatewayCredential, error) {
	repo := store.NewSiteCredentialRepository(h.db.DB())
	switch sitepkg.CredentialTypeForSiteType(candidate.Site.SiteType) {
	case "system_token":
		return h.siteModelTestGatewayCredential(ctx, candidate, requestedCredentialID)
	case "xlyra":
		if credential, err := h.siteModelTestGatewayCredential(ctx, candidate, requestedCredentialID); err == nil {
			return credential, nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return store.GatewayCredential{}, err
		}
		credential, err := repo.GetBySiteAndType(ctx, candidate.Site.ID, "xlyra_access_token")
		if err != nil {
			return store.GatewayCredential{}, err
		}
		return store.GatewayCredential{Credential: credential}, nil
	default:
		return h.siteModelTestGatewayCredential(ctx, candidate, requestedCredentialID)
	}
}

func (h Handler) siteModelTestGatewayCredential(ctx context.Context, candidate routeengine.Candidate, requestedCredentialID uuid.UUID) (store.GatewayCredential, error) {
	if requestedCredentialID != uuid.Nil {
		credentials, err := store.NewGatewayRepository(h.db.DB()).ListCredentialsForSiteModel(ctx, candidate.Site.ID, candidate.Model.SiteModelID)
		if err != nil {
			return store.GatewayCredential{}, err
		}
		return selectSiteModelTestGatewayCredential(credentials, requestedCredentialID)
	}
	credentials, err := h.gatewayCredentials(ctx, candidate)
	if err != nil {
		return store.GatewayCredential{}, err
	}
	return selectSiteModelTestGatewayCredential(credentials, requestedCredentialID)
}

func firstSiteModelTestGatewayCredential(credentials []store.GatewayCredential) (store.GatewayCredential, error) {
	return selectSiteModelTestGatewayCredential(credentials, uuid.Nil)
}

func selectSiteModelTestGatewayCredential(credentials []store.GatewayCredential, requestedCredentialID uuid.UUID) (store.GatewayCredential, error) {
	if len(credentials) == 0 {
		return store.GatewayCredential{}, fmt.Errorf("get site test credential: %w", gorm.ErrRecordNotFound)
	}
	if requestedCredentialID != uuid.Nil {
		for _, credential := range credentials {
			if credential.Credential.ID == requestedCredentialID {
				return credential, nil
			}
		}
		return store.GatewayCredential{}, fmt.Errorf("site credential is not available for this model: %w", gorm.ErrRecordNotFound)
	}
	return credentials[0], nil
}

func (h Handler) siteModelTestProtocolAdapter(ctx context.Context, request gatewayRequest, candidate routeengine.Candidate, protocol string) (gatewayProtocolAdapter, error) {
	if siteModelTestUsesSiteProtocolResolver(candidate.Site.SiteType) {
		return (openAIProtocolResolver{db: h.db}).Resolve(ctx, request, candidate)
	}
	switch protocol {
	case "", siteModelTestProtocolAuto:
		return (openAIProtocolResolver{db: h.db}).Resolve(ctx, request, candidate)
	case siteModelTestProtocolChatCompletions:
		return newOpenAIChatProtocolAdapter(request, candidate), nil
	case siteModelTestProtocolResponses:
		return newOpenAIResponsesProtocolAdapter(request), nil
	case siteModelTestProtocolMessages:
		return anthropicMessagesProtocolForCandidate(request, candidate), nil
	default:
		return nil, siteModelTestError(http.StatusBadRequest, "invalid_protocol", "protocol must be auto, chat_completions, responses, or messages")
	}
}

func siteModelTestUsesSiteProtocolResolver(siteType string) bool {
	return isCodexSite(siteType) || isAntigravitySite(siteType) || isGoogleSite(siteType) || isGrokSite(siteType)
}

func applyOpenAISiteModelTestUserAgent(req *http.Request, protocol gatewayProtocolAdapter) {
	if req == nil || strings.TrimSpace(req.Header.Get("User-Agent")) != "" {
		return
	}
	switch protocol.(type) {
	case openAIChatProtocolAdapter, openAIResponsesProtocolAdapter:
		req.Header.Set("User-Agent", openAISiteModelTestUA)
	}
}

func (h Handler) handleSiteModelTestStreamResponse(
	ctx context.Context,
	requestID string,
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
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		result.body = responseBody
		result.errorType = "upstream_http_error"
		result.errorMessage = fmt.Sprintf("upstream returned HTTP %d", resp.StatusCode)
		result = classifyGatewayUpstreamErrorWithTimeZone(candidate, result, responseBody, time.Now(), h.timeZone)
		result.failureResponse = diagnosticFailureResponse(responseBody)
		result.requestLogID = h.recordAttempt(ctx, requestID, uuid.Nil, canonicalModelID, candidate, result, upstreamResponseForMetadata(false, responseBody))
		return result
	}

	writer := &discardResponseWriter{}
	capture, responseStarted, proxyErr := protocol.ProxyStream(ctx, writer, resp, startedAt, candidate)
	result.responseStarted = responseStarted
	result.firstByteLatencyMS = capture.firstByteLatency
	result.streamCompleted = capture.streamCompleted
	result.streamReceivedDone = capture.sawDone
	result.streamEndReason = capture.endReason
	result.promptTokens = capture.usage.PromptTokens
	result.completionTokens = capture.usage.CompletionTokens
	result.cachedPromptTokens = capture.usage.CachedPromptTokens
	result.cacheWriteTokens = capture.usage.CacheWriteTokens
	result.imageCount = capture.usage.ImageCount
	result.estimatedCost = estimateCost(capture.usage, result.pricing)
	result = applyEstimatedCostBillingAdjustment(result)
	result.cacheWriteCost = cacheWriteCostForAttempt(capture.usage, result)
	result.latencyMS = time.Since(startedAt).Milliseconds()
	if proxyErr != nil {
		if streamSucceeded(capture) {
			result.success = true
		} else {
			result.errorType = "upstream_stream_failed"
			if errors.Is(proxyErr, context.Canceled) || errors.Is(proxyErr, context.DeadlineExceeded) {
				result.errorType = "downstream_client_cancelled"
			}
			result.errorMessage = proxyErr.Error()
		}
		result.requestLogID = h.recordAttempt(ctx, requestID, uuid.Nil, canonicalModelID, candidate, result, streamMetadataEnvelope(capture))
		return result
	}

	if !responseStarted {
		if capture.endReason == "upstream_stream_error" {
			result.statusCode = http.StatusBadGateway
			result.errorType = "upstream_stream_error"
			result.errorMessage = streamErrorMessageFromEndReason(capture.endReason)
			result.requestLogID = h.recordAttempt(ctx, requestID, uuid.Nil, canonicalModelID, candidate, result, streamMetadataEnvelope(capture))
			return result
		}
		result.errorType = "upstream_stream_empty"
		result.errorMessage = "upstream stream returned without any data"
		result.requestLogID = h.recordAttempt(ctx, requestID, uuid.Nil, canonicalModelID, candidate, result, streamMetadataEnvelope(capture))
		return result
	}

	if streamSucceeded(capture) {
		result.success = true
	} else {
		result.errorType = streamErrorTypeFromEndReason(capture.endReason)
		result.errorMessage = streamErrorMessageFromEndReason(capture.endReason)
	}
	result.requestLogID = h.recordAttempt(ctx, requestID, uuid.Nil, canonicalModelID, candidate, result, streamMetadataEnvelope(capture))
	return result
}

func siteModelTestDownstreamPath(endpointTypes []string) (string, error) {
	if len(endpointTypes) == 0 {
		return gatewayEndpointChatCompletions, nil
	}
	supportsText := false
	for _, endpointType := range endpointTypes {
		switch normalizeEndpointType(endpointType) {
		case upstreamEndpointTypeAnthropicMessages:
			return gatewayEndpointMessages, nil
		case upstreamEndpointTypeOpenAIResponse:
			supportsText = true
		case upstreamEndpointTypeOpenAI, upstreamEndpointTypeGoogleGemini:
			supportsText = true
		}
	}
	for _, endpointType := range endpointTypes {
		if normalizeEndpointType(endpointType) == upstreamEndpointTypeOpenAIResponse {
			return gatewayEndpointResponses, nil
		}
	}
	if supportsText {
		return gatewayEndpointChatCompletions, nil
	}
	return "", siteModelTestError(http.StatusBadRequest, "image_model_not_supported", "image-only models are not supported by the minimal text test")
}

func normalizeSiteModelTestProtocol(value string) (string, error) {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "", siteModelTestProtocolAuto:
		return siteModelTestProtocolAuto, nil
	case siteModelTestProtocolChatCompletions, "chat-completions", "/chat/completions", gatewayEndpointChatCompletions:
		return siteModelTestProtocolChatCompletions, nil
	case siteModelTestProtocolResponses, "openai_responses", "openai-responses", "/responses", gatewayEndpointResponses:
		return siteModelTestProtocolResponses, nil
	case siteModelTestProtocolMessages, "anthropic_messages", "anthropic-messages", "/messages", gatewayEndpointMessages:
		return siteModelTestProtocolMessages, nil
	default:
		return "", siteModelTestError(http.StatusBadRequest, "invalid_protocol", "protocol must be auto, chat_completions, responses, or messages")
	}
}

func siteModelTestDownstreamPathForProtocol(endpointTypes []string, protocol string) (string, error) {
	switch protocol {
	case "", siteModelTestProtocolAuto:
		return siteModelTestDownstreamPath(endpointTypes)
	case siteModelTestProtocolChatCompletions:
		return gatewayEndpointChatCompletions, nil
	case siteModelTestProtocolResponses:
		return gatewayEndpointResponses, nil
	case siteModelTestProtocolMessages:
		return gatewayEndpointMessages, nil
	default:
		return "", siteModelTestError(http.StatusBadRequest, "invalid_protocol", "protocol must be auto, chat_completions, responses, or messages")
	}
}

func siteModelTestGatewayRequest(path string, model string, prompt string, stream bool) (gatewayRequest, error) {
	payload := map[string]any{}
	switch path {
	case gatewayEndpointMessages:
		payload = map[string]any{
			"model":      model,
			"messages":   []any{map[string]any{"role": "user", "content": prompt}},
			"max_tokens": 16,
			"stream":     stream,
		}
		canonical, err := canonicalRequestFromAnthropicMessagesPayload(payload, model)
		if err != nil {
			return gatewayRequest{}, err
		}
		return gatewayRequest{DownstreamPath: path, RequestedModel: model, Stream: stream, Payload: payload, Canonical: &canonical, Diagnostic: true}, nil
	case gatewayEndpointResponses:
		payload = map[string]any{
			"model":             model,
			"input":             prompt,
			"max_output_tokens": 16,
			"stream":            stream,
		}
		if stream {
			payload["stream_options"] = map[string]any{"include_usage": true}
		}
		canonical, err := canonicalRequestFromOpenAIResponsesPayload(payload, model)
		if err != nil {
			return gatewayRequest{}, err
		}
		return gatewayRequest{DownstreamPath: path, RequestedModel: model, Stream: stream, Payload: payload, Canonical: &canonical, Diagnostic: true}, nil
	default:
		payload = map[string]any{
			"model":      model,
			"messages":   []any{map[string]any{"role": "user", "content": prompt}},
			"max_tokens": 16,
			"stream":     stream,
		}
		canonical, err := canonicalRequestFromOpenAIChatPayload(payload, model)
		if err != nil {
			return gatewayRequest{}, err
		}
		return gatewayRequest{DownstreamPath: gatewayEndpointChatCompletions, RequestedModel: model, Stream: stream, Payload: payload, Canonical: &canonical, Diagnostic: true}, nil
	}
}

func siteModelTestResponsePreview(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var root map[string]any
	if err := json.Unmarshal(body, &root); err == nil {
		if text := strings.TrimSpace(anyString(root["output_text"])); text != "" {
			return truncateSiteModelTestPreview(text)
		}
		if text := previewFromOpenAIChat(root); text != "" {
			return truncateSiteModelTestPreview(text)
		}
		if text := previewFromResponses(root); text != "" {
			return truncateSiteModelTestPreview(text)
		}
		if text := previewFromAnthropicMessages(root); text != "" {
			return truncateSiteModelTestPreview(text)
		}
		if compact, err := json.Marshal(root); err == nil {
			return truncateSiteModelTestPreview(string(compact))
		}
	}
	return truncateSiteModelTestPreview(string(body))
}

func previewFromOpenAIChat(root map[string]any) string {
	choices, _ := root["choices"].([]any)
	for _, rawChoice := range choices {
		choice, _ := rawChoice.(map[string]any)
		message, _ := choice["message"].(map[string]any)
		if text := extractMessageText(message["content"]); text != "" {
			return text
		}
	}
	return ""
}

func previewFromResponses(root map[string]any) string {
	output, _ := root["output"].([]any)
	for _, rawItem := range output {
		item, _ := rawItem.(map[string]any)
		content, _ := item["content"].([]any)
		for _, rawPart := range content {
			part, _ := rawPart.(map[string]any)
			if text := strings.TrimSpace(firstNonEmptyGatewayString(anyString(part["text"]), anyString(part["output_text"]))); text != "" {
				return text
			}
		}
	}
	return ""
}

func previewFromAnthropicMessages(root map[string]any) string {
	content, _ := root["content"].([]any)
	parts := make([]string, 0, len(content))
	for _, rawPart := range content {
		part, _ := rawPart.(map[string]any)
		if text := strings.TrimSpace(anyString(part["text"])); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func truncateSiteModelTestPreview(text string) string {
	text = strings.TrimSpace(text)
	if len(text) <= 500 {
		return text
	}
	return text[:500]
}

func siteModelTestError(status int, code string, message string) *SiteModelTestError {
	return &SiteModelTestError{StatusCode: status, Code: code, Message: message}
}

func emptyStringPointer(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

type discardResponseWriter struct {
	header http.Header
}

func (w *discardResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = http.Header{}
	}
	return w.header
}

func (w *discardResponseWriter) Write(data []byte) (int, error) {
	return len(data), nil
}

func (w *discardResponseWriter) WriteHeader(_ int) {}
