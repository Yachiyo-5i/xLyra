package jsplugin

import (
	"encoding/json"
	"strings"
)

// ProtocolEndpointContext is passed to decodeRequest.
type ProtocolEndpointContext struct {
	DownstreamPath string
}

// ProtocolCandidateContext is the candidate block inside build/parse ctx.
type ProtocolCandidateContext struct {
	SiteType      string
	BaseURL       string
	UpstreamName  string
	UpstreamModel string
}

// ProtocolBuildContext is passed to buildRequest and parseResponse.
type ProtocolBuildContext struct {
	Candidate ProtocolCandidateContext
}

// ProtocolDecodeResult is a successful decodeRequest return.
type ProtocolDecodeResult struct {
	Model string
}

// ProtocolDecodeFailure is decodeRequest { error: { status, code, message } }.
type ProtocolDecodeFailure struct {
	Status  int
	Code    string
	Message string
}

// ProtocolBuiltRequest is the cached buildRequest return.
type ProtocolBuiltRequest struct {
	Path    string
	Headers map[string]string
	Payload map[string]any
}

// ProtocolParseInput is the resp object for parseResponse.
type ProtocolParseInput struct {
	Status  int
	Headers map[string]string
	Body    string
	JSON    any
}

// ProtocolParsedResponse is a successful parseResponse return.
type ProtocolParsedResponse struct {
	Passthrough    bool
	StatusCode     int
	ContentType    string
	Body           []byte
	Usage          ProtocolUsage
	UsagePopulated bool
}

// ProtocolUsage maps plugin usage fields to gateway usage.
type ProtocolUsage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

func (pc ProtocolEndpointContext) asMap() map[string]any {
	return map[string]any{"downstreamPath": pc.DownstreamPath}
}

func (bc ProtocolBuildContext) asMap() map[string]any {
	return map[string]any{
		"candidate": map[string]any{
			"siteType":      bc.Candidate.SiteType,
			"baseURL":       bc.Candidate.BaseURL,
			"upstreamName":  bc.Candidate.UpstreamName,
			"upstreamModel": bc.Candidate.UpstreamModel,
		},
	}
}

func parseInputAsMap(input ProtocolParseInput) map[string]any {
	headers := input.Headers
	if headers == nil {
		headers = map[string]string{}
	}
	out := map[string]any{
		"status":  input.Status,
		"headers": headers,
		"body":    input.Body,
		"json":    input.JSON,
	}
	return out
}

func decodeProtocolDecode(value any) (ProtocolDecodeResult, *ProtocolDecodeFailure, error) {
	object, ok := value.(map[string]any)
	if !ok {
		return ProtocolDecodeResult{}, nil, shapeError("decodeRequest return value: expected object")
	}
	for key := range object {
		switch key {
		case "model", "error":
		default:
			return ProtocolDecodeResult{}, nil, shapeError("decodeRequest return value: unknown field %q", key)
		}
	}
	if raw, ok := object["error"]; ok && raw != nil {
		failure, err := decodeProtocolDecodeFailure(raw)
		return ProtocolDecodeResult{}, failure, err
	}
	model, ok := object["model"].(string)
	if !ok || strings.TrimSpace(model) == "" {
		return ProtocolDecodeResult{}, nil, shapeError("model: expected non-empty string")
	}
	return ProtocolDecodeResult{Model: strings.TrimSpace(model)}, nil, nil
}

func decodeProtocolDecodeFailure(value any) (*ProtocolDecodeFailure, error) {
	object, ok := value.(map[string]any)
	if !ok {
		return nil, shapeError("error: expected object")
	}
	for key := range object {
		switch key {
		case "status", "code", "message":
		default:
			return nil, shapeError("error: unknown field %q", key)
		}
	}
	status, err := intField(object["status"], "error.status")
	if err != nil {
		return nil, err
	}
	code, ok := object["code"].(string)
	if !ok || strings.TrimSpace(code) == "" {
		return nil, shapeError("error.code: expected string")
	}
	message, ok := object["message"].(string)
	if !ok || strings.TrimSpace(message) == "" {
		return nil, shapeError("error.message: expected string")
	}
	return &ProtocolDecodeFailure{Status: status, Code: strings.TrimSpace(code), Message: strings.TrimSpace(message)}, nil
}

func decodeProtocolBuild(value any) (ProtocolBuiltRequest, error) {
	object, ok := value.(map[string]any)
	if !ok {
		return ProtocolBuiltRequest{}, shapeError("buildRequest return value: expected object")
	}
	for key := range object {
		switch key {
		case "path", "headers", "payload", "error":
		default:
			return ProtocolBuiltRequest{}, shapeError("buildRequest return value: unknown field %q", key)
		}
	}
	if raw, ok := object["error"]; ok && raw != nil {
		message, _ := raw.(string)
		if message == "" {
			if nested, ok := raw.(map[string]any); ok {
				message, _ = nested["message"].(string)
			}
		}
		if strings.TrimSpace(message) == "" {
			message = "buildRequest failed"
		}
		return ProtocolBuiltRequest{}, shapeError("%s", strings.TrimSpace(message))
	}
	path, ok := object["path"].(string)
	if !ok || strings.TrimSpace(path) == "" {
		return ProtocolBuiltRequest{}, shapeError("path: expected string")
	}
	headers, err := stringMap(object["headers"], "headers")
	if err != nil {
		return ProtocolBuiltRequest{}, err
	}
	payload, err := objectMap(object["payload"], "payload")
	if err != nil {
		return ProtocolBuiltRequest{}, err
	}
	return ProtocolBuiltRequest{Path: strings.TrimSpace(path), Headers: headers, Payload: payload}, nil
}

func decodeProtocolParse(value any) (ProtocolParsedResponse, error) {
	object, ok := value.(map[string]any)
	if !ok {
		return ProtocolParsedResponse{}, shapeError("parseResponse return value: expected object")
	}
	for key := range object {
		switch key {
		case "passthrough", "status", "contentType", "body", "usage", "error":
		default:
			return ProtocolParsedResponse{}, shapeError("parseResponse return value: unknown field %q", key)
		}
	}
	if raw, ok := object["error"]; ok && raw != nil {
		message, _ := raw.(string)
		if strings.TrimSpace(message) == "" {
			message = "parseResponse failed"
		}
		return ProtocolParsedResponse{}, shapeError("%s", strings.TrimSpace(message))
	}
	out := ProtocolParsedResponse{}
	if raw, ok := object["passthrough"]; ok && raw != nil {
		value, ok := raw.(bool)
		if !ok {
			return ProtocolParsedResponse{}, shapeError("passthrough: expected boolean")
		}
		out.Passthrough = value
	}
	if raw, ok := object["status"]; ok && raw != nil {
		status, err := intField(raw, "status")
		if err != nil {
			return ProtocolParsedResponse{}, err
		}
		out.StatusCode = status
	}
	if raw, ok := object["contentType"]; ok && raw != nil {
		text, ok := raw.(string)
		if !ok {
			return ProtocolParsedResponse{}, shapeError("contentType: expected string")
		}
		out.ContentType = strings.TrimSpace(text)
	}
	if raw, ok := object["body"]; ok && raw != nil {
		switch typed := raw.(type) {
		case string:
			out.Body = []byte(typed)
		default:
			return ProtocolParsedResponse{}, shapeError("body: expected string")
		}
	}
	if raw, ok := object["usage"]; ok && raw != nil {
		usage, err := decodeProtocolUsage(raw)
		if err != nil {
			return ProtocolParsedResponse{}, err
		}
		out.Usage = usage
		out.UsagePopulated = true
	}
	if !out.Passthrough && out.Body == nil && out.StatusCode == 0 && !out.UsagePopulated {
		return ProtocolParsedResponse{}, shapeError("parseResponse return value: expected passthrough, body, or usage")
	}
	return out, nil
}

func decodeProtocolUsage(value any) (ProtocolUsage, error) {
	object, ok := value.(map[string]any)
	if !ok {
		return ProtocolUsage{}, shapeError("usage: expected object")
	}
	for key := range object {
		switch key {
		case "prompt_tokens", "completion_tokens", "total_tokens":
		default:
			return ProtocolUsage{}, shapeError("usage: unknown field %q", key)
		}
	}
	prompt, err := intFieldOptional(object["prompt_tokens"], "usage.prompt_tokens")
	if err != nil {
		return ProtocolUsage{}, err
	}
	completion, err := intFieldOptional(object["completion_tokens"], "usage.completion_tokens")
	if err != nil {
		return ProtocolUsage{}, err
	}
	total, err := intFieldOptional(object["total_tokens"], "usage.total_tokens")
	if err != nil {
		return ProtocolUsage{}, err
	}
	if total == 0 && (prompt > 0 || completion > 0) {
		total = prompt + completion
	}
	return ProtocolUsage{PromptTokens: prompt, CompletionTokens: completion, TotalTokens: total}, nil
}

func objectMap(value any, path string) (map[string]any, error) {
	if value == nil {
		return map[string]any{}, nil
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, shapeError("%s: expected object", path)
	}
	return object, nil
}

func intFieldOptional(value any, path string) (int, error) {
	if value == nil {
		return 0, nil
	}
	number, ok := asInt(value)
	if !ok {
		return 0, shapeError("%s: expected number", path)
	}
	return number, nil
}

func asInt(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	case json.Number:
		parsed, err := typed.Int64()
		if err != nil {
			return 0, false
		}
		return int(parsed), true
	default:
		return 0, false
	}
}

func intField(value any, path string) (int, error) {
	number, ok := asInt(value)
	if !ok {
		return 0, shapeError("%s: expected number", path)
	}
	return number, nil
}
