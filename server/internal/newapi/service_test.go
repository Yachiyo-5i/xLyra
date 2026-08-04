package newapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestNormalizeBaseURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		baseURL string
		want    string
	}{
		{
			name:    "trims whitespace and trailing slashes",
			baseURL: " \t https://newapi.example.com/// \n",
			want:    "https://newapi.example.com",
		},
		{
			name:    "keeps base path while trimming trailing slash",
			baseURL: "https://newapi.example.com/panel/",
			want:    "https://newapi.example.com/panel",
		},
		{
			name:    "keeps already normalized url",
			baseURL: "http://127.0.0.1:3000",
			want:    "http://127.0.0.1:3000",
		},
		{
			name:    "blank whitespace normalizes empty",
			baseURL: " \t\n ",
			want:    "",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := normalizeBaseURL(tt.baseURL); got != tt.want {
				t.Fatalf("normalizeBaseURL(%q) = %q, want %q", tt.baseURL, got, tt.want)
			}
		})
	}
}

func TestNewServiceConstructorsConfigureHTTPClient(t *testing.T) {
	t.Parallel()

	defaultService := NewService()
	if defaultService == nil {
		t.Fatal("expected NewService to return a service")
	}
	if defaultService.client.httpClient == nil {
		t.Fatal("expected NewService to configure a default HTTP client")
	}

	fallbackService := NewServiceWithHTTPClient(nil)
	if fallbackService.client.httpClient == nil {
		t.Fatal("expected nil custom HTTP client to fall back to a default client")
	}

	customClient := &http.Client{}
	customService := NewServiceWithHTTPClient(customClient)
	if customService.client.httpClient != customClient {
		t.Fatal("expected NewServiceWithHTTPClient to use the provided HTTP client")
	}
}

func TestServiceDetectUsesCustomHTTPClientAndNormalizedBaseURL(t *testing.T) {
	t.Parallel()

	var sawRequest bool
	service := NewServiceWithHTTPClient(&http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			sawRequest = true
			if got := r.URL.String(); got != "https://newapi.example.com/api/status" {
				t.Fatalf("request URL = %q, want normalized status URL", got)
			}

			return serviceJSONResponse(http.StatusOK, `{
				"success": true,
				"data": {
					"version": "v0.8.0",
					"quota_per_unit": 500000
				}
			}`), nil
		}),
	})

	result, err := service.Detect(context.Background(), " \thttps://newapi.example.com/// ")
	if err != nil {
		t.Fatalf("Detect returned error: %v", err)
	}
	if !sawRequest {
		t.Fatal("expected custom HTTP client to receive the request")
	}
	if !result.Matched || result.SiteType != "newapi" {
		t.Fatalf("expected newapi detection result, got %#v", result)
	}
}

func TestServiceDetectPropagatesClientError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("transport failed")
	service := NewServiceWithHTTPClient(&http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, wantErr
		}),
	})

	_, err := service.Detect(context.Background(), "https://newapi.example.com")
	if !errors.Is(err, wantErr) {
		t.Fatalf("Detect error = %v, want wrapped transport error", err)
	}
}

func TestServiceUserSummarySuccessNormalizesBaseURL(t *testing.T) {
	t.Parallel()

	seenPaths := map[string]int{}
	server := newapiTestServer(t, newapiTestRoutes{
		"/api/user/self": func(w http.ResponseWriter, r *http.Request) {
			assertUserAuth(t, r)
			seenPaths[r.URL.Path]++
			writeJSON(t, w, map[string]any{
				"success": true,
				"data": map[string]any{
					"id":    42,
					"quota": 1000,
				},
			})
		},
		"/api/token/": func(w http.ResponseWriter, r *http.Request) {
			assertUserAuth(t, r)
			seenPaths[r.URL.Path]++
			writeJSON(t, w, map[string]any{
				"success": true,
				"data":    []map[string]any{},
			})
		},
		"/api/user/models": func(w http.ResponseWriter, r *http.Request) {
			assertUserAuth(t, r)
			seenPaths[r.URL.Path]++
			writeJSON(t, w, map[string]any{
				"success": true,
				"data":    []string{"gpt-4o-mini"},
			})
		},
		"/api/pricing": func(w http.ResponseWriter, r *http.Request) {
			assertUserAuth(t, r)
			seenPaths[r.URL.Path]++
			writeJSON(t, w, map[string]any{
				"success": true,
				"data": map[string]any{
					"gpt-4o-mini": map[string]any{"model_ratio": 0.3},
				},
			})
		},
		"/api/user/checkin": func(w http.ResponseWriter, r *http.Request) {
			assertUserAuth(t, r)
			seenPaths[r.URL.Path]++
			writeJSON(t, w, map[string]any{
				"success": true,
				"data": map[string]any{
					"checked_in": false,
				},
			})
		},
	})

	result, err := NewService().UserSummary(context.Background(), server.URL+"///", "access-token", 42)
	if err != nil {
		t.Fatalf("UserSummary returned error: %v", err)
	}
	if !result.CheckinReady {
		t.Fatal("expected checkin to be ready")
	}
	for _, path := range []string{"/api/user/self", "/api/token/", "/api/user/models", "/api/pricing", "/api/user/checkin"} {
		if seenPaths[path] == 0 {
			t.Fatalf("expected UserSummary to request %s; saw %#v", path, seenPaths)
		}
	}
}

func TestServiceUserSummaryPropagatesRequiredEndpointError(t *testing.T) {
	t.Parallel()

	server := newapiTestServer(t, newapiTestRoutes{
		"/api/user/self": func(w http.ResponseWriter, r *http.Request) {
			assertUserAuth(t, r)
			writeNewAPIMessage(t, w, http.StatusBadGateway, "upstream unavailable")
		},
	})

	_, err := NewService().UserSummary(context.Background(), server.URL, "access-token", 42)
	if err == nil {
		t.Fatal("expected UserSummary to return an error")
	}
	if !strings.Contains(err.Error(), "get user self:") || !strings.Contains(err.Error(), "newapi returned 502") {
		t.Fatalf("UserSummary error = %v, want wrapped required endpoint error", err)
	}
}

func TestServiceAPIKeySummarySuccessNormalizesBaseURL(t *testing.T) {
	t.Parallel()

	seenPaths := map[string]bool{}
	server := newapiTestServer(t, newapiTestRoutes{
		"/api/usage/token/": func(w http.ResponseWriter, r *http.Request) {
			assertGatewayAuth(t, r)
			seenPaths[r.URL.Path] = true
			writeJSON(t, w, map[string]any{
				"success": true,
				"data": map[string]any{
					"total_used": 125,
				},
			})
		},
		"/v1/models": func(w http.ResponseWriter, r *http.Request) {
			assertGatewayAuth(t, r)
			seenPaths[r.URL.Path] = true
			writeJSON(t, w, map[string]any{
				"object": "list",
				"data": []map[string]any{
					{"id": "gpt-4o-mini", "object": "model"},
				},
			})
		},
	})

	result, err := NewService().APIKeySummary(context.Background(), server.URL+"///", "sk-test")
	if err != nil {
		t.Fatalf("APIKeySummary returned error: %v", err)
	}
	if result.Usage == nil || result.Models == nil {
		t.Fatalf("expected usage and models to be populated: %#v", result)
	}
	for _, path := range []string{"/api/usage/token/", "/v1/models"} {
		if !seenPaths[path] {
			t.Fatalf("expected APIKeySummary to request %s; saw %#v", path, seenPaths)
		}
	}
}

func TestServiceAPIKeySummaryPropagatesModelsError(t *testing.T) {
	t.Parallel()

	server := newapiTestServer(t, newapiTestRoutes{
		"/api/usage/token/": func(w http.ResponseWriter, r *http.Request) {
			assertGatewayAuth(t, r)
			writeJSON(t, w, map[string]any{
				"success": true,
				"data":    map[string]any{},
			})
		},
		"/v1/models": func(w http.ResponseWriter, r *http.Request) {
			assertGatewayAuth(t, r)
			writeNewAPIMessage(t, w, http.StatusServiceUnavailable, "models down")
		},
	})

	_, err := NewService().APIKeySummary(context.Background(), server.URL, "sk-test")
	if err == nil {
		t.Fatal("expected APIKeySummary to return an error")
	}
	if !strings.Contains(err.Error(), "get token models:") || !strings.Contains(err.Error(), "newapi returned 503") {
		t.Fatalf("APIKeySummary error = %v, want wrapped models endpoint error", err)
	}
}

func TestServiceDoCheckinSuccessNormalizesBaseURL(t *testing.T) {
	t.Parallel()

	server := newapiTestServer(t, newapiTestRoutes{
		"/api/user/checkin": func(w http.ResponseWriter, r *http.Request) {
			assertUserAuth(t, r)
			assertRequestMethod(t, r, http.MethodPost)

			writeJSON(t, w, map[string]any{
				"success": true,
				"message": "checked in",
			})
		},
	})

	result, err := NewService().DoCheckin(context.Background(), server.URL+"///", "access-token", 42)
	if err != nil {
		t.Fatalf("DoCheckin returned error: %v", err)
	}
	if result.Result == nil {
		t.Fatal("expected checkin result to be populated")
	}
}

func TestServiceDoCheckinPropagatesClientError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("checkin transport failed")
	service := NewServiceWithHTTPClient(&http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, wantErr
		}),
	})

	_, err := service.DoCheckin(context.Background(), "https://newapi.example.com", "access-token", 42)
	if !errors.Is(err, wantErr) {
		t.Fatalf("DoCheckin error = %v, want wrapped transport error", err)
	}
}

func TestServiceValidationRejectsInvalidInputs(t *testing.T) {
	t.Parallel()

	service := NewServiceWithHTTPClient(&http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			t.Fatalf("unexpected HTTP request for invalid input: %s %s", r.Method, r.URL.String())
			return nil, nil
		}),
	})

	tests := []struct {
		name    string
		call    func() error
		wantErr string
	}{
		{
			name: "detect requires base url",
			call: func() error {
				_, err := service.Detect(context.Background(), " \t\n ")
				return err
			},
			wantErr: "base_url is required",
		},
		{
			name: "user summary requires base url",
			call: func() error {
				_, err := service.UserSummary(context.Background(), " ", "access-token", 42)
				return err
			},
			wantErr: "base_url is required",
		},
		{
			name: "user summary requires positive user id",
			call: func() error {
				_, err := service.UserSummary(context.Background(), "https://newapi.example.com", "access-token", 0)
				return err
			},
			wantErr: "user_id must be greater than 0",
		},
		{
			name: "user summary requires access token",
			call: func() error {
				_, err := service.UserSummary(context.Background(), "https://newapi.example.com", " \t ", 42)
				return err
			},
			wantErr: "access_token is required",
		},
		{
			name: "api key summary requires base url",
			call: func() error {
				_, err := service.APIKeySummary(context.Background(), "", "sk-test")
				return err
			},
			wantErr: "base_url is required",
		},
		{
			name: "api key summary requires api key",
			call: func() error {
				_, err := service.APIKeySummary(context.Background(), "https://newapi.example.com", " \t ")
				return err
			},
			wantErr: "api_key is required",
		},
		{
			name: "primary api key requires base url",
			call: func() error {
				_, err := service.PrimaryAPIKey(context.Background(), "", "access-token", 42)
				return err
			},
			wantErr: "base_url is required",
		},
		{
			name: "primary api key requires positive user id",
			call: func() error {
				_, err := service.PrimaryAPIKey(context.Background(), "https://newapi.example.com", "access-token", 0)
				return err
			},
			wantErr: "user_id must be greater than 0",
		},
		{
			name: "primary api key requires access token",
			call: func() error {
				_, err := service.PrimaryAPIKey(context.Background(), "https://newapi.example.com", " ", 42)
				return err
			},
			wantErr: "access_token is required",
		},
		{
			name: "user api keys requires base url",
			call: func() error {
				_, err := service.UserAPIKeys(context.Background(), "", "access-token", 42)
				return err
			},
			wantErr: "base_url is required",
		},
		{
			name: "user api keys requires positive user id",
			call: func() error {
				_, err := service.UserAPIKeys(context.Background(), "https://newapi.example.com", "access-token", -1)
				return err
			},
			wantErr: "user_id must be greater than 0",
		},
		{
			name: "user api keys requires access token",
			call: func() error {
				_, err := service.UserAPIKeys(context.Background(), "https://newapi.example.com", "\t", 42)
				return err
			},
			wantErr: "access_token is required",
		},
		{
			name: "user api key summaries requires base url",
			call: func() error {
				_, err := service.UserAPIKeySummaries(context.Background(), "", "access-token", 42)
				return err
			},
			wantErr: "base_url is required",
		},
		{
			name: "user api key summaries requires positive user id",
			call: func() error {
				_, err := service.UserAPIKeySummaries(context.Background(), "https://newapi.example.com", "access-token", 0)
				return err
			},
			wantErr: "user_id must be greater than 0",
		},
		{
			name: "user api key summaries requires access token",
			call: func() error {
				_, err := service.UserAPIKeySummaries(context.Background(), "https://newapi.example.com", " \n", 42)
				return err
			},
			wantErr: "access_token is required",
		},
		{
			name: "checkin requires base url",
			call: func() error {
				_, err := service.DoCheckin(context.Background(), "", "access-token", 42)
				return err
			},
			wantErr: "base_url is required",
		},
		{
			name: "checkin requires positive user id",
			call: func() error {
				_, err := service.DoCheckin(context.Background(), "https://newapi.example.com", "access-token", -1)
				return err
			},
			wantErr: "user_id must be greater than 0",
		},
		{
			name: "checkin requires access token",
			call: func() error {
				_, err := service.DoCheckin(context.Background(), "https://newapi.example.com", " ", 42)
				return err
			},
			wantErr: "access_token is required",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.call()
			if err == nil {
				t.Fatalf("expected %q error", tt.wantErr)
			}
			if err.Error() != tt.wantErr {
				t.Fatalf("error = %q, want %q", err.Error(), tt.wantErr)
			}
		})
	}
}
