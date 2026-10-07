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
