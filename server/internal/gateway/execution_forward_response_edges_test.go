package gateway

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/credential"
	routeengine "xlyra/server/internal/router"
	"xlyra/server/internal/store"
)

func TestForwardGatewayRequestReturnsCredentialLookupFailure(t *testing.T) {
	t.Parallel()

	db := gatewayGormWithQueryCallback(t, func(tx *gorm.DB) {
		tx.AddError(gorm.ErrInvalidDB)
	})

	handler := Handler{
		logger:   gatewayDiscardLogger(),
		db:       gatewayStoreWithGorm(t, db),
		recorder: Recorder{},
	}
	result := handler.forwardGatewayRequest(
		context.Background(),
		httptest.NewRecorder(),
		"req-forward",
		2,
		uuid.New(),
		uuid.New(),
		routeengine.Candidate{
			Site:  routeengine.CandidateSite{ID: uuid.New()},
			Model: routeengine.CandidateModel{SiteModelID: uuid.New()},
		},
		gatewayRequest{DownstreamPath: gatewayEndpointResponses, Stream: true, Diagnostic: true},
		nil,
		&testGatewayProtocol{name: "test_protocol"},
	)

	if result.statusCode != http.StatusBadGateway {
		t.Fatalf("statusCode = %d, want %d", result.statusCode, http.StatusBadGateway)
	}
	if result.errorType != "upstream_credential_unavailable" {
		t.Fatalf("errorType = %q, want upstream_credential_unavailable", result.errorType)
	}
	if !result.stream || result.downstreamPath != gatewayEndpointResponses || result.upstreamProtocol != "test_protocol" || !result.diagnostic {
		t.Fatalf("result metadata = %#v", result)
	}
	if result.requestLogID != uuid.Nil {
		t.Fatalf("requestLogID = %s, want nil when recorder store is unavailable", result.requestLogID)
	}
}

func TestForwardGatewayRequestReturnsPayloadEncodeFailureAfterCredentialSelection(t *testing.T) {
	t.Parallel()

	siteID := uuid.New()
	siteModelID := uuid.New()
	credentialID := uuid.New()
	credentialService := credential.NewService("test-master")
	encrypted, masked, err := credentialService.Encrypt("test-secret")
	if err != nil {
		t.Fatalf("encrypt credential: %v", err)
	}
	db := gatewayOfflineGorm(t)
	installGatewayCredentialQueries(t, db, siteID, siteModelID, store.SiteCredential{
		ID:              credentialID,
		SiteID:          siteID,
		CredentialType:  "api_key",
		EncryptedSecret: encrypted,
		MaskedSecret:    masked,
		Meta:            store.JSON(`{"enabled":true}`),
		CreatedAt:       time.Unix(10, 0),
	})

	wantErr := errors.New("payload encode failed")
	handler := Handler{
		logger:      gatewayDiscardLogger(),
		db:          gatewayStoreWithGorm(t, db),
		credentials: credentialService,
		recorder:    Recorder{},
	}
	result := handler.forwardGatewayRequest(
		context.Background(),
		httptest.NewRecorder(),
		"req-forward",
		1,
		uuid.New(),
		uuid.New(),
		routeengine.Candidate{
			Site:  routeengine.CandidateSite{ID: siteID, BaseURL: "https://upstream.example.test"},
			Model: routeengine.CandidateModel{SiteModelID: siteModelID, UpstreamName: "upstream-model"},
		},
		gatewayRequest{DownstreamPath: gatewayEndpointChatCompletions},
		nil,
		&testGatewayProtocol{name: "test_protocol", payloadErr: wantErr},
	)

	if result.statusCode != http.StatusBadRequest {
		t.Fatalf("statusCode = %d, want %d", result.statusCode, http.StatusBadRequest)
	}
	if result.errorType != "request_encode_failed" || !strings.Contains(result.errorMessage, wantErr.Error()) {
		t.Fatalf("encode failure = type %q message %q", result.errorType, result.errorMessage)
	}
	if result.contentType != "application/json" || !strings.Contains(string(result.body), "request_encode_failed") || !strings.Contains(string(result.body), wantErr.Error()) {
		t.Fatalf("encode failure body = content-type %q body %q", result.contentType, string(result.body))
	}
	if result.credentialID != credentialID || result.credentialMasked != masked || result.credentialAttempt != 1 || result.credentialTotal != 1 {
		t.Fatalf("credential metadata = %#v", result)
	}
}

func TestReadResponseBodyWithLimitRejectsOversizeBody(t *testing.T) {
	t.Parallel()

	body, err := readResponseBodyWithLimit(strings.NewReader("12345"), 4)
	if err == nil || body != nil || !strings.Contains(err.Error(), "4 byte limit") {
		t.Fatalf("readResponseBodyWithLimit = body %q err %v, want explicit oversize error", body, err)
	}
}

func TestHandleStreamResponseRecordsNon2xxBodyWithoutProxying(t *testing.T) {
	t.Parallel()

	body := `{"error":{"message":"limited"}}`
	result := Handler{logger: gatewayDiscardLogger(), exposeRouteSite: true}.handleStreamResponse(
		context.Background(),
		httptest.NewRecorder(),
		"req-forward",
		uuid.New(),
		uuid.New(),
		routeengine.Candidate{Site: routeengine.CandidateSite{SiteType: "openai"}},
		&testGatewayProtocol{},
		&http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		},
		gatewayAttemptResult{attempt: 1, downstreamPath: gatewayEndpointResponses, stream: true},
		time.Now(),
	)

	if result.statusCode != http.StatusTooManyRequests || result.upstreamStatusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %d upstream = %d, want 429/429", result.statusCode, result.upstreamStatusCode)
	}
	if result.errorType != "upstream_credential_limited" || result.errorMessage != "limited" {
		t.Fatalf("stream error = %q %q", result.errorType, result.errorMessage)
	}
	if string(result.body) != body || result.failureResponse != body {
		t.Fatalf("body/failureResponse = %q %#v", string(result.body), result.failureResponse)
	}
}

func TestHandleStreamResponseEmptySuccessfulUpstreamMarksSemanticFailure(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	result := Handler{logger: gatewayDiscardLogger(), exposeRouteSite: true}.handleStreamResponse(
		context.Background(),
		recorder,
		"req-forward",
		uuid.New(),
		uuid.New(),
		routeengine.Candidate{Site: routeengine.CandidateSite{Name: "unused-site"}},
		&testGatewayProtocol{streamCapture: streamCaptureState{}, streamStarted: false},
		&http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader("")),
		},
		gatewayAttemptResult{attempt: 1, downstreamPath: gatewayEndpointResponses, stream: true},
		time.Now(),
	)

	if result.success {
		t.Fatal("empty stream should not be successful")
	}
	if result.errorType != "upstream_stream_empty" || result.errorMessage != "upstream stream returned without any data" {
		t.Fatalf("empty stream result = %#v", result)
	}
	if result.responseStarted {
		t.Fatal("empty stream should not mark the downstream response as started")
	}
	if got := recorder.Header().Get(RouteSiteHeader); got != "" {
		t.Fatalf("%s = %q, want empty for an unstarted stream", RouteSiteHeader, got)
	}
}

func TestHandleStreamResponseDownstreamCancelAfterHeadersUses499(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	result := Handler{logger: gatewayDiscardLogger(), exposeRouteSite: true}.handleStreamResponse(
		context.Background(),
		recorder,
		"req-forward",
		uuid.New(),
		uuid.New(),
		routeengine.Candidate{Site: routeengine.CandidateSite{Name: "tokenfree"}},
		&testGatewayProtocol{
			streamCapture: streamCaptureState{endReason: "downstream_client_cancelled", firstByteLatency: 7},
			streamStarted: true,
			streamErr:     context.Canceled,
		},
		&http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader("data: partial\n\n")),
		},
		gatewayAttemptResult{attempt: 1, downstreamPath: gatewayEndpointResponses, stream: true},
		time.Now(),
	)

	if result.statusCode != 499 || result.errorType != "downstream_client_cancelled" {
		t.Fatalf("cancel result = status %d type %q", result.statusCode, result.errorType)
	}
	if !result.responseStarted || result.firstByteLatencyMS != 7 {
		t.Fatalf("stream metadata = responseStarted %v firstByte %d", result.responseStarted, result.firstByteLatencyMS)
	}
	if got := recorder.Header().Get(RouteSiteHeader); got != "tokenfree" {
		t.Fatalf("%s = %q, want tokenfree", RouteSiteHeader, got)
	}
}

func TestHandleStreamResponseWebSocketStateFailureUses413AndUsage(t *testing.T) {
	t.Parallel()

	result := Handler{logger: gatewayDiscardLogger()}.handleStreamResponse(
		context.Background(),
		httptest.NewRecorder(),
		"req-forward",
		uuid.New(),
		uuid.New(),
		routeengine.Candidate{},
		&testGatewayProtocol{
			streamCapture: streamCaptureState{endReason: "downstream_stream_write_failed"},
			streamStarted: true,
			streamErr: &responsesWebSocketStateCommitError{
				message: "state too large",
				usage:   gatewayUsage{PromptTokens: 11, CompletionTokens: 7},
			},
		},
		&http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(""))},
		gatewayAttemptResult{attempt: 1, downstreamPath: gatewayEndpointResponses, stream: true},
		time.Now(),
	)

	if result.success || result.statusCode != http.StatusRequestEntityTooLarge || result.errorType != "websocket_state_too_large" {
		t.Fatalf("state failure result = %#v", result)
	}
	if result.promptTokens != 11 || result.completionTokens != 7 {
		t.Fatalf("usage = %d/%d", result.promptTokens, result.completionTokens)
	}
}

func TestHandleStreamResponseWebSocketWriteFailureUses499(t *testing.T) {
	t.Parallel()

	writeErr := &responsesWebSocketClientWriteError{err: errors.New("socket closed"), usage: gatewayUsage{PromptTokens: 13, CompletionTokens: 5}}
	result := Handler{logger: gatewayDiscardLogger()}.handleStreamResponse(
		context.Background(),
		httptest.NewRecorder(),
		"req-forward",
		uuid.New(),
		uuid.New(),
		routeengine.Candidate{},
		&testGatewayProtocol{streamCapture: streamCaptureState{endReason: "downstream_stream_write_failed"}, streamStarted: true, streamErr: writeErr},
		&http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(""))},
		gatewayAttemptResult{attempt: 1, downstreamPath: gatewayEndpointResponses, stream: true},
		time.Now(),
	)

	if result.success || result.statusCode != 499 || result.errorType != "downstream_client_cancelled" {
		t.Fatalf("write failure result = %#v", result)
	}
	if result.promptTokens != 13 || result.completionTokens != 5 {
		t.Fatalf("usage = %d/%d", result.promptTokens, result.completionTokens)
	}
}

func TestHandleStreamResponseGenericWriteFailureIsNotClientCancellation(t *testing.T) {
	t.Parallel()

	result := Handler{logger: gatewayDiscardLogger()}.handleStreamResponse(
		context.Background(),
		httptest.NewRecorder(),
		"req-forward",
		uuid.New(),
		uuid.New(),
		routeengine.Candidate{},
		&testGatewayProtocol{streamCapture: streamCaptureState{endReason: "downstream_stream_write_failed"}, streamStarted: true, streamErr: errors.New("transform failed")},
		&http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(""))},
		gatewayAttemptResult{attempt: 1, downstreamPath: gatewayEndpointResponses, stream: true},
		time.Now(),
	)

	if result.errorType != "upstream_stream_failed" {
		t.Fatalf("errorType = %q", result.errorType)
	}
}

func TestHandleBufferedResponseReturnsTransformFailure(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("transform broke")
	result := Handler{logger: gatewayDiscardLogger()}.handleBufferedResponse(
		context.Background(),
		"req-forward",
		uuid.New(),
		uuid.New(),
		routeengine.Candidate{},
		&testGatewayProtocol{bufferedErr: wantErr},
		&http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"error":"bad-shape"}`)),
		},
		gatewayAttemptResult{attempt: 1, downstreamPath: gatewayEndpointResponses, currency: "USD"},
		time.Now(),
	)

	if result.statusCode != http.StatusBadGateway {
		t.Fatalf("statusCode = %d, want %d", result.statusCode, http.StatusBadGateway)
	}
	if result.errorType != "upstream_response_transform_failed" || !strings.Contains(result.errorMessage, wantErr.Error()) {
		t.Fatalf("transform failure = type %q message %q", result.errorType, result.errorMessage)
	}
	if result.failureResponse != `{"error":"bad-shape"}` {
		t.Fatalf("failureResponse = %#v, want raw body string", result.failureResponse)
	}
}

type testGatewayProtocol struct {
	name          string
	payload       map[string]any
	payloadErr    error
	buffered      gatewayBufferedResponse
	bufferedErr   error
	streamCapture streamCaptureState
	streamStarted bool
	streamErr     error
}

func (p *testGatewayProtocol) ProtocolName() string {
	if p.name != "" {
		return p.name
	}
	return "test_protocol"
}

func (p *testGatewayProtocol) BuildUpstreamPayload(gatewayRequest, routeengine.Candidate) (map[string]any, error) {
	if p.payloadErr != nil {
		return nil, p.payloadErr
	}
	if p.payload != nil {
		return clonePayload(p.payload), nil
	}
	return map[string]any{"model": "test-upstream"}, nil
}

func (p *testGatewayProtocol) UpstreamPath(baseURL string) string {
	return strings.TrimRight(baseURL, "/") + "/test"
}

func (p *testGatewayProtocol) TransformBufferedResponse(int, http.Header, []byte) (gatewayBufferedResponse, error) {
	if p.bufferedErr != nil {
		return gatewayBufferedResponse{}, p.bufferedErr
	}
	if p.buffered.StatusCode == 0 {
		p.buffered.StatusCode = http.StatusOK
	}
	return p.buffered, nil
}

func (p *testGatewayProtocol) ProxyStream(context.Context, http.ResponseWriter, *http.Response, time.Time, routeengine.Candidate) (streamCaptureState, bool, error) {
	return p.streamCapture, p.streamStarted, p.streamErr
}

func installGatewayCredentialQueries(t *testing.T, db *gorm.DB, siteID uuid.UUID, siteModelID uuid.UUID, credential store.SiteCredential) {
	t.Helper()

	gatewayReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *[]store.SiteAPIKeyModel:
			*dest = []store.SiteAPIKeyModel{{
				SiteID:            siteID,
				SiteCredentialID:  credential.ID,
				SiteModelID:       uuid.NullUUID{UUID: siteModelID, Valid: true},
				UpstreamModelName: "test-upstream",
				Available:         true,
				Enabled:           true,
				UpdatedAt:         time.Unix(20, 0),
			}}
			tx.Statement.RowsAffected = 1
		case *[]store.SiteCredential:
			*dest = []store.SiteCredential{credential}
			tx.Statement.RowsAffected = 1
		case *[]store.SiteAPIKeyState:
			*dest = []store.SiteAPIKeyState{{
				SiteCredentialID: credential.ID,
				SiteID:           siteID,
				Enabled:          true,
				SyncStatus:       "synced",
			}}
			tx.Statement.RowsAffected = 1
		case *[]store.RouteCooldown:
			*dest = nil
			tx.Statement.RowsAffected = 0
		case *[]store.SiteModelPricing:
			*dest = nil
			tx.Statement.RowsAffected = 0
		default:
			tx.AddError(gorm.ErrRecordNotFound)
		}
	})
}
