package gateway

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	routeengine "xlyra/server/internal/router"
	"xlyra/server/internal/store"
)

func TestRecordGatewayRequestRejectsNilStore(t *testing.T) {
	t.Parallel()

	_, _, err := (Recorder{}).RecordGatewayRequest(context.Background(), GatewayRequestRecord{
		RequestID: "nil-store",
	})
	if err == nil || !strings.Contains(err.Error(), "store is not initialized") {
		t.Fatalf("RecordGatewayRequest error = %v, want store is not initialized", err)
	}
}

func TestRecordRequestFailureReturnsWhenStoreMissing(t *testing.T) {
	t.Parallel()

	Handler{logger: gatewayDiscardLogger()}.recordRequestFailure(
		context.Background(),
		"gateway-failure",
		uuid.New(),
		time.Now(),
		http.StatusBadGateway,
		"upstream_error",
		"upstream failed",
		"gpt-diagnostic",
		false,
		"diagnostic",
		gatewayEndpointChatCompletions,
	)
}

func TestSiteModelTestCredentialRejectsOAuthSitesWithoutService(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		siteType  string
		wantError string
	}{
		{name: "codex", siteType: " codex ", wantError: "codex_oauth_unavailable"},
		{name: "antigravity", siteType: "ANTIGRAVITY", wantError: "antigravity_oauth_unavailable"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, _, _, credentialID, _, errType, err := (Handler{}).siteModelTestCredential(context.Background(), routeengine.Candidate{
				Site: routeengine.CandidateSite{
					ID:       uuid.New(),
					SiteType: tt.siteType,
				},
			}, uuid.Nil)
			if err == nil {
				t.Fatal("siteModelTestCredential returned nil error")
			}
			if errType != tt.wantError {
				t.Fatalf("error type = %q, want %q", errType, tt.wantError)
			}
			if credentialID != uuid.Nil {
				t.Fatalf("credential ID = %s, want nil", credentialID)
			}
		})
	}
}

func TestSelectSiteModelTestGatewayCredentialUsesExactRequestedKey(t *testing.T) {
	t.Parallel()

	firstID := uuid.New()
	requestedID := uuid.New()
	credentials := []store.GatewayCredential{
		{Credential: store.SiteCredential{ID: firstID}},
		{Credential: store.SiteCredential{ID: requestedID}},
	}
	selected, err := selectSiteModelTestGatewayCredential(credentials, requestedID)
	if err != nil || selected.Credential.ID != requestedID {
		t.Fatalf("selected credential = %#v error = %v", selected, err)
	}
	if _, err := selectSiteModelTestGatewayCredential(credentials, uuid.New()); err == nil {
		t.Fatal("missing requested credential should be rejected")
	}
}

func TestHandleSiteModelTestStreamResponseClassifiesHTTPError(t *testing.T) {
	t.Parallel()

	body := `{"error":{"message":"upstream rejected"}}`
	resp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
	result := (Handler{logger: gatewayDiscardLogger()}).handleSiteModelTestStreamResponse(
		context.Background(),
		"stream-http",
		uuid.New(),
		routeengine.Candidate{
			Site:  routeengine.CandidateSite{ID: uuid.New(), SiteType: "openai"},
			Model: routeengine.CandidateModel{SiteModelID: uuid.New()},
		},
		siteModelTestProtocolAdapter{},
		resp,
		gatewayAttemptResult{stream: true, currency: "USD", downstreamPath: gatewayEndpointChatCompletions},
		time.Now(),
	)

	if result.statusCode != http.StatusTooManyRequests || result.upstreamStatusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %d/%d, want 429/429", result.statusCode, result.upstreamStatusCode)
	}
	if result.errorType != "upstream_credential_limited" {
		t.Fatalf("errorType = %q, want upstream_credential_limited", result.errorType)
	}
	if result.errorMessage != "upstream rejected" {
		t.Fatalf("errorMessage = %q, want upstream payload message", result.errorMessage)
	}
	if string(result.body) != body {
		t.Fatalf("body = %q, want %q", string(result.body), body)
	}
	if result.failureResponse == nil {
		t.Fatal("expected diagnostic failure response")
	}
}

func TestHandleSiteModelTestStreamResponseMapsCancelledProxyError(t *testing.T) {
	t.Parallel()

	result := (Handler{logger: gatewayDiscardLogger()}).handleSiteModelTestStreamResponse(
		context.Background(),
		"stream-cancelled",
		uuid.New(),
		routeengine.Candidate{
			Site:  routeengine.CandidateSite{ID: uuid.New(), SiteType: "openai"},
			Model: routeengine.CandidateModel{SiteModelID: uuid.New()},
		},
		siteModelTestProtocolAdapter{proxyErr: context.Canceled},
		&http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader("data: {}\n\n")),
		},
		gatewayAttemptResult{stream: true, currency: "USD", downstreamPath: gatewayEndpointChatCompletions},
		time.Now(),
	)

	if result.errorType != "downstream_client_cancelled" {
		t.Fatalf("errorType = %q, want downstream_client_cancelled", result.errorType)
	}
	if !errors.Is(context.Canceled, context.Canceled) || result.errorMessage != context.Canceled.Error() {
		t.Fatalf("errorMessage = %q, want %q", result.errorMessage, context.Canceled.Error())
	}
	if result.responseStarted {
		t.Fatal("cancelled proxy should not mark response started")
	}
}

func TestDiscardResponseWriterWriteHeaderIsNoop(t *testing.T) {
	t.Parallel()

	writer := &discardResponseWriter{}
	writer.WriteHeader(http.StatusCreated)
	writer.Header().Set("X-Test", "kept")
	writer.WriteHeader(http.StatusInternalServerError)

	if got := writer.Header().Get("X-Test"); got != "kept" {
		t.Fatalf("header = %q, want kept", got)
	}
}

type siteModelTestProtocolAdapter struct {
	proxyErr error
}

func (a siteModelTestProtocolAdapter) ProtocolName() string {
	return "site_model_test"
}

func (a siteModelTestProtocolAdapter) BuildUpstreamPayload(gatewayRequest, routeengine.Candidate) (map[string]any, error) {
	return map[string]any{}, nil
}

func (a siteModelTestProtocolAdapter) UpstreamPath(baseURL string) string {
	return strings.TrimRight(baseURL, "/") + "/site-model-test"
}

func (a siteModelTestProtocolAdapter) TransformBufferedResponse(int, http.Header, []byte) (gatewayBufferedResponse, error) {
	return gatewayBufferedResponse{}, nil
}

func (a siteModelTestProtocolAdapter) ProxyStream(context.Context, http.ResponseWriter, *http.Response, time.Time, routeengine.Candidate) (streamCaptureState, bool, error) {
	return streamCaptureState{endReason: "downstream_client_cancelled"}, false, a.proxyErr
}
