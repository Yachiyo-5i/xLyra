package jsplugin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCallRewritePayload(t *testing.T) {
	t.Parallel()
	plugin := providerPlugin(t, "acme.example", `
export function rewritePayload(ctx, payload) {
  payload.ctxKeys = Object.keys(ctx).sort().join(",");
  payload.ctxSite = ctx.siteType;
  payload.ctxModel = ctx.model;
  payload.ctxProtocol = ctx.protocol;
  payload.ctxStream = ctx.stream;
  delete payload.dropme;
  payload.added = "yes";
  return payload;
}`)
	out, err := plugin.CallRewritePayload(context.Background(), "rewritePayload", RewritePayloadContext{
		SiteType: "acme.example/chat",
		Model:    "upstream-m",
		Stream:   false,
		Protocol: "openai_chat",
	}, map[string]any{"model": "upstream-m", "stream": false, "dropme": "x", "messages": []any{"hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if out["ctxKeys"] != "model,protocol,siteType,stream" || out["ctxSite"] != "acme.example/chat" || out["ctxModel"] != "upstream-m" || out["ctxProtocol"] != "openai_chat" || out["ctxStream"] != false {
		t.Fatalf("ctx echo = %#v", out)
	}
	if _, ok := out["dropme"]; ok || out["added"] != "yes" || out["model"] != "upstream-m" || out["stream"] != false {
		t.Fatalf("rewritten = %#v", out)
	}
	if _, ok := out["messages"]; !ok {
		t.Fatal("messages was removed")
	}
}

func TestCallRewritePayloadRejectsShapeAndThrow(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{"null", `export function rewritePayload() { return null; }`, KindShape},
		{"array", `export function rewritePayload() { return []; }`, KindShape},
		{"string", `export function rewritePayload() { return "nope"; }`, KindShape},
		{"number", `export function rewritePayload() { return 1; }`, KindShape},
		{"throw", `export function rewritePayload() { throw new Error("boom"); }`, "boom"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plugin := providerPlugin(t, "acme.example", tc.source)
			_, err := plugin.CallRewritePayload(context.Background(), "rewritePayload", RewritePayloadContext{}, map[string]any{"model": "m"})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %s", err, tc.want)
			}
		})
	}
}

func TestCallRewritePayloadTimeout(t *testing.T) {
	t.Parallel()
	plugin := providerPlugin(t, "acme.example", `export function rewritePayload() { for (;;) {} }`)
	started := time.Now()
	_, err := plugin.CallRewritePayload(context.Background(), "rewritePayload", RewritePayloadContext{}, map[string]any{"model": "m"})
	elapsed := time.Since(started)
	if err == nil || !strings.Contains(err.Error(), KindTimeout) {
		t.Fatalf("error = %v", err)
	}
	if elapsed < 40*time.Millisecond || elapsed > 180*time.Millisecond {
		t.Fatalf("elapsed %s, want the 50ms protocol timeout", elapsed)
	}
}

func TestCallRewritePayloadLimitsAndMissingHook(t *testing.T) {
	t.Parallel()
	oversized := providerPlugin(t, "acme.example", `export function rewritePayload(ctx, payload) { payload.ran = true; return payload; }`)
	occupyPluginPool(t, oversized)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := oversized.CallRewritePayload(ctx, "rewritePayload", RewritePayloadContext{}, map[string]any{
		"blob": strings.Repeat("a", int(MaxResultBytes)),
	})
	if err == nil || !strings.Contains(err.Error(), "payload exceeds") || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("oversize input error = %v", err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("oversize input waited for a runtime")
	}
	var call *CallError
	if !errors.As(err, &call) || call.Kind != KindResponseTooLarge || call.PluginID != "acme.example" {
		t.Fatalf("oversize input = %#v", err)
	}

	// race 检测器下 50 ms 预算可能先耗尽。
	huge := providerPlugin(t, "acme.huge", `export function rewritePayload() { return { blob: "x".repeat(2 * 1024 * 1024) }; }`)
	_, hugeErr := huge.CallRewritePayload(context.Background(), "rewritePayload", RewritePayloadContext{}, map[string]any{"model": "m"})
	var hugeCall *CallError
	if !errors.As(hugeErr, &hugeCall) || (hugeCall.Kind != KindResponseTooLarge && hugeCall.Kind != KindTimeout) {
		t.Fatalf("oversize output error = %v", hugeErr)
	}

	bare := providerPlugin(t, "acme.bare", `export function listModels() { return { result: { models: [] } }; }`)
	occupyPluginPool(t, bare)
	missingCtx, missingCancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer missingCancel()
	started = time.Now()
	_, err = bare.CallRewritePayload(missingCtx, "rewritePayload", RewritePayloadContext{}, map[string]any{"model": "m"})
	if err == nil || !strings.Contains(err.Error(), "rewritePayload hook is missing") || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("missing hook error = %v", err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("missing hook waited for a runtime")
	}
}

func TestCallRewritePayloadConcurrent(t *testing.T) {
	plugin := providerPlugin(t, "acme.example", `export function rewritePayload(ctx, payload) { payload.added = "yes"; return payload; }`)
	var wg sync.WaitGroup
	errCh := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := plugin.CallRewritePayload(context.Background(), "rewritePayload", RewritePayloadContext{
				SiteType: "acme.example/chat", Model: "m", Protocol: "openai_chat",
			}, map[string]any{"model": "m"})
			if err != nil || out["added"] != "yes" || out["model"] != "m" {
				errCh <- fmt.Errorf("%v payload %#v", err, out)
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
}

func occupyPluginPool(t *testing.T, plugin *Plugin) {
	t.Helper()
	plugin.pool.wait = 5 * time.Second
	held := make([]*session, 0, plugin.pool.max)
	for i := 0; i < plugin.pool.max; i++ {
		session, err := plugin.pool.Acquire(context.Background())
		if err != nil {
			t.Fatalf("acquire: %v", err)
		}
		held = append(held, session)
	}
	t.Cleanup(func() {
		for _, session := range held {
			plugin.pool.Release(session, false)
		}
	})
}
