package jsplugin

import (
	"context"
	"encoding/json"
	"fmt"
)

func (p *Plugin) runProtocolFixture(ctx context.Context, fixture Fixture) error {
	if fixture.Payload == nil {
		return fmt.Errorf("payload is required")
	}
	endpointCtx := ProtocolEndpointContext{DownstreamPath: p.Manifest.Protocol.DownstreamPath}
	decoded, failure, _, _, err := p.CallDecodeRequest(ctx, endpointCtx, fixture.Payload)
	if err != nil {
		return err
	}
	if fixture.Expect.Error != "" {
		if failure == nil {
			return fmt.Errorf("got a decode result, want error %q", fixture.Expect.Error)
		}
		if failure.Message != fixture.Expect.Error {
			return fmt.Errorf("decode error %q, want %q", failure.Message, fixture.Expect.Error)
		}
		return nil
	}
	if failure != nil {
		return fmt.Errorf("decode failed: %s", failure.Message)
	}
	if want, ok := fixture.Expect.Decode["model"].(string); ok && decoded.Model != want {
		return fmt.Errorf("decode model = %q, want %q", decoded.Model, want)
	}
	buildCtx := protocolBuildContextFromFixture(fixture)
	built, _, _, err := p.CallBuildRequest(ctx, buildCtx, fixture.Payload)
	if err != nil {
		return err
	}
	if err := matchProtocolRequest(built, fixture.Expect.Request); err != nil {
		return err
	}
	if err := p.runSignFixture(ctx, buildCtx, built, fixture); err != nil {
		return err
	}
	if fixture.Response.Status == 0 {
		return nil
	}
	body := fixture.Response.Body
	if body == "" && fixture.Response.JSON != nil {
		encoded, err := json.Marshal(fixture.Response.JSON)
		if err != nil {
			return err
		}
		body = string(encoded)
	}
	input := ProtocolParseInput{
		Status:  fixture.Response.Status,
		Body:    body,
		JSON:    fixture.Response.JSON,
		Headers: map[string]string{},
	}
	if fixture.Response.Status < 200 || fixture.Response.Status >= 300 {
		return p.runParseErrorFixture(ctx, buildCtx, input, fixture)
	}
	parsed, _, _, err := p.CallParseResponse(ctx, buildCtx, input)
	if err != nil {
		return err
	}
	return matchProtocolParse(parsed, fixture.Expect.Parse)
}

func protocolBuildContextFromFixture(fixture Fixture) ProtocolBuildContext {
	return ProtocolBuildContext{
		Candidate: ProtocolCandidateContext{
			SiteType:      fixture.Candidate.SiteType,
			BaseURL:       fixture.Candidate.BaseURL,
			UpstreamName:  fixture.Candidate.UpstreamName,
			UpstreamModel: fixture.Candidate.UpstreamModel,
		},
	}
}

func matchProtocolRequest(got ProtocolBuiltRequest, want map[string]any) error {
	if want == nil {
		return nil
	}
	if path, ok := want["path"].(string); ok && got.Path != path {
		return fmt.Errorf("request path = %q, want %q", got.Path, path)
	}
	if raw, ok := want["payload"].(map[string]any); ok {
		if got.Payload["model"] != raw["model"] {
			return fmt.Errorf("request payload model = %#v, want %#v", got.Payload["model"], raw["model"])
		}
	}
	return nil
}

func matchProtocolParse(got ProtocolParsedResponse, want map[string]any) error {
	if want == nil {
		return nil
	}
	if value, ok := want["passthrough"].(bool); ok && got.Passthrough != value {
		return fmt.Errorf("passthrough = %v, want %v", got.Passthrough, value)
	}
	if raw, ok := want["usage"].(map[string]any); ok {
		if prompt, ok := raw["prompt_tokens"].(float64); ok && got.Usage.PromptTokens != int(prompt) {
			return fmt.Errorf("prompt_tokens = %d, want %d", got.Usage.PromptTokens, int(prompt))
		}
		if completion, ok := raw["completion_tokens"].(float64); ok && got.Usage.CompletionTokens != int(completion) {
			return fmt.Errorf("completion_tokens = %d, want %d", got.Usage.CompletionTokens, int(completion))
		}
	}
	return nil
}

// runSignFixture shows signRequest the request buildRequest produced, in the
// form the host sends it: the plugin's headers and the body as sorted-key JSON.
func (p *Plugin) runSignFixture(ctx context.Context, buildCtx ProtocolBuildContext, built ProtocolBuiltRequest, fixture Fixture) error {
	if !p.HasHook(HookSignRequest) {
		if fixture.Expect.Sign != nil {
			return fmt.Errorf("expect.sign is set but the plugin does not export %s", HookSignRequest)
		}
		return nil
	}
	body, err := json.Marshal(built.Payload)
	if err != nil {
		return err
	}
	base := buildCtx.Candidate.BaseURL
	if base == "" {
		base = p.Manifest.Protocol.DefaultBaseURL
	}
	url, err := ResolveProbeURL(base, built.Path, nil)
	if err != nil {
		return err
	}
	headers, err := FilterRequestHeaders(built.Headers)
	if err != nil {
		return err
	}
	if headers == nil {
		headers = map[string]string{}
	}
	headers["Content-Type"] = "application/json"
	signed, _, _, err := p.CallSignRequest(ctx, buildCtx, ProtocolSignInput{
		Method: p.Manifest.Protocol.Method, URL: url, Headers: headers, Body: string(body),
	})
	if err != nil {
		return err
	}
	return matchProtocolSign(signed, fixture.Expect.Sign)
}

func matchProtocolSign(got ProtocolSignResult, want map[string]any) error {
	for key, expected := range want {
		var actual any
		switch key {
		case "stringToSign":
			actual = got.StringToSign
		case "algorithm":
			actual = got.Algorithm
		case "encoding":
			actual = got.Encoding
		case "header":
			actual = got.Header
		case "prefix":
			actual = got.Prefix
		case "headers":
			if !equalJSON(got.Headers, expected) {
				return fmt.Errorf("sign headers = %v, want %v", got.Headers, expected)
			}
			continue
		default:
			return fmt.Errorf("expect.sign: unknown field %q", key)
		}
		if actual != expected {
			return fmt.Errorf("sign %s = %q, want %q", key, actual, expected)
		}
	}
	return nil
}

func (p *Plugin) runParseErrorFixture(ctx context.Context, buildCtx ProtocolBuildContext, input ProtocolParseInput, fixture Fixture) error {
	if !p.HasHook(HookParseError) {
		if fixture.Expect.ParseError != nil {
			return fmt.Errorf("expect.parseError is set but the plugin does not export %s", HookParseError)
		}
		return nil
	}
	parsed, _, _, err := p.CallParseError(ctx, buildCtx, input)
	if err != nil {
		return err
	}
	for key, expected := range fixture.Expect.ParseError {
		switch key {
		case "body":
			if parsed.Body != expected {
				return fmt.Errorf("parseError body = %q, want %q", parsed.Body, expected)
			}
		case "contentType":
			if parsed.ContentType != expected {
				return fmt.Errorf("parseError contentType = %q, want %q", parsed.ContentType, expected)
			}
		default:
			return fmt.Errorf("expect.parseError: unknown field %q", key)
		}
	}
	return nil
}

func equalJSON(a, b any) bool {
	left, errA := json.Marshal(a)
	right, errB := json.Marshal(b)
	return errA == nil && errB == nil && string(left) == string(right)
}
