package newapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	httpClient *http.Client
}

func NewClient() Client {
	return Client{
		httpClient: &http.Client{Timeout: 20 * time.Second},
	}
}

func NewClientWithHTTPClient(httpClient *http.Client) Client {
	if httpClient == nil {
		return NewClient()
	}
	return Client{httpClient: httpClient}
}

func (c Client) Detect(ctx context.Context, baseURL string) (DetectResult, error) {
	var payload map[string]any
	if err := c.getJSON(ctx, baseURL, "/api/status", nil, &payload); err != nil {
		return DetectResult{}, err
	}

	data, _ := payload["data"].(map[string]any)
	features := map[string]any{}
	indicatorCount := 0

	for _, key := range []string{
		"version",
		"quota_per_unit",
		"quota_display_type",
		"checkin_enabled",
		"HeaderNavModules",
		"SidebarModulesAdmin",
		"default_use_auto_group",
	} {
		if value, ok := data[key]; ok {
			features[key] = value
			indicatorCount++
		}
	}

	success, _ := payload["success"].(bool)
	if success {
		indicatorCount++
	}

	confidence := float64(indicatorCount) / 8.0
	matched := success && indicatorCount >= 3

	siteType := "unknown"
	if matched {
		siteType = "newapi"
	}

	return DetectResult{
		SiteType:   siteType,
		Matched:    matched,
		Confidence: confidence,
		Features:   features,
		RawStatus:  payload,
	}, nil
}

func (c Client) UserSummary(ctx context.Context, baseURL string, accessToken string, userID int) (UserSummary, error) {
	headers := userHeaders(accessToken, userID)

	var userResp map[string]any
	if err := c.getJSON(ctx, baseURL, "/api/user/self", headers, &userResp); err != nil {
		return UserSummary{}, fmt.Errorf("get user self: %w", err)
	}

	var tokensResp map[string]any
	if err := c.getJSON(ctx, baseURL, "/api/token/", headers, &tokensResp); err != nil {
		tokensResp = unavailablePayload(err)
	}

	var modelsResp map[string]any
	if err := c.getJSON(ctx, baseURL, "/api/user/models", headers, &modelsResp); err != nil {
		modelsResp = unavailablePayload(err)
	}

	var pricingResp map[string]any
	if err := c.getJSON(ctx, baseURL, "/api/pricing", headers, &pricingResp); err != nil {
		pricingResp = unavailablePayload(err)
	}

	var checkinResp map[string]any
	checkinReady := false
	if err := c.getJSON(ctx, baseURL, "/api/user/checkin", headers, &checkinResp); err == nil {
		if success, _ := checkinResp["success"].(bool); success {
			checkinReady = true
		}
	} else {
		checkinResp = unavailablePayload(err)
	}

	apiKeySummaries, err := c.UserAPIKeySummaries(ctx, baseURL, accessToken, userID)
	if err != nil {
		apiKeySummaries = UserAPIKeysSummary{}
	}

	return UserSummary{
		User:            userResp,
		APIKeys:         tokensResp,
		APIKeySummaries: apiKeySummaries,
		UserModels:      modelsResp,
		Pricing:         pricingResp,
		Checkin:         checkinResp,
		CheckinReady:    checkinReady,
	}, nil
}

func (c Client) UserAPIKeys(ctx context.Context, baseURL string, accessToken string, userID int) ([]UserAPIKey, error) {
	headers := userHeaders(accessToken, userID)

	var tokensResp map[string]any
	if err := c.getJSON(ctx, baseURL, "/api/token/", headers, &tokensResp); err != nil {
		return nil, fmt.Errorf("get user api keys: %w", err)
	}

	keys := extractUserAPIKeys(tokensResp)
	ids := tokenIDs(keys)
	if len(ids) == 0 {
		return keys, nil
	}

	var batchResp map[string]any
	if err := c.doJSON(ctx, http.MethodPost, baseURL, "/api/token/batch/keys", headers, map[string]any{"ids": ids}, &batchResp); err != nil {
		return uniqueUserAPIKeys(keys), nil
	}

	fullKeys := extractBatchTokenKeys(batchResp)
	for i := range keys {
		if key := strings.TrimSpace(fullKeys[keys[i].ID]); key != "" {
			keys[i].Key = key
			keys[i].MaskedKey = maskAPIKey(key)
		}
	}

	return uniqueUserAPIKeys(keys), nil
}

func (c Client) PrimaryAPIKey(ctx context.Context, baseURL string, accessToken string, userID int) (string, error) {
	keys, err := c.UserAPIKeys(ctx, baseURL, accessToken, userID)
	if err != nil {
		return "", err
	}
	for _, key := range keys {
		if strings.TrimSpace(key.Key) != "" {
			return key.Key, nil
		}
	}

	return "", fmt.Errorf("no api keys returned for newapi user")
}

func (c Client) UserAPIKeySummaries(ctx context.Context, baseURL string, accessToken string, userID int) (UserAPIKeysSummary, error) {
	keys, err := c.UserAPIKeys(ctx, baseURL, accessToken, userID)
	if err != nil {
		return UserAPIKeysSummary{}, err
	}

	items := make([]UserAPIKeySummary, 0, len(keys))
	for _, key := range keys {
		item := UserAPIKeySummary{
			ID:        key.ID,
			Name:      key.Name,
			Status:    key.Status,
			Key:       key.Key,
			MaskedKey: key.MaskedKey,
			ModelIDs:  []string{},
		}

		if strings.TrimSpace(key.Key) == "" {
			item.Error = "api key value is empty"
			items = append(items, item)
			continue
		}

		summary, err := c.APIKeySummary(ctx, baseURL, key.Key)
		if err != nil {
			item.Error = err.Error()
			items = append(items, item)
			continue
		}

		item.Usage = summary.Usage
		if !hasUsageData(item.Usage) {
			item.Usage = usageFromTokenRaw(key.Raw)
		}
		item.Models = summary.Models
		item.ModelIDs = modelIDsFromModelsPayload(summary.Models)
		items = append(items, item)
	}

	return UserAPIKeysSummary{
		Count: len(items),
		Items: items,
	}, nil
}

func (c Client) DoCheckin(ctx context.Context, baseURL string, accessToken string, userID int) (CheckinResult, error) {
	var payload map[string]any
	if err := c.doJSON(ctx, http.MethodPost, baseURL, "/api/user/checkin", userHeaders(accessToken, userID), nil, &payload); err != nil {
		return CheckinResult{}, err
	}

	return CheckinResult{Result: payload}, nil
}

func (c Client) APIKeySummary(ctx context.Context, baseURL string, apiKey string) (APIKeySummary, error) {
	headers := gatewayHeaders(apiKey)

	var usageResp map[string]any
	if err := c.getJSON(ctx, baseURL, "/api/usage/token/", headers, &usageResp); err != nil {
		usageResp = map[string]any{
			"success": false,
			"message": err.Error(),
		}
	}

	var modelsResp map[string]any
	if err := c.getJSON(ctx, baseURL, "/v1/models", headers, &modelsResp); err != nil {
		return APIKeySummary{}, fmt.Errorf("get token models: %w", err)
	}

	return APIKeySummary{
		Usage:  usageResp,
		Models: modelsResp,
	}, nil
}

func (c Client) ListGatewayModels(ctx context.Context, baseURL string, apiKey string) ([]GatewayModel, error) {
	var payload struct {
		Data []map[string]any `json:"data"`
	}
	if err := c.getJSON(ctx, baseURL, "/v1/models", gatewayHeaders(apiKey), &payload); err != nil {
		return nil, err
	}

	models := make([]GatewayModel, 0, len(payload.Data))
	for _, item := range payload.Data {
		id, _ := item["id"].(string)
		if id == "" {
			continue
		}

		object, _ := item["object"].(string)
		ownedBy, _ := item["owned_by"].(string)
		models = append(models, GatewayModel{
			ID:      id,
			Object:  object,
			OwnedBy: ownedBy,
			Raw:     item,
		})
	}

	return models, nil
}

func (c Client) getJSON(ctx context.Context, baseURL string, path string, headers map[string]string, target any) error {
	return c.doJSON(ctx, http.MethodGet, baseURL, path, headers, nil, target)
}

func (c Client) doJSON(ctx context.Context, method string, baseURL string, path string, headers map[string]string, body any, target any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(baseURL, "/")+path, reader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		if strings.TrimSpace(value) != "" {
			req.Header.Set(key, value)
		}
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("call newapi: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("newapi returned %d: %s", resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
	}

	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	return nil
}

func userHeaders(accessToken string, userID int) map[string]string {
	return map[string]string{
		"Authorization": strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(accessToken, "Bearer "), "bearer ")),
		"New-Api-User":  strconv.Itoa(userID),
	}
}

func gatewayHeaders(apiKey string) map[string]string {
	key := strings.TrimSpace(apiKey)
	if !strings.HasPrefix(strings.ToLower(key), "bearer ") {
		key = "Bearer " + key
	}

	return map[string]string{
		"Authorization": key,
	}
}

func extractTokenIDs(payload map[string]any) []int {
	items := tokenItems(payload)
	ids := make([]int, 0, len(items))
	for _, item := range items {
		if id, ok := numberAsInt(item["id"]); ok {
			ids = append(ids, id)
		}
	}

	return ids
}

func extractUserAPIKeys(payload map[string]any) []UserAPIKey {
	items := tokenItems(payload)
	keys := make([]UserAPIKey, 0, len(items))
	for _, item := range items {
		id, _ := numberAsInt(item["id"])
		name, _ := item["name"].(string)
		key, _ := item["key"].(string)
		key = strings.TrimSpace(key)
		fullKey := ""
		maskedKey := key
		if key != "" && !isMaskedKey(key) {
			fullKey = normalizeAPIKeyValue(key)
			maskedKey = maskAPIKey(fullKey)
		}

		keys = append(keys, UserAPIKey{
			ID:        id,
			Name:      strings.TrimSpace(name),
			Status:    item["status"],
			Key:       fullKey,
			MaskedKey: maskedKey,
			Raw:       item,
		})
	}

	return keys
}

func extractBatchTokenKeys(payload map[string]any) map[int]string {
	data, _ := payload["data"].(map[string]any)
	keysPayload, _ := data["keys"].(map[string]any)

	keys := make(map[int]string, len(keysPayload))
	for rawID, value := range keysPayload {
		id, err := strconv.Atoi(rawID)
		if err != nil {
			continue
		}
		key, _ := value.(string)
		key = normalizeAPIKeyValue(key)
		if key != "" {
			keys[id] = key
		}
	}

	return keys
}

func tokenIDs(keys []UserAPIKey) []int {
	ids := make([]int, 0, len(keys))
	for _, key := range keys {
		if key.ID > 0 {
			ids = append(ids, key.ID)
		}
	}

	return ids
}

func hasUsableAPIKey(keys []UserAPIKey) bool {
	for _, key := range keys {
		if strings.TrimSpace(key.Key) != "" {
			return true
		}
	}

	return false
}

func uniqueUserAPIKeys(keys []UserAPIKey) []UserAPIKey {
	seenIDs := map[int]struct{}{}
	seenKeys := map[string]struct{}{}
	result := make([]UserAPIKey, 0, len(keys))
	for _, key := range keys {
		key.Key = strings.TrimSpace(key.Key)
		if key.Key != "" {
			if _, ok := seenKeys[key.Key]; ok {
				continue
			}
			seenKeys[key.Key] = struct{}{}
		}
		if key.ID > 0 {
			if _, ok := seenIDs[key.ID]; ok {
				continue
			}
			seenIDs[key.ID] = struct{}{}
		}
		result = append(result, key)
	}

	return result
}

func modelIDsFromModelsPayload(models any) []string {
	modelMap, ok := models.(map[string]any)
	if !ok {
		return []string{}
	}

	data, ok := modelMap["data"].([]any)
	if !ok {
		return []string{}
	}

	ids := make([]string, 0, len(data))
	for _, item := range data {
		modelItem, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id, _ := modelItem["id"].(string)
		if strings.TrimSpace(id) != "" {
			ids = append(ids, id)
		}
	}

	return ids
}

func hasUsageData(usage any) bool {
	usageMap, ok := usage.(map[string]any)
	if !ok {
		return false
	}
	data, ok := usageMap["data"].(map[string]any)
	if !ok {
		return false
	}
	_, hasGranted := data["total_granted"]
	_, hasUsed := data["total_used"]
	_, hasAvailable := data["total_available"]
	return hasGranted || hasUsed || hasAvailable
}

func usageFromTokenRaw(raw map[string]any) map[string]any {
	remain, _ := numberAsInt(raw["remain_quota"])
	used, _ := numberAsInt(raw["used_quota"])
	expiredAt, _ := numberAsInt(raw["expired_time"])

	return map[string]any{
		"success": true,
		"source":  "token_list",
		"data": map[string]any{
			"object":               "token_usage",
			"name":                 raw["name"],
			"total_granted":        remain + used,
			"total_used":           used,
			"total_available":      remain,
			"unlimited_quota":      raw["unlimited_quota"],
			"model_limits":         raw["model_limits"],
			"model_limits_enabled": raw["model_limits_enabled"],
			"expires_at":           expiredAt,
		},
	}
}

func isMaskedKey(key string) bool {
	return strings.Contains(key, "*") || strings.Contains(key, "...")
}

func normalizeAPIKeyValue(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(key), "sk-") {
		return key
	}
	return "sk-" + key
}

func maskAPIKey(key string) string {
	key = strings.TrimSpace(key)
	if len(key) <= 8 {
		return "****"
	}

	return key[:4] + "..." + key[len(key)-4:]
}

func unavailablePayload(err error) map[string]any {
	return map[string]any{
		"success": false,
		"message": err.Error(),
	}
}

func tokenItems(payload map[string]any) []map[string]any {
	data, _ := payload["data"].(map[string]any)
	if items, ok := data["items"].([]any); ok {
		return mapsFromAnySlice(items)
	}

	if items, ok := payload["data"].([]any); ok {
		return mapsFromAnySlice(items)
	}

	return nil
}

func mapsFromAnySlice(items []any) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if mapped, ok := item.(map[string]any); ok {
			result = append(result, mapped)
		}
	}

	return result
}

func numberAsInt(value any) (int, bool) {
	switch v := value.(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	default:
		return 0, false
	}
}
