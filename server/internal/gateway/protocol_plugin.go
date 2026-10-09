package gateway

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"net/http"
	"strings"
	"time"

	"xlyra/server/internal/adapter"
	"xlyra/server/internal/jsplugin"
	routeengine "xlyra/server/internal/router"
)

type jsProtocolAdapter struct {
	plugin    *jsplugin.Plugin
	ctx       context.Context
	request   gatewayRequest
	candidate routeengine.Candidate

	built     *jsplugin.ProtocolBuiltRequest
	buildErr  error
	buildDone bool
}

func newJSProtocolAdapter(ctx context.Context, plugin *jsplugin.Plugin, request gatewayRequest, candidate routeengine.Candidate) *jsProtocolAdapter {
	return &jsProtocolAdapter{plugin: plugin, ctx: ctx, request: request, candidate: candidate}
}

func (a *jsProtocolAdapter) ProtocolName() string {
	return a.plugin.Manifest.Protocol.Name
}

func (a *jsProtocolAdapter) CredentialEndpointTypes() []string {
	return []string{a.plugin.Manifest.Protocol.EndpointType}
}

func (a *jsProtocolAdapter) UpstreamMethod() string {
	return a.plugin.Manifest.Protocol.Method
}

func (a *jsProtocolAdapter) ApplyUpstreamAuth(req *http.Request, upstreamKey string) {
	section := a.plugin.Manifest.Protocol
	switch {
	case section.Auth == "none":
		return
	case section.Auth == "bearer":
		req.Header.Set("Authorization", "Bearer "+upstreamKey)
	default:
		header := section.AuthHeader()
		if header != "" {
			req.Header.Set(header, upstreamKey)
		}
	}
}

func (a *jsProtocolAdapter) ApplyUpstreamHeaders(req *http.Request, _ string, _ string, _ bool) {
	built, err := a.ensureBuilt()
	if err != nil || built == nil {
		return
	}
	filtered, err := jsplugin.FilterRequestHeaders(built.Headers)
	if err != nil {
		return
	}
	authHeader := a.plugin.Manifest.Protocol.AuthHeader()
	for key, value := range filtered {
		if authHeader != "" && strings.EqualFold(key, authHeader) {
			continue
		}
		req.Header.Set(key, value)
	}
}

func (a *jsProtocolAdapter) BuildUpstreamPayload(_ gatewayRequest, _ routeengine.Candidate) (map[string]any, error) {
	built, err := a.ensureBuilt()
	if err != nil {
		return nil, err
	}
	return clonePayload(built.Payload), nil
}

func (a *jsProtocolAdapter) UpstreamPath(baseURL string) string {
	built, err := a.ensureBuilt()
	if err != nil || built == nil {
		return ""
	}
	resolvedBase := strings.TrimSpace(baseURL)
	if resolvedBase == "" {
		resolvedBase = strings.TrimSpace(a.plugin.Manifest.Protocol.DefaultBaseURL)
	}
	if resolvedBase == "" {
		resolvedBase = strings.TrimRight(strings.TrimSpace(adapter.TypeSafeDefaultBaseURL), "/")
	}
	url, err := jsplugin.ResolveProbeURL(resolvedBase, built.Path, nil)
	if err != nil {
		a.buildErr = err
		return ""
	}
	return url
}

func (a *jsProtocolAdapter) TransformBufferedResponse(statusCode int, headers http.Header, body []byte) (gatewayBufferedResponse, error) {
	contentType := strings.TrimSpace(headers.Get("Content-Type"))
	if statusCode < 200 || statusCode >= 300 {
		a.recordProtocolResult(true, false)
		return a.rewriteUpstreamError(statusCode, headers, contentType, body), nil
	}
	if len(body) > jsplugin.MaxProtocolResponseBody {
		return gatewayBufferedResponse{}, pluginFailure(fmt.Errorf("%s: upstream body exceeds plugin limit", jsplugin.KindResponseTooLarge))
	}
	var jsonBody any
	_ = json.Unmarshal(body, &jsonBody)
	input := jsplugin.ProtocolParseInput{
		Status:  statusCode,
		Headers: jsplugin.FilterResponseHeaders(headers),
		Body:    string(body),
		JSON:    jsonBody,
	}
	parsed, logs, dropped, err := a.plugin.CallParseResponse(a.ctx, a.buildContext(), input)
	jsplugin.EmitLogs(a.ctx, a.plugin, "parseResponse", 0, logs, dropped)
	if err != nil {
		a.recordProtocolResult(true, false)
		return gatewayBufferedResponse{}, pluginFailure(err)
	}
	response := gatewayBufferedResponse{
		StatusCode:  statusCode,
		ContentType: stringValue(&contentType, "application/json"),
		Body:        body,
	}
	if parsed.Passthrough {
		response.Body = body
	} else {
		if parsed.StatusCode > 0 {
			response.StatusCode = parsed.StatusCode
		}
		if parsed.ContentType != "" {
			response.ContentType = parsed.ContentType
		}
		if parsed.Body != nil {
			response.Body = parsed.Body
		}
	}
	if parsed.UsagePopulated {
		response.Usage = gatewayUsage{
			PromptTokens:     parsed.Usage.PromptTokens,
			CompletionTokens: parsed.Usage.CompletionTokens,
			TotalTokens:      parsed.Usage.TotalTokens,
		}
	}
	a.recordProtocolResult(false, false)
	return response, nil
}

// rewriteUpstreamError lets the optional parseError hook replace the body of a
// non-2xx response. The status stays as the upstream sent it so failure
// handling is unchanged, and a hook that fails leaves the original response.
func (a *jsProtocolAdapter) rewriteUpstreamError(statusCode int, headers http.Header, contentType string, body []byte) gatewayBufferedResponse {
	original := gatewayBufferedResponse{StatusCode: statusCode, ContentType: contentType, Body: body}
	if !a.plugin.HasHook(jsplugin.HookParseError) || len(body) > jsplugin.MaxProtocolResponseBody {
		return original
	}
	var jsonBody any
	_ = json.Unmarshal(body, &jsonBody)
	input := jsplugin.ProtocolErrorInput{
		Status:  statusCode,
		Headers: jsplugin.FilterResponseHeaders(headers),
		Body:    string(body),
		JSON:    jsonBody,
	}
	parsed, logs, dropped, err := a.plugin.CallParseError(a.ctx, a.buildContext(), input)
	jsplugin.EmitLogs(a.ctx, a.plugin, jsplugin.HookParseError, 0, logs, dropped)
	if err != nil {
		return original
	}
	original.Body = []byte(parsed.Body)
	if parsed.ContentType != "" {
		original.ContentType = parsed.ContentType
	}
	return original
}

// SignUpstreamRequest runs the optional signRequest hook. The plugin names the
// text to sign; the HMAC key is the site credential and stays in this function.
func (a *jsProtocolAdapter) SignUpstreamRequest(req *http.Request, body []byte, upstreamKey string) error {
	if !a.plugin.HasHook(jsplugin.HookSignRequest) {
		return nil
	}
	if len(body) > jsplugin.MaxSignBodyBytes {
		return fmt.Errorf("signRequest: request body exceeds %d bytes", jsplugin.MaxSignBodyBytes)
	}
	flat := make(map[string]string, len(req.Header))
	for name, values := range req.Header {
		flat[name] = strings.Join(values, ", ")
	}
	visible, err := jsplugin.FilterRequestHeaders(flat)
	if err != nil {
		return fmt.Errorf("signRequest: %w", err)
	}
	signed, logs, dropped, err := a.plugin.CallSignRequest(a.ctx, a.buildContext(), jsplugin.ProtocolSignInput{
		Method:  req.Method,
		URL:     req.URL.String(),
		Headers: visible,
		Body:    string(body),
	})
	jsplugin.EmitLogs(a.ctx, a.plugin, jsplugin.HookSignRequest, 0, logs, dropped)
	if err != nil {
		return fmt.Errorf("signRequest: %w", err)
	}
	signature, err := signString(signed, upstreamKey)
	if err != nil {
		return fmt.Errorf("signRequest: %w", err)
	}
	for name, value := range signed.Headers {
		req.Header.Set(name, value)
	}
	req.Header.Set(signed.Header, signed.Prefix+signature)
	return nil
}

func signString(signed jsplugin.ProtocolSignResult, key string) (string, error) {
	var newHash func() hash.Hash
	switch signed.Algorithm {
	case "hmac-sha256":
		newHash = sha256.New
	case "hmac-sha1":
		newHash = sha1.New
	case "hmac-sha512":
		newHash = sha512.New
	default:
		return "", fmt.Errorf("unsupported algorithm %q", signed.Algorithm)
	}
	mac := hmac.New(newHash, []byte(key))
	mac.Write([]byte(signed.StringToSign))
	sum := mac.Sum(nil)
	if signed.Encoding == "base64" {
		return base64.StdEncoding.EncodeToString(sum), nil
	}
	return hex.EncodeToString(sum), nil
}

func (a *jsProtocolAdapter) recordProtocolResult(failed bool, interrupted bool) {
	if a == nil || a.plugin == nil {
		return
	}
	if breaker := jsplugin.DefaultCatalog().Breaker(); breaker != nil {
		breaker.RecordProtocolCall(a.plugin, failed, interrupted)
	}
}

func (a *jsProtocolAdapter) ProxyStream(_ context.Context, _ http.ResponseWriter, _ *http.Response, _ time.Time, _ routeengine.Candidate) (streamCaptureState, bool, error) {
	return streamCaptureState{}, false, nil
}

func (a *jsProtocolAdapter) ensureBuilt() (*jsplugin.ProtocolBuiltRequest, error) {
	if a.buildDone {
		return a.built, a.buildErr
	}
	a.buildDone = true
	payload := clonePayload(a.request.Payload)
	built, logs, dropped, err := a.plugin.CallBuildRequest(a.ctx, a.buildContext(), payload)
	jsplugin.EmitLogs(a.ctx, a.plugin, "buildRequest", 0, logs, dropped)
	if err != nil {
		a.buildErr = pluginFailure(err)
		return nil, a.buildErr
	}
	a.built = &built
	return a.built, nil
}

func (a *jsProtocolAdapter) buildContext() jsplugin.ProtocolBuildContext {
	return jsplugin.ProtocolBuildContext{
		Candidate: jsplugin.ProtocolCandidateContext{
			SiteType:      a.candidate.Site.SiteType,
			BaseURL:       a.candidate.Site.BaseURL,
			UpstreamName:  a.candidate.Model.UpstreamName,
			UpstreamModel: a.candidate.Model.UpstreamName,
		},
	}
}

func pluginFailure(err error) error {
	if err == nil {
		return nil
	}
	var call *jsplugin.CallError
	if ok := errorAsCall(err, &call); ok {
		return err
	}
	return err
}

func errorAsCall(err error, target **jsplugin.CallError) bool {
	for err != nil {
		if call, ok := err.(*jsplugin.CallError); ok {
			*target = call
			return true
		}
		err = unwrapOnce(err)
	}
	return false
}

func unwrapOnce(err error) error {
	type unwrapper interface{ Unwrap() error }
	if u, ok := err.(unwrapper); ok {
		return u.Unwrap()
	}
	return nil
}
