package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func assertAuthErrorIs(t *testing.T, label string, err error, want error) {
	t.Helper()

	if !errors.Is(err, want) {
		t.Fatalf("%s error = %v, want %v", label, err, want)
	}
}

func assertAuthErrorString(t *testing.T, label string, err error, want string) {
	t.Helper()

	if err == nil || err.Error() != want {
		t.Fatalf("%s error = %v, want %s", label, err, want)
	}
}

func assertAuthErrorContains(t *testing.T, label string, err error, want string) {
	t.Helper()

	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("%s error = %v, want to contain %q", label, err, want)
	}
}

func authTestRequest(method string, target string) *http.Request {
	return httptest.NewRequest(method, target, nil)
}

func authRecorder() *httptest.ResponseRecorder {
	return httptest.NewRecorder()
}

func authPerform(handler http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := authRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func assertAuthErrorCode(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()

	body := authDecodeErrorEnvelope(t, rec, status)
	if body.Error.Code != code {
		t.Fatalf("error code = %q, want %q", body.Error.Code, code)
	}
}

func assertAuthJSONError(t *testing.T, rec *httptest.ResponseRecorder, status int, code string, message string) {
	t.Helper()

	body := authDecodeErrorEnvelope(t, rec, status)
	if body.Error.Code != code || body.Error.Message != message {
		t.Fatalf("error = {%q %q}, want {%q %q}", body.Error.Code, body.Error.Message, code, message)
	}
}

func authDecodeErrorEnvelope(t *testing.T, rec *httptest.ResponseRecorder, status int) struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
} {
	t.Helper()

	if rec.Code != status {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, status, rec.Body.String())
	}
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return body
}
