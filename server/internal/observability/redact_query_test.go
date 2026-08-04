package observability

import (
	"strings"
	"testing"
)

func TestRedactSensitiveQuery(t *testing.T) {
	t.Parallel()

	out := redactSensitiveQuery("code=secretauthcode&state=xyz&provider=codex&scope=openid")
	if strings.Contains(out, "secretauthcode") || strings.Contains(out, "state=xyz") {
		t.Fatalf("sensitive params leaked: %q", out)
	}
	if !strings.Contains(out, "provider=codex") || !strings.Contains(out, "scope=openid") {
		t.Fatalf("non-sensitive params dropped: %q", out)
	}
	if !strings.Contains(out, "code=%5Bredacted%5D") && !strings.Contains(out, "code=[redacted]") {
		t.Fatalf("code not redacted: %q", out)
	}
	if redactSensitiveQuery("") != "" {
		t.Fatal("empty query should stay empty")
	}
}
