package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"xlyra/server/internal/jsplugin"
)

const (
	runRequestTimeout = 20 * time.Second
	runBodyLimit      = 1 << 20
)

// runRun sends one real probe from this machine. The key is read from an
// environment variable and only used in the Authorization header; the plugin
// never sees it, same as on the server.
func runRun(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	baseURL := fs.String("base-url", "", "site base URL (default: manifest quotaProbe.defaultBaseURL)")
	keyEnv := fs.String("key-env", "", "environment variable holding the API key (not needed for site_detect)")
	siteType := fs.String("site-type", "custom", "ctx.siteType passed to the plugin")
	credType := fs.String("credential-type", "api_key", "ctx.credentialType passed to the plugin")
	_ = fs.Parse(args)
	dir := "."
	if fs.NArg() > 0 {
		dir = fs.Arg(0)
	}
	built, err := buildProject(dir)
	if err != nil {
		fatal(err)
	}
	_, pkg, plugin, err := built.assemble()
	if err != nil {
		fatal(err)
	}
	kind := pkg.Manifest.Kind
	spec := findKind(kind)
	if spec == nil || spec.Family != jsplugin.FamilyStepped {
		fatal(fmt.Errorf("run supports the kinds that fetch from a site (quota_probe, model_list, credential_check, site_detect), not %q", kind))
	}
	// site_detect talks to public endpoints only, so it takes no key.
	secret := ""
	if spec.Credential == "api_key" {
		if *keyEnv == "" {
			fatal(fmt.Errorf("--key-env is required"))
		}
		if secret = os.Getenv(*keyEnv); secret == "" {
			fatal(fmt.Errorf("environment variable %s is empty", *keyEnv))
		}
	}
	mode, defaultBase := pkg.Manifest.BaseURLSettings()
	canonical, err := canonicalBase(mode, defaultBase, *baseURL)
	if err != nil {
		fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	result, err := driveSteps(ctx, steppedCaller(plugin, kind), jsplugin.ProbeContext{
		SiteType:       *siteType,
		BaseURL:        canonical,
		CredentialType: *credType,
		Now:            time.Now().UnixMilli(),
	}, secret)
	if err != nil {
		fatal(err)
	}
	out, _ := json.MarshalIndent(result, "", "  ")
	fmt.Fprintln(os.Stdout, string(out))
}

func findKind(name string) *jsplugin.KindInfo {
	for _, info := range jsplugin.SupportedKinds() {
		if info.Name == name {
			return &info
		}
	}
	return nil
}

// stepOutcome is one hook return, whatever the kind.
type stepOutcome struct {
	Request *jsplugin.ProbeRequest
	Result  any
	Error   string
	Logs    []jsplugin.LogEntry
}

type stepCaller func(ctx context.Context, probeCtx jsplugin.ProbeContext, steps []jsplugin.ProbeStep) (stepOutcome, error)

func steppedCaller(plugin *jsplugin.Plugin, kind string) stepCaller {
	return func(ctx context.Context, probeCtx jsplugin.ProbeContext, steps []jsplugin.ProbeStep) (stepOutcome, error) {
		if kind == jsplugin.KindQuotaProbe {
			decision, err := plugin.CallProbe(ctx, probeCtx, steps)
			outcome := stepOutcome{Request: decision.Request, Error: decision.Error, Logs: decision.Logs}
			if decision.Result != nil {
				outcome.Result = resultMap(decision.Result)
			}
			return outcome, err
		}
		decision, err := plugin.CallStepped(ctx, probeCtx, steps)
		outcome := stepOutcome{Request: decision.Request, Error: decision.Error, Logs: decision.Logs}
		if decision.Result != nil {
			outcome.Result = jsplugin.ResultToMap(decision.Result)
		}
		return outcome, err
	}
}

func driveSteps(ctx context.Context, call stepCaller, probeCtx jsplugin.ProbeContext, secret string) (any, error) {
	client := http.Client{Timeout: runRequestTimeout}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		return jsplugin.SameOrigin(probeCtx.BaseURL, req.URL.String())
	}
	var steps []jsplugin.ProbeStep
	for step := 0; step < jsplugin.MaxProbeSteps; step++ {
		outcome, err := call(ctx, probeCtx, steps)
		logs := outcome.Logs
		if callErr, ok := err.(*jsplugin.CallError); ok {
			logs = callErr.Logs
		}
		for _, entry := range logs {
			fmt.Fprintf(os.Stderr, "[plugin %s] %s %v\n", entry.Level, entry.Message, entry.Fields)
		}
		if err != nil {
			return nil, err
		}
		if outcome.Error != "" {
			return nil, fmt.Errorf("plugin error: %s", outcome.Error)
		}
		if outcome.Result != nil {
			return outcome.Result, nil
		}
		if outcome.Request == nil {
			return nil, fmt.Errorf("hook returned an empty decision")
		}
		if step == jsplugin.MaxProbeSteps-1 {
			break
		}
		response, err := doRequest(ctx, &client, probeCtx.BaseURL, secret, outcome.Request)
		if err != nil {
			return nil, err
		}
		steps = append(steps, jsplugin.ProbeStep{Request: *outcome.Request, Response: response})
	}
	return nil, fmt.Errorf("hook exceeded %d steps", jsplugin.MaxProbeSteps)
}

func doRequest(ctx context.Context, client *http.Client, base, secret string, request *jsplugin.ProbeRequest) (jsplugin.ProbeResponse, error) {
	headers, err := jsplugin.FilterRequestHeaders(request.Headers)
	if err != nil {
		return jsplugin.ProbeResponse{}, err
	}
	target, err := jsplugin.ResolveProbeURL(base, request.Path, request.Query)
	if err != nil {
		return jsplugin.ProbeResponse{}, fmt.Errorf("%s: %w", jsplugin.KindURLRejected, err)
	}
	var body io.Reader
	if request.Method != http.MethodGet && request.Method != http.MethodHead && request.Body != nil {
		encoded, err := encodeBody(request.Body)
		if err != nil {
			return jsplugin.ProbeResponse{}, err
		}
		body = bytes.NewReader(encoded)
		if _, ok := request.Body.(string); !ok {
			if _, exists := headers["Content-Type"]; !exists {
				headers["Content-Type"] = "application/json"
			}
		}
	}
	httpReq, err := http.NewRequestWithContext(ctx, request.Method, target, body)
	if err != nil {
		return jsplugin.ProbeResponse{}, err
	}
	for key, value := range headers {
		httpReq.Header.Set(key, value)
	}
	if secret != "" {
		httpReq.Header.Set("Authorization", "Bearer "+secret)
	}
	if httpReq.Header.Get("Accept") == "" {
		httpReq.Header.Set("Accept", "application/json")
	}
	fmt.Fprintf(os.Stderr, "-> %s %s\n", request.Method, target)
	resp, err := client.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return jsplugin.ProbeResponse{}, ctx.Err()
		}
		fmt.Fprintf(os.Stderr, "<- transport error: %v\n", err)
		return jsplugin.ProbeResponse{Status: 0, Error: err.Error()}, nil
	}
	defer func() { _ = resp.Body.Close() }()
	buf, err := io.ReadAll(io.LimitReader(resp.Body, runBodyLimit+1))
	if err != nil {
		return jsplugin.ProbeResponse{}, err
	}
	if len(buf) > runBodyLimit {
		return jsplugin.ProbeResponse{}, fmt.Errorf("%s: response is too large", jsplugin.KindResponseTooLarge)
	}
	fmt.Fprintf(os.Stderr, "<- %d (%d bytes)\n", resp.StatusCode, len(buf))
	var payload any
	if json.Unmarshal(buf, &payload) != nil {
		payload = nil
	}
	return jsplugin.ProbeResponse{
		Status:  resp.StatusCode,
		Headers: jsplugin.FilterResponseHeaders(resp.Header),
		Body:    string(buf),
		JSON:    payload,
	}, nil
}

func encodeBody(body any) ([]byte, error) {
	var encoded []byte
	if text, ok := body.(string); ok {
		encoded = []byte(text)
	} else {
		var err error
		if encoded, err = json.Marshal(body); err != nil {
			return nil, err
		}
	}
	if len(encoded) > jsplugin.MaxProbeRequestBody {
		return nil, fmt.Errorf("request body is too large")
	}
	return encoded, nil
}

// canonicalBase mirrors the server's baseURLMode handling for the modes a
// manifest can declare.
func canonicalBase(mode, defaultBase, baseURL string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = strings.TrimRight(strings.TrimSpace(defaultBase), "/")
	}
	if base == "" {
		return "", fmt.Errorf("--base-url is required (the manifest has no defaultBaseURL)")
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid base URL %q", base)
	}
	switch mode {
	case "origin":
		return parsed.Scheme + "://" + parsed.Host, nil
	case "trim_v1":
		return strings.TrimSuffix(base, "/v1"), nil
	case "trim_v1_fold":
		if strings.HasSuffix(strings.ToLower(base), "/v1") {
			return base[:len(base)-len("/v1")], nil
		}
		return base, nil
	default:
		return base, nil
	}
}

func resultMap(result *jsplugin.ProbeResult) map[string]any {
	out := map[string]any{"kind": result.Kind, "entries": []map[string]any{}}
	if result.Plan != "" {
		out["plan"] = result.Plan
	}
	if result.ExpiresAt != "" {
		out["expiresAt"] = result.ExpiresAt
	}
	if result.IsAvailable != nil {
		out["isAvailable"] = *result.IsAvailable
	}
	entries := make([]map[string]any, 0, len(result.Entries))
	for _, entry := range result.Entries {
		item := map[string]any{"label": entry.Label}
		if entry.Unit != "" {
			item["unit"] = entry.Unit
		}
		if entry.ResetAt != "" {
			item["resetAt"] = entry.ResetAt
		}
		if entry.Unlimited {
			item["unlimited"] = true
		}
		for key, value := range map[string]*float64{
			"remaining": entry.Remaining, "limit": entry.Limit, "used": entry.Used,
			"cashBalance": entry.CashBalance, "voucherBalance": entry.VoucherBalance,
			"grantedBalance": entry.GrantedBalance, "toppedUpBalance": entry.ToppedUpBalance,
		} {
			if value != nil {
				item[key] = *value
			}
		}
		entries = append(entries, item)
	}
	out["entries"] = entries
	return out
}
