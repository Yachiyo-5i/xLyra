package site

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"xlyra/server/internal/config"
	"xlyra/server/internal/jsplugin"
)

func (s *Service) probeQuota(ctx context.Context, client *http.Client, probeType, siteType, credentialType, baseURL, secret string) QuotaProbeResult {
	if s == nil || s.jsPlugins == nil {
		return quotaProbeUnavailable("js plugin registry is not available")
	}
	if !jsplugin.HeapLimitAvailable() {
		return quotaProbeUnavailable("js plugin allocation budget is not available")
	}
	plugin, ok := s.jsPlugins.ByProbeType(probeType)
	if !ok {
		return quotaProbeUnavailable(fmt.Sprintf("quota probe %q is not loaded", probeType))
	}
	return probeQuotaJS(ctx, client, plugin, siteType, credentialType, baseURL, secret)
}

// probeQuota runs a built-in quota probe plugin. Tests use this entry point.
func probeQuota(ctx context.Context, client *http.Client, probeType, baseURL, secret string) QuotaProbeResult {
	registry, err := jsplugin.LoadBuiltins()
	if err != nil {
		return quotaProbeUnavailable(err.Error())
	}
	if !jsplugin.HeapLimitAvailable() {
		return quotaProbeUnavailable("js plugin allocation budget is not available")
	}
	plugin, ok := registry.ByProbeType(probeType)
	if !ok {
		return quotaProbeUnavailable(fmt.Sprintf("quota probe %q is not loaded", probeType))
	}
	return probeQuotaJS(ctx, client, plugin, quotaProbeSiteType(probeType), "api_key", baseURL, secret)
}

func quotaProbeUnavailable(message string) QuotaProbeResult {
	return QuotaProbeResult{Status: "error", Error: message, FetchedAt: time.Now().UTC()}
}

func quotaProbeSiteType(probeType string) string {
	switch probeType {
	case QuotaProbeTypeKimi:
		return "kimi"
	case QuotaProbeTypeGLM:
		return "glm"
	case QuotaProbeTypeMoonshot:
		return "moonshot"
	case QuotaProbeTypeDeepSeek:
		return "deepseek"
	case QuotaProbeTypeNewAPI:
		return "newapi"
	case QuotaProbeTypeSub2API:
		return "sub2api"
	case QuotaProbeTypeXLyra:
		return "xlyra"
	default:
		return probeType
	}
}

// ValidateJSPluginLists ensures built-in js plugins can run. Legacy quota_probes and
// protocols lists in config are ignored; builtins are always used when loaded.
func ValidateJSPluginLists(cfg config.GeneralJSPluginConfig) error {
	_ = cfg
	if !jsplugin.HeapLimitAvailable() {
		return fmt.Errorf("js_plugin: allocation budget is not available")
	}
	registry, err := jsplugin.LoadBuiltins()
	if err != nil {
		return err
	}
	for _, probeType := range []string{
		QuotaProbeTypeSub2API, QuotaProbeTypeNewAPI, QuotaProbeTypeXLyra,
		QuotaProbeTypeKimi, QuotaProbeTypeGLM, QuotaProbeTypeMoonshot, QuotaProbeTypeDeepSeek,
	} {
		if _, ok := registry.ByProbeType(probeType); !ok {
			return fmt.Errorf("js_plugin: builtin quota probe %q is not loaded", probeType)
		}
	}
	return nil
}

func probeQuotaJS(ctx context.Context, client *http.Client, plugin *jsplugin.Plugin, siteType, credentialType, baseURL, secret string) QuotaProbeResult {
	result := QuotaProbeResult{Status: "error", FetchedAt: time.Now().UTC()}
	canonical, err := canonicalQuotaBase(plugin, baseURL)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	httpClient := http.Client{Timeout: quotaProbeRequestTimeout}
	if client != nil {
		httpClient = *client
	}
	httpClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		if err := jsplugin.SameOrigin(canonical, req.URL.String()); err != nil {
			return &redirectRejectedError{err: err}
		}
		return nil
	}
	probeCtx := jsplugin.ProbeContext{
		SiteType:       siteType,
		BaseURL:        canonical,
		CredentialType: credentialType,
		Now:            time.Now().UTC().UnixMilli(),
	}
	var steps []jsplugin.ProbeStep
	for step := 0; step < jsplugin.MaxProbeSteps; step++ {
		if err := ctx.Err(); err != nil {
			result.Error = err.Error()
			return result
		}
		decision, err := plugin.CallProbe(ctx, probeCtx, steps)
		logs, dropped := decision.Logs, decision.DroppedLogs
		if call, ok := err.(*jsplugin.CallError); ok {
			logs, dropped = call.Logs, call.DroppedLogs
		}
		emitPluginLogs(ctx, plugin, step, logs, dropped)
		if err != nil {
			logProbeFailure(plugin, step, err)
			result.Error = err.Error()
			return result
		}
		if decision.Error != "" {
			result.Error = decision.Error
			return result
		}
		if decision.Result != nil {
			return quotaResultFromJS(result, decision.Result)
		}
		if decision.Request == nil {
			result.Error = jsplugin.KindShape + ": probe return value: expected exactly one of request, result, error"
			return result
		}
		if step == jsplugin.MaxProbeSteps-1 {
			result.Error = "probe exceeded 6 steps"
			return result
		}
		response, err := doProbeHTTP(ctx, &httpClient, canonical, secret, decision.Request)
		if err != nil {
			result.Error = err.Error()
			return result
		}
		steps = append(steps, jsplugin.ProbeStep{Request: *decision.Request, Response: response})
	}
	result.Error = "probe exceeded 6 steps"
	return result
}

func quotaResultFromJS(result QuotaProbeResult, parsed *jsplugin.ProbeResult) QuotaProbeResult {
	result.Plan = parsed.Plan
	if parsed.ExpiresAt != "" {
		expires := parsed.ExpiresAt
		result.ExpiresAt = &expires
	}
	result.IsAvailable = parsed.IsAvailable
	if len(parsed.Entries) == 0 {
		result.Error = "probe response did not contain quota data"
		return result
	}
	entries := make([]QuotaProbeEntry, 0, len(parsed.Entries))
	for _, entry := range parsed.Entries {
		converted := QuotaProbeEntry{
			Label:           entry.Label,
			Unit:            entry.Unit,
			Remaining:       entry.Remaining,
			Limit:           entry.Limit,
			Used:            entry.Used,
			Unlimited:       entry.Unlimited,
			CashBalance:     entry.CashBalance,
			VoucherBalance:  entry.VoucherBalance,
			GrantedBalance:  entry.GrantedBalance,
			ToppedUpBalance: entry.ToppedUpBalance,
		}
		if entry.ResetAt != "" {
			reset := entry.ResetAt
			converted.ResetAt = &reset
		}
		entries = append(entries, converted)
	}
	result.Status = "ok"
	result.Kind = parsed.Kind
	result.Entries = entries
	result.Error = ""
	return result
}

func doProbeHTTP(ctx context.Context, client *http.Client, canonical, secret string, request *jsplugin.ProbeRequest) (jsplugin.ProbeResponse, error) {
	headers, err := jsplugin.FilterRequestHeaders(request.Headers)
	if err != nil {
		return jsplugin.ProbeResponse{}, &jsplugin.CallError{Kind: jsplugin.KindShape, Detail: err.Error()}
	}
	target, err := jsplugin.ResolveProbeURL(canonical, request.Path, request.Query)
	if err != nil {
		return jsplugin.ProbeResponse{}, &jsplugin.CallError{Kind: jsplugin.KindURLRejected, Detail: err.Error()}
	}
	var body io.Reader
	if request.Method != http.MethodGet && request.Method != http.MethodHead && request.Body != nil {
		encoded, err := encodeProbeBody(request.Body)
		if err != nil {
			return jsplugin.ProbeResponse{}, err
		}
		body = bytes.NewReader(encoded)
		if _, ok := request.Body.(string); !ok {
			if _, exists := headers["Content-Type"]; !exists {
				headers["Content-Type"] = "application/json"
			}
		}
	}
	httpReq, err := http.NewRequestWithContext(ctx, request.Method, target, body)
	if err != nil {
		return jsplugin.ProbeResponse{}, err
	}
	for key, value := range headers {
		httpReq.Header.Set(key, value)
	}
	httpReq.Header.Set("Authorization", "Bearer "+secret)
	if httpReq.Header.Get("Accept") == "" {
		httpReq.Header.Set("Accept", "application/json")
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		var rejected *redirectRejectedError
		if errors.As(err, &rejected) {
			return jsplugin.ProbeResponse{}, &jsplugin.CallError{Kind: jsplugin.KindURLRejected, Detail: rejected.Error()}
		}
		if ctx.Err() != nil {
			return jsplugin.ProbeResponse{}, ctx.Err()
		}
		return jsplugin.ProbeResponse{Status: 0, Error: err.Error()}, nil
	}
	defer func() { _ = resp.Body.Close() }()
	buf, err := io.ReadAll(io.LimitReader(resp.Body, quotaProbeBodyLimit+1))
	if err != nil {
		return jsplugin.ProbeResponse{}, err
	}
	if len(buf) > quotaProbeBodyLimit {
		return jsplugin.ProbeResponse{}, &jsplugin.CallError{Kind: jsplugin.KindResponseTooLarge, Detail: "响应过大"}
	}
	var payload any
	if json.Unmarshal(buf, &payload) != nil {
		payload = nil
	}
	return jsplugin.ProbeResponse{
		Status:  resp.StatusCode,
		Headers: jsplugin.FilterResponseHeaders(resp.Header),
		Body:    string(buf),
		JSON:    payload,
	}, nil
}

func encodeProbeBody(body any) ([]byte, error) {
	var encoded []byte
	switch typed := body.(type) {
	case string:
		encoded = []byte(typed)
	default:
		var err error
		encoded, err = json.Marshal(typed)
		if err != nil {
			return nil, err
		}
	}
	if len(encoded) > jsplugin.MaxProbeRequestBody {
		return nil, &jsplugin.CallError{Kind: jsplugin.KindShape, Detail: "request body is too large"}
	}
	return encoded, nil
}

func canonicalQuotaBase(plugin *jsplugin.Plugin, baseURL string) (string, error) {
	mode := plugin.Manifest.QuotaProbe.BaseURLMode
	if mode == "" {
		mode = "as_is"
	}
	return canonicalFromMode(mode, plugin.Manifest.QuotaProbe.DefaultBaseURL, baseURL)
}

type redirectRejectedError struct {
	err error
}

func (e *redirectRejectedError) Error() string {
	if e.err == nil {
		return "redirect left the site origin"
	}
	return e.err.Error()
}

func (e *redirectRejectedError) Unwrap() error { return e.err }

func canonicalFromMode(mode, defaultBase, baseURL string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = strings.TrimRight(strings.TrimSpace(defaultBase), "/")
	}
	if base == "" {
		return "", fmt.Errorf("quota probe requires a base URL")
	}
	switch mode {
	case "origin":
		parsed, err := url.Parse(base)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return "", fmt.Errorf("invalid base URL %q", baseURL)
		}
		return parsed.Scheme + "://" + parsed.Host, nil
	case "trim_v1":
		return strings.TrimSuffix(base, "/v1"), nil
	case "trim_v1_fold":
		if strings.HasSuffix(strings.ToLower(base), "/v1") {
			return base[:len(base)-len("/v1")], nil
		}
		return base, nil
	case "as_is":
		return canonicalAsIs(base)
	default:
		return "", fmt.Errorf("unsupported base URL mode %q", mode)
	}
}

func canonicalAsIs(baseURL string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return "", fmt.Errorf("quota probe requires a base URL")
	}
	if _, err := url.ParseRequestURI(base); err != nil {
		return "", fmt.Errorf("invalid base URL %q", baseURL)
	}
	return base, nil
}

func emitPluginLogs(ctx context.Context, plugin *jsplugin.Plugin, step int, logs []jsplugin.LogEntry, dropped int) {
	for _, entry := range logs {
		level := slog.LevelInfo
		switch entry.Level {
		case "debug":
			level = slog.LevelDebug
		case "warn":
			level = slog.LevelWarn
		}
		attrs := []any{"js_hook", "probe", "js_step", step}
		if plugin != nil {
			attrs = append(attrs, "js_plugin_id", plugin.Manifest.ID, "js_plugin_version", plugin.Manifest.Version)
		}
		if len(entry.Fields) > 0 {
			fields := make([]any, 0, len(entry.Fields)*2)
			for key, value := range entry.Fields {
				fields = append(fields, key, value)
			}
			attrs = append(attrs, slog.Group("js_fields", fields...))
		}
		slog.Log(ctx, level, entry.Message, attrs...)
	}
	if dropped > 0 {
		slog.WarnContext(ctx, "js plugin log lines dropped", "js_hook", "probe", "js_step", step, "dropped", dropped)
	}
}

func logProbeFailure(plugin *jsplugin.Plugin, step int, err error) {
	attrs := []any{"js_hook", "probe", "js_step", step, "error", err}
	var call *jsplugin.CallError
	if ok := errorAs(err, &call); ok {
		attrs = append(attrs,
			"js_plugin_id", call.PluginID,
			"js_plugin_version", call.PluginVersion,
			"js_gen", call.Gen,
			"js_duration_ms", call.DurationMS,
			"js_interrupt_reason", call.Interrupt,
		)
	} else if plugin != nil {
		attrs = append(attrs, "js_plugin_id", plugin.Manifest.ID, "js_plugin_version", plugin.Manifest.Version)
	}
	slog.Warn("js quota probe failed", attrs...)
}

func errorAs(err error, target **jsplugin.CallError) bool {
	for err != nil {
		if call, ok := err.(*jsplugin.CallError); ok {
			*target = call
			return true
		}
		unwrap, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = unwrap.Unwrap()
	}
	return false
}
