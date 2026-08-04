package newapi

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

type Service struct {
	client Client
}

func NewService() *Service {
	return &Service{client: NewClient()}
}

func NewServiceWithHTTPClient(httpClient *http.Client) *Service {
	return &Service{client: NewClientWithHTTPClient(httpClient)}
}

func (s *Service) Detect(ctx context.Context, baseURL string) (DetectResult, error) {
	baseURL = normalizeBaseURL(baseURL)
	if baseURL == "" {
		return DetectResult{}, fmt.Errorf("base_url is required")
	}

	return s.client.Detect(ctx, baseURL)
}

func (s *Service) UserSummary(ctx context.Context, baseURL string, accessToken string, userID int) (UserSummary, error) {
	baseURL = normalizeBaseURL(baseURL)
	if baseURL == "" {
		return UserSummary{}, fmt.Errorf("base_url is required")
	}
	if userID <= 0 {
		return UserSummary{}, fmt.Errorf("user_id must be greater than 0")
	}
	if strings.TrimSpace(accessToken) == "" {
		return UserSummary{}, fmt.Errorf("access_token is required")
	}

	return s.client.UserSummary(ctx, baseURL, accessToken, userID)
}

func (s *Service) APIKeySummary(ctx context.Context, baseURL string, apiKey string) (APIKeySummary, error) {
	baseURL = normalizeBaseURL(baseURL)
	if baseURL == "" {
		return APIKeySummary{}, fmt.Errorf("base_url is required")
	}
	if strings.TrimSpace(apiKey) == "" {
		return APIKeySummary{}, fmt.Errorf("api_key is required")
	}

	return s.client.APIKeySummary(ctx, baseURL, apiKey)
}

func (s *Service) PrimaryAPIKey(ctx context.Context, baseURL string, accessToken string, userID int) (string, error) {
	baseURL = normalizeBaseURL(baseURL)
	if baseURL == "" {
		return "", fmt.Errorf("base_url is required")
	}
	if userID <= 0 {
		return "", fmt.Errorf("user_id must be greater than 0")
	}
	if strings.TrimSpace(accessToken) == "" {
		return "", fmt.Errorf("access_token is required")
	}

	return s.client.PrimaryAPIKey(ctx, baseURL, accessToken, userID)
}

func (s *Service) UserAPIKeys(ctx context.Context, baseURL string, accessToken string, userID int) ([]UserAPIKey, error) {
	baseURL = normalizeBaseURL(baseURL)
	if baseURL == "" {
		return nil, fmt.Errorf("base_url is required")
	}
	if userID <= 0 {
		return nil, fmt.Errorf("user_id must be greater than 0")
	}
	if strings.TrimSpace(accessToken) == "" {
		return nil, fmt.Errorf("access_token is required")
	}

	return s.client.UserAPIKeys(ctx, baseURL, accessToken, userID)
}

func (s *Service) UserAPIKeySummaries(ctx context.Context, baseURL string, accessToken string, userID int) (UserAPIKeysSummary, error) {
	baseURL = normalizeBaseURL(baseURL)
	if baseURL == "" {
		return UserAPIKeysSummary{}, fmt.Errorf("base_url is required")
	}
	if userID <= 0 {
		return UserAPIKeysSummary{}, fmt.Errorf("user_id must be greater than 0")
	}
	if strings.TrimSpace(accessToken) == "" {
		return UserAPIKeysSummary{}, fmt.Errorf("access_token is required")
	}

	return s.client.UserAPIKeySummaries(ctx, baseURL, accessToken, userID)
}

func (s *Service) DoCheckin(ctx context.Context, baseURL string, accessToken string, userID int) (CheckinResult, error) {
	baseURL = normalizeBaseURL(baseURL)
	if baseURL == "" {
		return CheckinResult{}, fmt.Errorf("base_url is required")
	}
	if userID <= 0 {
		return CheckinResult{}, fmt.Errorf("user_id must be greater than 0")
	}
	if strings.TrimSpace(accessToken) == "" {
		return CheckinResult{}, fmt.Errorf("access_token is required")
	}

	return s.client.DoCheckin(ctx, baseURL, accessToken, userID)
}

func normalizeBaseURL(baseURL string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/")
}
