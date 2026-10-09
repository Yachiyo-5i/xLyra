package jsplugin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// SteppedDecision is one hook return of a stepped kind other than quota_probe:
// the next request, the final result, or an error. Result holds a
// *ModelListResult, *CredentialCheckResult or *SiteDetectResult.
type SteppedDecision struct {
	Request     *ProbeRequest
	Result      any
	Error       string
	Logs        []LogEntry
	DroppedLogs int
}

func newSteppedResult(kind string) any {
	switch kind {
	case KindModelList:
		return &ModelListResult{}
	case KindCredentialCheck:
		return &CredentialCheckResult{}
	case KindSiteDetect:
		return &SiteDetectResult{}
	}
	return nil
}

// CallStepped runs the hook of a model_list, credential_check or site_detect
// plugin once. steps are the exchanges finished so far.
func (p *Plugin) CallStepped(ctx context.Context, probeCtx ProbeContext, steps []ProbeStep) (SteppedDecision, error) {
	spec, ok := lookupKind(p.Manifest.Kind)
	if !ok || spec.Family != FamilyStepped || p.Manifest.Kind == KindQuotaProbe {
		return SteppedDecision{}, fmt.Errorf("kind %q is not a stepped kind", p.Manifest.Kind)
	}
	hook := spec.Hooks[0]
	ctxValue := probeCtx.asMap()
	if p.Manifest.Kind == KindSiteDetect {
		ctxValue = encodeValue(DetectContext{BaseURL: probeCtx.BaseURL, Now: probeCtx.Now}).(map[string]any)
	}
	out, logs, dropped, err := p.callHook(ctx, hook, spec.Timeout, len(steps), ctxValue, stepsAsValue(steps))
	if err != nil {
		return SteppedDecision{Logs: logs, DroppedLogs: dropped}, err
	}
	decision, err := decodeSteppedDecision(out, p.Manifest.Kind)
	if err != nil {
		return SteppedDecision{Logs: logs, DroppedLogs: dropped}, annotate(p, err, hook, len(steps), 0)
	}
	decision.Logs, decision.DroppedLogs = logs, dropped
	return decision, nil
}

func decodeSteppedDecision(value any, kind string) (SteppedDecision, error) {
	object, ok := value.(map[string]any)
	if !ok {
		return SteppedDecision{}, shapeError("hook return value: expected object")
	}
	for key := range object {
		switch key {
		case "request", "result", "error":
		default:
			return SteppedDecision{}, shapeError("hook return value: unknown field %q", key)
		}
	}
	_, hasRequest := object["request"]
	_, hasResult := object["result"]
	_, hasError := object["error"]
	set := 0
	for _, present := range []bool{hasRequest, hasResult, hasError} {
		if present {
			set++
		}
	}
	if set != 1 {
		return SteppedDecision{}, shapeError("hook return value: expected exactly one of request, result, error")
	}
	switch {
	case hasError:
		message, ok := object["error"].(string)
		if !ok || message == "" {
			return SteppedDecision{}, shapeError("error: expected non-empty string")
		}
		return SteppedDecision{Error: message}, nil
	case hasRequest:
		request, err := decodeRequest(object["request"])
		if err != nil {
			return SteppedDecision{}, err
		}
		return SteppedDecision{Request: &request}, nil
	}
	result := newSteppedResult(kind)
	if err := decodeInto(object["result"], result, "result"); err != nil {
		return SteppedDecision{}, err
	}
	return SteppedDecision{Result: result}, nil
}

// CallErrorClassify runs an error_classifier plugin.
func (p *Plugin) CallErrorClassify(ctx context.Context, c ErrorClassifyContext, in ErrorClassifyInput) (ErrorClassifyResult, error) {
	var out ErrorClassifyResult
	if err := p.callOneShot(ctx, KindErrorClassifier, &out, encodeValue(c), encodeValue(in)); err != nil {
		return ErrorClassifyResult{}, err
	}
	if len(out.Reason) > 200 {
		return ErrorClassifyResult{}, annotate(p, shapeError("result.reason: longer than 200 bytes"), "classify", 0, 0)
	}
	return out, nil
}

// CallModelMetadata runs a model_metadata plugin. Every id in the result must
// be one of the input names.
func (p *Plugin) CallModelMetadata(ctx context.Context, c ModelMetadataContext, in ModelMetadataInput) (ModelMetadataResult, error) {
	var out ModelMetadataResult
	if err := p.callOneShot(ctx, KindModelMetadata, &out, encodeValue(c), encodeValue(in)); err != nil {
		return ModelMetadataResult{}, err
	}
	asked := make(map[string]bool, len(in.Models))
	for _, name := range in.Models {
		asked[name] = true
	}
	for i, entry := range out.Models {
		if !asked[entry.ID] {
			return ModelMetadataResult{}, annotate(p, shapeError("result.models[%d].id: %q was not in the input", i, entry.ID), "describeModels", 0, 0)
		}
	}
	return out, nil
}

// CallPricingParse runs a pricing_parse plugin on the JSON a site published.
func (p *Plugin) CallPricingParse(ctx context.Context, c PricingParseContext, payload any) (PricingParseResult, error) {
	var out PricingParseResult
	if err := p.callOneShot(ctx, KindPricingParse, &out, encodeValue(c), payload); err != nil {
		return PricingParseResult{}, err
	}
	return out, nil
}

// CallAutomation runs an automation plugin's handle hook for one event. The
// result is checked against the permissions the manifest declares and the
// targets the event carries; xLyra still decides whether to carry it out.
func (p *Plugin) CallAutomation(ctx context.Context, c AutomationContext, event AutomationEvent) (AutomationResult, error) {
	if c.Config == nil {
		c.Config = map[string]any{}
	}
	if c.State == nil {
		c.State = map[string]any{}
	}
	if event.Current == nil {
		event.Current = map[string]any{}
	}
	var out AutomationResult
	if err := p.callOneShot(ctx, KindAutomation, &out, encodeValue(c), encodeValue(event)); err != nil {
		return AutomationResult{}, err
	}
	if err := validateAutomationResult(&out, p.Manifest.Automation.Permissions, event.Targets); err != nil {
		return AutomationResult{}, annotate(p, err, "handle", 0, 0)
	}
	return out, nil
}

func (p *Plugin) callOneShot(ctx context.Context, kind string, out any, args ...any) error {
	spec, ok := lookupKind(kind)
	if !ok || p.Manifest.Kind != kind {
		return fmt.Errorf("plugin %s is not a %s plugin", p.Manifest.ID, kind)
	}
	hook := spec.Hooks[0]
	started := time.Now()
	value, _, _, err := p.callHook(ctx, hook, spec.Timeout, 0, args...)
	if err != nil {
		return err
	}
	if err := decodeInto(value, out, "result"); err != nil {
		return annotate(p, err, hook, 0, time.Since(started).Milliseconds())
	}
	return nil
}

// runOneShotFixture calls the hook with the fixture input and compares the result.
func (p *Plugin) runOneShotFixture(ctx context.Context, fixture Fixture) error {
	var (
		result any
		err    error
	)
	siteType := fixture.Ctx.SiteType
	switch p.Manifest.Kind {
	case KindErrorClassifier:
		var in ErrorClassifyInput
		if err = decodeFixtureInput(fixture.Input, &in); err != nil {
			return err
		}
		result, err = p.CallErrorClassify(ctx, ErrorClassifyContext{SiteType: siteType}, in)
	case KindModelMetadata:
		var in ModelMetadataInput
		if err = decodeFixtureInput(fixture.Input, &in); err != nil {
			return err
		}
		result, err = p.CallModelMetadata(ctx, ModelMetadataContext{SiteType: siteType}, in)
	case KindAutomation:
		var in AutomationEvent
		if err = decodeFixtureInput(fixture.Input, &in); err != nil {
			return err
		}
		result, err = p.CallAutomation(ctx, AutomationContext{
			Event:     in.Type,
			Now:       fixture.Ctx.Now,
			BindingID: "fixture",
			Config:    fixture.Ctx.Config,
			State:     fixture.Ctx.State,
		}, in)
	case KindPricingParse:
		if fixture.Input == nil {
			return fmt.Errorf("input is required")
		}
		result, err = p.CallPricingParse(ctx, PricingParseContext{SiteType: siteType}, fixture.Input)
	default:
		return fmt.Errorf("selftest does not support kind %q", p.Manifest.Kind)
	}
	if err != nil {
		if fixture.Expect.Error != "" && strings.Contains(err.Error(), fixture.Expect.Error) {
			return nil
		}
		return err
	}
	if fixture.Expect.Error != "" {
		return fmt.Errorf("got a result, want error %q", fixture.Expect.Error)
	}
	return diffAgainst(result, fixture.Expect.Result)
}

// decodeFixtureInput reads a fixture input into a contract struct the same way
// a hook result is read, so fixtures cannot carry shapes the host would not send.
func decodeFixtureInput(input any, out any) error {
	if input == nil {
		return fmt.Errorf("input is required")
	}
	return decodeInto(input, out, "input")
}

func (p *Plugin) runSteppedFixture(ctx context.Context, fixture Fixture) error {
	probeCtx := ProbeContext{
		SiteType:       fixture.Ctx.SiteType,
		BaseURL:        fixture.Ctx.BaseURL,
		CredentialType: fixture.Ctx.CredentialType,
		Now:            fixture.Ctx.Now,
	}
	var steps []ProbeStep
	var requests []ProbeRequest
	used := make([]bool, len(fixture.Responses))
	for step := 0; step < MaxProbeSteps; step++ {
		decision, err := p.CallStepped(ctx, probeCtx, steps)
		if err != nil {
			return err
		}
		if decision.Error != "" {
			if decision.Error != fixture.Expect.Error {
				return fmt.Errorf("error %q, want %q", decision.Error, fixture.Expect.Error)
			}
			return nil
		}
		if decision.Result != nil {
			if fixture.Expect.Error != "" {
				return fmt.Errorf("got a result, want error %q", fixture.Expect.Error)
			}
			if err := matchRequests(requests, fixture.Expect.Requests); err != nil {
				return err
			}
			return diffAgainst(decision.Result, fixture.Expect.Result)
		}
		if decision.Request == nil {
			return fmt.Errorf("hook returned an empty decision")
		}
		requests = append(requests, *decision.Request)
		response, ok := takeResponse(decision.Request, fixture.Responses, used)
		if !ok {
			return fmt.Errorf("unexpected request %s %s", decision.Request.Method, decision.Request.Path)
		}
		steps = append(steps, ProbeStep{Request: *decision.Request, Response: response})
	}
	return fmt.Errorf("hook exceeded %d steps", MaxProbeSteps)
}

// diffAgainst compares a result with the fixture expectation. Only the fields
// the fixture names are checked.
func diffAgainst(result any, want map[string]any) error {
	if want == nil {
		return fmt.Errorf("expect.result is required")
	}
	encoded, err := json.Marshal(encodeValue(result))
	if err != nil {
		return err
	}
	var got map[string]any
	if err := json.Unmarshal(encoded, &got); err != nil {
		return err
	}
	return diffValue("result", got, want)
}

// ResultToMap renders a kind's result in the plain form hooks return and
// fixtures compare against, using the JS field names.
func ResultToMap(result any) any { return encodeValue(result) }
