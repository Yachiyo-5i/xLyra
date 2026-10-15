package jsplugin

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// Fixture is one recorded probe conversation.
type Fixture struct {
	Name      string             `json:"name"`
	Ctx       fixtureContext     `json:"ctx"`
	Responses []FixtureResponse  `json:"responses"`
	Expect    fixtureExpectation `json:"expect"`
	Payload   map[string]any     `json:"payload"`
	Input     any                `json:"input"`
	Candidate fixtureCandidate   `json:"candidate"`
	Response  fixtureUpstream    `json:"response"`
}

type fixtureCandidate struct {
	SiteType      string `json:"siteType"`
	BaseURL       string `json:"baseURL"`
	UpstreamName  string `json:"upstreamName"`
	UpstreamModel string `json:"upstreamModel"`
}

type fixtureUpstream struct {
	Status int    `json:"status"`
	Body   string `json:"body"`
	JSON   any    `json:"json"`
}

type fixtureContext struct {
	SiteType       string `json:"siteType"`
	BaseURL        string `json:"baseURL"`
	CredentialType string `json:"credentialType"`
	Now            int64  `json:"now"`
	// Inputs and State stand in for what the admin picked and the binding's
	// saved state in an automation fixture.
	Inputs map[string]any `json:"inputs"`
	State  map[string]any `json:"state"`
}

type FixtureResponse struct {
	Match  FixtureMatch `json:"match"`
	Status int          `json:"status"`
	Body   string       `json:"body"`
	JSON   any          `json:"json"`
	Error  string       `json:"error"`
}

type FixtureMatch struct {
	Method     string `json:"method"`
	Path       string `json:"path"`
	PathPrefix string `json:"pathPrefix"`
}

type fixtureExpectation struct {
	Requests []FixtureMatch `json:"requests"`
	Result   map[string]any `json:"result"`
	Error    string         `json:"error"`
	Decode   map[string]any `json:"decode"`
	Request  map[string]any `json:"request"`
	Parse    map[string]any `json:"parse"`
	// Sign is compared with the signRequest result (stringToSign, algorithm, header, ...).
	Sign map[string]any `json:"sign"`
	// ParseError is compared with the parseError result (contentType, body).
	ParseError map[string]any `json:"parseError"`
}

// SelfTest runs the package fixtures inside the sandbox.
func (p *Plugin) SelfTest(ctx context.Context) error {
	if len(p.fixtures) == 0 {
		return fmt.Errorf("%s: no fixtures", p.Manifest.ID)
	}
	for _, fixture := range p.fixtures {
		if err := p.RunFixture(ctx, fixture); err != nil {
			return fmt.Errorf("%s fixture %q: %w", p.Manifest.ID, fixture.Name, err)
		}
	}
	return nil
}

// RunFixture runs one fixture and returns the first mismatch. The developer
// CLI uses it to report fixtures one by one; SelfTest stops at the first failure.
func (p *Plugin) RunFixture(ctx context.Context, fixture Fixture) error {
	switch p.Manifest.Kind {
	case KindQuotaProbe:
		return p.runProbeFixture(ctx, fixture)
	case KindProtocol:
		return p.runProtocolFixture(ctx, fixture)
	}
	spec, ok := lookupKind(p.Manifest.Kind)
	if !ok {
		return fmt.Errorf("selftest does not support kind %q", p.Manifest.Kind)
	}
	switch spec.Family {
	case FamilyStepped:
		return p.runSteppedFixture(ctx, fixture)
	case FamilyOneShot, FamilyEvent:
		return p.runOneShotFixture(ctx, fixture)
	}
	return fmt.Errorf("selftest does not support kind %q", p.Manifest.Kind)
}

func (p *Plugin) runProbeFixture(ctx context.Context, fixture Fixture) error {
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
		decision, err := p.CallProbe(ctx, probeCtx, steps)
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
			return matchResult(decision.Result, fixture.Expect.Result)
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
	return fmt.Errorf("probe exceeded %d steps", MaxProbeSteps)
}

func takeResponse(request *ProbeRequest, responses []FixtureResponse, used []bool) (ProbeResponse, bool) {
	for i, response := range responses {
		if used[i] || !response.Match.matches(request) {
			continue
		}
		used[i] = true
		body := response.Body
		payload := response.JSON
		if body == "" && payload != nil {
			encoded, err := json.Marshal(payload)
			if err == nil {
				body = string(encoded)
			}
		}
		if payload == nil && body != "" {
			_ = json.Unmarshal([]byte(body), &payload)
		}
		return ProbeResponse{
			Status:  response.Status,
			Body:    body,
			JSON:    payload,
			Error:   response.Error,
			Headers: map[string]string{},
		}, true
	}
	return ProbeResponse{}, false
}

func (m FixtureMatch) matches(request *ProbeRequest) bool {
	if m.Method != "" && !strings.EqualFold(m.Method, request.Method) {
		return false
	}
	if m.Path != "" && request.Path != m.Path {
		return false
	}
	if m.PathPrefix != "" && !strings.HasPrefix(request.Path, m.PathPrefix) {
		return false
	}
	return m.Path != "" || m.PathPrefix != ""
}

func matchRequests(got []ProbeRequest, want []FixtureMatch) error {
	if len(got) != len(want) {
		return fmt.Errorf("request count %d, want %d", len(got), len(want))
	}
	for i := range want {
		if !want[i].matches(&got[i]) {
			return fmt.Errorf("request %d was %s %s", i, got[i].Method, got[i].Path)
		}
	}
	return nil
}

func matchResult(got *ProbeResult, want map[string]any) error {
	encoded, err := json.Marshal(resultMap(got))
	if err != nil {
		return err
	}
	var actual map[string]any
	if err := json.Unmarshal(encoded, &actual); err != nil {
		return err
	}
	return diffValue("result", actual, want)
}

func resultMap(result *ProbeResult) map[string]any {
	entries := make([]any, len(result.Entries))
	for i, entry := range result.Entries {
		item := map[string]any{"label": entry.Label}
		if entry.Unit != "" {
			item["unit"] = entry.Unit
		}
		if entry.Remaining != nil {
			item["remaining"] = *entry.Remaining
		}
		if entry.Limit != nil {
			item["limit"] = *entry.Limit
		}
		if entry.Used != nil {
			item["used"] = *entry.Used
		}
		if entry.Unlimited {
			item["unlimited"] = true
		}
		if entry.ResetAt != "" {
			item["resetAt"] = entry.ResetAt
		}
		if entry.CashBalance != nil {
			item["cashBalance"] = *entry.CashBalance
		}
		if entry.VoucherBalance != nil {
			item["voucherBalance"] = *entry.VoucherBalance
		}
		if entry.GrantedBalance != nil {
			item["grantedBalance"] = *entry.GrantedBalance
		}
		if entry.ToppedUpBalance != nil {
			item["toppedUpBalance"] = *entry.ToppedUpBalance
		}
		entries[i] = item
	}
	out := map[string]any{"kind": result.Kind, "entries": entries}
	if result.Plan != "" {
		out["plan"] = result.Plan
	}
	if result.ExpiresAt != "" {
		out["expiresAt"] = result.ExpiresAt
	}
	if result.IsAvailable != nil {
		out["isAvailable"] = *result.IsAvailable
	}
	return out
}

func diffValue(path string, got, want any) error {
	switch expected := want.(type) {
	case float64:
		number, ok := asFloat(got)
		if !ok || math.Abs(number-expected) > 1e-9 {
			return fmt.Errorf("%s = %#v, want %#v", path, got, want)
		}
		return nil
	case string:
		if got != want {
			return fmt.Errorf("%s = %#v, want %#v", path, got, want)
		}
		return nil
	case bool:
		if got != want {
			return fmt.Errorf("%s = %#v, want %#v", path, got, want)
		}
		return nil
	case []any:
		items, ok := got.([]any)
		if !ok || len(items) != len(expected) {
			return fmt.Errorf("%s = %#v, want %#v", path, got, want)
		}
		for i := range expected {
			if err := diffValue(fmt.Sprintf("%s[%d]", path, i), items[i], expected[i]); err != nil {
				return err
			}
		}
		return nil
	case map[string]any:
		object, ok := got.(map[string]any)
		if !ok {
			return fmt.Errorf("%s = %#v, want object", path, got)
		}
		for key, value := range expected {
			if err := diffValue(path+"."+key, object[key], value); err != nil {
				return err
			}
		}
		return nil
	default:
		if fmt.Sprint(got) != fmt.Sprint(want) {
			return fmt.Errorf("%s = %#v, want %#v", path, got, want)
		}
		return nil
	}
}

func asFloat(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case int64:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case json.Number:
		number, err := typed.Float64()
		return number, err == nil
	default:
		return 0, false
	}
}
