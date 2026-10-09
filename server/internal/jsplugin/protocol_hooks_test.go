package jsplugin

import (
	"context"
	"strings"
	"testing"
)

const signPluginSource = `
export function decodeRequest(ctx, payload) { return { model: payload.model }; }
export function buildRequest(ctx, payload) {
  return { path: "/v1/run", headers: { "X-App": "demo" }, payload: { model: payload.model, q: payload.q } };
}
export function parseResponse(ctx, resp) { return { passthrough: true }; }
export function signRequest(ctx, req) {
  return {
    stringToSign: req.method + "\n" + req.url + "\n" + req.headers["X-App"] + "\n" + req.body,
    algorithm: "hmac-sha256",
    encoding: "base64",
    header: "x-signature",
    prefix: "SIG ",
    headers: { "X-Stamp": "1700000000" },
  };
}
export function parseError(ctx, resp) {
  return { contentType: "application/json", body: JSON.stringify({ error: { message: resp.json.msg } }) };
}
`

func protocolHookPlugin(t *testing.T, auth, source string, fixtures []Fixture) (*Plugin, error) {
	t.Helper()
	manifest := Manifest{
		ID: "acme.signed", Name: "signed", Version: "1.0.0", APIVersion: 1, HostAPI: 1, Kind: KindProtocol,
		Protocol: ProtocolSection{
			Name: "signed", DownstreamPath: "/v1/plugins/signed/run", EndpointType: "plugin:acme.signed",
			Auth: auth, DefaultBaseURL: "https://api.example.com",
		},
		SHA256: map[string]string{"plugin.js": hashSource(meta("acme.signed", KindProtocol) + source)},
	}
	return NewPlugin(manifest, meta("acme.signed", KindProtocol)+source, fixtures, 1)
}

func TestProtocolOptionalHooksAreOptional(t *testing.T) {
	plain := `
export function decodeRequest(ctx, payload) { return { model: payload.model }; }
export function buildRequest(ctx, payload) { return { path: "/v1/run", payload: {} }; }
export function parseResponse(ctx, resp) { return { passthrough: true }; }
`
	plugin, err := protocolHookPlugin(t, "bearer", plain, nil)
	if err != nil {
		t.Fatalf("plugin without optional hooks: %v", err)
	}
	if plugin.HasHook(HookSignRequest) || plugin.HasHook(HookParseError) {
		t.Fatal("optional hooks reported present")
	}
}

func TestSignRequestNeedsAuthNone(t *testing.T) {
	_, err := protocolHookPlugin(t, "bearer", signPluginSource, nil)
	if err == nil || !strings.Contains(err.Error(), `protocol.auth "none"`) {
		t.Fatalf("err = %v, want auth none requirement", err)
	}
	if _, err := protocolHookPlugin(t, "none", signPluginSource, nil); err != nil {
		t.Fatalf("auth none: %v", err)
	}
}

func TestSignRequestResult(t *testing.T) {
	plugin, err := protocolHookPlugin(t, "none", signPluginSource, nil)
	if err != nil {
		t.Fatal(err)
	}
	signed, _, _, err := plugin.CallSignRequest(context.Background(), ProtocolBuildContext{}, ProtocolSignInput{
		Method: "POST", URL: "https://api.example.com/v1/run", Headers: map[string]string{"X-App": "demo"}, Body: `{"q":"hi"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "POST\nhttps://api.example.com/v1/run\ndemo\n{\"q\":\"hi\"}"
	if signed.StringToSign != want || signed.Header != "X-Signature" || signed.Prefix != "SIG " ||
		signed.Algorithm != "hmac-sha256" || signed.Encoding != "base64" || signed.Headers["X-Stamp"] != "1700000000" {
		t.Fatalf("signed = %#v", signed)
	}
}

func TestSignResultIsStrict(t *testing.T) {
	good := map[string]any{"stringToSign": "x", "algorithm": "hmac-sha256", "header": "X-Sig"}
	if _, err := decodeProtocolSign(good); err != nil {
		t.Fatalf("good: %v", err)
	}
	cases := map[string]func(map[string]any){
		"algorithm":         func(m map[string]any) { m["algorithm"] = "md5" },
		"encoding":          func(m map[string]any) { m["encoding"] = "rot13" },
		"empty string":      func(m map[string]any) { m["stringToSign"] = "" },
		"unknown field":     func(m map[string]any) { m["key"] = "leak" },
		"host header":       func(m map[string]any) { m["header"] = "Host" },
		"cookie header":     func(m map[string]any) { m["header"] = "Cookie" },
		"bad header name":   func(m map[string]any) { m["header"] = "X Sig" },
		"newline prefix":    func(m map[string]any) { m["prefix"] = "a\r\nX: y" },
		"extra same as sig": func(m map[string]any) { m["headers"] = map[string]any{"X-Sig": "1"} },
		"extra forbidden":   func(m map[string]any) { m["headers"] = map[string]any{"Cookie": "1"} },
		"too long":          func(m map[string]any) { m["stringToSign"] = strings.Repeat("a", MaxStringToSignBytes+1) },
	}
	for name, mutate := range cases {
		input := map[string]any{}
		for k, v := range good {
			input[k] = v
		}
		mutate(input)
		if _, err := decodeProtocolSign(input); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := decodeProtocolSign(map[string]any{"stringToSign": "x", "algorithm": "hmac-sha1", "header": "Authorization"}); err != nil {
		t.Fatalf("Authorization may carry a signature: %v", err)
	}
}

func TestParseErrorHookAndFixtures(t *testing.T) {
	fixtures := []Fixture{
		{
			Name:      "signed request",
			Payload:   map[string]any{"model": "m", "q": "hi"},
			Candidate: fixtureCandidate{BaseURL: "https://api.example.com"},
			Expect: fixtureExpectation{Sign: map[string]any{
				"stringToSign": "POST\nhttps://api.example.com/v1/run\ndemo\n{\"model\":\"m\",\"q\":\"hi\"}",
				"header":       "X-Signature",
				"algorithm":    "hmac-sha256",
			}},
		},
		{
			Name:     "upstream error",
			Payload:  map[string]any{"model": "m", "q": "hi"},
			Response: fixtureUpstream{Status: 429, JSON: map[string]any{"msg": "slow down"}},
			Expect: fixtureExpectation{ParseError: map[string]any{
				"body": `{"error":{"message":"slow down"}}`, "contentType": "application/json",
			}},
		},
	}
	plugin, err := protocolHookPlugin(t, "none", signPluginSource, fixtures)
	if err != nil {
		t.Fatal(err)
	}
	if err := plugin.SelfTest(context.Background()); err != nil {
		t.Fatalf("SelfTest: %v", err)
	}

	bad := []Fixture{fixtures[0]}
	bad[0].Expect.Sign = map[string]any{"header": "X-Other"}
	plugin, err = protocolHookPlugin(t, "none", signPluginSource, bad)
	if err != nil {
		t.Fatal(err)
	}
	if err := plugin.SelfTest(context.Background()); err == nil {
		t.Fatal("wrong sign expectation passed")
	}
}

func TestSignedFixtureWithoutHookFails(t *testing.T) {
	plain := `
export function decodeRequest(ctx, payload) { return { model: payload.model }; }
export function buildRequest(ctx, payload) { return { path: "/v1/run", payload: {} }; }
export function parseResponse(ctx, resp) { return { passthrough: true }; }
`
	fixtures := []Fixture{{Name: "x", Payload: map[string]any{"model": "m"}, Expect: fixtureExpectation{Sign: map[string]any{"header": "X"}}}}
	plugin, err := protocolHookPlugin(t, "bearer", plain, fixtures)
	if err != nil {
		t.Fatal(err)
	}
	if err := plugin.SelfTest(context.Background()); err == nil {
		t.Fatal("expect.sign without a signRequest hook should fail")
	}
}
