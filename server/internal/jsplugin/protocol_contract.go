package jsplugin

import (
	"encoding/json"
	"net/textproto"
	"strings"
)

// MaxSignBodyBytes is the largest upstream request body shown to signRequest.
const MaxSignBodyBytes = 1 << 20

// MaxStringToSignBytes bounds the string a signRequest hook asks Go to sign.
const MaxStringToSignBytes = 64 << 10

// SignAlgorithms are the HMAC algorithms Go signs with. The key is the site
// credential and never leaves Go.
var SignAlgorithms = []string{"hmac-sha256", "hmac-sha1", "hmac-sha512"}

// SignEncodings are how the signature bytes are written into the header.
var SignEncodings = []string{"hex", "base64"}

// ProtocolEndpointContext is passed to decodeRequest.
type ProtocolEndpointContext struct {
	DownstreamPath string `ts:"downstreamPath"`
}

// ProtocolCandidateContext is the candidate block inside build/parse ctx.
type ProtocolCandidateContext struct {
	SiteType      string `ts:"siteType"`
	BaseURL       string `ts:"baseURL"`
	UpstreamName  string `ts:"upstreamName"`
	UpstreamModel string `ts:"upstreamModel"`
}

// ProtocolBuildContext is passed to buildRequest and parseResponse.
type ProtocolBuildContext struct {
	Candidate ProtocolCandidateContext `ts:"candidate"`
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
	Path    string            `ts:"path"`
	Headers map[string]string `ts:"headers,optional"`
	Payload map[string]any    `ts:"payload"`
}

// ProtocolParseInput is the resp object for parseResponse.
type ProtocolParseInput struct {
	Status  int               `ts:"status"`
	Headers map[string]string `ts:"headers"`
	Body    string            `ts:"body"`
	JSON    any               `ts:"json"`
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
	PromptTokens     int `ts:"prompt_tokens,optional"`
	CompletionTokens int `ts:"completion_tokens,optional"`
	TotalTokens      int `ts:"total_tokens,optional"`
}

// ProtocolSignInput is the final upstream request shown to signRequest. The
// credential and cookies are not in it; the plugin decides what to sign and Go
// signs it with the credential.
type ProtocolSignInput struct {
	Method  string            `ts:"method"`
	URL     string            `ts:"url,doc=Full upstream URL including the query string."`
	Headers map[string]string `ts:"headers,doc=Headers about to be sent, without credentials."`
	Body    string            `ts:"body,doc=The exact request body that will be sent (at most 1 MiB)."`
}

// ProtocolSignResult tells Go what to sign and where to put the signature.
type ProtocolSignResult struct {
	StringToSign string            `ts:"stringToSign,doc=The exact text to sign. At most 64 KiB."`
	Algorithm    string            `ts:"algorithm,type=SignAlgorithm"`
	Encoding     string            `ts:"encoding,optional,type=SignEncoding,doc=Defaults to hex."`
	Header       string            `ts:"header,doc=Header that carries the signature, such as Authorization or X-Signature."`
	Prefix       string            `ts:"prefix,optional,doc=Text put before the encoded signature, such as 'HMAC-SHA256 '."`
	Headers      map[string]string `ts:"headers,optional,doc=Extra non-secret headers to send, such as a timestamp or nonce."`
}

// ProtocolErrorInput is the non-2xx upstream response shown to parseError.
type ProtocolErrorInput = ProtocolParseInput

// ProtocolErrorResult replaces the body the client sees for an upstream error.
// The status is kept as the upstream sent it, so failure handling is unchanged.
type ProtocolErrorResult struct {
	ContentType string `ts:"contentType,optional"`
	Body        string `ts:"body,doc=The error body returned to the client."`
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

func signInputAsMap(input ProtocolSignInput) map[string]any {
	headers := input.Headers
	if headers == nil {
		headers = map[string]string{}
	}
	return map[string]any{"method": input.Method, "url": input.URL, "headers": headers, "body": input.Body}
}

// forbiddenSignHeaders can never carry a plugin-chosen signature or extra value.
var forbiddenSignHeaders = map[string]struct{}{
	"host": {}, "content-length": {}, "transfer-encoding": {}, "connection": {},
	"cookie": {}, "proxy-authorization": {}, "content-type": {}, "accept": {},
}

func validSignHeaderName(name string) bool {
	if name == "" || len(name) > 128 {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	_, forbidden := forbiddenSignHeaders[strings.ToLower(name)]
	return !forbidden
}

func decodeProtocolSign(value any) (ProtocolSignResult, error) {
	object, ok := value.(map[string]any)
	if !ok {
		return ProtocolSignResult{}, shapeError("signRequest return value: expected object")
	}
	for key := range object {
		switch key {
		case "stringToSign", "algorithm", "encoding", "header", "prefix", "headers", "error":
		default:
			return ProtocolSignResult{}, shapeError("signRequest return value: unknown field %q", key)
		}
	}
	if raw, ok := object["error"]; ok && raw != nil {
		message, _ := raw.(string)
		if strings.TrimSpace(message) == "" {
			message = "signRequest failed"
		}
		return ProtocolSignResult{}, shapeError("%s", strings.TrimSpace(message))
	}
	var out ProtocolSignResult
	text, ok := object["stringToSign"].(string)
	if !ok || text == "" {
		return out, shapeError("stringToSign: expected non-empty string")
	}
	if len(text) > MaxStringToSignBytes {
		return out, shapeError("stringToSign: longer than %d bytes", MaxStringToSignBytes)
	}
	out.StringToSign = text
	algorithm, _ := object["algorithm"].(string)
	if !contains(SignAlgorithms, algorithm) {
		return out, shapeError("algorithm: %q is not one of %s", algorithm, strings.Join(SignAlgorithms, ","))
	}
	out.Algorithm = algorithm
	out.Encoding = "hex"
	if raw, ok := object["encoding"]; ok && raw != nil {
		encoding, _ := raw.(string)
		if !contains(SignEncodings, encoding) {
			return out, shapeError("encoding: %q is not one of %s", encoding, strings.Join(SignEncodings, ","))
		}
		out.Encoding = encoding
	}
	header, _ := object["header"].(string)
	header = strings.TrimSpace(header)
	if !validSignHeaderName(header) {
		return out, shapeError("header: %q is not allowed", header)
	}
	out.Header = textproto.CanonicalMIMEHeaderKey(header)
	if raw, ok := object["prefix"]; ok && raw != nil {
		prefix, ok := raw.(string)
		if !ok || len(prefix) > 128 || strings.ContainsAny(prefix, "\r\n") {
			return out, shapeError("prefix: expected a short single-line string")
		}
		out.Prefix = prefix
	}
	extra, err := stringMap(object["headers"], "headers")
	if err != nil {
		return out, err
	}
	for name, value := range extra {
		if !validSignHeaderName(name) || strings.EqualFold(name, header) {
			return out, shapeError("headers.%s: not allowed", name)
		}
		if strings.ContainsAny(value, "\r\n") {
			return out, shapeError("headers.%s: must be a single line", name)
		}
	}
	out.Headers = extra
	return out, nil
}

func decodeProtocolError(value any) (ProtocolErrorResult, error) {
	object, ok := value.(map[string]any)
	if !ok {
		return ProtocolErrorResult{}, shapeError("parseError return value: expected object")
	}
	var out ProtocolErrorResult
	if err := decodeInto(object, &out, ""); err != nil {
		return ProtocolErrorResult{}, err
	}
	return out, nil
}
