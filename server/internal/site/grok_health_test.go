package site

import (
	"testing"

	"github.com/google/uuid"
)

func TestGrokHealthOutcome(t *testing.T) {
	cases := []struct {
		name        string
		credentials int
		tested      int
		passed      int
		wantOK      bool
		wantError   string
	}{
		{"no accounts", 0, 0, 0, false, "missing_credential"},
		{"all disabled", 3, 0, 0, false, "validation_failed"},
		{"all failed", 2, 2, 0, false, "validation_failed"},
		{"one of two healthy", 2, 2, 1, true, ""},
		{"single healthy", 1, 1, 1, true, ""},
		{"disabled one, other healthy", 2, 1, 1, true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ok, errType, message := grokHealthOutcome(tc.credentials, tc.tested, tc.passed)
			if ok != tc.wantOK {
				t.Fatalf("success = %v, want %v", ok, tc.wantOK)
			}
			if errType != tc.wantError {
				t.Fatalf("errorType = %q, want %q", errType, tc.wantError)
			}
			if ok && message == "" {
				t.Fatal("healthy outcome must carry a message")
			}
			if !ok && errType == "" {
				t.Fatal("unhealthy outcome must carry an error type")
			}
		})
	}
}

func TestGrokAccountHealthResult(t *testing.T) {
	cred := APIKeyCredential{Name: "acct-1"}
	cred.Credential.ID = uuid.New()

	ok := grokAccountHealthResult(cred, "ok", "")
	if ok["status"] != "ok" || ok["name"] != "acct-1" {
		t.Fatalf("unexpected ok result: %v", ok)
	}
	if _, present := ok["message"]; present {
		t.Fatal("ok result must not carry a message")
	}

	failed := grokAccountHealthResult(APIKeyCredential{}, "failed", "boom")
	if failed["status"] != "failed" || failed["message"] != "boom" {
		t.Fatalf("unexpected failed result: %v", failed)
	}
	if _, present := failed["name"]; present {
		t.Fatal("empty name must be omitted")
	}
}

func TestAccountPoolHealthMessage(t *testing.T) {
	t.Parallel()

	grokMessage := "no grok accounts configured"
	if got := accountPoolHealthMessage("grok", grokMessage); got != grokMessage {
		t.Fatalf("grok message = %q", got)
	}
	if got := accountPoolHealthMessage("acme.example/chat", grokMessage); got != "no accounts configured" {
		t.Fatalf("plugin message = %q", got)
	}
	if got := accountPoolHealthMessage("acme.example/chat", "all 2 grok accounts failed validation"); got != "all 2 accounts failed validation" {
		t.Fatalf("plugin failure message = %q", got)
	}
	okMessage := "ok (1/2 accounts healthy)"
	if got := accountPoolHealthMessage("acme.example/chat", okMessage); got != okMessage {
		t.Fatalf("plugin success message = %q", got)
	}
	if got := accountPoolHealthMessage("grok", okMessage); got != okMessage {
		t.Fatalf("grok success message = %q", got)
	}
}
