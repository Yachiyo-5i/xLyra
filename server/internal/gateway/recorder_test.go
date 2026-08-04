package gateway

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"xlyra/server/internal/config"
	"xlyra/server/internal/store"
)

func TestRecorderRejectsNilStore(t *testing.T) {
	t.Parallel()

	recorder := NewRecorder(nil, nil, config.LoadTimeZone("UTC"))
	if _, _, err := recorder.RecordGatewayRequest(t.Context(), GatewayRequestRecord{RequestID: "req-1"}); err == nil {
		t.Fatal("expected nil store recorder to reject writes")
	}
}

func TestRecorderTimeZoneUsesProvidedLocation(t *testing.T) {
	t.Parallel()

	timeZone := config.LoadTimeZone("Asia/Shanghai")
	recorder := NewRecorder(nil, nil, timeZone)
	if recorder.timeZone.Name != "Asia/Shanghai" || recorder.timeZone.Location == nil {
		t.Fatalf("recorder timezone = %#v, want Asia/Shanghai", recorder.timeZone)
	}

	resolved := recorderTimeZone(config.TimeZone{Name: "UTC"})
	if resolved.Location == nil {
		t.Fatalf("timezone without location should resolve, got %#v", resolved)
	}
}

func TestRecorderLocksAndUpdatesAPIKeyBeforeCreatingRequestDetail(t *testing.T) {
	db := grokCredentialTransactionGorm(t)
	db.DisableNestedTransaction = true
	apiKeyID := uuid.New()
	createErr := errors.New("stop after request log create")
	steps := make([]string, 0, 3)

	if err := db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
		apiKey, ok := tx.Statement.Dest.(*store.APIKey)
		if !ok {
			tx.AddError(errors.New("unexpected recorder query destination"))
			return
		}
		locking, ok := tx.Statement.Clauses["FOR"].Expression.(clause.Locking)
		if !ok || locking.Strength != clause.LockingStrengthUpdate {
			tx.AddError(errors.New("api key usage query must lock the row"))
			return
		}
		steps = append(steps, "api_key_lock")
		*apiKey = store.APIKey{ID: apiKeyID, QuotaUnlimited: true, QuotaDailyUnlimited: true, QuotaWeeklyUnlimited: true}
		tx.Statement.RowsAffected = 1
	}); err != nil {
		t.Fatalf("replace recorder query callback: %v", err)
	}
	if err := db.Callback().Update().Replace("gorm:update", func(tx *gorm.DB) {
		updates, ok := tx.Statement.Dest.(map[string]any)
		if !ok {
			tx.AddError(errors.New("api key usage update must be field-limited"))
			return
		}
		expected := []string{"quota_used", "quota_daily_used", "quota_daily_window_start", "quota_weekly_used", "quota_weekly_window_start"}
		if len(updates) != len(expected) {
			tx.AddError(errors.New("api key usage update contains unexpected fields"))
			return
		}
		for _, field := range expected {
			if _, ok := updates[field]; !ok {
				tx.AddError(errors.New("api key usage update is missing " + field))
				return
			}
		}
		steps = append(steps, "api_key_update")
		tx.Statement.RowsAffected = 1
	}); err != nil {
		t.Fatalf("replace recorder update callback: %v", err)
	}
	if err := db.Callback().Create().Replace("gorm:create", func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*store.RequestLog); !ok {
			tx.AddError(errors.New("unexpected recorder create destination"))
			return
		}
		steps = append(steps, "request_log")
		tx.AddError(createErr)
	}); err != nil {
		t.Fatalf("replace recorder create callback: %v", err)
	}

	cost := 1.25
	_, _, err := NewRecorder(gatewayStoreWithGorm(t, db), nil, config.LoadTimeZone("UTC")).RecordGatewayRequest(t.Context(), GatewayRequestRecord{
		RequestID:     "req-lock-order",
		APIKeyID:      apiKeyID,
		EstimatedCost: &cost,
	})
	if !errors.Is(err, createErr) {
		t.Fatalf("RecordGatewayRequest error = %v, want request log create failure", err)
	}
	expectedSteps := []string{"api_key_lock", "api_key_update", "request_log"}
	if len(steps) != len(expectedSteps) {
		t.Fatalf("recorder steps = %v, want %v", steps, expectedSteps)
	}
	for i := range expectedSteps {
		if steps[i] != expectedSteps[i] {
			t.Fatalf("recorder steps = %v, want %v", steps, expectedSteps)
		}
	}
}

func TestRecorderNullableHelpers(t *testing.T) {
	t.Parallel()

	if got := nullableUUID(uuid.Nil); got != nil {
		t.Fatalf("nullableUUID(nil) = %#v, want nil", got)
	}
	id := uuid.New()
	if got := nullableUUID(id); got != id {
		t.Fatalf("nullableUUID(valid) = %#v, want %s", got, id)
	}

	if got := nullableString(""); got != nil {
		t.Fatalf("nullableString(empty) = %#v, want nil", got)
	}
	if got := nullableString("error"); got != "error" {
		t.Fatalf("nullableString(valid) = %#v, want error", got)
	}

	if got := nullableInt(0); got != nil {
		t.Fatalf("nullableInt(0) = %#v, want nil", got)
	}
	if got := nullableInt(42); got != 42 {
		t.Fatalf("nullableInt(42) = %#v, want 42", got)
	}

	if got := nullableInt64(-1); got != nil {
		t.Fatalf("nullableInt64(-1) = %#v, want nil", got)
	}
	if got := nullableInt64(99); got != int64(99) {
		t.Fatalf("nullableInt64(99) = %#v, want 99", got)
	}

	if got := nullableFloat(nil); got != nil {
		t.Fatalf("nullableFloat(nil) = %#v, want nil", got)
	}
	value := 1.25
	if got := nullableFloat(&value); got != value {
		t.Fatalf("nullableFloat(valid) = %#v, want %v", got, value)
	}
}

func TestDetachedRecordingContextCarriesRequestIDWithTimeout(t *testing.T) {
	t.Parallel()

	ctx := context.WithValue(context.Background(), recordingContextKey{}, "req-parent")
	recordCtx, cancel := detachedRecordingContext(ctx)
	defer cancel()

	if got, _ := recordCtx.Value(recordingContextKey{}).(string); got != "req-parent" {
		t.Fatalf("recording context requestID = %q, want req-parent", got)
	}
	deadline, ok := recordCtx.Deadline()
	if !ok {
		t.Fatal("expected detached recording context to have a deadline")
	}
	if remaining := time.Until(deadline); remaining <= 0 || remaining > gatewayRecordingTimeout {
		t.Fatalf("recording context timeout remaining = %v", remaining)
	}

	recordCtx, cancel = detachedRecordingContext(nil)
	defer cancel()
	if got := recordCtx.Value(recordingContextKey{}); got != nil {
		t.Fatalf("nil parent context should not carry requestID, got %#v", got)
	}
}

func TestResponseModeLabel(t *testing.T) {
	t.Parallel()

	if got := responseModeLabel(true); got != "stream" {
		t.Fatalf("responseModeLabel(true) = %q, want stream", got)
	}
	if got := responseModeLabel(false); got != "non_stream" {
		t.Fatalf("responseModeLabel(false) = %q, want non_stream", got)
	}
}
