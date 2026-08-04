package oauth

import (
	"context"
	"strings"
	"testing"
)

func TestDetectAndImportRejectsMalformedJSONBeforeStoreAccess(t *testing.T) {
	t.Parallel()

	result := NewImportService(nil, "master-key", nil).DetectAndImport(context.Background(), []byte(`{"accounts":`), false)

	assertSingleFailedImport(t, result, "invalid JSON:")
}

func TestDetectAndImportRejectsMalformedSub2APIAndEmptyAccountsBeforeStoreAccess(t *testing.T) {
	t.Parallel()

	service := NewImportService(nil, "master-key", nil)
	for _, tc := range []struct {
		name      string
		payload   string
		wantError string
	}{
		{
			name:      "malformed_accounts",
			payload:   `{"accounts":"not-a-list"}`,
			wantError: "parse Sub2API format:",
		},
		{
			name:      "empty_accounts",
			payload:   `{"accounts":[]}`,
			wantError: "file contains no accounts",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := service.DetectAndImport(context.Background(), []byte(tc.payload), false)

			assertSingleFailedImport(t, result, tc.wantError)
		})
	}
}

func TestDetectAndImportRejectsMalformedChatGPTTokenBeforeStoreAccess(t *testing.T) {
	t.Parallel()

	result := NewImportService(nil, "master-key", nil).DetectAndImport(context.Background(), []byte(`{"tokens":"not-an-object"}`), false)

	assertSingleFailedImport(t, result, "parse ChatGPT token format:")
}

func TestDetectAndImportRejectsUnsupportedCPATypeBeforeStoreAccess(t *testing.T) {
	t.Parallel()

	result := NewImportService(nil, "master-key", nil).DetectAndImport(context.Background(), []byte(`{"type":"unknown","access_token":"access"}`), false)

	assertSingleFailedImport(t, result, `unsupported type "unknown"`)
}

func assertSingleFailedImport(t *testing.T, result ImportResult, wantError string) {
	t.Helper()

	if result.Meta.Total != 1 || result.Meta.Failed != 1 || result.Meta.Accepted != 0 || result.Meta.Queued != 0 || result.Meta.Succeeded != 0 {
		t.Fatalf("unexpected import meta: %#v", result.Meta)
	}
	if len(result.Items) != 1 {
		t.Fatalf("items length = %d, want 1: %#v", len(result.Items), result.Items)
	}
	item := result.Items[0]
	if item.Status != "failed" {
		t.Fatalf("status = %q, want failed", item.Status)
	}
	if !strings.Contains(item.Error, wantError) {
		t.Fatalf("error = %q, want to contain %q", item.Error, wantError)
	}
}
