package gateway

import (
	"context"
	"encoding/json"
	"fmt"
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
		return gatewayBufferedResponse{StatusCode: statusCode, ContentType: contentType, Body: body}, nil
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
		StatusCode: statusCode,
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
