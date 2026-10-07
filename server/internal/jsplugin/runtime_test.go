package jsplugin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestBuiltinSelfTest(t *testing.T) {
	registry, err := LoadBuiltins()
	if err != nil {
		t.Fatalf("load builtins: %v", err)
	}
	for _, probeType := range []string{"kimi", "glm", "moonshot", "deepseek", "sub2api", "newapi", "xlyra"} {
		plugin, ok := registry.ByProbeType(probeType)
		if !ok {
			t.Fatalf("missing builtin %s", probeType)
		}
		if err := plugin.SelfTest(context.Background()); err != nil {
			t.Fatalf("selftest %s: %v", probeType, err)
		}
	}
}

func TestHeapLimitAvailable(t *testing.T) {
	if !HeapLimitAvailable() {
		t.Fatal("allocation budget and result bound must both be configured")
	}
}

func TestAllocLimitDiscardsRuntime(t *testing.T) {
	for _, source := range []string{
		`let s = "x"; for (;;) s += s;`,
		`new Array(1e8).fill(0);`,
	} {
		plugin := mustTestPlugin(t, "xlyra.test.bomb", wrapProbe(source))
		_, err := plugin.CallProbe(context.Background(), ProbeContext{}, nil)
		if err == nil || !strings.Contains(err.Error(), KindHeapLimit) {
			t.Fatalf("source %q error = %v, want %s", source, err, KindHeapLimit)
		}
		plugin.pool.mu.Lock()
		total, idle := plugin.pool.total, len(plugin.pool.idle)
		plugin.pool.mu.Unlock()
		if total != 0 || idle != 0 {
			t.Fatalf("pool after alloc limit total=%d idle=%d", total, idle)
		}
	}
}

func TestResultTooLargeKeepsRuntime(t *testing.T) {
	source := wrapProbe(`const o = { text: "x".repeat(400) }; return Array(1e5).fill(o);`)
	plugin := mustTestPlugin(t, "xlyra.test.bomb", source)
	_, err := plugin.CallProbe(context.Background(), ProbeContext{}, nil)
	if err == nil || !strings.Contains(err.Error(), KindResponseTooLarge) {
		t.Fatalf("error = %v, want %s", err, KindResponseTooLarge)
	}
	plugin.pool.mu.Lock()
	total, idle := plugin.pool.total, len(plugin.pool.idle)
	plugin.pool.mu.Unlock()
	if total != 1 || idle != 1 {
		t.Fatalf("pool after result limit total=%d idle=%d, want the runtime reused", total, idle)
	}
}

func TestPoolWaitLimitIsExhausted(t *testing.T) {
	pool, held := heldPool(t)
	pool.wait = 30 * time.Millisecond
	before := runtimeGoroutines()
	_, err := pool.Acquire(context.Background())
	var call *CallError
	if !errors.As(err, &call) || call.Kind != KindPoolExhausted {
		t.Fatalf("error = %v, want %s", err, KindPoolExhausted)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("pool exhaustion must not unwrap to a context error")
	}
	pool.Release(held, true)
	time.Sleep(20 * time.Millisecond)
	if after := runtimeGoroutines(); after > before+2 {
		t.Fatalf("goroutines before %d after %d", before, after)
	}
}

func TestPoolWaitReturnsCallerContextError(t *testing.T) {
	pool, held := heldPool(t)
	defer pool.Release(held, true)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := pool.Acquire(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want the caller deadline", err)
	}
	var call *CallError
	if errors.As(err, &call) {
		t.Fatalf("caller deadline was wrapped as %s", call.Kind)
	}
}

func TestCallProbeReportsPoolExhausted(t *testing.T) {
	plugin := mustTestPlugin(t, "xlyra.test.bomb", wrapProbe(`return { result: { kind: "balance", entries: [{ label: "balance", remaining: 1 }] } };`))
	plugin.pool.max = 1
	plugin.pool.wait = 30 * time.Millisecond
	held, err := plugin.pool.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer plugin.pool.Release(held, true)
	_, err = plugin.CallProbe(context.Background(), ProbeContext{}, nil)
	var call *CallError
	if !errors.As(err, &call) || call.Kind != KindPoolExhausted || call.PluginID != "xlyra.test.bomb" {
		t.Fatalf("error = %#v, want annotated %s", err, KindPoolExhausted)
	}
}

func TestPluginLogIsBounded(t *testing.T) {
	source := wrapProbe(`
  log.info("short", { blob: "x".repeat(4096) });
  log.warn("kept", { step: 1 });
  for (let i = 0; i < 30; i++) log.debug("line " + i);
  return { result: { kind: "balance", entries: [{ label: "balance", remaining: 1 }] } };`)
	plugin := mustTestPlugin(t, "xlyra.test.bomb", source)
	decision, err := plugin.CallProbe(context.Background(), ProbeContext{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(decision.Logs) != maxPluginLogs || decision.DroppedLogs != 12 {
		t.Fatalf("logs = %d dropped = %d", len(decision.Logs), decision.DroppedLogs)
	}
	big := decision.Logs[0]
	if big.Fields != nil || len(big.Message) > maxPluginLogBytes || !strings.HasPrefix(big.Message, `short {"blob":"xxx`) {
		t.Fatalf("oversized entry kept %d fields, message %d bytes", len(big.Fields), len(big.Message))
	}
	kept := decision.Logs[1]
	if kept.Level != "warn" || kept.Message != "kept" || kept.Fields["step"] == nil {
		t.Fatalf("small entry = %#v", kept)
	}
}

func TestClipUTF8BytesStaysWithinLimit(t *testing.T) {
	for _, value := range []string{strings.Repeat("中", 400), strings.Repeat("a", 2000), "short"} {
		got := clipUTF8Bytes(value, maxPluginLogBytes)
		if len(got) > maxPluginLogBytes || !utf8.ValidString(got) {
			t.Fatalf("clip %d bytes -> %d bytes valid=%v", len(value), len(got), utf8.ValidString(got))
		}
	}
}

func heldPool(t *testing.T) (*pool, *session) {
	t.Helper()
	program, err := compileProgram("pool.js", wrapProbe(`return { result: { kind: "balance", entries: [{ label: "balance", remaining: 1 }] } };`), "probe")
	if err != nil {
		t.Fatal(err)
	}
	pool, err := newPool(0, 1, func() (*session, error) { return newSession(program) })
	if err != nil {
		t.Fatal(err)
	}
	held, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return pool, held
}

func TestRuntimeStateIsNotShared(t *testing.T) {
	source := `let n = 0;
export const meta = { apiVersion: 1, id: "xlyra.test.state", kind: "quota_probe" };
export function probe() {
  n += 1;
  return { result: { kind: "balance", entries: [{ label: "balance", remaining: n }] } };
}`
	plugin := mustTestPlugin(t, "xlyra.test.state", source)
	program := plugin.program
	first, err := newSession(program)
	if err != nil {
		t.Fatal(err)
	}
	second, err := newSession(program)
	if err != nil {
		t.Fatal(err)
	}
	probe := program.hooks["probe"]
	one, _, _, _, err := first.call(context.Background(), time.Second, probe, []any{map[string]any{}, []any{}})
	if err != nil {
		t.Fatal(err)
	}
	two, _, _, _, err := second.call(context.Background(), time.Second, probe, []any{map[string]any{}, []any{}})
	if err != nil {
		t.Fatal(err)
	}
	again, _, _, _, err := first.call(context.Background(), time.Second, probe, []any{map[string]any{}, []any{}})
	if err != nil {
		t.Fatal(err)
	}
	if remaining(one) != 1 || remaining(two) != 1 || remaining(again) != 2 {
		t.Fatalf("runtimes leaked state: first %v second %v again %v", remaining(one), remaining(two), remaining(again))
	}
}

func remaining(value any) float64 {
	object, _ := value.(map[string]any)
	result, _ := object["result"].(map[string]any)
	entries, _ := result["entries"].([]any)
	entry, _ := entries[0].(map[string]any)
	switch n := entry["remaining"].(type) {
	case float64:
		return n
	case int64:
		return float64(n)
	default:
		return -1
	}
}

func wrapProbe(body string) string {
	return `export const meta = { apiVersion: 1, id: "xlyra.test.bomb", kind: "quota_probe" };
export function probe() { ` + body + ` }`
}

func runtimeGoroutines() int { return runtime.NumGoroutine() }

func TestHookTimeoutDiscardsRuntime(t *testing.T) {
	source := `export const meta = { apiVersion: 1, id: "xlyra.test.loop", kind: "quota_probe" };
export function probe() { for (;;) {} }`
	plugin := mustTestPlugin(t, "xlyra.test.loop", source)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := plugin.CallProbe(ctx, ProbeContext{Now: 1}, nil)
	if err == nil || !strings.Contains(err.Error(), KindTimeout) {
		t.Fatalf("error = %v, want timeout", err)
	}
	plugin.pool.mu.Lock()
	total := plugin.pool.total
	idle := len(plugin.pool.idle)
	plugin.pool.mu.Unlock()
	if total != 0 || idle != 0 {
		t.Fatalf("pool after interrupt total=%d idle=%d, want the runtime discarded", total, idle)
	}
}

func TestPromiseHookIsShapeError(t *testing.T) {
	source := `export const meta = { apiVersion: 1, id: "xlyra.test.promise", kind: "quota_probe" };
export async function probe() { return { result: { kind: "balance", entries: [] } }; }`
	plugin := mustTestPlugin(t, "xlyra.test.promise", source)
	_, err := plugin.CallProbe(context.Background(), ProbeContext{}, nil)
	if err == nil || !strings.Contains(err.Error(), KindShape) {
		t.Fatalf("error = %v, want shape error", err)
	}
}

func TestEvalAndImportAreRejected(t *testing.T) {
	evalSource := `export const meta = { apiVersion: 1, id: "xlyra.test.eval", kind: "quota_probe" };
export function probe() { return eval("1"); }`
	plugin := mustTestPlugin(t, "xlyra.test.eval", evalSource)
	_, err := plugin.CallProbe(context.Background(), ProbeContext{}, nil)
	if err == nil || !strings.Contains(err.Error(), "DisableDynamicCode") {
		t.Fatalf("eval error = %v", err)
	}

	importSource := `import "other.js";
export const meta = { apiVersion: 1, id: "xlyra.test.import", kind: "quota_probe" };
export function probe() { return { result: { kind: "balance", entries: [] } }; }`
	manifest := testManifest("xlyra.test.import", importSource)
	if _, err := NewPlugin(manifest, importSource, []Fixture{{Name: "unused"}}, 0); err == nil {
		t.Fatal("static import was accepted")
	}
}

func TestHostFunctions(t *testing.T) {
	source := `export const meta = { apiVersion: 1, id: "xlyra.test.host", kind: "quota_probe" };
export function probe() {
  if (codec.base64Decode(codec.base64Encode("hi")) !== "hi") return { error: "base64" };
  if (codec.base64URLDecode(codec.base64URLEncode("hi")) !== "hi") return { error: "base64url" };
  const mac = crypto.hmacSHA256("key", "msg");
  if (typeof mac !== "string" || mac.length < 8) return { error: "hmac" };
  const digest = crypto.sha256("hi");
  const token = jwt.signHS256({ sub: "a" }, "secret");
  const payload = jwt.decodeHS256(token, "secret");
  if (!payload || payload.sub !== "a") return { error: "jwt" };
  try { jwt.decodeHS256(token, "other"); return { error: "jwt accepted a bad secret" }; } catch (e) {}
  if (typeof utils.uuid() !== "string") return { error: "uuid" };
  log.info("probe ok");
  return { result: { kind: "balance", plan: digest, entries: [{ label: "balance", remaining: 1 }] } };
}`
	plugin := mustTestPlugin(t, "xlyra.test.host", source)
	decision, err := plugin.CallProbe(context.Background(), ProbeContext{}, nil)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if decision.Result == nil || decision.Result.Plan == "" {
		t.Fatalf("decision = %#v", decision)
	}
	sum, err := sha256Hex("hi")
	if err != nil || decision.Result.Plan != sum {
		t.Fatalf("sha256 plan = %q, want %q (%v)", decision.Result.Plan, sum, err)
	}
}

func TestMoejsImportOnlyInEngine(t *testing.T) {
	err := filepath.WalkDir(".", func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(body)
		if strings.Contains(text, "github.com/Calcium-Ion/moejs") {
			t.Errorf("%s imports Calcium-Ion/moejs", path)
		}
		if strings.Contains(text, "github.com/Yachiyo-5i/moejs") && filepath.Base(path) != "engine.go" {
			t.Errorf("%s imports moejs; only engine.go may do that", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	mod, err := os.ReadFile(filepath.Join("..", "..", "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(mod), "Calcium-Ion/moejs") {
		t.Fatal("go.mod tracks Calcium-Ion/moejs")
	}
	if !strings.Contains(string(mod), "github.com/Yachiyo-5i/moejs") {
		t.Fatal("go.mod does not require the fork")
	}
	if strings.Contains(string(mod), "replace github.com/Yachiyo-5i/moejs") {
		t.Fatal("go.mod replaces the fork; require a published tag instead")
	}
}

func mustTestPlugin(t *testing.T, id, source string) *Plugin {
	t.Helper()
	plugin, err := NewPlugin(testManifest(id, source), source, []Fixture{{Name: "unused"}}, 0)
	if err != nil {
		t.Fatalf("new plugin: %v", err)
	}
	return plugin
}

func testManifest(id, source string) Manifest {
	return Manifest{
		ID: id, Name: id, Version: "1.0.0", APIVersion: 1, HostAPI: 1,
		XLyra: ">=1.14.0", Kind: "quota_probe", License: "AGPL-3.0",
		QuotaProbe: QuotaProbeSection{BaseURLMode: "as_is"},
		SHA256:     map[string]string{"plugin.js": HashSource(source)},
	}
}
