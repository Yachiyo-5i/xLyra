package gateway

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"xlyra/server/internal/auth"
	"xlyra/server/internal/store"
)

func TestGatewayRecordingGuardsNilStores(t *testing.T) {
	t.Parallel()

	if _, _, err := (Recorder{}).RecordChatCompletion(context.Background(), CompletionRecord{RequestID: "req-recording"}); err == nil {
		t.Fatal("RecordChatCompletion with nil store returned nil error")
	}

	Handler{}.recordRequestFailure(
		context.Background(),
		"req-recording",
		uuid.New(),
		time.Now(),
		http.StatusBadGateway,
		"route_selection_failed",
		"route failed",
		"gpt-recording",
		true,
		"route_plan",
		gatewayEndpointResponses,
	)
}

func TestBuildModelsPayloadAccessErrorsAndEmptyAllowList(t *testing.T) {
	t.Parallel()

	accessErrDB, _ := gatewayGormWithQueryError(t, "access lookup failed")
	_, err := (Handler{auth: auth.NewService(accessErrDB, "test-master-key")}).buildModelsPayload(context.Background(), store.APIKey{ID: uuid.New()})
	if err == nil || !strings.Contains(err.Error(), "access lookup failed") {
		t.Fatalf("buildModelsPayload access error = %v, want access lookup failed", err)
	}

	apiKeyID := uuid.New()
	allowListDB := gatewayGormWithQueryCallback(t, func(tx *gorm.DB) {
		switch dest := tx.Statement.Dest.(type) {
		case *store.APIKey:
			*dest = store.APIKey{ID: apiKeyID, ModelPolicy: "allow_all", SitePolicy: "allow_list"}
			tx.Statement.RowsAffected = 1
		case *[]store.APIKeySitePermission:
			*dest = nil
			tx.Statement.RowsAffected = 0
		case *[]store.APIKeySiteGroupPermission:
			*dest = nil
			tx.Statement.RowsAffected = 0
		default:
			tx.AddError(errors.New("empty site allow-list should not query models"))
		}
	})
	payload, err := (Handler{auth: auth.NewService(allowListDB, "test-master-key")}).buildModelsPayload(context.Background(), store.APIKey{ID: apiKeyID})
	if err != nil {
		t.Fatalf("buildModelsPayload empty allow-list returned error: %v", err)
	}
	rows, ok := payload["data"].([]map[string]any)
	if payload["object"] != "list" || !ok || len(rows) != 0 {
		t.Fatalf("empty allow-list payload = %#v", payload)
	}
}

func TestPrewarmModelsCacheGuardsAndListError(t *testing.T) {
	t.Parallel()

	db := gatewayOfflineGorm(t)
	handler := Handler{
		logger:      gatewayDiscardLogger(),
		db:          gatewayStoreWithGorm(t, db),
		auth:        auth.NewService(db, "test-master-key"),
		modelsCache: newModelsCache(),
	}
	apiKeyID := uuid.New()

	handler.PrewarmModelsCacheForAPIKey(context.Background(), store.APIKey{ID: apiKeyID, Status: "disabled", QuotaUnlimited: true})
	if _, ok := handler.modelsCache.get(apiKeyID); ok {
		t.Fatal("disabled API key should not be cached")
	}

	expiredAt := time.Now().Add(-time.Minute)
	handler.PrewarmModelsCacheForAPIKey(context.Background(), store.APIKey{ID: apiKeyID, Status: "active", ExpiresAt: &expiredAt, QuotaUnlimited: true})
	if _, ok := handler.modelsCache.get(apiKeyID); ok {
		t.Fatal("expired API key should not be cached")
	}

	handler.PrewarmModelsCacheForAPIKey(context.Background(), store.APIKey{
		ID:         apiKeyID,
		Status:     "active",
		QuotaLimit: sql.NullFloat64{Float64: 10, Valid: true},
		QuotaUsed:  10,
	})
	if _, ok := handler.modelsCache.get(apiKeyID); ok {
		t.Fatal("quota-exhausted API key should not be cached")
	}

	gatewayReplaceQueryCallback(t, db, func(tx *gorm.DB) {
		tx.AddError(errors.New("list api keys failed"))
	})
	handler.PrewarmModelsCache(context.Background())
}

func TestTestSiteModelGuardErrors(t *testing.T) {
	t.Parallel()

	_, err := (Handler{}).TestSiteModel(context.Background(), SiteModelTestInput{})
	var testErr *SiteModelTestError
	if !errors.As(err, &testErr) || testErr.Code != "gateway_unavailable" || testErr.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("nil db error = %v, want gateway_unavailable", err)
	}

	_, err = (Handler{db: gatewayStoreWithGorm(t, gatewayOfflineGorm(t))}).TestSiteModel(context.Background(), SiteModelTestInput{
		Timeout: time.Millisecond,
	})
	if !errors.As(err, &testErr) || testErr.Code != "invalid_timeout" || testErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid timeout error = %v, want invalid_timeout", err)
	}
}

func TestProxyResponsesStreamPassthroughRejectsMissingBody(t *testing.T) {
	t.Parallel()

	capture, started, err := proxyResponsesStreamPassthrough(context.Background(), httptest.NewRecorder(), &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}, time.Now())
	assertMissingBodyStreamCapture(t, "proxyResponsesStreamPassthrough", capture, started, err)
}
