package site

import "testing"

func TestCheckinSuccessDefaultsToSuccessUnlessExplicitFalse(t *testing.T) {
	t.Parallel()

	if !checkinSuccess(nil) {
		t.Fatal("nil checkin payload should be treated as success")
	}
	if !checkinSuccess("ok") {
		t.Fatal("non-map checkin payload should be treated as success")
	}
	if !checkinSuccess(map[string]any{"message": "done"}) {
		t.Fatal("missing success field should be treated as success")
	}
	if !checkinSuccess(map[string]any{"success": true}) {
		t.Fatal("success=true should be success")
	}
	if checkinSuccess(map[string]any{"success": false}) {
		t.Fatal("success=false should be skipped")
	}
	if !checkinSuccess(map[string]any{"success": "false"}) {
		t.Fatal("non-bool success field should keep success-default behavior")
	}
}

func TestCheckinMessagePrefersMessageThenError(t *testing.T) {
	t.Parallel()

	if got := checkinMessage(map[string]any{"message": " checked in ", "error": "failed"}); got != "checked in" {
		t.Fatalf("message = %q, want checked in", got)
	}
	if got := checkinMessage(map[string]any{"message": " ", "error": " not ready "}); got != "not ready" {
		t.Fatalf("error fallback = %q, want not ready", got)
	}
	if got := checkinMessage("not a map"); got != "" {
		t.Fatalf("non-map message = %q, want empty", got)
	}
	if got := checkinMessage(map[string]any{"message": " ", "error": "\t"}); got != "" {
		t.Fatalf("blank map fields message = %q, want empty", got)
	}
}

func TestDefaultCheckinMessageTrimsValueAndUsesFallback(t *testing.T) {
	t.Parallel()

	if got := defaultCheckinMessage(" done ", "fallback"); got != "done" {
		t.Fatalf("message = %q, want done", got)
	}
	if got := defaultCheckinMessage(" ", "fallback"); got != "fallback" {
		t.Fatalf("fallback = %q, want fallback", got)
	}
	if got := defaultCheckinMessage("", "  fallback  "); got != "  fallback  " {
		t.Fatalf("fallback preservation = %q, want fallback preserved exactly", got)
	}
}
