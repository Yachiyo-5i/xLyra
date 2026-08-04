package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	routeengine "xlyra/server/internal/router"
)

type gatewayProtocolStub struct{}

func (gatewayProtocolStub) ProtocolName() string {
	return "stub"
}

func (gatewayProtocolStub) BuildUpstreamPayload(gatewayRequest, routeengine.Candidate) (map[string]any, error) {
	return nil, nil
}

func (gatewayProtocolStub) UpstreamPath(string) string {
	return ""
}

func (gatewayProtocolStub) TransformBufferedResponse(int, http.Header, []byte) (gatewayBufferedResponse, error) {
	return gatewayBufferedResponse{}, nil
}

func (gatewayProtocolStub) ProxyStream(context.Context, http.ResponseWriter, *http.Response, time.Time, routeengine.Candidate) (streamCaptureState, bool, error) {
	return streamCaptureState{}, false, nil
}

func assertMissingBodyStreamCapture(t *testing.T, name string, capture streamCaptureState, started bool, err error) {
	t.Helper()

	if err == nil {
		t.Fatalf("%s with missing body returned nil error", name)
	}
	if started {
		t.Fatalf("%s missing body should not start the downstream response", name)
	}
	if capture.endReason != "upstream_stream_missing_body" {
		t.Fatalf("%s endReason = %q, want upstream_stream_missing_body", name, capture.endReason)
	}
}

func assertCancelledStreamCapture(t *testing.T, name string, rec *httptest.ResponseRecorder, capture streamCaptureState, started bool, err error) {
	t.Helper()

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("%s error = %v, want context.Canceled", name, err)
	}
	if started {
		t.Fatalf("%s cancelled context should not start the downstream response", name)
	}
	if capture.endReason != "downstream_client_cancelled" {
		t.Fatalf("%s endReason = %q, want downstream_client_cancelled", name, capture.endReason)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("%s cancelled stream wrote %q, want empty body", name, rec.Body.String())
	}
}

func assertEndpointFailure(t *testing.T, name string, failure *chatFailure, wantCode string, wantStage string) {
	t.Helper()

	if failure == nil {
		t.Fatalf("expected %s failure", name)
	}
	if failure.status != http.StatusBadRequest || failure.code != wantCode || failure.stage != wantStage {
		t.Fatalf("%s failure = %+v, want status=%d code=%q stage=%q", name, failure, http.StatusBadRequest, wantCode, wantStage)
	}
}

func decodeEndpointRequest(t *testing.T, adapter gatewayEndpointAdapter, body string) (gatewayRequest, *chatFailure) {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, adapter.DownstreamPath(), strings.NewReader(body))
	return adapter.DecodeRequest(req)
}

func requireDecodedEndpointRequest(t *testing.T, adapter gatewayEndpointAdapter, body string, wantPath string, wantModel string) gatewayRequest {
	t.Helper()

	request, failure := decodeEndpointRequest(t, adapter, body)
	if failure != nil {
		t.Fatalf("DecodeRequest returned failure: %+v", failure)
	}
	if request.DownstreamPath != wantPath {
		t.Fatalf("DownstreamPath = %q, want %q", request.DownstreamPath, wantPath)
	}
	if request.RequestedModel != wantModel {
		t.Fatalf("RequestedModel = %q, want %q", request.RequestedModel, wantModel)
	}
	return request
}

func assertEndpointDecodeFailure(t *testing.T, name string, adapter gatewayEndpointAdapter, body string, wantCode string, wantStage string) {
	t.Helper()

	_, failure := decodeEndpointRequest(t, adapter, body)
	assertEndpointFailure(t, name, failure, wantCode, wantStage)
}

func gatewayRequestWithID(method string, target string, body string, requestID string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if requestID == "" {
		return req
	}
	return req.WithContext(context.WithValue(req.Context(), middleware.RequestIDKey, requestID))
}

func gatewayHeaderRequest(t *testing.T, method string, target string) *http.Request {
	t.Helper()

	req, err := http.NewRequest(method, target, nil)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	return req
}

func assertGatewayHeader(t *testing.T, req *http.Request, key string, want string) {
	t.Helper()

	if got := req.Header.Get(key); got != want {
		t.Fatalf("%s = %q, want %q", key, got, want)
	}
}

func assertNoGatewayHeader(t *testing.T, req *http.Request, key string) {
	t.Helper()

	if got := req.Header.Get(key); got != "" {
		t.Fatalf("%s = %q, want empty", key, got)
	}
}

func assertGatewayErrorEnvelope(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantCode string, wantRequestID string) {
	t.Helper()

	if rec.Code != wantStatus {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, wantStatus, rec.Body.String())
	}
	var body struct {
		Error struct {
			Code      string `json:"code"`
			RequestID string `json:"request_id"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Error.Code != wantCode || body.Error.RequestID != wantRequestID {
		t.Fatalf("unexpected error envelope: %#v, want code=%q request_id=%q", body.Error, wantCode, wantRequestID)
	}
}

type gatewayBodyProtocolStub struct {
	gatewayProtocolStub
	body        []byte
	contentType string
	err         error
}

func (p gatewayBodyProtocolStub) BuildUpstreamBody(gatewayRequest, map[string]any) ([]byte, string, error) {
	return p.body, p.contentType, p.err
}

func TestBuildUpstreamRequestBodyUsesJSONFallback(t *testing.T) {
	t.Parallel()

	body, contentType, err := buildUpstreamRequestBody(gatewayProtocolStub{}, gatewayRequest{}, map[string]any{
		"model": "gpt-test",
		"input": "hello",
	})
	if err != nil {
		t.Fatalf("buildUpstreamRequestBody returned error: %v", err)
	}
	if contentType != "application/json" {
		t.Fatalf("contentType = %q, want application/json", contentType)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("body was not JSON: %v", err)
	}
	if payload["model"] != "gpt-test" || payload["input"] != "hello" {
		t.Fatalf("payload = %#v, want model/input", payload)
	}
}

func TestBuildUpstreamRequestBodyUsesBodyAdapter(t *testing.T) {
	t.Parallel()

	body, contentType, err := buildUpstreamRequestBody(gatewayBodyProtocolStub{
		body:        []byte("custom body"),
		contentType: "text/plain",
	}, gatewayRequest{}, map[string]any{"ignored": true})
	if err != nil {
		t.Fatalf("buildUpstreamRequestBody returned error: %v", err)
	}
	if string(body) != "custom body" {
		t.Fatalf("body = %q, want custom body", string(body))
	}
	if contentType != "text/plain" {
		t.Fatalf("contentType = %q, want text/plain", contentType)
	}
}

func TestBuildUpstreamRequestBodyDefaultsBlankAdapterContentType(t *testing.T) {
	t.Parallel()

	body, contentType, err := buildUpstreamRequestBody(gatewayBodyProtocolStub{
		body: []byte("custom body"),
	}, gatewayRequest{}, nil)
	if err != nil {
		t.Fatalf("buildUpstreamRequestBody returned error: %v", err)
	}
	if string(body) != "custom body" {
		t.Fatalf("body = %q, want custom body", string(body))
	}
	if contentType != "application/json" {
		t.Fatalf("contentType = %q, want application/json", contentType)
	}
}

func TestBuildUpstreamRequestBodyReturnsAdapterError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("adapter failed")
	_, _, err := buildUpstreamRequestBody(gatewayBodyProtocolStub{err: wantErr}, gatewayRequest{}, nil)
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
}

func TestWriteUpstreamBody(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		contentType string
		wantType    string
	}{
		{
			name:        "defaults blank content type",
			contentType: "",
			wantType:    "application/json",
		},
		{
			name:        "keeps explicit content type",
			contentType: "text/event-stream",
			wantType:    "text/event-stream",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			recorder := httptest.NewRecorder()
			writeUpstreamBody(recorder, http.StatusAccepted, tt.contentType, []byte("ok"))

			if recorder.Code != http.StatusAccepted {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusAccepted)
			}
			if got := recorder.Header().Get("Content-Type"); got != tt.wantType {
				t.Fatalf("Content-Type = %q, want %q", got, tt.wantType)
			}
			if got := recorder.Body.String(); got != "ok" {
				t.Fatalf("body = %q, want ok", got)
			}
		})
	}
}

func TestDiagnosticFailureResponse(t *testing.T) {
	t.Parallel()

	if got := diagnosticFailureResponse(nil); got != nil {
		t.Fatalf("diagnosticFailureResponse(nil) = %#v, want nil", got)
	}
	if got := diagnosticFailureResponse([]byte(" \n\t ")); got != nil {
		t.Fatalf("diagnosticFailureResponse(blank) = %#v, want nil", got)
	}
	if got := diagnosticFailureResponse([]byte(" \n upstream failed \t ")); got != "upstream failed" {
		t.Fatalf("diagnosticFailureResponse(trimmed) = %#v, want upstream failed", got)
	}

	got, ok := diagnosticFailureResponse([]byte(strings.Repeat("x", 4100))).(string)
	if !ok {
		t.Fatalf("diagnosticFailureResponse(long) = %T, want string", got)
	}
	if len(got) != 4096 {
		t.Fatalf("diagnosticFailureResponse(long) length = %d, want 4096", len(got))
	}
}
