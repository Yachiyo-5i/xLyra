package newapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestGatewayClientListModelsSkipsMissingIDsAndKeepsRawPayload(t *testing.T) {
	t.Parallel()

	var sawAuth string
	client := newTestClient(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", req.Method)
		}
		if req.URL.String() != "https://newapi.example.com/v1/models" {
			t.Fatalf("url = %s, want https://newapi.example.com/v1/models", req.URL.String())
		}
		sawAuth = req.Header.Get("Authorization")
		return serviceJSONResponse(http.StatusOK, `{"data":[{"id":"gpt-4o","object":"model","owned_by":"openai"},{"id":"","object":"ignored"},{"object":"missing-id"}]}`), nil
	})

	models, err := client.ListGatewayModels(context.Background(), "https://newapi.example.com/", "raw-key")

	if err != nil {
		t.Fatalf("ListGatewayModels returned error: %v", err)
	}
	if sawAuth != "Bearer raw-key" {
		t.Fatalf("Authorization = %q, want Bearer raw-key", sawAuth)
	}
	if len(models) != 1 {
		t.Fatalf("models = %#v, want one model with non-empty id", models)
	}
	if models[0].ID != "gpt-4o" || models[0].Object != "model" || models[0].OwnedBy != "openai" {
		t.Fatalf("unexpected model = %#v", models[0])
	}
	if models[0].Raw["id"] != "gpt-4o" {
		t.Fatalf("raw payload = %#v, want original model item", models[0].Raw)
	}
}

func TestGatewayClientGetJSONReportsTransportStatusAndDecodeErrors(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		transport roundTripFunc
		want      string
	}{
		{
			name: "transport",
			transport: func(*http.Request) (*http.Response, error) {
				return nil, errors.New("network unavailable")
			},
			want: "network unavailable",
		},
		{
			name: "status",
			transport: func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusTeapot,
					Body:       io.NopCloser(strings.NewReader(" short body ")),
					Header:     make(http.Header),
				}, nil
			},
			want: "newapi returned 418: short body",
		},
		{
			name: "decode",
			transport: func(*http.Request) (*http.Response, error) {
				return serviceJSONResponse(http.StatusOK, `{not-json`), nil
			},
			want: "decode response",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			client := newTestClient(tc.transport)
			var payload map[string]any
			err := client.getJSON(context.Background(), "https://newapi.example.com", "/api/status", nil, &payload)

			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("getJSON error = %v, want to contain %q", err, tc.want)
			}
		})
	}
}

func TestNewClientWithNilHTTPClientUsesDefaultTimeout(t *testing.T) {
	t.Parallel()

	client := NewClientWithHTTPClient(nil)

	if client.httpClient == nil {
		t.Fatal("expected fallback HTTP client")
	}
	if client.httpClient.Timeout <= 0 {
		t.Fatalf("fallback timeout = %s, want positive timeout", client.httpClient.Timeout)
	}
}
