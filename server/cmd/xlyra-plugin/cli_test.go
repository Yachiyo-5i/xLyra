package main

import (
	"bytes"
	"crypto/ed25519"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"xlyra/server/internal/jsplugin"
)

func newProject(t *testing.T, kind, id string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), id)
	if err := scaffold(dir, kind, id, ""); err != nil {
		t.Fatal(err)
	}
	return dir
}

// A freshly scaffolded project must pass its own fixtures and the server's
// upload checks, for every kind this build supports. A new kind without a
// working template fails here.
func TestScaffoldedProjectsPassVerify(t *testing.T) {
	for _, info := range jsplugin.SupportedKinds() {
		kind := info.Name
		t.Run(kind, func(t *testing.T) {
			dir := newProject(t, kind, "acme-"+strings.ReplaceAll(kind, "_", "-"))
			built, err := buildProject(dir)
			if err != nil {
				t.Fatal(err)
			}
			raw, _, plugin, err := built.assemble()
			if err != nil {
				t.Fatal(err)
			}
			var failed int
			failed = runFixtures(plugin, built.Fixtures, new(bytes.Buffer))
			if failed != 0 {
				t.Fatalf("%d fixtures failed", failed)
			}
			if err := verifyPackage(raw); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestBuildRejectsExternalAndDynamicImports(t *testing.T) {
	for name, line := range map[string]string{
		"bare":    `import x from "lodash"; export const z = x;`,
		"dynamic": `export const z = () => import("./other");`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := newProject(t, jsplugin.KindQuotaProbe, "imports-"+name)
			entry := filepath.Join(dir, "src", "index.ts")
			raw, _ := os.ReadFile(entry)
			if err := os.WriteFile(entry, append([]byte(line+"\n"), raw...), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := buildProject(dir); err == nil || !strings.Contains(err.Error(), "not allowed") {
				t.Fatalf("err = %v, want an import rejection", err)
			}
		})
	}
}

func TestFailingFixtureIsReported(t *testing.T) {
	dir := newProject(t, jsplugin.KindQuotaProbe, "failing-fixture")
	path := filepath.Join(dir, "fixtures", "example.json")
	raw, _ := os.ReadFile(path)
	if err := os.WriteFile(path, []byte(strings.Replace(string(raw), `"remaining": 12.5`, `"remaining": 99`, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	built, err := buildProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, _, plugin, err := built.assemble()
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if failed := runFixtures(plugin, built.Fixtures, &out); failed != 1 {
		t.Fatalf("failed = %d, want 1", failed)
	}
	if !strings.Contains(out.String(), "remaining") {
		t.Fatalf("output does not name the mismatch:\n%s", out.String())
	}
}

func TestSignedPackageVerifiesAndTamperingFails(t *testing.T) {
	dir := newProject(t, jsplugin.KindQuotaProbe, "signed-plugin")
	built, err := buildProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	raw, _, _, err := built.assemble()
	if err != nil {
		t.Fatal(err)
	}
	_, priv, _ := ed25519.GenerateKey(nil)
	signed, err := jsplugin.AttachSignature(raw, priv)
	if err != nil {
		t.Fatal(err)
	}
	pkg, _, err := loadPackage(signed)
	if err != nil || pkg.Signer == "" {
		t.Fatalf("signer = %q, err = %v", pkg.Signer, err)
	}
	if _, _, err := loadPackage(signed[:len(signed)/2]); err == nil {
		t.Fatal("truncated package was accepted")
	}
}

// build must keep .xlp files that pack wrote into dist/.
func TestWriteTreeKeepsPackages(t *testing.T) {
	root := t.TempDir()
	keep := filepath.Join(root, "x.xlp")
	if err := os.WriteFile(keep, []byte("pkg"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeTree(root, map[string][]byte{"plugin.js": []byte("a"), "fixtures/f.json": []byte("{}")}); err != nil {
		t.Fatal(err)
	}
	if err := writeTree(root, map[string][]byte{"plugin.js": []byte("b")}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatal("xlp was removed")
	}
	if _, err := os.Stat(filepath.Join(root, "fixtures")); err == nil {
		t.Fatal("stale fixtures were kept")
	}
}

func TestUsageListsSupportedKinds(t *testing.T) {
	out := captureStderr(t, usage)
	names := make([]string, 0, len(jsplugin.SupportedKinds()))
	for _, info := range jsplugin.SupportedKinds() {
		names = append(names, info.Name)
	}
	want := "[--kind " + strings.Join(names, "|") + "]"
	if !strings.Contains(out, want) || !strings.Contains(want, "site_provider") {
		t.Fatalf("usage kind list missing %q:\n%s", want, out)
	}
}

func TestKindsListsSiteProvider(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = old }()
	runKinds()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "site_provider") || !strings.Contains(out, "provider") {
		t.Fatalf("kinds output missing site_provider:\n%s", out)
	}
}

func TestInitRefusesNonEmptyDirAndReservedID(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := scaffold(dir, jsplugin.KindQuotaProbe, "valid-id", ""); err == nil {
		t.Fatal("non-empty dir was accepted")
	}
	if err := scaffold(filepath.Join(t.TempDir(), "p"), jsplugin.KindQuotaProbe, "xlyra.mine", ""); err == nil {
		t.Fatal("xlyra. prefix was accepted")
	}
}

// The SDK types are generated from the Go contract (go generate ./cmd/xlyra-plugin).
// Both copies must match the generator output, so a contract change cannot ship
// without regenerating them.
func TestSDKTypesInSync(t *testing.T) {
	want, err := jsplugin.TypeDeclarations()
	if err != nil {
		t.Fatal(err)
	}
	embedded, err := templates.ReadFile("templates/types/xlyra.d.ts")
	if err != nil {
		t.Fatal(err)
	}
	published, err := os.ReadFile(filepath.Join("..", "..", "..", "sdk", "js", "index.d.ts"))
	if err != nil {
		t.Fatal(err)
	}
	for name, got := range map[string][]byte{"templates/types/xlyra.d.ts": embedded, "sdk/js/index.d.ts": published} {
		if string(got) != want {
			t.Errorf("%s is stale; run `go generate ./cmd/xlyra-plugin` in server/", name)
		}
	}
	for _, needle := range []string{"SiteProviderProbe", "SiteProviderListModels", "SiteProviderCheck", "SiteProviderClassify", "SiteProviderRewritePayload", "SiteProviderHooks", "RewritePayloadContext"} {
		if !strings.Contains(want, needle) {
			t.Errorf("generated types missing %s", needle)
		}
	}
}

func TestRunSiteProviderHook(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "provider")
	if err := scaffold(dir, jsplugin.KindSiteProvider, "acme.example", "Example"); err != nil {
		t.Fatal(err)
	}
	source := `export const meta = { apiVersion: 1, id: "acme.example", kind: "site_provider" };
export function listModels(ctx, steps) {
  if (steps.length === 0) return { request: { method: "GET", path: "/v1/models" } };
  const rows = (steps[0].response.json && steps[0].response.json.data) || [];
  return { result: { models: rows.map((row) => ({ name: row.id })) } };
}
`
	if err := os.WriteFile(filepath.Join(dir, "src", "index.ts"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	err := executeRun([]string{dir})
	if err == nil || !strings.Contains(err.Error(), "--hook") || !strings.Contains(err.Error(), "listModels") {
		t.Fatalf("missing --hook error = %v", err)
	}
	if err := executeRun([]string{"--hook", "classify", dir}); err == nil || !strings.Contains(err.Error(), "test") {
		t.Fatalf("classify error = %v", err)
	}
	if err := executeRun([]string{"--hook", "probe", dir}); err == nil || !strings.Contains(err.Error(), "not exported") || !strings.Contains(err.Error(), "listModels") {
		t.Fatalf("unexported hook error = %v", err)
	}

	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer sk-test" {
			http.Error(w, "missing key", http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `{"data":[{"id":"demo-model"}]}`)
	}))
	defer server.Close()
	t.Setenv("SITE_KEY", "sk-test")
	out := captureStdout(t, func() {
		err = executeRun([]string{"--hook", "listModels", "--base-url", server.URL, "--key-env", "SITE_KEY", "--site-type", "acme.example/example", dir})
	})
	if err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 1 {
		t.Fatalf("upstream hits = %d, want one request then a result", hits.Load())
	}
	if !strings.Contains(out, "demo-model") {
		t.Fatalf("output = %s", out)
	}

	other := newProject(t, jsplugin.KindQuotaProbe, "acme-quota")
	if err := executeRun([]string{"--hook", "listModels", other}); err == nil || !strings.Contains(err.Error(), "only for site_provider") || !strings.Contains(err.Error(), "quota_probe") {
		t.Fatalf("other kind error = %v", err)
	}
}

func TestSiteProviderFixturesPassTestAndVerify(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "provider")
	if err := scaffold(dir, jsplugin.KindSiteProvider, "acme.example", "Example"); err != nil {
		t.Fatal(err)
	}
	source := `export const meta = { apiVersion: 1, id: "acme.example", kind: "site_provider" };
export function probe(ctx, steps) {
  if (steps.length === 0) return { request: { method: "GET", path: "/api/usage" } };
  const total = Number(steps[0].response.json.total);
  const used = Number(steps[0].response.json.used);
  return { result: { kind: "balance", entries: [{ label: "balance", remaining: total - used }] } };
}
export function listModels(ctx, steps) {
  if (steps.length === 0) return { request: { method: "GET", path: "/v1/models" } };
  const rows = (steps[0].response.json && steps[0].response.json.data) || [];
  return { result: { models: rows.map((row) => ({ name: row.id })) } };
}
export function check(ctx, steps) {
  if (steps.length === 0) return { request: { method: "GET", path: "/api/me" } };
  return { result: { status: steps[0].response.status === 200 ? "ok" : "invalid" } };
}
export function classify(ctx, input) {
  return { class: input.status === 429 ? "limited" : "unknown" };
}
export function rewritePayload(ctx, payload) {
  const next = Object.assign({}, payload);
  next.model = ctx.model;
  next.stream = ctx.stream;
  return next;
}
`
	if err := os.WriteFile(filepath.Join(dir, "src", "index.ts"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "fixtures"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"probe.json": `{
  "name": "probe ok",
  "hook": "probe",
  "ctx": {"siteType": "acme.example/example", "baseURL": "https://api.example.com", "credentialType": "api_key", "now": 1},
  "responses": [{"match": {"method": "GET", "path": "/api/usage"}, "status": 200, "json": {"total": 10, "used": 4}}],
  "expect": {"requests": [{"path": "/api/usage"}], "result": {"kind": "balance", "entries": [{"label": "balance", "remaining": 6}]}}
}`,
		"list-models.json": `{
  "name": "list models",
  "hook": "listModels",
  "ctx": {"siteType": "acme.example/example", "baseURL": "https://api.example.com", "credentialType": "api_key"},
  "responses": [{"match": {"method": "GET", "path": "/v1/models"}, "status": 200, "json": {"data": [{"id": "demo"}]}}],
  "expect": {"requests": [{"path": "/v1/models"}], "result": {"models": [{"name": "demo"}]}}
}`,
		"check.json": `{
  "name": "check ok",
  "hook": "check",
  "ctx": {"siteType": "acme.example/example", "baseURL": "https://api.example.com", "credentialType": "api_key"},
  "responses": [{"match": {"path": "/api/me"}, "status": 200, "json": {}}],
  "expect": {"requests": [{"path": "/api/me"}], "result": {"status": "ok"}}
}`,
		"classify.json": `{
  "name": "classify limited",
  "hook": "classify",
  "ctx": {"siteType": "acme.example/example"},
  "input": {"status": 429, "code": "rate_limit"},
  "expect": {"result": {"class": "limited"}}
}`,
		"rewrite.json": `{
  "name": "rewrite payload",
  "hook": "rewritePayload",
  "ctx": {"siteType": "acme.example/example", "model": "m1", "stream": true, "protocol": "openai_chat"},
  "input": {"model": "m1", "stream": true, "temperature": 0.2},
  "expect": {"result": {"model": "m1", "stream": true, "temperature": 0.2}}
}`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, "fixtures", name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var passed bool
	out := captureStdout(t, func() { passed = testOnce(dir) })
	if !passed {
		t.Fatalf("test failed:\n%s", out)
	}
	for _, needle := range []string{"probe ok", "list models", "check ok", "classify limited", "rewrite payload", "hook=probe", "hook=listModels", "hook=check", "hook=classify", "hook=rewritePayload", "5 passed, 0 failed"} {
		if !strings.Contains(out, needle) {
			t.Fatalf("test output missing %q:\n%s", needle, out)
		}
	}
	built, err := buildProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	raw, _, _, err := built.assemble()
	if err != nil {
		t.Fatal(err)
	}
	verifyOut := captureStdout(t, func() { err = verifyPackage(raw) })
	if err != nil {
		t.Fatal(err)
	}
	for _, needle := range []string{"fixtures/probe.json", "probe ok", "hook=probe", "fixtures/rewrite.json", "hook=rewritePayload"} {
		if !strings.Contains(verifyOut, needle) {
			t.Fatalf("verify output missing %q:\n%s", needle, verifyOut)
		}
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = old }()
	fn()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = old }()
	fn()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestBuildProjectIncludesSiteProviderIcon(t *testing.T) {
	dir := newProject(t, "site_provider", "acme-icon")
	manifestPath := filepath.Join(dir, "manifest.json")
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	next := strings.Replace(string(raw), `"subtitle":`, `"icon": "assets/icon.png",
      "subtitle":`, 1)
	if next == string(raw) {
		t.Fatal("manifest has no subtitle to attach an icon to")
	}
	if err := os.WriteFile(manifestPath, []byte(next), 0o644); err != nil {
		t.Fatal(err)
	}
	icon := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0x00}
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "icon.png"), icon, 0o644); err != nil {
		t.Fatal(err)
	}
	built, err := buildProject(dir)
	if err != nil {
		t.Fatal(err)
	}
	if string(built.Files["assets/icon.png"]) != string(icon) {
		t.Fatalf("package icon = %d bytes", len(built.Files["assets/icon.png"]))
	}
	if _, _, _, err := built.assemble(); err != nil {
		t.Fatal(err)
	}
}
