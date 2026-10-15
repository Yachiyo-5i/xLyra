package jsplugin

import (
	"encoding/json"
	"fmt"
	"strings"
)

var probeMethods = map[string]struct{}{
	"GET": {}, "POST": {}, "PUT": {}, "PATCH": {}, "DELETE": {},
}

// ProbeContext is the credential-free object passed to a quota probe hook.
type ProbeContext struct {
	SiteType       string `ts:"siteType"`
	BaseURL        string `ts:"baseURL"`
	CredentialType string `ts:"credentialType"`
	Now            int64  `ts:"now,doc=Unix time in milliseconds."`
}

// ProbeStep is one finished HTTP exchange, shown back to the plugin.
type ProbeStep struct {
	Request  ProbeRequest  `ts:"request"`
	Response ProbeResponse `ts:"response"`
}

// ProbeRequest is a plugin-authored HTTP request before the host joins the URL.
type ProbeRequest struct {
	Method  string            `ts:"method,type=ProbeMethod"`
	Path    string            `ts:"path,doc=Path under the site base URL. Absolute URLs are rejected."`
	Headers map[string]string `ts:"headers,optional"`
	Query   map[string]string `ts:"query,optional"`
	Body    any               `ts:"body,optional,type=Json"`
}

// ProbeResponse is the HTTP result shown to the plugin. JSON is nil when the body is not JSON.
type ProbeResponse struct {
	Status  int               `ts:"status"`
	Headers map[string]string `ts:"headers"`
	Body    string            `ts:"body"`
	JSON    any               `ts:"json,doc=Parsed body, or null when the body is not JSON."`
	Error   string            `ts:"error,optional"`
}

// ProbeEntry is one quota window. Field names match the hook contract.
type ProbeEntry struct {
	Label           string   `ts:"label"`
	Unit            string   `ts:"unit,optional"`
	Remaining       *float64 `ts:"remaining"`
	Limit           *float64 `ts:"limit"`
	Used            *float64 `ts:"used"`
	Unlimited       bool     `ts:"unlimited,optional"`
	ResetAt         string   `ts:"resetAt,optional"`
	CashBalance     *float64 `ts:"cashBalance"`
	VoucherBalance  *float64 `ts:"voucherBalance"`
	GrantedBalance  *float64 `ts:"grantedBalance"`
	ToppedUpBalance *float64 `ts:"toppedUpBalance"`
}

// ProbeResult is a successful probe payload. Status and fetched_at stay in Go.
type ProbeResult struct {
	Kind        string       `ts:"kind"`
	Plan        string       `ts:"plan,optional"`
	ExpiresAt   string       `ts:"expiresAt,optional"`
	IsAvailable *bool        `ts:"isAvailable"`
	Entries     []ProbeEntry `ts:"entries"`
}

// Decision is one hook return: the next request, the final result, or a probe error.
type Decision struct {
	Request     *ProbeRequest
	Result      *ProbeResult
	Error       string
	Logs        []LogEntry
	DroppedLogs int
}

func (pc ProbeContext) asMap() map[string]any {
	return map[string]any{
		"siteType":       pc.SiteType,
		"baseURL":        pc.BaseURL,
		"credentialType": pc.CredentialType,
		"now":            pc.Now,
	}
}

func stepsAsValue(steps []ProbeStep) []any {
	out := make([]any, len(steps))
	for i, step := range steps {
		headers := step.Response.Headers
		if headers == nil {
			headers = map[string]string{}
		}
		response := map[string]any{
			"status":  step.Response.Status,
			"headers": headers,
			"body":    step.Response.Body,
			"json":    step.Response.JSON,
		}
		if step.Response.Error != "" {
			response["error"] = step.Response.Error
		}
		request := map[string]any{
			"method": step.Request.Method,
			"path":   step.Request.Path,
		}
		if len(step.Request.Headers) > 0 {
			request["headers"] = step.Request.Headers
		}
		if len(step.Request.Query) > 0 {
			request["query"] = step.Request.Query
		}
		if step.Request.Body != nil {
			request["body"] = step.Request.Body
		}
		out[i] = map[string]any{"request": request, "response": response}
	}
	return out
}

func decodeDecision(value any) (Decision, error) {
	object, ok := value.(map[string]any)
	if !ok {
		return Decision{}, shapeError("probe return value: expected object")
	}
	for key := range object {
		switch key {
		case "request", "result", "error":
		default:
			return Decision{}, shapeError("probe return value: unknown field %q", key)
		}
	}
	_, hasRequest := object["request"]
	_, hasResult := object["result"]
	_, hasError := object["error"]
	set := 0
	if hasRequest {
		set++
	}
	if hasResult {
		set++
	}
	if hasError {
		set++
	}
	if set != 1 {
		return Decision{}, shapeError("probe return value: expected exactly one of request, result, error")
	}
	if hasError {
		message, ok := object["error"].(string)
		if !ok || strings.TrimSpace(message) == "" {
			return Decision{}, shapeError("error: expected non-empty string")
		}
		return Decision{Error: message}, nil
	}
	if hasRequest {
		request, err := decodeRequest(object["request"])
		if err != nil {
			return Decision{}, err
		}
		return Decision{Request: &request}, nil
	}
	result, err := decodeResult(object["result"])
	if err != nil {
		return Decision{}, err
	}
	return Decision{Result: &result}, nil
}

func decodeRequest(value any) (ProbeRequest, error) {
	object, ok := value.(map[string]any)
	if !ok {
		return ProbeRequest{}, shapeError("request: expected object")
	}
	for key := range object {
		switch key {
		case "method", "path", "headers", "query", "body":
		default:
			return ProbeRequest{}, shapeError("request: unknown field %q", key)
		}
	}
	method := "GET"
	if raw, ok := object["method"]; ok {
		text, ok := raw.(string)
		if !ok {
			return ProbeRequest{}, shapeError("request.method: expected string")
		}
		method = strings.ToUpper(strings.TrimSpace(text))
	}
	if _, ok := probeMethods[method]; !ok {
		return ProbeRequest{}, shapeError("request.method: unsupported method %q", method)
	}
	path, ok := object["path"].(string)
	if !ok || strings.TrimSpace(path) == "" {
		return ProbeRequest{}, shapeError("request.path: expected string")
	}
	headers, err := stringMap(object["headers"], "request.headers")
	if err != nil {
		return ProbeRequest{}, err
	}
	query, err := stringMap(object["query"], "request.query")
	if err != nil {
		return ProbeRequest{}, err
	}
	body := object["body"]
	if body != nil {
		switch body.(type) {
		case string, map[string]any, []any:
		default:
			return ProbeRequest{}, shapeError("request.body: expected object, array, or string")
		}
	}
	return ProbeRequest{Method: method, Path: path, Headers: headers, Query: query, Body: body}, nil
}

func decodeResult(value any) (ProbeResult, error) {
	object, ok := value.(map[string]any)
	if !ok {
		return ProbeResult{}, shapeError("result: expected object")
	}
	for key := range object {
		switch key {
		case "kind", "plan", "expiresAt", "isAvailable", "entries":
		default:
			return ProbeResult{}, shapeError("result: unknown field %q", key)
		}
	}
	kind, ok := object["kind"].(string)
	if !ok || strings.TrimSpace(kind) == "" {
		return ProbeResult{}, shapeError("result.kind: expected string")
	}
	plan, err := optionalString(object["plan"], "result.plan")
	if err != nil {
		return ProbeResult{}, err
	}
	expiresAt, err := optionalString(object["expiresAt"], "result.expiresAt")
	if err != nil {
		return ProbeResult{}, err
	}
	var available *bool
	if raw, ok := object["isAvailable"]; ok && raw != nil {
		value, ok := raw.(bool)
		if !ok {
			return ProbeResult{}, shapeError("result.isAvailable: expected boolean")
		}
		available = &value
	}
	rawEntries, ok := object["entries"]
	if !ok {
		return ProbeResult{}, shapeError("result.entries: expected array")
	}
	items, ok := rawEntries.([]any)
	if !ok {
		return ProbeResult{}, shapeError("result.entries: expected array")
	}
	entries := make([]ProbeEntry, 0, len(items))
	for i, item := range items {
		entry, err := decodeEntry(item, i)
		if err != nil {
			return ProbeResult{}, err
		}
		entries = append(entries, entry)
	}
	return ProbeResult{Kind: kind, Plan: plan, ExpiresAt: expiresAt, IsAvailable: available, Entries: entries}, nil
}

func decodeEntry(value any, index int) (ProbeEntry, error) {
	object, ok := value.(map[string]any)
	if !ok {
		return ProbeEntry{}, shapeError("result.entries[%d]: expected object", index)
	}
	path := fmt.Sprintf("result.entries[%d]", index)
	for key := range object {
		switch key {
		case "label", "unit", "remaining", "limit", "used", "unlimited", "resetAt",
			"cashBalance", "voucherBalance", "grantedBalance", "toppedUpBalance":
		default:
			return ProbeEntry{}, shapeError("%s: unknown field %q", path, key)
		}
	}
	label, ok := object["label"].(string)
	if !ok || strings.TrimSpace(label) == "" {
		return ProbeEntry{}, shapeError("%s.label: expected string", path)
	}
	unit, err := optionalString(object["unit"], path+".unit")
	if err != nil {
		return ProbeEntry{}, err
	}
	resetAt, err := optionalString(object["resetAt"], path+".resetAt")
	if err != nil {
		return ProbeEntry{}, err
	}
	unlimited := false
	if raw, ok := object["unlimited"]; ok && raw != nil {
		value, ok := raw.(bool)
		if !ok {
			return ProbeEntry{}, shapeError("%s.unlimited: expected boolean", path)
		}
		unlimited = value
	}
	remaining, err := optionalFloat(object["remaining"], path+".remaining")
	if err != nil {
		return ProbeEntry{}, err
	}
	limit, err := optionalFloat(object["limit"], path+".limit")
	if err != nil {
		return ProbeEntry{}, err
	}
	used, err := optionalFloat(object["used"], path+".used")
	if err != nil {
		return ProbeEntry{}, err
	}
	cash, err := optionalFloat(object["cashBalance"], path+".cashBalance")
	if err != nil {
		return ProbeEntry{}, err
	}
	voucher, err := optionalFloat(object["voucherBalance"], path+".voucherBalance")
	if err != nil {
		return ProbeEntry{}, err
	}
	granted, err := optionalFloat(object["grantedBalance"], path+".grantedBalance")
	if err != nil {
		return ProbeEntry{}, err
	}
	topped, err := optionalFloat(object["toppedUpBalance"], path+".toppedUpBalance")
	if err != nil {
		return ProbeEntry{}, err
	}
	return ProbeEntry{
		Label: label, Unit: unit, Remaining: remaining, Limit: limit, Used: used, Unlimited: unlimited,
		ResetAt: resetAt, CashBalance: cash, VoucherBalance: voucher, GrantedBalance: granted, ToppedUpBalance: topped,
	}, nil
}

func optionalString(value any, path string) (string, error) {
	if value == nil {
		return "", nil
	}
	text, ok := value.(string)
	if !ok {
		return "", shapeError("%s: expected string", path)
	}
	return text, nil
}

func optionalFloat(value any, path string) (*float64, error) {
	if value == nil {
		return nil, nil
	}
	switch typed := value.(type) {
	case float64:
		return &typed, nil
	case int64:
		out := float64(typed)
		return &out, nil
	case int:
		out := float64(typed)
		return &out, nil
	case json.Number:
		parsed, err := typed.Float64()
		if err != nil {
			return nil, shapeError("%s: expected number", path)
		}
		return &parsed, nil
	default:
		return nil, shapeError("%s: expected number", path)
	}
}

func stringMap(value any, path string) (map[string]string, error) {
	if value == nil {
		return nil, nil
	}
	switch typed := value.(type) {
	case map[string]string:
		return typed, nil
	case map[string]any:
		out := make(map[string]string, len(typed))
		for key, item := range typed {
			text, ok := item.(string)
			if !ok {
				return nil, shapeError("%s.%s: expected string", path, key)
			}
			out[key] = text
		}
		return out, nil
	default:
		return nil, shapeError("%s: expected object", path)
	}
}
