package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// CodexUsageAvailable reports whether a normalized Codex usage snapshot shows
// spendable quota. Every window the upstream returned must have remaining quota;
// a window the upstream omitted (for example after OpenAI drops the 5-hour limit
// and only reports the weekly window) is ignored. Error snapshots, and snapshots
// without any recognized window, are treated as unavailable so a cooldown is never
// lifted on incomplete data.
func CodexUsageAvailable(usage any) bool {
	payload, ok := usage.(map[string]any)
	if !ok {
		return false
	}
	if success, ok := payload["success"].(bool); ok && !success {
		return false
	}
	_, hasFiveHour := payload["five_hour"].(map[string]any)
	_, hasWeekly := payload["weekly"].(map[string]any)
	if !hasFiveHour && !hasWeekly {
		return false
	}
	return codexUsageAvailable(payload)
}

// CodexShouldPrimeFiveHour reports whether the account's 5-hour window is idle and
// worth priming: the upstream still reports a 5-hour window, it currently has
// quota, and its countdown has not started (no future reset_at). Sending one
// minimal request in this state starts the 5-hour countdown immediately instead of
// waiting for the user's first real request, saving idle wait time.
func CodexShouldPrimeFiveHour(usage any, now time.Time) bool {
	payload, ok := usage.(map[string]any)
	if !ok {
		return false
	}
	window, ok := payload["five_hour"].(map[string]any)
	if !ok || len(window) == 0 {
		return false
	}
	if intFromAny(window["remaining_percent"]) <= 0 {
		return false
	}
	// reset_at is normalized to a positive unix timestamp, a raw string, or nil.
	// A nil (or already-elapsed) reset means the window is not currently counting.
	switch resetAt := window["reset_at"].(type) {
	case nil:
		return true
	case int64:
		return resetAt <= now.Unix()
	case int:
		return int64(resetAt) <= now.Unix()
	case float64:
		return int64(resetAt) <= now.Unix()
	case string:
		return strings.TrimSpace(resetAt) == ""
	default:
		return false
	}
}

// PrimeUsageWindow sends a single minimal Codex responses request so the account's
// 5-hour usage window starts counting immediately, instead of only when the user's
// first real request arrives. The model must be an upstream model name the account
// can use. Codex forces streaming responses, so the (small) body is drained and
// discarded; any 2xx is treated as success.
func (a Codex) PrimeUsageWindow(ctx context.Context, site SiteConfig, auth SystemAuth, model string) error {
	trimmedModel := strings.TrimSpace(model)
	if trimmedModel == "" {
		return fmt.Errorf("codex prime request requires a model")
	}
	request := map[string]any{
		"model":        trimmedModel,
		"instructions": "",
		"input": []any{
			map[string]any{
				"type": "message",
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_text", "text": "ping"},
				},
			},
		},
		// Codex forces these; set them explicitly since we bypass the gateway policy.
		"stream": true,
		"store":  false,
	}
	body, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("marshal codex prime request: %w", err)
	}
	endpoints := codexResponsesEndpoints(site.BaseURL)
	var lastErr error
	for _, endpoint := range endpoints {
		if lastErr = a.postStreamDiscard(ctx, site, endpoint, auth.AccessToken, auth.AccountID, body); lastErr == nil {
			return nil
		}
	}
	return lastErr
}

// postStreamDiscard POSTs a JSON body to a Codex streaming endpoint and discards the
// response body. It returns nil for any 2xx status.
func (a Codex) postStreamDiscard(ctx context.Context, site SiteConfig, endpoint string, accessToken string, accountID string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("create codex request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(accessToken))
	req.Header.Set("Origin", codexOriginURL)
	req.Header.Set("Referer", codexRefererURL)
	req.Header.Set("User-Agent", CodexUserAgent)
	if trimmed := strings.TrimSpace(accountID); trimmed != "" {
		req.Header.Set("ChatGPT-Account-Id", trimmed)
	}
	resp, err := httpClientForSite(site, a.client).Do(req)
	if err != nil {
		return fmt.Errorf("call codex upstream: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return codexUpstreamHTTPError{
			statusCode:  resp.StatusCode,
			contentType: resp.Header.Get("Content-Type"),
			body:        string(errBody),
		}
	}
	// Drain a bounded prefix so the request is fully accepted upstream, then stop.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	return nil
}
